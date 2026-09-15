package session

import (
	"reflect"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCatalogIncludesPublicPlansWithoutPrivateBlocks(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	message := protocol.Message{ID: "plan-answer", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "Proposal follows"},
		{Type: protocol.BlockPlan, Text: "# A safe plan\n1. Inspect the code", PlanComplete: true},
		{Type: protocol.BlockThinking, Text: "private reasoning"},
		{Type: protocol.BlockProviderData, Text: "private continuity"},
	}}
	id, _ := catalogFixture(t, root, cwd, "Planning", message)
	before := catalogSnapshot(t, root)
	page, err := NewCatalog(root, cwd).Messages(t.Context(), id, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].Text != "Proposal follows\n# A safe plan\n1. Inspect the code" {
		t.Fatalf("public plan projection: %+v", page)
	}
	if !reflect.DeepEqual(before, catalogSnapshot(t, root)) {
		t.Fatal("catalog plan read changed session files")
	}
}
