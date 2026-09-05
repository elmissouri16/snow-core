// Package shellanalysis performs bounded, conservative static analysis of shell source.
package shellanalysis

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
	"mvdan.cc/sh/v3/syntax"
)

const (
	maxSourceBytes = 64 << 10
	maxASTNodes    = 10_000
	maxEffects     = 128
	maxVariables   = 128
	maxValueBytes  = 4 << 10
	maxDepth       = 4
	scopeVersion   = "shell-analysis-v2"
)

type state struct {
	cwd  string
	vars map[string]string
}

type analyzer struct {
	ctx          context.Context
	roots        []string
	home         string
	depth        int
	nodes        int
	commandDepth int
	source       string
	startCWD     string
	policy       []pathRule
	resolver     pathResolver
	err          error
	effects      map[string]permission.Effect
	paths        map[string]struct{}
	caps         map[permission.Capability]struct{}
	unknown      bool
	rememberable bool
}

// Analyze parses command using POSIX shell grammar and returns a deterministic,
// bounded description of statically visible effects.
func Analyze(ctx context.Context, command, cwd string, roots []string, home string) (permission.Analysis, error) {
	return AnalyzeWithOptions(ctx, command, cwd, roots, home, Options{})
}

// Options adds operator-owned protected resources; it cannot relax built-in policy.
type Options struct{ ProtectedPaths []string }

// AnalyzeWithOptions performs the same bounded preflight with additional policy.
func AnalyzeWithOptions(ctx context.Context, command, cwd string, roots []string, home string, opts Options) (permission.Analysis, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return permission.Analysis{}, err
	}
	if len(command) > maxSourceBytes {
		return permission.Analysis{}, fmt.Errorf("shell analysis: source exceeds %d bytes", maxSourceBytes)
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return permission.Analysis{}, fmt.Errorf("shell analysis: resolve cwd: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(absCWD); err == nil {
		absCWD = resolved
	}
	cleanHome := ""
	if home != "" {
		cleanHome = filepath.Clean(home)
		if resolved, err := filepath.EvalSymlinks(cleanHome); err == nil {
			cleanHome = resolved
		}
	}
	a := &analyzer{
		ctx: ctx, home: cleanHome, source: command, startCWD: absCWD, effects: make(map[string]permission.Effect),
		paths: make(map[string]struct{}), caps: make(map[permission.Capability]struct{}), rememberable: true,
	}
	var policyErr error
	a.policy, policyErr = preparePathRules(cleanHome, opts.ProtectedPaths, &a.resolver)
	if policyErr != nil {
		return permission.Analysis{}, policyErr
	}
	for _, root := range roots {
		if abs, err := filepath.Abs(root); err == nil {
			abs = filepath.Clean(abs)
			if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
				abs = resolved
			}
			a.roots = append(a.roots, abs)
		}
	}
	slices.Sort(a.roots)
	a.roots = slices.Compact(a.roots)
	vars := make(map[string]string)
	if a.home != "" {
		vars["HOME"] = a.home
	}
	if err := a.parse(command, syntax.LangPOSIX, &state{cwd: filepath.Clean(absCWD), vars: vars}); err != nil {
		return permission.Analysis{}, err
	}
	if a.err != nil {
		return permission.Analysis{}, a.err
	}
	return a.result(), nil
}

func (a *analyzer) parse(source string, variant syntax.LangVariant, st *state) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if a.depth >= maxDepth {
		a.addIncomplete("nested shell depth limit reached")
		return nil
	}
	file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(source), "agent-command")
	if err != nil {
		return fmt.Errorf("shell analysis: parse: %w", err)
	}
	exceeded := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil {
			return true
		}
		a.nodes++
		if a.nodes > maxASTNodes {
			exceeded = true
			return false
		}
		return true
	})
	if exceeded {
		a.addIncomplete("shell AST node limit reached")
		return nil
	}
	a.depth++
	defer func() { a.depth-- }()
	return a.stmts(file.Stmts, st)
}

func (a *analyzer) stmts(stmts []*syntax.Stmt, st *state) error {
	for _, stmt := range stmts {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		for _, redir := range stmt.Redirs {
			a.redirect(redir, st)
		}
		if stmt.Background {
			a.addEffect(permission.Effect{Type: "process", Capability: permission.CapabilityProcessExec, Operation: "background", Reason: "background process", Confidence: "high"})
		}
		commandState := st
		if stmt.Background {
			copy := cloneState(st)
			commandState = &copy
		}
		if err := a.command(stmt.Cmd, commandState); err != nil {
			return err
		}
	}
	return nil
}

func (a *analyzer) call(call *syntax.CallExpr, st *state) error {
	local := cloneState(st)
	for _, assign := range call.Assigns {
		if assign.Name == nil {
			continue
		}
		// POSIX expands ordinary command arguments against the pre-assignment
		// environment: HOME=/tmp cat "$HOME/file" still uses the old HOME.
		value, ok := "", true
		if assign.Value != nil {
			value, ok = a.word(assign.Value, &local)
		}
		if !ok || len(value) > maxValueBytes {
			a.addUnknown("dynamic or oversized assignment " + assign.Name.Value)
			delete(local.vars, assign.Name.Value)
			continue
		}
		if _, exists := local.vars[assign.Name.Value]; !exists && len(local.vars) >= maxVariables {
			a.addUnknown("shell variable limit reached")
			continue
		}
		local.vars[assign.Name.Value] = value
	}
	if len(call.Args) == 0 {
		*st = local
		return nil
	}
	args := make([]string, len(call.Args))
	for i, word := range call.Args {
		value, ok := a.word(word, st)
		if !ok {
			a.addUnknown("dynamic command or argument")
			value = ""
		}
		args[i] = value
	}
	if args[0] == "" {
		a.addUnknown("dynamic command name")
		return nil
	}
	if len(call.Assigns) > 0 {
		a.addUnknown("command environment assignments")
		err := a.classify(args, &local)
		if spec := commandSpecs[args[0]]; spec != nil && spec.Builtin {
			joinState(st, local)
		}
		return err
	}
	return a.classify(args, st)
}

func (a *analyzer) redirect(redir *syntax.Redirect, st *state) {
	switch redir.Op {
	case syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
		if redir.Hdoc != nil {
			if _, ok := a.word(redir.Hdoc, st); !ok {
				a.addUnknown("dynamic here-document content")
			}
		}
		return
	case syntax.DplIn, syntax.DplOut:
		resource, ok := a.word(redir.Word, st)
		if !ok || (resource != "-" && !isDecimal(resource)) {
			a.addUnknown("dynamic file-descriptor redirection")
		}
		return
	}
	resource, ok := a.word(redir.Word, st)
	if !ok || resource == "" {
		a.addUnknown("dynamic redirection target")
		return
	}
	operation := "write"
	if redir.Op == syntax.RdrInOut {
		a.addPathEffect("write", resource, "redirection", "high", st)
	}
	if redir.Op == syntax.RdrIn || redir.Op == syntax.RdrInOut {
		operation = "read"
	}
	a.addPathEffect(operation, resource, "redirection", "high", st)
}

func isDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func cloneState(in *state) state {
	out := state{cwd: in.cwd, vars: make(map[string]string, len(in.vars))}
	for key, value := range in.vars {
		out.vars[key] = value
	}
	return out
}

func (a *analyzer) addUnknown(reason string) {
	a.unknown = true
	a.rememberable = false
	a.addEffect(permission.Effect{Type: "unknown", Capability: permission.CapabilityUnknown, Operation: "unknown", Reason: reason, Confidence: "low", Dynamic: true})
}

func (a *analyzer) addIncomplete(reason string) {
	a.unknown = true
	a.rememberable = false
	a.caps[permission.CapabilityAnalysisIncomplete] = struct{}{}
	a.addEffect(permission.Effect{Type: "analysis", Capability: permission.CapabilityAnalysisIncomplete, Operation: "incomplete", Reason: reason, Confidence: "high", Dynamic: true})
}

func (a *analyzer) addEffect(effect permission.Effect) {
	key := strings.Join([]string{effect.Type, string(effect.Capability), effect.Operation, effect.Resource, effect.Command, effect.Reason, effect.Confidence, fmt.Sprint(effect.Dynamic)}, "\x00")
	if _, exists := a.effects[key]; exists {
		return
	}
	if len(a.effects) >= maxEffects {
		a.unknown = true
		a.rememberable = false
		a.caps[permission.CapabilityAnalysisIncomplete] = struct{}{}
		return
	}
	a.effects[key] = effect
	a.caps[effect.Capability] = struct{}{}
	if filepath.IsAbs(effect.Resource) {
		a.paths[effect.Resource] = struct{}{}
	}
}

func (a *analyzer) result() permission.Analysis {
	effectKeys := make([]string, 0, len(a.effects))
	for key := range a.effects {
		effectKeys = append(effectKeys, key)
	}
	slices.Sort(effectKeys)
	effects := make([]permission.Effect, 0, len(effectKeys))
	for _, key := range effectKeys {
		effects = append(effects, a.effects[key])
	}
	caps := make([]permission.Capability, 0, len(a.caps))
	for capability := range a.caps {
		caps = append(caps, capability)
	}
	slices.Sort(caps)
	paths := make([]string, 0, len(a.paths))
	for path := range a.paths {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	var canonical strings.Builder
	writeScopeField(&canonical, "version", scopeVersion)
	writeScopeField(&canonical, "specification", specificationDigest)
	writeScopeField(&canonical, "cwd", a.startCWD)
	writeScopeField(&canonical, "source", a.source)
	for _, rule := range a.policy {
		writeScopeField(&canonical, "policy-path", rule.Path)
		writeScopeField(&canonical, "policy-capability", string(rule.Capability))
	}
	for _, root := range a.roots {
		writeScopeField(&canonical, "root", root)
	}
	for _, capability := range caps {
		writeScopeField(&canonical, "capability", string(capability))
	}
	for _, effect := range effects {
		writeScopeField(&canonical, "effect-capability", string(effect.Capability))
		writeScopeField(&canonical, "effect-operation", effect.Operation)
		writeScopeField(&canonical, "effect-resource", effect.Resource)
		writeScopeField(&canonical, "effect-command", effect.Command)
	}
	scope := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical.String())))
	summary := fmt.Sprintf("%d inferred effect(s)", len(effects))
	return permission.Analysis{Effects: effects, Capabilities: caps, Paths: paths, Summary: summary, Unknown: a.unknown, Rememberable: a.rememberable && !a.unknown, ScopeKey: scope, ScopeLabel: "this command and its resources in this working directory"}
}

func writeScopeField(dst *strings.Builder, name, value string) {
	fmt.Fprintf(dst, "%d:%s%d:%s", len(name), name, len(value), value)
}
