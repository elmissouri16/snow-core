package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"net"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Reuse the real client/drain pattern without changing shared process fixtures.
// The peer exposes exact JSONL requests and permits controlled ACK/completion
// ordering, including a caller disconnect after admission.
func composerRuntimePeer(t *testing.T, capable bool) (*RuntimeManager, *liveRuntime, net.Conn, *bufio.Reader) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	local, peer := net.Pipe()
	if err := peer.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ready := protocol.NewRPCReady("composer-test")
	ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool {
		return c == "goal_run" || c == "usage" || c == "context_report"
	})
	if !capable {
		ready.Capabilities = slices.DeleteFunc(ready.Capabilities, func(c string) bool { return c == "multimodal_prompts" })
	}
	sent := make(chan error, 1)
	go func() {
		data, err := json.Marshal(ready)
		if err == nil {
			_, err = peer.Write(append(data, '\n'))
		}
		sent <- err
	}()
	client, err := clientrpc.New(ctx, local, clientrpc.Options{})
	if err != nil {
		cancel()
		_ = peer.Close()
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	registry := registryTestOpen(t, filepath.Join(t.TempDir(), "manager"))
	project, err := registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &liveRuntime{ctx: ctx, cancel: cancel, project: project, instanceID: "instance", worker: &process.Worker{Client: client}, drained: make(chan struct{}), assistant: -1, plan: -1, snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: "instance", SessionID: "session", Status: "idle"}}
	m := &RuntimeManager{workers: map[string]*liveRuntime{project.ID: r}}
	go r.drain()
	t.Cleanup(func() {
		cancel()
		_ = peer.Close()
		_ = client.Close()
		select {
		case <-r.drained:
		case <-time.After(5 * time.Second):
			t.Error("composer drain did not stop")
		}
	})
	return m, r, peer, bufio.NewReader(peer)
}

func composerReadRequest(t *testing.T, input *bufio.Reader) protocol.RPCRequest {
	t.Helper()
	line, err := input.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if len(line) >= 4<<20 {
		t.Fatalf("request exceeds worker encoded cap: %d", len(line))
	}
	var request protocol.RPCRequest
	if err := json.Unmarshal(line, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestComposerPromptExactRPCAndCompletion(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "ack first", true: "completion first"}[early], func(t *testing.T) {
			m, r, peer, input := composerRuntimePeer(t, true)
			data := composerTestPNG(t)
			var content []protocol.ContentBlock
			for range composerMaxImages {
				content = append(content,
					protocol.ContentBlock{Type: protocol.BlockText, Text: "Attachment: $name.png\nexact attachment label"},
					protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: data},
				)
			}
			text := "Inspect attached images"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- m.PromptContent(ctx, r.project.ID, r.instanceID, text, content) }()
			req := composerReadRequest(t, input)
			if req.ID == "" || req.Type != "prompt" || req.Message != text || !reflect.DeepEqual(req.Content, content) || len(req.Params) != 0 {
				t.Fatalf("RPC forwarding changed: %+v", req)
			}
			if strings.Contains(req.Message, "$name") || !strings.Contains(req.Content[0].Text, "$name") {
				t.Fatal("attachment text was promoted into the skill-activating message")
			}
			cancel() // An admitted worker mutation outlives the HTTP request.
			if early {
				runtimeActivityEmit(t, peer, protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: req.ID, Status: protocol.RPCPromptCompletedStatus})
			}
			runtimeActivityEmit(t, peer, protocol.RPCResponse{Type: "response", ID: req.ID, Command: "prompt", Success: true})
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			if !early {
				snapshot, _ := m.Snapshot(r.project.ID)
				if snapshot.Status != "running" || snapshot.Recovery.State != RecoveryAdmitted {
					t.Fatalf("ack ended turn: %+v", snapshot)
				}
				if err := m.PromptContent(t.Context(), r.project.ID, r.instanceID, text, content); !errors.Is(err, ErrRuntimeBusy) {
					t.Fatalf("overlap = %v", err)
				}
				runtimeActivityEmit(t, peer, protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: req.ID, Status: protocol.RPCPromptCompletedStatus})
			}
			snapshot := runtimeWait(t, m, r.project.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && s.Recovery.State == RecoveryCompleted })
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			imageJSON, _ := json.Marshal(data)
			if bytes.Contains(encoded, imageJSON) || bytes.Contains(encoded, []byte(`"data"`)) || len(snapshot.Messages) != 1 || snapshot.Messages[0].Text != text {
				t.Fatalf("unsafe transcript projection: %s", encoded)
			}
		})
	}
}

func TestComposerPromptAdmissionFences(t *testing.T) {
	for _, gate := range []string{"capability", "stale instance", "busy", "cancel", "goal pending", "goal active", "queue", "control", "recovery"} {
		t.Run(gate, func(t *testing.T) {
			m, r, _, _ := composerRuntimePeer(t, gate != "capability")
			instance := r.instanceID
			want := ErrRuntimeBusy
			r.mu.Lock()
			switch gate {
			case "capability":
				want = ErrRuntimeInvalid
			case "stale instance":
				instance = "old"
				want = ErrRuntimeInvalid
			case "busy":
				r.busy = true
			case "cancel":
				r.snapshot.CancelRequested = true
			case "goal pending":
				r.goal.pending = true
			case "goal active":
				r.goal.active = true
			case "queue":
				r.snapshot.Queue = &RuntimeQueue{Items: []RuntimeQueueItem{{ID: "held", Text: "retain", State: "held"}}}
				want = ErrRuntimeQueueReview
			case "control":
				r.control.Lock()
				defer r.control.Unlock()
			case "recovery":
				r.recoveryStore = composerFailRecovery{}
				want = ErrRecoveryStorage
			}
			before := r.snapshot.clone()
			r.mu.Unlock()
			err := m.PromptContent(t.Context(), r.project.ID, instance, "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "attachment"}})
			if !errors.Is(err, want) {
				t.Fatalf("gate: got %v, want %v", err, want)
			}
			r.mu.Lock()
			after := r.snapshot.clone()
			r.mu.Unlock()
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("gate mutated public state: before=%+v after=%+v", before, after)
			}
		})
	}
}

type composerFailRecovery struct{}

func (composerFailRecovery) LoadRecovery(context.Context, string) (RecoveryHint, bool, error) {
	return RecoveryHint{}, false, nil
}
func (composerFailRecovery) SaveRecovery(context.Context, string, RecoveryHint) error {
	return errors.New("write failed")
}

func TestComposerPromptRejectedAndLegacyUnaffected(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "content rejection", true: "legacy no capability"}[legacy], func(t *testing.T) {
			m, r, peer, input := composerRuntimePeer(t, !legacy)
			result := make(chan error, 1)
			go func() {
				if legacy {
					result <- m.Prompt(t.Context(), r.project.ID, r.instanceID, "legacy")
					return
				}
				result <- m.PromptContent(t.Context(), r.project.ID, r.instanceID, "", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: composerTestPNG(t)}})
			}()
			req := composerReadRequest(t, input)
			if legacy && (req.Message != "legacy" || len(req.Content) != 0) {
				t.Fatalf("legacy RPC changed: %+v", req)
			}
			if !legacy {
				snapshot, _ := m.Snapshot(r.project.ID)
				if snapshot.Messages[0].Text != "[Image attachment]" {
					t.Fatal("missing safe image-only summary")
				}
			}
			runtimeActivityEmit(t, peer, protocol.RPCResponse{Type: "response", ID: req.ID, Command: "prompt", Success: legacy})
			err := <-result
			if legacy {
				if err != nil {
					t.Fatal(err)
				}
				for _, invalid := range []string{"", " ", "bad\x00", "\xff", strings.Repeat("x", composerMaxPrompt+1)} {
					if err := m.Prompt(t.Context(), r.project.ID, r.instanceID, invalid); !errors.Is(err, ErrRuntimeInvalid) {
						t.Fatalf("legacy validation changed: %v", err)
					}
				}
			} else {
				if !errors.Is(err, ErrRuntimeInvalid) {
					t.Fatalf("rejection: %v", err)
				}
				snapshot, _ := m.Snapshot(r.project.ID)
				if len(snapshot.Messages) != 0 || snapshot.Status != "idle" || snapshot.Recovery.State != RecoveryRejected {
					t.Fatalf("rejection not rolled back: %+v", snapshot)
				}
			}
		})
	}
}

func TestComposerPromptEncodedWorkerBudget(t *testing.T) {
	// Worst-case JSON escaping (six bytes per text byte) plus base64 images
	// stays below the worker's 4MiB JSONL frame cap, including request metadata.
	data := composerTestPNG(t)
	data = append(data, make([]byte, composerMaxImageBytes-len(data))...)
	text := strings.Repeat("\x01", composerMaxPrompt)
	content := []protocol.ContentBlock{{Type: protocol.BlockText, Text: strings.Repeat("\x01", composerMaxText-composerMaxPrompt)}, {Type: protocol.BlockImage, MIMEType: "image/png", Data: data}}
	if err := validateComposerContent(text, content); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(protocol.RPCRequest{ID: "request-123456789", Type: "prompt", Message: text, Content: content})
	if err != nil || len(wire)+1 >= 4<<20 {
		t.Fatalf("unsafe encoded budget: %d %v", len(wire), err)
	}
}
