package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider/fake"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestMailboxFIFOAndProviderDelivery(t *testing.T) {
	prov := fake.NewRecorded()
	st := session.NewMemoryStore(session.Options{})
	a, err := New(Options{Provider: prov, Registry: tools.NewRegistry(), Session: st, Permission: permission.NewService(permission.ModeDeny, nil), Model: protocol.Model{Provider: "fake", ID: "fake-1", SupportsTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for i := 0; i < 3; i++ {
		msg := protocol.AgentMessage{ID: fmt.Sprintf("m%d", i), Author: "/root/c", Recipient: "/root", Kind: protocol.AgentMessageNormal, Content: fmt.Sprintf("mail-%d", i), CreatedAt: time.Now().UnixMilli()}
		if err := a.EnqueueMailbox(msg); err != nil {
			t.Fatal(err)
		}
	}
	msgs, _ := a.Messages()
	if len(msgs) != 3 {
		t.Fatalf("persisted=%d", len(msgs))
	}
	for i, m := range msgs {
		if m.Role != protocol.RoleAgent || m.ID != fmt.Sprintf("m%d", i) {
			t.Fatalf("fifo[%d]=%+v", i, m)
		}
	}
	if !a.PendingMailbox() {
		t.Fatal("idle persisted mail must remain unread")
	}
	if err := a.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if a.PendingMailbox() {
		t.Fatal("provider delivery did not acknowledge mail")
	}
	calls := prov.RecordedCalls()
	if len(calls) != 1 {
		t.Fatalf("calls=%d", len(calls))
	}
	for i := 0; i < 3; i++ {
		if calls[0].Messages[i].Role != protocol.RoleAgent {
			t.Fatalf("request[%d]=%s", i, calls[0].Messages[i].Role)
		}
	}
}

func TestMailboxConcurrentProducersLinearize(t *testing.T) {
	prov := fake.NewRecorded()
	st := session.NewMemoryStore(session.Options{})
	a, err := New(Options{Provider: prov, Registry: tools.NewRegistry(), Session: st, Permission: permission.NewService(permission.ModeDeny, nil), Model: protocol.Model{Provider: "fake", ID: "fake-1", SupportsTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if err := a.EnqueueMailbox(protocol.AgentMessage{ID: fmt.Sprintf("c%02d", i), Author: "/root/c", Recipient: "/root", Kind: protocol.AgentMessageNormal, Content: "x", CreatedAt: time.Now().UnixMilli()}); err != nil {
				t.Errorf("enqueue: %v", err)
			}
		}(i)
	}
	wg.Wait()
	msgs, err := a.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != n {
		t.Fatalf("messages=%d", len(msgs))
	}
	seen := map[string]bool{}
	parent := "root"
	for _, m := range msgs {
		if seen[m.ID] {
			t.Fatalf("duplicate %s", m.ID)
		}
		seen[m.ID] = true
		if m.ParentID != parent {
			t.Fatalf("nonlinear parent %s want %s", m.ParentID, parent)
		}
		parent = m.ID
	}
}
