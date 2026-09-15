package web

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestHostAPIKeyWorkerFixture(t *testing.T) {
	if os.Getenv("SNOW_API_KEY_FIXTURE") != "1" {
		return
	}
	emit := func(value any) {
		data, err := json.Marshal(value)
		if err != nil {
			os.Exit(11)
		}
		_, _ = os.Stdout.Write(append(data, '\n'))
	}
	mode := os.Getenv("SNOW_API_KEY_FIXTURE_MODE")
	ready := protocol.NewRPCReady("fixture")
	ready.Capabilities = []string{"runtime_free_control", protocol.RPCAPIKeyControlCapability}
	if mode == "eager" {
		ready.Capabilities = []string{protocol.RPCAPIKeyControlCapability}
	}
	if mode == "unsupported" {
		ready.Capabilities = []string{"runtime_free_control"}
	}
	emit(ready)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(12)
		}
		var input protocol.HostAPIKeySetRequest
		if json.Unmarshal(request.Params, &input) != nil {
			os.Exit(13)
		}
		// Fixture records only allowlisted command metadata, NEVER the request or key.
		if log := os.Getenv("SNOW_API_KEY_FIXTURE_LOG"); log != "" {
			_ = os.WriteFile(log, []byte(request.Type), 0600)
		}
		result := apiKeyTestStatus(input.ProviderID, false)
		if request.Type == "api_key_set" {
			if input.Secret != apiKeyCanary || input.ExpectedRevision != "missing" {
				os.Exit(14)
			}
			result.ReplaceRequired = true
			result.Revision = strings.Repeat("b", 64)
			result.Status.State = "configured"
			result.Status.Reason = "credential_present"
		}
		response := protocol.RPCResponse{ID: request.ID, Type: "response", Command: request.Type, Success: true, Data: result}
		switch mode {
		case "reject":
			response.Success = false
			response.Error = apiKeyCanary
		case "extensions":
			response.Data = map[string]any{"provider_id": result.ProviderID, "api_key_supported": result.APIKeySupported, "replace_required": result.ReplaceRequired, "revision": result.Revision, "status": result.Status, "checked_locally": true, "applies_to": "future_runtime", "secret": apiKeyCanary}
		case "oversized":
			response.Data = map[string]string{"private": strings.Repeat("x", 65<<10)}
		}
		emit(response)
	}
	os.Exit(0)
}
func apiKeyTestWorker(t *testing.T, mode string) (*WorkerControl, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	log := filepath.Join(root, "commands.log")
	c := NewWorkerControl(exe, root, nil)
	c.start = func(ctx context.Context, options process.Options) (*process.Worker, error) {
		if options.Dir != root || strings.Join(options.Args, " ") != "--mode rpc --rpc-startup control" {
			t.Error("unsafe API-key worker startup")
		}
		options.Args = []string{"-test.run=^TestHostAPIKeyWorkerFixture$"}
		options.Env = append(options.Env, "SNOW_API_KEY_FIXTURE=1", "SNOW_API_KEY_FIXTURE_MODE="+mode, "SNOW_API_KEY_FIXTURE_LOG="+log)
		return process.Start(ctx, options)
	}
	return c, log
}
func TestHostAPIKeyWorkerProjectionAndCapabilities(t *testing.T) {
	for _, mode := range []string{"", "extensions", "eager", "unsupported", "reject", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			c, log := apiKeyTestWorker(t, mode)
			result, err := c.InspectAPIKey(t.Context(), "openai-compatible")
			success := mode == "" || mode == "extensions"
			if success != (err == nil) {
				t.Fatalf("inspect error class: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), apiKeyCanary) {
				t.Fatal("worker diagnostic leaked")
			}
			if success {
				result, err = c.SetAPIKey(t.Context(), protocol.HostAPIKeySetRequest{ProviderID: "openai-compatible", ExpectedRevision: "missing", Secret: apiKeyCanary})
				if err != nil || result.Status.State != "configured" {
					t.Fatal("write-only worker failed")
				}
				encoded, err := json.Marshal(result)
				if err != nil || strings.Contains(string(encoded), apiKeyCanary) || strings.Contains(string(encoded), "\"secret\"") {
					t.Fatal("secret projection leaked")
				}
				command, err := os.ReadFile(log)
				if err != nil || string(command) != "api_key_set" {
					t.Fatal("unexpected fixture command record")
				}
			}
			if len(c.slots) != 0 {
				t.Fatal("idle API-key worker retained")
			}
		})
	}
}
func TestHostAPIKeySharesWriteGateAndBounds(t *testing.T) {
	c := NewWorkerControl("/operator/snow", "/operator/manager", nil)
	entered := make(chan time.Duration, 1)
	release := make(chan struct{})
	c.start = func(ctx context.Context, _ process.Options) (*process.Worker, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("missing write deadline")
		}
		entered <- time.Until(deadline)
		<-release
		return nil, errors.New("fixture")
	}
	finished := make(chan struct{})
	go func() {
		_, _ = c.SetAPIKey(t.Context(), protocol.HostAPIKeySetRequest{ProviderID: "openai-compatible", ExpectedRevision: "missing", Secret: apiKeyCanary})
		close(finished)
	}()
	if remaining := <-entered; remaining <= 3*time.Second || remaining > 5*time.Second {
		t.Errorf("write deadline %v", remaining)
	}
	_, err := c.UpdateDefaults(t.Context(), "global", "", protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: strings.Repeat("a", 64), Global: &protocol.HostGlobalDefaultsPatch{Thinking: &protocol.HostStringOperation{Op: "reset"}}})
	if !errors.Is(err, ErrHostControlBusy) {
		t.Fatal("defaults write bypassed API-key serialization")
	}
	close(release)
	<-finished
	c.start = func(context.Context, process.Options) (*process.Worker, error) {
		t.Error("invalid key started a worker")
		return nil, ErrHostControlUnavailable
	}
	for _, secret := range []string{"", strings.Repeat("x", 4097), "key\ncontrol", string([]byte{0xff}), " padded "} {
		_, err := c.SetAPIKey(t.Context(), protocol.HostAPIKeySetRequest{ProviderID: "openai-compatible", ExpectedRevision: "missing", Secret: secret})
		if !errors.Is(err, ErrHostControlInvalid) {
			t.Fatal("invalid key reached worker")
		}
	}
	if err := c.call(t.Context(), "project", "project-id", "api_key_set", struct{}{}, nil); !errors.Is(err, ErrHostControlInvalid) {
		t.Fatal("project-scoped API-key worker admitted")
	}
}
