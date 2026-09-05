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
	scopeVersion   = "shell-analysis-v1"
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
	effects      map[string]permission.Effect
	paths        map[string]struct{}
	caps         map[permission.Capability]struct{}
	unknown      bool
	rememberable bool
}

// Analyze parses command using POSIX shell grammar and returns a deterministic,
// bounded description of statically visible effects.
func Analyze(ctx context.Context, command, cwd string, roots []string, home string) (permission.Analysis, error) {
	if ctx == nil {
		ctx = context.Background()
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
		ctx: ctx, home: cleanHome, effects: make(map[string]permission.Effect),
		paths: make(map[string]struct{}), caps: make(map[permission.Capability]struct{}), rememberable: true,
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
	nodes := 0
	exceeded := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil {
			return true
		}
		nodes++
		if nodes > maxASTNodes {
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
		if err := a.command(stmt.Cmd, st); err != nil {
			return err
		}
	}
	return nil
}

func (a *analyzer) command(cmd syntax.Command, st *state) error {
	switch cmd := cmd.(type) {
	case *syntax.CallExpr:
		return a.call(cmd, st)
	case *syntax.BinaryCmd:
		if err := a.stmts([]*syntax.Stmt{cmd.X}, st); err != nil {
			return err
		}
		return a.stmts([]*syntax.Stmt{cmd.Y}, st)
	case *syntax.Subshell:
		copy := cloneState(st)
		return a.stmts(cmd.Stmts, &copy)
	case *syntax.Block:
		return a.stmts(cmd.Stmts, st)
	case *syntax.IfClause:
		copy := cloneState(st)
		if err := a.stmts(cmd.Cond, &copy); err != nil {
			return err
		}
		if err := a.stmts(cmd.Then, &copy); err != nil {
			return err
		}
		for branch := cmd.Else; branch != nil; branch = branch.Else {
			if err := a.stmts(branch.Cond, &copy); err != nil {
				return err
			}
			if err := a.stmts(branch.Then, &copy); err != nil {
				return err
			}
		}
		return nil
	case *syntax.WhileClause:
		copy := cloneState(st)
		if err := a.stmts(cmd.Cond, &copy); err != nil {
			return err
		}
		return a.stmts(cmd.Do, &copy)
	case *syntax.ForClause:
		copy := cloneState(st)
		return a.stmts(cmd.Do, &copy)
	default:
		a.addIncomplete(fmt.Sprintf("unsupported shell construct %T", cmd))
		return nil
	}
}

func (a *analyzer) call(call *syntax.CallExpr, st *state) error {
	local := cloneState(st)
	for _, assign := range call.Assigns {
		if assign.Name == nil || assign.Value == nil {
			continue
		}
		// POSIX expands ordinary command arguments against the pre-assignment
		// environment: HOME=/tmp cat "$HOME/file" still uses the old HOME.
		value, ok := a.word(assign.Value, st)
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
		}
		args[i] = value
	}
	if args[0] == "" {
		a.addUnknown("dynamic command name")
		return nil
	}
	if len(call.Assigns) > 0 && filepath.Base(args[0]) != "cd" {
		return a.classify(args, &local)
	}
	return a.classify(args, st)
}

func (a *analyzer) word(word *syntax.Word, st *state) (string, bool) {
	var b strings.Builder
	for _, part := range word.Parts {
		value, ok := a.wordPart(part, st)
		if !ok {
			return b.String(), false
		}
		b.WriteString(value)
	}
	value := b.String()
	if value == "~" {
		if a.home == "" {
			return "", false
		}
		value = a.home
	} else if after, ok := strings.CutPrefix(value, "~/"); ok {
		if a.home == "" {
			return "", false
		}
		value = filepath.Join(a.home, after)
	}
	return value, true
}

func (a *analyzer) wordPart(part syntax.WordPart, st *state) (string, bool) {
	switch part := part.(type) {
	case *syntax.Lit:
		return part.Value, true
	case *syntax.SglQuoted:
		return part.Value, true
	case *syntax.DblQuoted:
		var b strings.Builder
		for _, nested := range part.Parts {
			value, ok := a.wordPart(nested, st)
			if !ok {
				return b.String(), false
			}
			b.WriteString(value)
		}
		return b.String(), true
	case *syntax.ParamExp:
		if part.Param == nil || part.Excl || part.Length || part.Index != nil || part.Slice != nil || part.Repl != nil || part.Exp != nil {
			return "", false
		}
		value, ok := st.vars[part.Param.Value]
		return value, ok
	case *syntax.CmdSubst:
		copy := cloneState(st)
		_ = a.stmts(part.Stmts, &copy)
		return "", false
	default:
		return "", false
	}
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
	return permission.Analysis{Effects: effects, Capabilities: caps, Paths: paths, Summary: summary, Unknown: a.unknown, Rememberable: a.rememberable && !a.unknown, ScopeKey: scope, ScopeLabel: "matching effects and resources in this workspace"}
}

func writeScopeField(dst *strings.Builder, name, value string) {
	fmt.Fprintf(dst, "%d:%s%d:%s", len(name), name, len(value), value)
}
