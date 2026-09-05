package shellanalysis

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func (a *analyzer) word(word *syntax.Word, st *state) (string, bool) {
	if word == nil {
		return "", false
	}
	return a.wordParts(word.Parts, st, false)
}

func (a *analyzer) wordParts(parts []syntax.WordPart, st *state, quoted bool) (string, bool) {
	var b strings.Builder
	complete := true
	for i, part := range parts {
		value, ok := a.wordPart(part, st, quoted)
		if !quoted && i == 0 {
			if lit, literal := part.(*syntax.Lit); literal && strings.HasPrefix(lit.Value, "~") {
				switch {
				case lit.Value == "~", strings.HasPrefix(lit.Value, "~/"):
					value, ok = st.vars["HOME"]
					if ok {
						value += strings.TrimPrefix(lit.Value, "~")
					}
				default:
					ok = false // named-user lookup is runtime state
				}
			}
		}
		if !ok || b.Len()+len(value) > maxValueBytes {
			complete = false
			continue
		}
		b.WriteString(value)
	}
	if !complete {
		return "", false
	}
	return b.String(), true
}

func (a *analyzer) wordPart(part syntax.WordPart, st *state, quoted bool) (string, bool) {
	switch part := part.(type) {
	case *syntax.Lit:
		// Preserve quote context. Escapes require shell-specific decoding; do
		// not turn an unexpanded or partially decoded word into a concrete path.
		if strings.Contains(part.Value, "\\") || (!quoted && (strings.ContainsAny(part.Value, "*?") || (strings.Contains(part.Value, "[") && strings.Contains(part.Value, "]")))) {
			return "", false
		}
		return part.Value, true
	case *syntax.SglQuoted:
		if part.Dollar {
			return "", false
		}
		return part.Value, true
	case *syntax.DblQuoted:
		return a.wordParts(part.Parts, st, true)
	case *syntax.ParamExp:
		if part.Param == nil || part.Excl || part.Length || part.Index != nil || part.Slice != nil || part.Repl != nil || part.Exp != nil {
			// Inspect substitutions even in unsupported parameter operators.
			syntax.Walk(part, func(node syntax.Node) bool {
				if sub, ok := node.(*syntax.CmdSubst); ok {
					a.substitution(sub, st)
					return false
				}
				return true
			})
			return "", false
		}
		value, ok := st.vars[part.Param.Value]
		if !quoted {
			return "", false
		} // field splitting and IFS are runtime state
		return value, ok
	case *syntax.CmdSubst:
		a.substitution(part, st)
		return "", false
	default:
		return "", false
	}
}

func (a *analyzer) substitution(sub *syntax.CmdSubst, st *state) {
	copy := cloneState(st)
	if err := a.stmts(sub.Stmts, &copy); err != nil && a.err == nil {
		a.err = err
	}
}
