package shellanalysis

import "strings"

func (a *analyzer) find(args []string, st *state) {
	a.addUnknown("find traversal and expression effects depend on runtime state")
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
