package shellanalysis

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
	"mvdan.cc/sh/v3/syntax"
)

var shellBuiltins = map[string]bool{
	"echo": true, "printf": true, "pwd": true, "true": true, "false": true, ":": true,
	"export": true, "unset": true, "set": true, "shift": true, "wait": true, "read": true,
	"test": true, "[": true,
}

func (a *analyzer) classify(args []string, st *state) error {
	return a.classifyCommand(args, st, true)
}

func (a *analyzer) classifyCommand(args []string, st *state, allowShellBuiltins bool) error {
	rawName := args[0]
	name := filepath.Base(rawName)
	if allowShellBuiltins && rawName == name && name == "cd" {
		target := a.home
		if len(args) > 1 && args[1] != "" {
			target = args[1]
		}
		if target == "" {
			a.addUnknown("dynamic cd target")
			return nil
		}
		st.cwd = a.resolvePath(target, st.cwd)
		return nil
	}
	if allowShellBuiltins && rawName == name && shellBuiltins[name] {
		return nil
	}
	a.addEffect(permission.Effect{Type: "process", Capability: permission.CapabilityProcessExec, Operation: "execute", Command: name, Reason: "execute command", Confidence: "high"})

	switch name {
	case "cat", "head", "tail", "less", "ls", "stat":
		a.pathArgs("read", name, args[1:], st)
	case "grep":
		a.grep(args[1:], st)
	case "find":
		a.find(args[1:], st)
	case "cp":
		a.copyMove("cp", args[1:], st)
	case "install":
		if installDirectoryMode(args[1:]) {
			for _, value := range installDirectoryOperands(args[1:]) {
				a.addPathEffect("write", value, name, "high", st)
			}
		} else {
			a.copyMove("install", args[1:], st)
		}
	case "mv":
		a.copyMove("mv", args[1:], st)
	case "rm", "rmdir":
		a.pathArgs("delete", name, args[1:], st)
	case "mkdir", "touch", "ln", "tee":
		a.pathArgs("write", name, args[1:], st)
	case "chmod", "chown":
		values := positional(args[1:])
		if name == "chmod" && len(values) > 0 && setIDMode(values[0]) {
			a.addEffect(permission.Effect{Type: "privilege", Capability: permission.CapabilityPrivilegeEscalation, Operation: "set-id", Command: name, Reason: "set-user-ID or set-group-ID permission", Confidence: "high"})
		}
		if len(values) > 1 {
			a.pathArgs("write", name, values[1:], st)
		}
	case "curl", "wget", "ssh", "scp", "rsync":
		a.networkPaths(name, args[1:], st)
		a.network(name, args[1:])
	case "git":
		a.git(args[1:], st)
	case "sh", "bash":
		return a.nestedShell(name, args, st)
	case "command", "exec", "nohup", "env":
		child, complete := wrappedCommandArgs(name, args[1:])
		if !complete {
			a.addUnknown("unsupported " + name + " wrapper options")
		}
		if len(child) == 0 {
			a.addUnknown("dynamic wrapped command")
			return nil
		}
		return a.classifyCommand(child, st, name == "command")
	case "sudo", "doas", "su", "pkexec":
		a.addEffect(permission.Effect{Type: "privilege", Capability: permission.CapabilityPrivilegeEscalation, Operation: "escalate", Command: name, Reason: "privilege escalation", Confidence: "high"})
	case "systemctl":
		subcommand := firstSystemctlSubcommand(args[1:])
		switch subcommand {
		case "enable", "disable", "reenable", "link", "preset", "preset-all", "mask", "unmask", "edit", "revert":
			a.addEffect(permission.Effect{Type: "persistence", Capability: permission.CapabilityPersistenceWrite, Operation: subcommand, Command: name, Reason: "persistent service configuration", Confidence: "high"})
		case "status", "is-active", "is-enabled", "list-units", "list-unit-files", "show", "cat", "help", "--help", "--version":
		default:
			a.addUnknown("unclassified systemctl operation: " + subcommand)
		}
	case "launchctl", "crontab":
		a.addEffect(permission.Effect{Type: "persistence", Capability: permission.CapabilityPersistenceWrite, Operation: "configure", Command: name, Reason: "persistent startup configuration", Confidence: "high"})
	case "go", "npm", "pnpm", "yarn", "pip", "pip3", "python", "python3":
		if containsAny(args[1:], "get", "install", "download", "add") {
			a.addEffect(permission.Effect{Type: "network", Capability: permission.CapabilityNetworkRead, Operation: "read", Command: name, Reason: "dependency download", Confidence: "medium"})
		}
		a.addUnknown(name + " child effects are not statically analyzed")
	default:
		a.addUnknown("unknown command: " + name)
		for _, arg := range positional(args[1:]) {
			if looksLikePath(arg) {
				path := a.resolvePath(arg, st.cwd)
				a.addEffect(permission.Effect{Type: "filesystem", Capability: permission.CapabilityUnknown, Operation: "potential access", Resource: path, Command: name, Reason: "path-shaped argument to unknown command", Confidence: "low", Dynamic: true})
			}
		}
	}
	return nil
}

func (a *analyzer) grep(args []string, st *state) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-f", "--file", "--exclude-from":
			if i+1 >= len(args) {
				a.addIncomplete("incomplete grep file option")
				continue
			}
			a.addPathEffect("read", args[i+1], "grep", "high", st)
			i++
		default:
			for _, prefix := range []string{"-f", "--file=", "--exclude-from="} {
				if value, ok := strings.CutPrefix(arg, prefix); ok && value != "" {
					a.addPathEffect("read", value, "grep", "high", st)
				}
			}
			if !strings.HasPrefix(arg, "-") {
				a.addPathEffect("read", arg, "grep", "high", st)
			}
		}
	}
}

func firstSystemctlSubcommand(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func (a *analyzer) find(args []string, st *state) {
	roots := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") || arg == "(" || arg == ")" || arg == "!" {
			break
		}
		roots = append(roots, arg)
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}
	for _, root := range roots {
		a.addPathEffect("read", root, "find", "high", st)
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-delete":
			for _, root := range roots {
				a.addPathEffect("delete", root, "find", "high", st)
			}
			a.addUnknown("find -delete target set depends on runtime traversal")
		case "-fprint", "-fprintf":
			if i+1 < len(args) {
				a.addPathEffect("write", args[i+1], "find", "high", st)
				i++
			} else {
				a.addUnknown("find output path is missing")
			}
		case "-exec", "-execdir", "-ok", "-okdir":
			start := i + 1
			end := start
			for end < len(args) && args[end] != ";" && args[end] != "+" {
				end++
			}
			if start < end {
				_ = a.classifyCommand(args[start:end], st, false)
			}
			a.addUnknown("find child command targets depend on runtime traversal")
			i = end
		}
	}
}

func (a *analyzer) networkPaths(name string, args []string, st *state) {
	switch name {
	case "curl":
		for i := 0; i < len(args); i++ {
			arg := args[i]
			switch arg {
			case "-d", "--data", "--data-raw", "--data-binary":
				if i+1 < len(args) {
					if value, ok := strings.CutPrefix(args[i+1], "@"); ok {
						a.addPathEffect("read", value, name, "high", st)
					}
					i++
				}
			case "--upload-file", "-T", "--key", "--cert", "--cacert", "--unix-socket":
				if i+1 < len(args) {
					a.addPathEffect("read", args[i+1], name, "high", st)
					i++
				}
			case "--config", "-K":
				if i+1 < len(args) {
					a.addPathEffect("read", args[i+1], name, "high", st)
					a.addIncomplete("curl config semantics are not analyzed")
					i++
				}
			case "--output", "-o":
				if i+1 < len(args) {
					a.addPathEffect("write", args[i+1], name, "high", st)
					i++
				}
			default:
				for _, prefix := range []string{"-d@", "--data=@", "--data-raw=@", "--data-binary=@"} {
					if value, ok := strings.CutPrefix(arg, prefix); ok {
						a.addPathEffect("read", value, name, "high", st)
					}
				}
				for _, prefix := range []string{"-T", "--upload-file=", "--key=", "--cert=", "--cacert=", "--unix-socket="} {
					if value, ok := strings.CutPrefix(arg, prefix); ok && value != "" {
						a.addPathEffect("read", value, name, "high", st)
					}
				}
				for _, prefix := range []string{"-K", "--config="} {
					if value, ok := strings.CutPrefix(arg, prefix); ok && value != "" {
						a.addPathEffect("read", value, name, "high", st)
						a.addIncomplete("curl config semantics are not analyzed")
					}
				}
				for _, prefix := range []string{"-o", "--output="} {
					if value, ok := strings.CutPrefix(arg, prefix); ok && value != "" {
						a.addPathEffect("write", value, name, "high", st)
					}
				}
			}
		}
	case "wget":
		for i := 0; i < len(args); i++ {
			arg := args[i]
			switch arg {
			case "-O", "--output-document":
				if i+1 < len(args) {
					a.addPathEffect("write", args[i+1], name, "high", st)
					i++
				}
			case "--post-file", "--body-file", "-i", "--input-file", "--certificate", "--private-key", "--ca-certificate", "--load-cookies":
				if i+1 < len(args) {
					a.addPathEffect("read", args[i+1], name, "high", st)
					i++
				}
			case "--config":
				if i+1 < len(args) {
					a.addPathEffect("read", args[i+1], name, "high", st)
					a.addIncomplete("wget config semantics are not analyzed")
					i++
				}
			default:
				for _, prefix := range []string{"--post-file=", "--body-file=", "--input-file=", "--certificate=", "--private-key=", "--ca-certificate=", "--load-cookies="} {
					if value, ok := strings.CutPrefix(arg, prefix); ok {
						a.addPathEffect("read", value, name, "high", st)
					}
				}
				if value, ok := strings.CutPrefix(arg, "--config="); ok {
					a.addPathEffect("read", value, name, "high", st)
					a.addIncomplete("wget config semantics are not analyzed")
				}
				for _, prefix := range []string{"-O", "--output-document="} {
					if value, ok := strings.CutPrefix(arg, prefix); ok && value != "" {
						a.addPathEffect("write", value, name, "high", st)
					}
				}
			}
		}
	case "ssh":
		for i := 0; i < len(args); i++ {
			arg := args[i]
			if arg == "-i" || arg == "--identity-file" || arg == "-F" || arg == "--config" || arg == "-o" {
				if i+1 < len(args) {
					value := args[i+1]
					if arg == "-i" || arg == "--identity-file" {
						a.addPathEffect("read", value, name, "high", st)
					} else {
						a.addIncomplete("SSH config semantics are not analyzed")
					}
					i++
				}
				continue
			}
			for _, prefix := range []string{"-i", "--identity-file="} {
				if value, ok := strings.CutPrefix(arg, prefix); ok && value != "" {
					a.addPathEffect("read", value, name, "high", st)
				}
			}
			if strings.HasPrefix(arg, "-F") || strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "-o") {
				a.addIncomplete("SSH config semantics are not analyzed")
			}
		}
	case "scp", "rsync":
		a.remoteTransferOptions(name, args, st)
		values := positional(args)
		for i, value := range values {
			if remotePath(value) {
				continue
			}
			operation := "read"
			if i == len(values)-1 && slices.ContainsFunc(values[:i], remotePath) {
				operation = "write"
			}
			a.addPathEffect(operation, value, name, "high", st)
		}
	}
}

func (a *analyzer) remoteTransferOptions(name string, args []string, st *state) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if name == "scp" {
			switch arg {
			case "-i":
				if i+1 < len(args) {
					a.addPathEffect("read", args[i+1], name, "high", st)
					i++
				}
			case "-F":
				if i+1 < len(args) {
					a.addPathEffect("read", args[i+1], name, "high", st)
					a.addIncomplete("SCP config semantics are not analyzed")
					i++
				}
			case "-o", "-S", "-J":
				a.addIncomplete("SCP transport options are not analyzed")
				if i+1 < len(args) {
					i++
				}
			default:
				if value, ok := strings.CutPrefix(arg, "-i"); ok && value != "" {
					a.addPathEffect("read", value, name, "high", st)
				}
				if strings.HasPrefix(arg, "-F") || strings.HasPrefix(arg, "-o") || strings.HasPrefix(arg, "-S") || strings.HasPrefix(arg, "-J") {
					a.addIncomplete("SCP transport options are not analyzed")
				}
			}
			continue
		}
		switch arg {
		case "-e", "--rsh", "--rsync-path":
			a.addIncomplete("rsync transport command semantics are not analyzed")
			if i+1 < len(args) {
				i++
			}
		default:
			if strings.HasPrefix(arg, "-e") || strings.HasPrefix(arg, "--rsh=") || strings.HasPrefix(arg, "--rsync-path=") {
				a.addIncomplete("rsync transport command semantics are not analyzed")
			}
		}
	}
}

func remotePath(value string) bool {
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return true
	}
	return strings.Contains(value, ":") && !filepath.IsAbs(value)
}

func (a *analyzer) nestedShell(name string, args []string, st *state) error {
	for i := 1; i+1 < len(args); i++ {
		if args[i] != "-c" {
			continue
		}
		if args[i+1] == "" {
			a.addUnknown("dynamic nested shell source")
			return nil
		}
		variant := syntaxVariant(name)
		copy := cloneState(st)
		return a.parse(args[i+1], variant, &copy)
	}
	a.addUnknown("interactive or file-based shell execution")
	return nil
}

func syntaxVariant(name string) syntax.LangVariant {
	if name == "bash" {
		return syntax.LangBash
	}
	return syntax.LangPOSIX
}

func (a *analyzer) git(args []string, st *state) {
	if len(args) == 0 {
		a.addUnknown("dynamic git operation")
		return
	}
	sub := args[0]
	switch sub {
	case "status", "diff", "log", "show", "rev-parse", "remote":
		a.addEffect(permission.Effect{Type: "git", Capability: permission.CapabilityGitRead, Operation: sub, Resource: st.cwd, Command: "git", Confidence: "high"})
	case "fetch", "pull", "clone":
		a.addEffect(permission.Effect{Type: "git", Capability: permission.CapabilityGitRemoteRead, Operation: sub, Resource: firstPositional(args[1:]), Command: "git", Confidence: "high"})
	case "push":
		a.addEffect(permission.Effect{Type: "git", Capability: permission.CapabilityGitRemoteWrite, Operation: sub, Resource: firstPositional(args[1:]), Command: "git", Confidence: "high"})
	case "checkout", "switch", "restore", "reset", "clean":
		a.addEffect(permission.Effect{Type: "git", Capability: permission.CapabilityGitWrite, Operation: sub, Resource: st.cwd, Command: "git", Confidence: "high"})
	default:
		a.addUnknown("unclassified git operation: " + sub)
	}
}

func (a *analyzer) network(name string, args []string) {
	write := hasNetworkWriteOption(args) || name == "scp" || name == "rsync"
	capability := permission.CapabilityNetworkRead
	operation := "read"
	if write {
		capability = permission.CapabilityNetworkWrite
		operation = "write"
	}
	resources := networkResources(name, args)
	if len(resources) == 0 {
		a.addUnknown("dynamic network destination")
		resources = []string{""}
	}
	for _, resource := range resources {
		a.addEffect(permission.Effect{Type: "network", Capability: capability, Operation: operation, Resource: resource, Command: name, Reason: "network access", Confidence: "high", Dynamic: resource == ""})
	}
}

func networkResources(name string, args []string) []string {
	var resources []string
	switch name {
	case "curl", "wget":
		for _, arg := range args {
			if parsed, err := url.Parse(arg); err == nil && parsed.Host != "" {
				resources = append(resources, parsed.Scheme+"://"+parsed.Host)
			}
		}
	case "ssh":
		if endpoint := sshEndpoint(args); endpoint != "" {
			resources = append(resources, endpoint)
		}
	case "scp", "rsync":
		for _, arg := range args {
			if endpoint := remoteEndpoint(arg); endpoint != "" {
				resources = append(resources, endpoint)
			}
		}
	}
	return resources
}

func sshEndpoint(args []string) string {
	port := ""
	optionsWithValue := "bcDEeFIiJLlmOoPpQRSWw"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return formatSSHEndpoint(args[i+1], port)
			}
			return ""
		}
		if arg == "-p" && i+1 < len(args) {
			port = args[i+1]
			i++
			continue
		}
		if len(arg) == 2 && arg[0] == '-' && strings.ContainsRune(optionsWithValue, rune(arg[1])) {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return formatSSHEndpoint(arg, port)
	}
	return ""
}

func formatSSHEndpoint(host, port string) string {
	if host == "" {
		return ""
	}
	if port != "" {
		return "ssh://" + host + ":" + port
	}
	return "ssh://" + host
}

func remoteEndpoint(value string) string {
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host + parsed.Path
	}
	if filepath.IsAbs(value) {
		return ""
	}
	host, path, ok := strings.Cut(value, ":")
	if !ok || host == "" {
		return ""
	}
	return "ssh://" + host + "/" + strings.TrimPrefix(path, "/")
}

func hasNetworkWriteOption(args []string) bool {
	for _, arg := range args {
		if slices.Contains([]string{"-X", "--request", "-d", "--data", "--data-raw", "--data-binary", "--upload-file", "-T", "--post-file", "--body-file"}, arg) {
			return true
		}
		for _, prefix := range []string{"-d", "-T", "--request=", "--data=", "--data-raw=", "--data-binary=", "--upload-file=", "--post-file=", "--body-file="} {
			if strings.HasPrefix(arg, prefix) {
				return true
			}
		}
	}
	return false
}

func (a *analyzer) pathArgs(operation, command string, args []string, st *state) {
	for _, value := range positional(args) {
		if value == "" {
			a.addUnknown("dynamic path for " + command)
			continue
		}
		a.addPathEffect(operation, value, command, "high", st)
	}
}

func (a *analyzer) addPathEffect(operation, value, command, confidence string, st *state) {
	if value == "-" || value == "" {
		return
	}
	path := a.resolvePath(value, st.cwd)
	if resolved, changed := resolvePathSymlinks(path, operation == "delete"); changed {
		path = resolved
		a.rememberable = false
	}
	capability := a.pathCapability(operation, path)
	reason := operation + " filesystem path"
	if protected := a.protectedCapability(operation, path); protected != "" {
		capability = protected
		switch protected {
		case permission.CapabilityCredentialsRead:
			reason = "protected credential read"
		case permission.CapabilitySSHAuthorizationWrite:
			reason = "SSH authorization change"
		case permission.CapabilityRawDeviceAccess:
			reason = "raw device access"
		case permission.CapabilityDockerSocketAccess:
			reason = "container control socket access"
		case permission.CapabilityPersistenceWrite:
			reason = "persistent startup configuration"
		}
	}
	a.addEffect(permission.Effect{Type: "filesystem", Capability: capability, Operation: operation, Resource: path, Command: command, Reason: reason, Confidence: confidence})
}

func (a *analyzer) resolvePath(value, cwd string) string {
	if value == "~" && a.home != "" {
		return a.home
	}
	if after, ok := strings.CutPrefix(value, "~/"); ok && a.home != "" {
		value = filepath.Join(a.home, after)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(cwd, value)
	}
	return filepath.Clean(value)
}

func resolvePathSymlinks(path string, preserveFinal bool) (string, bool) {
	original := filepath.Clean(path)
	current := original
	parts := make([]string, 0, 4)
	if preserveFinal {
		parts = append(parts, filepath.Base(current))
		current = filepath.Dir(current)
	}
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(parts) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, parts[i])
			}
			resolved = filepath.Clean(resolved)
			return resolved, resolved != original
		}
		parent := filepath.Dir(current)
		if parent == current {
			return original, false
		}
		parts = append(parts, filepath.Base(current))
		current = parent
	}
}

func (a *analyzer) pathCapability(operation, path string) permission.Capability {
	workspace := false
	for _, root := range a.roots {
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			workspace = true
			break
		}
	}
	switch operation {
	case "read":
		if workspace {
			return permission.CapabilityFilesystemReadWorkspace
		}
		return permission.CapabilityFilesystemReadExternal
	case "delete":
		if workspace {
			return permission.CapabilityFilesystemDeleteWorkspace
		}
		return permission.CapabilityFilesystemDeleteExternal
	default:
		if workspace {
			return permission.CapabilityFilesystemWriteWorkspace
		}
		return permission.CapabilityFilesystemWriteExternal
	}
}

func (a *analyzer) protectedCapability(operation, path string) permission.Capability {
	clean := protectedPath(path)
	lower := strings.ToLower(clean)
	if strings.HasPrefix(lower, "/dev/") && (strings.Contains(lower, "/disk") || strings.Contains(lower, "/sd") || strings.Contains(lower, "/nvme") || lower == "/dev/mem" || lower == "/dev/kmem") {
		return permission.CapabilityRawDeviceAccess
	}
	if lower == protectedLiteral("/var/run/docker.sock") || lower == protectedLiteral("/run/docker.sock") || strings.HasSuffix(lower, "/podman/podman.sock") {
		return permission.CapabilityDockerSocketAccess
	}
	if operation == "read" && a.isCredentialPath(clean) {
		return permission.CapabilityCredentialsRead
	}
	if operation != "read" {
		if a.home != "" && clean == filepath.Join(protectedPath(a.home), ".ssh", "authorized_keys") {
			return permission.CapabilitySSHAuthorizationWrite
		}
		if a.isPersistencePath(clean) {
			return permission.CapabilityPersistenceWrite
		}
	}
	return ""
}

func (a *analyzer) isCredentialPath(path string) bool {
	if a.home == "" {
		return false
	}
	path = protectedPath(path)
	home := protectedPath(a.home)
	ssh := filepath.Join(home, ".ssh")
	if strings.HasPrefix(path, ssh+string(filepath.Separator)) {
		base := filepath.Base(path)
		return strings.HasPrefix(base, "id_") && !strings.HasSuffix(base, ".pub")
	}
	known := []string{
		filepath.Join(home, ".aws", "credentials"), filepath.Join(home, ".kube", "config"),
		filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"),
	}
	if slices.Contains(known, path) {
		return true
	}
	return strings.HasPrefix(path, filepath.Join(home, ".gnupg")+string(filepath.Separator))
}

func (a *analyzer) isPersistencePath(path string) bool {
	path = protectedPath(path)
	if a.home != "" {
		home := protectedPath(a.home)
		for _, name := range []string{".bashrc", ".bash_profile", ".profile", ".zshrc", ".zprofile"} {
			if path == filepath.Join(home, name) {
				return true
			}
		}
		for _, dir := range []string{".config/systemd/user", "Library/LaunchAgents"} {
			if strings.HasPrefix(path, filepath.Join(home, dir)+string(filepath.Separator)) {
				return true
			}
		}
	}
	for _, dir := range []string{"/etc/cron.d", "/etc/systemd/system", "/Library/LaunchAgents", "/Library/LaunchDaemons"} {
		dir = protectedLiteral(dir)
		if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (a *analyzer) copyMove(name string, args []string, st *state) {
	sources, destination, directory, ok := copyMoveOperands(args, st.cwd)
	if !ok {
		a.addIncomplete("ambiguous or incomplete " + name + " destination")
	}
	for _, value := range sources {
		operation := "read"
		if name == "mv" {
			operation = "delete"
		}
		a.addPathEffect(operation, value, name, "high", st)
	}
	if destination == "" {
		return
	}
	a.addPathEffect("write", destination, name, "high", st)
	if directory {
		for _, source := range sources {
			a.addPathEffect("write", filepath.Join(destination, filepath.Base(filepath.Clean(source))), name, "high", st)
		}
	}
}

func installDirectoryMode(args []string) bool {
	for _, arg := range args {
		if arg == "-d" || arg == "--directory" {
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(strings.TrimPrefix(arg, "-"), "d") {
			return true
		}
	}
	return false
}

func installDirectoryOperands(args []string) []string {
	values := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(values, args[i+1:]...)
		}
		if slices.Contains([]string{"-m", "--mode", "-o", "--owner", "-g", "--group", "-S", "--suffix"}, arg) {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		values = append(values, arg)
	}
	return values
}

func protectedLiteral(path string) string {
	resolved, _ := resolvePathSymlinks(path, false)
	return protectedPath(resolved)
}

func protectedPath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func copyMoveOperands(args []string, cwd string) (sources []string, destination string, directory bool, ok bool) {
	values := make([]string, 0, len(args))
	targetDirectory := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-t", "--target-directory":
			if i+1 >= len(args) {
				return nil, "", false, false
			}
			targetDirectory = args[i+1]
			i++
		case "--":
			values = append(values, args[i+1:]...)
			i = len(args)
		default:
			if value, found := strings.CutPrefix(arg, "--target-directory="); found {
				targetDirectory = value
				continue
			}
			if strings.HasPrefix(arg, "-") {
				continue
			}
			values = append(values, arg)
		}
	}
	if targetDirectory != "" {
		return values, targetDirectory, true, len(values) > 0
	}
	if len(values) < 2 {
		return values, "", false, false
	}
	destination = values[len(values)-1]
	directory = strings.HasSuffix(destination, string(filepath.Separator))
	resolvedDestination := destination
	if !filepath.IsAbs(resolvedDestination) {
		resolvedDestination = filepath.Join(cwd, resolvedDestination)
	}
	if info, err := os.Stat(resolvedDestination); err == nil && info.IsDir() {
		directory = true
	}
	if len(values) > 2 && !directory {
		return values[:len(values)-1], destination, false, false
	}
	return values[:len(values)-1], destination, directory, true
}

func wrappedCommandArgs(name string, args []string) ([]string, bool) {
	complete := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return args[i+1:], complete
		}
		switch name {
		case "env":
			if strings.Contains(arg, "=") && !strings.HasPrefix(arg, "-") {
				continue
			}
			switch arg {
			case "-i", "--ignore-environment", "-0", "--null":
				continue
			case "-u", "--unset", "-C", "--chdir", "-S", "--split-string":
				if i+1 >= len(args) {
					return nil, false
				}
				i++
				continue
			}
			if strings.HasPrefix(arg, "--unset=") || strings.HasPrefix(arg, "--chdir=") || strings.HasPrefix(arg, "--split-string=") {
				continue
			}
		case "exec":
			switch arg {
			case "-c", "-l":
				continue
			case "-a":
				if i+1 >= len(args) {
					return nil, false
				}
				i++
				continue
			}
		case "command":
			if arg == "-p" {
				continue
			}
			if arg == "-v" || arg == "-V" {
				return nil, false
			}
		case "nohup":
			if arg == "--help" || arg == "--version" {
				return nil, false
			}
		}
		if strings.HasPrefix(arg, "-") {
			complete = false
			continue
		}
		return args[i:], complete
	}
	return nil, complete
}

func positional(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "" && !strings.HasPrefix(arg, "-") {
			out = append(out, arg)
		}
	}
	return out
}
func firstPositional(args []string) string {
	values := positional(args)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func containsAny(args []string, values ...string) bool {
	for _, arg := range args {
		for _, value := range values {
			if arg == value {
				return true
			}
		}
	}
	return false
}
func looksLikePath(value string) bool {
	return filepath.IsAbs(value) || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "~/")
}

func setIDMode(mode string) bool {
	if strings.Contains(mode, "u+s") || strings.Contains(mode, "g+s") {
		return true
	}
	return len(mode) >= 4 && (mode[0] == '4' || mode[0] == '2' || mode[0] == '6')
}
