package snowsdk

import (
	"context"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestSessionPlanModeAPI(t *testing.T) {
	s, err := Open(context.Background(), Options{Provider: "fake", NoSession: true, PermissionMode: "allow", CollaborationMode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Mode() != protocol.ModePlan {
		t.Fatalf("mode = %q", s.Mode())
	}
	state := s.StateEvent()
	if state.Type != protocol.EvModeChanged || state.Mode == nil || state.Mode.Mode != protocol.ModePlan {
		t.Fatalf("state = %+v", state)
	}
	if err := s.PromptWithMode(context.Background(), "answer", protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	if s.Mode() != protocol.ModeDefault {
		t.Fatalf("mode after prompt = %q", s.Mode())
	}
	if err := s.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
}
