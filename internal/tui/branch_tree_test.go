package tui

import (
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestOrderBranchesPreorderAndDepth(t *testing.T) {
	branches := []protocol.SessionBranch{{ID: "c", Name: "child-2", ParentID: "b", CreatedAt: 3}, {ID: "b", Name: "child", ParentID: "main", CreatedAt: 2}, {ID: "main", Name: "main", CreatedAt: 1}}
	got := orderBranches(branches)
	if len(got) != 3 || got[0].ID != "main" || got[1].ID != "b" || got[2].ID != "c" {
		t.Fatalf("%+v", got)
	}
	if depth := branchDepth(got, got[2]); depth != 2 {
		t.Fatalf("depth=%d", depth)
	}
}
