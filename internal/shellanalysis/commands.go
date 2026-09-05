package shellanalysis

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
	"mvdan.cc/sh/v3/syntax"
)

func (a *analyzer) classify(args []string, st *state) error {
	return a.classifyCommand(args, st, true)
}

func (a *analyzer) classifyCommand(args []string, st *state, allowBuiltins bool) error {
	if len(args) == 0 || args[0] == "" {
		a.addUnknown("unresolved command")
		return nil
	}
	if a.commandDepth >= 32 {
		a.addIncomplete("command wrapper depth limit reached")
		return nil
	}
	a.commandDepth++
	defer func() { a.commandDepth-- }()
	rawName := args[0]
	name := filepath.Base(rawName)
	spec := commandSpecs[name]
	builtin := spec != nil && spec.Builtin && allowBuiltins && rawName == name
	if !builtin {
		a.addEffect(permission.Effect{Type: "process", Capability: permission.CapabilityProcessExec, Operation: "execute", Command: name, Reason: "execute command", Confidence: "high"})
	}
	if rawName != name {
		a.addUnknown("qualified executable identity is not established")
	}
	if spec == nil || (spec.Builtin && !builtin) {
		a.addUnknown("unknown executable: " + name)
		for _, arg := range args[1:] {
			if filepath.IsAbs(arg) || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
				if st.cwd == "" && !filepath.IsAbs(arg) {
					continue
				}
				a.addEffect(permission.Effect{Type: "filesystem", Capability: permission.CapabilityUnknown, Operation: "potential access", Resource: a.resolvePath(arg, st.cwd), Command: name, Confidence: "low", Dynamic: true})
			}
		}
		return nil
	}
	if spec.Handler == "noop" {
		if spec.Operation == "format" && len(args) > 1 && strings.HasPrefix(args[1], "-") {
			a.addUnknown("format builtin options can change shell state")
			clear(st.vars)
		}
		return nil
	}
	if spec.Handler == "find" {
		a.find(args[1:], st)
		return a.err
	}
	if !builtin && !(spec.Handler == "wrapper" && spec.Operation == "builtin") {
		copy := cloneState(st)
		return a.applySpec(name, spec, args[1:], &copy)
	}
	return a.applySpec(name, spec, args[1:], st)
}

func (a *analyzer) applySpec(name string, spec *commandSpec, args []string, st *state) error {
	parsed := parseOptions(spec, args)
	if parsed.unknown {
		a.addUnknown("unsupported or incomplete options for " + name)
	}
	if spec.Opaque {
		a.addUnknown(name + " runtime configuration or child effects are not statically known")
	}
	for _, opt := range parsed.options {
		switch opt.role {
		case "read", "pattern_file", "upload":
			a.addPathEffect("read", opt.value, name, "high", st)
		case "write":
			a.addPathEffect("write", opt.value, name, "high", st)
		case "data_file":
			if file, ok := strings.CutPrefix(opt.value, "@"); ok {
				a.addPathEffect("read", file, name, "high", st)
			}
		case "config":
			a.addIncomplete(name + " external configuration or embedded command is not analyzed")
		case "unknown", "recursive":
			a.addUnknown(name + " option has runtime-dependent effects")
		case "chdir":
			a.addUnknown(name + " changes the execution directory")
			if st.cwd == "" && !filepath.IsAbs(opt.value) {
				a.addUnknown("unresolved working directory")
				continue
			}
			st.cwd = a.resolvePath(opt.value, st.cwd)
		case "mode":
			a.modeEffect(name, opt.value)
		}
	}
	if len(spec.Subcommands) > 0 && len(parsed.operands) > 0 {
		if sub, ok := spec.Subcommands[parsed.operands[0]]; ok {
			copy := cloneState(st)
			return a.applySpec(name, sub, parsed.operands[1:], &copy)
		}
		a.addUnknown("unclassified subcommand for " + name)
	}
	switch spec.Handler {
	case "subcommands":
		a.addUnknown("unclassified operation for " + name)
	case "opaque", "invalidate":
		a.addUnknown(name + " effects are not statically analyzed")
		if spec.Builtin {
			clear(st.vars)
		}
	case "unset":
		for _, key := range parsed.operands {
			delete(st.vars, key)
		}
		if parsed.unknown {
			clear(st.vars)
		}
	case "cd":
		target, ok := st.vars["HOME"]
		if len(parsed.operands) > 0 {
			target, ok = parsed.operands[0], parsed.operands[0] != ""
		}
		if !ok || parsed.unknown || target == "-" || (st.cwd == "" && !filepath.IsAbs(target)) {
			st.cwd = ""
			a.addUnknown("unresolved cd target")
			return nil
		}
		st.cwd = a.resolvePath(target, st.cwd)
		// A later statement can also run after cd fails, or CDPATH may redirect it.
		a.addUnknown("cd success and shell directory state depend on runtime")
	case "paths":
		operands := parsed.operands
		if len(operands) == 0 && spec.DefaultPath != "" {
			operands = []string{spec.DefaultPath}
		}
		for _, value := range operands {
			a.addPathEffect(spec.Operation, value, name, "high", st)
		}
	case "grep":
		operands := parsed.operands
		if !parsed.has("pattern") && !parsed.has("pattern_file") && len(operands) > 0 {
			operands = operands[1:]
		}
		for _, value := range operands {
			a.addPathEffect("read", value, name, "high", st)
		}
		if parsed.has("recursive") && len(operands) == 0 {
			a.addPathEffect("read", ".", name, "high", st)
		}
	case "copy":
		a.copyOperands(name, spec, parsed, st)
	case "mode":
		operands := parsed.operands
		if !parsed.has("read") && len(operands) > 0 {
			a.modeEffect(name, operands[0])
			operands = operands[1:]
		}
		for _, value := range operands {
			a.addPathEffect("write", value, name, "high", st)
		}
	case "capability":
		a.addEffect(permission.Effect{Type: "policy", Capability: spec.Capability, Operation: spec.Operation, Command: name, Reason: "protected operation", Confidence: "high"})
	case "git":
		resource := st.cwd
		if spec.Capability == permission.CapabilityGitRemoteRead || spec.Capability == permission.CapabilityGitRemoteWrite {
			resource = ""
			if len(parsed.operands) > 0 {
				resource = parsed.operands[0]
			}
			if resource == "" {
				a.addUnknown("remote destination depends on repository configuration")
			}
		}
		a.addEffect(permission.Effect{Type: "git", Capability: spec.Capability, Operation: spec.Operation, Resource: resource, Command: name, Confidence: "high"})
		// Git reads configuration, attributes and hooks even for familiar verbs.
		a.addUnknown("Git configuration and child effects are runtime-dependent")
	case "network", "ssh", "transfer":
		a.networkEffects(name, spec, parsed, st)
	case "shell":
		source := parsed.last("script")
		if source == "" {
			a.addUnknown("interactive or file-based shell execution")
			return nil
		}
		copy := cloneState(st)
		a.addUnknown("nested shell environment and startup state are runtime-dependent")
		variant := syntax.LangPOSIX
		if spec.Operation == "bash" {
			variant = syntax.LangBash
		}
		return a.parse(source, variant, &copy)
	case "wrapper":
		if parsed.has("enumerate") {
			return nil
		}
		child := parsed.operands
		copy := cloneState(st)
		if parsed.has("clear_env") {
			clear(copy.vars)
			a.addUnknown("wrapper changes environment")
		}
		for _, opt := range parsed.options {
			if opt.role == "unset_env" {
				delete(copy.vars, opt.value)
				a.addUnknown("wrapper changes environment")
			}
		}
		if spec.Operation == "env" {
			for len(child) > 0 {
				key, value, ok := strings.Cut(child[0], "=")
				if !ok {
					break
				}
				if len(copy.vars) < maxVariables && len(value) <= maxValueBytes {
					copy.vars[key] = value
				}
				a.addUnknown("wrapper changes environment")
				child = child[1:]
			}
		}
		if spec.Operation == "builtin" {
			return a.classifyCommand(child, st, true)
		}
		return a.classifyCommand(child, &copy, false)
	default:
		a.addIncomplete("invalid command specification handler")
	}
	return nil
}

func (a *analyzer) copyOperands(name string, spec *commandSpec, parsed parsedArgs, st *state) {
	values := parsed.operands
	if parsed.has("directory") {
		for _, value := range values {
			a.addPathEffect("write", value, name, "high", st)
		}
		return
	}
	destination := parsed.last("target")
	directory := destination != ""
	if !directory {
		if len(values) < 2 {
			a.addIncomplete("ambiguous or incomplete " + name + " destination")
			return
		}
		destination = values[len(values)-1]
		values = values[:len(values)-1]
		directory = strings.HasSuffix(destination, "/")
		if filepath.IsAbs(destination) || st.cwd != "" {
			if info, err := os.Stat(a.resolvePath(destination, st.cwd)); err == nil && info.IsDir() {
				directory = true
			}
		}
	}
	if parsed.has("no_target") {
		directory = false
	}
	for _, source := range values {
		a.addPathEffect(spec.Operation, source, name, "high", st)
	}
	a.addPathEffect("write", destination, name, "high", st)
	if directory {
		for _, source := range values {
			if source == "" {
				a.addUnknown("dynamic copy source")
				continue
			}
			a.addPathEffect("write", filepath.Join(destination, filepath.Base(source)), name, "high", st)
		}
	}
	if len(values) > 1 && !directory {
		a.addIncomplete("ambiguous " + name + " destination")
	}
	// Destination directory status, links, and overwrite behavior can change.
	a.addUnknown(name + " destination and filesystem state can change")
}

func (a *analyzer) modeEffect(name, mode string) {
	setID := false
	if bits, err := strconv.ParseUint(mode, 8, 32); err == nil {
		setID = bits&0o6000 != 0
	} else {
		for clause := range strings.SplitSeq(mode, ",") {
			if i := strings.IndexAny(clause, "+-="); i >= 0 && clause[i] != '-' && strings.Contains(clause[i+1:], "s") {
				setID = true
			}
		}
	}
	if setID {
		a.addEffect(permission.Effect{Type: "privilege", Capability: permission.CapabilityPrivilegeEscalation, Operation: "set-id", Command: name, Reason: "set-user-ID or set-group-ID permission", Confidence: "high"})
	}
}

func (a *analyzer) networkEffects(name string, spec *commandSpec, parsed parsedArgs, st *state) {
	capability := permission.CapabilityNetworkRead
	operation := "read"
	if spec.Handler == "transfer" || parsed.has("upload") || parsed.has("network_write") || parsed.has("data_file") {
		capability = permission.CapabilityNetworkWrite
		operation = "write"
	}
	resources := []string{}
	for i, value := range parsed.operands {
		switch spec.Handler {
		case "network":
			if endpoint := urlEndpoint(value); endpoint != "" {
				resources = append(resources, endpoint)
			}
		case "ssh":
			if i == 0 && value != "" {
				endpoint := "ssh://" + value
				if port := parsed.last("port"); port != "" {
					endpoint += ":" + port
				}
				resources = append(resources, endpoint)
			}
		case "transfer":
			if endpoint := remoteEndpoint(value); endpoint != "" {
				resources = append(resources, endpoint)
			} else {
				effect := "read"
				if i == len(parsed.operands)-1 {
					effect = "write"
				}
				a.addPathEffect(effect, value, name, "high", st)
			}
		}
	}
	if len(resources) == 0 {
		a.addUnknown("network destination depends on runtime")
		resources = []string{""}
	}
	for _, resource := range resources {
		a.addEffect(permission.Effect{Type: "network", Capability: capability, Operation: operation, Resource: resource, Command: name, Reason: "network access", Confidence: "high", Dynamic: resource == ""})
	}
}

// Check internal data integrity without invoking any external executable.
func validateHandler(name string) error {
	switch name {
	case "noop", "opaque", "invalidate", "unset", "cd", "paths", "grep", "copy", "mode", "capability", "git", "network", "ssh", "transfer", "shell", "wrapper", "subcommands", "find":
		return nil
	default:
		return fmt.Errorf("invalid command handler %q", name)
	}
}
