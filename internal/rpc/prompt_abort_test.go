package rpc

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRPCProviderAbortDoesNotHideCompletionError(t *testing.T) {
	a := editRPCApp(t)
	if err := a.Agent.SetProviderAndModel(fake.New([]fake.Step{{Kind: fake.StepDone, Stop: protocol.StopAborted}}), a.Agent.Model()); err != nil {
		t.Fatal(err)
	}
	ctx, outcome := agent.CapturePromptOutcome(t.Context())
	if err := a.Agent.Prompt(ctx, "abort"); err != nil || !outcome.ProviderAborted() {
		t.Fatalf("abort evidence: err=%v aborted=%t", err, outcome.ProviderAborted())
	}
	for _, terminalErr := range []error{errors.New("accounting failed"), app.ErrMessageEditOutcomeUnknown} {
		var output bytes.Buffer
		srv := New(t.Context(), a, &bytes.Buffer{}, &output)
		srv.completePrompt(Request{ID: "failed", Type: "prompt"}, ctx, make(chan struct{}), terminalErr, outcome)
		frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
		if len(frames) != 2 {
			t.Fatalf("missing failure response/completion: %s", output.Bytes())
		}
		var completed protocol.RPCPromptCompleted
		if err := json.Unmarshal(frames[1], &completed); err != nil {
			t.Fatal(err)
		}
		if completed.Status != protocol.RPCPromptFailedStatus || completed.Error != terminalErr.Error() {
			t.Fatalf("provider abort hid real failure: %+v", completed)
		}
	}
}

func TestRPCProviderAbortCompletion(t *testing.T) {
	for _, command := range []string{"prompt", "content", "mode", "content-mode", "message_edit_commit", "message_regenerate_commit"} {
		t.Run(command, func(t *testing.T) {
			a := regenerateRPCApp(t)
			aborted := fake.New([]fake.Step{{Kind: fake.StepText, Text: "partial"}, {Kind: fake.StepDone, Stop: protocol.StopAborted}})
			if err := a.Agent.SetProviderAndModel(aborted, a.Agent.Model()); err != nil {
				t.Fatal(err)
			}
			req := Request{ID: "provider-abort", Type: command}
			switch command {
			case "prompt", "content", "mode", "content-mode":
				req.Type = "prompt"
				req.Message = "abort at provider"
				if command == "content" || command == "content-mode" {
					req.Content = []protocol.ContentBlock{protocol.NewTextBlock(req.Message)}
				}
				if command == "mode" || command == "content-mode" {
					req.Mode = "plan"
				}
			case "message_edit_commit":
				prepared := editRPCPrepared(t, a)
				params, err := json.Marshal(protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"})
				if err != nil {
					t.Fatal(err)
				}
				req.Params = params
			case "message_regenerate_commit":
				messages, err := a.Agent.Messages()
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := a.PrepareMessageRegenerate(t.Context(), protocol.RPCMessageRegeneratePrepareParams{SessionID: a.Session.ID(), EntryID: messages[len(messages)-1].ID})
				if err != nil {
					t.Fatal(err)
				}
				params, err := json.Marshal(protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken})
				if err != nil {
					t.Fatal(err)
				}
				req.Params = params
			}
			var output bytes.Buffer
			srv := New(t.Context(), a, &bytes.Buffer{}, &output)
			check := func(req Request, want string) {
				t.Helper()
				output.Reset()
				if err := srv.handle(t.Context(), req); err != nil {
					t.Fatal(err)
				}
				srv.promptWG.Wait()
				frames := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
				if len(frames) != 2 {
					t.Fatalf("want acknowledgement and completion, got %s", output.Bytes())
				}
				var completed protocol.RPCPromptCompleted
				if err := json.Unmarshal(frames[1], &completed); err != nil {
					t.Fatal(err)
				}
				if completed.Type != protocol.RPCTypePromptCompleted || completed.RequestID != req.ID || string(completed.Status) != want {
					t.Errorf("completion = %+v, want %s", completed, want)
				}
				if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, frames[1])); err != nil {
					t.Fatal(err)
				}
			}
			check(req, "canceled")
			if t.Context().Err() != nil {
				t.Fatal("provider abort canceled caller context")
			}
			messages, err := a.Agent.Messages()
			if err != nil || len(messages) == 0 || messages[len(messages)-1].StopReason != protocol.StopAborted {
				t.Fatalf("aborted history = %+v, err = %v", messages, err)
			}
			// The next operation must not inherit evidence from this aborted turn.
			if err := a.Agent.SetProviderAndModel(fake.New(nil), a.Agent.Model()); err != nil {
				t.Fatal(err)
			}
			check(Request{ID: "next", Type: "prompt", Message: "continue explicitly"}, "completed")
		})
	}
}
