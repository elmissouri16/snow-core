package shellanalysis

import (
	"fmt"

	"mvdan.cc/sh/v3/syntax"
)

// Join retains only values established on every possible execution path.
func joinState(dst *state, other state) {
	if dst.cwd != other.cwd {
		dst.cwd = ""
	}
	for key, value := range dst.vars {
		if next, ok := other.vars[key]; !ok || value != next {
			delete(dst.vars, key)
		}
	}
}

func (a *analyzer) command(cmd syntax.Command, st *state) error {
	switch cmd := cmd.(type) {
	case nil:
		return nil
	case *syntax.CallExpr:
		return a.call(cmd, st)
	case *syntax.BinaryCmd:
		if cmd.Op == syntax.Pipe || cmd.Op == syntax.PipeAll {
			left, right := cloneState(st), cloneState(st)
			if err := a.stmts([]*syntax.Stmt{cmd.X}, &left); err != nil {
				return err
			}
			if err := a.stmts([]*syntax.Stmt{cmd.Y}, &right); err != nil {
				return err
			}
			// Some shells can run the final pipeline stage in the parent.
			joinState(st, right)
			return nil
		}
		if err := a.stmts([]*syntax.Stmt{cmd.X}, st); err != nil {
			return err
		}
		right := cloneState(st)
		if err := a.stmts([]*syntax.Stmt{cmd.Y}, &right); err != nil {
			return err
		}
		joinState(st, right)
		return nil
	case *syntax.Subshell:
		copy := cloneState(st)
		return a.stmts(cmd.Stmts, &copy)
	case *syntax.Block:
		return a.stmts(cmd.Stmts, st)
	case *syntax.IfClause:
		return a.conditional(cmd, st)
	case *syntax.WhileClause:
		// An arbitrary iteration count makes a single traversal unsound. Drop
		// constants before inspecting visible effects, and never memoize it.
		a.addUnknown("loop state depends on runtime iterations")
		st.cwd = ""
		clear(st.vars)
		if err := a.stmts(cmd.Cond, st); err != nil {
			return err
		}
		if err := a.stmts(cmd.Do, st); err != nil {
			return err
		}
		st.cwd = ""
		clear(st.vars)
		return nil
	case *syntax.ForClause:
		if iter, ok := cmd.Loop.(*syntax.WordIter); ok {
			for _, word := range iter.Items {
				_, _ = a.word(word, st)
			}
		}
		a.addUnknown("loop state depends on runtime iterations")
		st.cwd = ""
		clear(st.vars)
		if err := a.stmts(cmd.Do, st); err != nil {
			return err
		}
		st.cwd = ""
		clear(st.vars)
		return nil
	default:
		a.addIncomplete(fmt.Sprintf("unsupported shell construct %T", cmd))
		return nil
	}
}

func (a *analyzer) conditional(cmd *syntax.IfClause, st *state) error {
	if err := a.stmts(cmd.Cond, st); err != nil {
		return err
	}
	thenState, elseState := cloneState(st), cloneState(st)
	if err := a.stmts(cmd.Then, &thenState); err != nil {
		return err
	}
	if cmd.Else != nil {
		if err := a.conditional(cmd.Else, &elseState); err != nil {
			return err
		}
	}
	joinState(&thenState, elseState)
	*st = thenState
	return nil
}
