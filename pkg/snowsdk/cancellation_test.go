package snowsdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type deadlineSDKProvider struct{ *fake.Provider }

func (p deadlineSDKProvider) Chat(ctx context.Context, _ protocol.ChatRequest) (protocol.EventStream, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestSDKPromptMethodsReturnCallerDeadline(t *testing.T) {
	for _, method := range []string{"Prompt", "PromptContent", "PromptWithMode", "PromptContentWithMode"} {
		t.Run(method, func(t *testing.T) {
			s, err := Open(t.Context(), Options{Provider: "fake", NoSession: true, PermissionMode: "deny", NoPlugins: true, NoMCP: true, NoSkills: true})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.app.Agent.SetProviderAndModel(deadlineSDKProvider{fake.New(nil)}, s.Model()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			switch method {
			case "Prompt":
				err = s.Prompt(ctx, "work")
			case "PromptContent":
				err = s.PromptContent(ctx, "work", nil)
			case "PromptWithMode":
				err = s.PromptWithMode(ctx, "work", protocol.ModeDefault)
			case "PromptContentWithMode":
				err = s.PromptContentWithMode(ctx, "work", nil, protocol.ModeDefault)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s error=%v", method, err)
			}
			messages, err := s.Messages()
			if err != nil || len(messages) != 2 || messages[1].StopReason != protocol.StopAborted {
				t.Fatalf("aborted history=%+v err=%v", messages, err)
			}
		})
	}
}
