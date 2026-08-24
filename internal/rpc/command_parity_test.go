package rpc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestProtocolCommandInventoryHasDispatcherCase(t *testing.T) {
	known := make(map[string]bool)
	for _, command := range protocol.KnownRPCCommands() {
		known[command] = true
	}
	seen := make(map[string]bool)
	files := []struct {
		path      string
		functions map[string]bool
	}{
		{path: "server.go", functions: map[string]bool{"handle": true}},
		{path: "subagent_commands.go", functions: map[string]bool{"isSubagentCommand": true, "handleSubagentCommand": true}},
	}
	for _, source := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), source.path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || !source.functions[fn.Name.Name] || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				clause, ok := node.(*ast.CaseClause)
				if !ok {
					return true
				}
				for _, expression := range clause.List {
					literal, ok := expression.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					value, err := strconv.Unquote(literal.Value)
					if err == nil && known[value] {
						seen[value] = true
					}
				}
				return true
			})
		}
	}
	for command := range known {
		if !seen[command] {
			t.Errorf("protocol command %q has no RPC dispatcher case", command)
		}
	}
}
