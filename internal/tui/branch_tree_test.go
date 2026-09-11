package tui

import (
	"fmt"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestBranchDepthBoundaries(t *testing.T) {
	parents := map[string]string{"a": "b", "b": "a"}
	for i := range 12 {
		parents[fmt.Sprint(i)] = fmt.Sprint(i + 1)
	}
	for _, tc := range []struct {
		name, parent string
		want         int
	}{
		{name: "root", want: 0},
		{name: "missing parent", parent: "missing", want: 1},
		{name: "cycle", parent: "a", want: 2},
		{name: "depth cap", parent: "0", want: 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := branchDepth(parents, protocol.SessionBranch{ParentID: tc.parent}); got != tc.want {
				t.Fatalf("depth=%d want %d", got, tc.want)
			}
		})
	}
}

func BenchmarkBranchSelectionCard(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := Model{branches: make([]protocol.SessionBranch, count)}
			for i := range count {
				m.branches[i] = protocol.SessionBranch{ID: fmt.Sprint(i), ParentID: "0", Name: "branch"}
			}
			m.branches[0].ParentID = ""
			b.ReportAllocs()
			for b.Loop() {
				if got := len(m.treeCard().items); got != count {
					b.Fatalf("rows=%d want %d", got, count)
				}
			}
		})
	}
}

func TestOrderBranchesPreorderAndDepth(t *testing.T) {
	branches := []protocol.SessionBranch{{ID: "c", Name: "child-2", ParentID: "b", CreatedAt: 3}, {ID: "b", Name: "child", ParentID: "main", CreatedAt: 2}, {ID: "main", Name: "main", CreatedAt: 1}}
	got := orderBranches(branches)
	if len(got) != 3 || got[0].ID != "main" || got[1].ID != "b" || got[2].ID != "c" {
		t.Fatalf("%+v", got)
	}
	if depth := branchDepth(branchParents(got), got[2]); depth != 2 {
		t.Fatalf("depth=%d", depth)
	}
}
