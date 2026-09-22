package rpc

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSessionSetModelPersistsConversationWithoutChangingOperatorConfig(t *testing.T) {
	a, calls := discoveryTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"inactive-model","supports_thinking":true,"thinking_levels":["high"]}]}`)
	})
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	if err := srv.handle(t.Context(), Request{Type: "models_discover"}); err != nil {
		t.Fatal(err)
	}
	beforeFile, err := os.ReadFile(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeStat, err := os.Stat(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	beforePersisted, err := json.Marshal(a.PersistedCfg, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	beforeEffective, err := json.Marshal(a.Cfg, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	beforeSession, _, err := a.Agent.SessionIdentity()
	if err != nil {
		t.Fatal(err)
	}

	output.Reset()
	req := Request{ID: "select", Type: "session_set_model", Provider: "inactive", Model: "inactive-model"}
	if err := srv.handle(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, output.Bytes())); err != nil {
		t.Fatal(err)
	}
	var ack protocol.RPCResponse
	if err := json.Unmarshal(output.Bytes(), &ack); err != nil {
		t.Fatal(err)
	}
	if !ack.Success || ack.Command != "session_set_model" || ack.Data != nil {
		t.Fatalf("ack=%+v", ack)
	}
	provider, model, models := a.ActiveModelsSnapshot()
	if provider != "inactive" || model.ID != "inactive-model" || a.Agent.Model().ID != model.ID || a.Agent.Model().Provider != provider || len(models) != 1 {
		t.Fatalf("live selection not changed: provider=%s model=%+v", provider, model)
	}
	if calls.Load() != 1 {
		t.Fatalf("selection unexpectedly repeated cached discovery: calls=%d", calls.Load())
	}
	metadata := a.Session.(session.MetadataStore)
	raw, found, err := metadata.Metadata(sessionModelMetadataKey)
	if err != nil || !found {
		t.Fatalf("conversation selection metadata missing: %q %v %v", raw, found, err)
	}
	var saved savedSessionModel
	if err := json.Unmarshal([]byte(raw), &saved, json.RejectUnknownMembers(true)); err != nil || saved != (savedSessionModel{Version: 1, Provider: "inactive", Model: "inactive-model", Thinking: "off"}) {
		t.Fatalf("saved conversation selection = %+v, raw=%q, err=%v", saved, raw, err)
	}
	// Current supported effort is preserved without an extra request parameter.
	if err := a.Agent.SetThinking(protocol.ThinkingHigh); err != nil {
		t.Fatal(err)
	}
	if err := srv.handle(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if a.Agent.Thinking() != protocol.ThinkingHigh {
		t.Fatal("selection reset supported effort")
	}
	output.Reset()
	if err := srv.handle(t.Context(), Request{Type: "session_info"}); err != nil {
		t.Fatal(err)
	}
	var info struct {
		Data protocol.RPCSessionInfo `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Data.SessionID != beforeSession || info.Data.Provider != "inactive" || info.Data.Model != "inactive-model" || info.Data.Thinking != protocol.ThinkingHigh {
		t.Fatalf("current session metadata=%+v", info.Data)
	}
	activeModel, err := a.ResolveProviderModel(t.Context(), "openai-compatible", "active-model")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetProviderModelThinkingContext(t.Context(), "openai-compatible", activeModel, protocol.ThinkingOff); err != nil {
		t.Fatal(err)
	}
	if err := srv.restoreSessionModel(t.Context()); err != nil {
		t.Fatal(err)
	}
	provider, model, _ = a.ActiveModelsSnapshot()
	if provider != "inactive" || model.ID != "inactive-model" || a.Agent.Thinking() != protocol.ThinkingHigh || calls.Load() != 1 {
		t.Fatalf("saved conversation selection not restored: provider=%q model=%+v thinking=%q calls=%d", provider, model, a.Agent.Thinking(), calls.Load())
	}
	// Switching back to a model that cannot think safely resets effort to off.
	if err := srv.handle(t.Context(), Request{Type: "session_set_model", Provider: "openai-compatible", Model: "active-model"}); err != nil {
		t.Fatal(err)
	}
	if a.Agent.Thinking() != protocol.ThinkingOff {
		t.Fatal("unsupported effort retained")
	}

	afterFile, err := os.ReadFile(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	afterStat, err := os.Stat(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	afterPersisted, err := json.Marshal(a.PersistedCfg, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	afterEffective, err := json.Marshal(a.Cfg, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeFile, afterFile) || !beforeStat.ModTime().Equal(afterStat.ModTime()) || !os.SameFile(beforeStat, afterStat) {
		t.Fatal("session-only model selection wrote/replaced host config")
	}
	if !bytes.Equal(beforePersisted, afterPersisted) || !bytes.Equal(beforeEffective, afterEffective) {
		t.Fatal("session-only model selection changed persisted/effective config or project selection maps")
	}
}

func TestSessionModelRestoresAfterAppRestart(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(t.TempDir(), "sessions"))
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/active/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"active-model"}]}`)
		case "/saved/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"saved-model"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()
	configPath := filepath.Join(t.TempDir(), "config.json")
	config := fmt.Sprintf(`{"providers":{"openai-compatible":{"base_url":%q},"saved":{"type":"openai-compatible","base_url":%q}}}`, remote.URL+"/active", remote.URL+"/saved")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	open := func() *app.App {
		a, err := app.New(t.Context(), app.Options{Provider: "openai-compatible", ConfigPath: configPath, Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true, CWD: cwd})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	first := open()
	var output bytes.Buffer
	server := New(t.Context(), first, strings.NewReader(""), &output)
	if err := server.handle(t.Context(), Request{Type: "models_discover"}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := server.handle(t.Context(), Request{Type: "session_set_model", Provider: "saved", Model: "saved-model"}); err != nil {
		t.Fatal(err)
	}
	sessionID := first.Session.ID()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := open()
	defer second.Close()
	if provider, model, _ := second.ActiveModelsSnapshot(); provider != "openai-compatible" || model.ID != "active-model" {
		t.Fatalf("restart did not begin from host default: provider=%q model=%q", provider, model.ID)
	}
	output.Reset()
	server = New(t.Context(), second, strings.NewReader(""), &output)
	params, err := json.Marshal(struct {
		SessionID string `json:"session_id"`
	}{sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.handle(t.Context(), Request{Type: "session_open", Params: params}); err != nil {
		t.Fatal(err)
	}
	provider, model, _ := second.ActiveModelsSnapshot()
	if second.Session.ID() != sessionID || provider != "saved" || model.ID != "saved-model" {
		t.Fatalf("reopened conversation selection = session:%q provider:%q model:%q", second.Session.ID(), provider, model.ID)
	}
}

func TestSessionSetModelRejectsUnknownPairsWithoutDiscovery(t *testing.T) {
	a, calls := discoveryTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid selection discovered inactive provider")
	})
	before := a.Agent.Model()
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	for _, req := range []Request{
		{Type: "session_set_model"},
		{Type: "session_set_model", Model: "active-model"},
		{Type: "session_set_model", Provider: "openai-compatible"},
		{Type: "session_set_model", Provider: "inactive", Model: "inactive-model"},
		{Type: "session_set_model", Provider: "openai-compatible", Model: "custom-unknown"},
		{Type: "session_set_model", Provider: "unknown", Model: "active-model"},
		{Type: "session_set_model", Provider: "openai-compatible", Model: "active-model", Thinking: "high"},
		{Type: "session_set_model", Provider: "openai-compatible", Model: "active-model", Params: []byte(`{"supports_tools":true}`)},
		{Type: "session_set_model", Provider: "openai-compatible", Model: strings.Repeat("m", 513)},
	} {
		err := srv.handle(t.Context(), req)
		if err == nil || rpcErrorCode(err) != "invalid" {
			t.Fatalf("invalid request accepted: %+v err=%v", req, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := srv.handle(ctx, Request{Type: "session_set_model", Provider: "openai-compatible", Model: "active-model"}); err == nil || rpcErrorCode(err) != "canceled" {
		t.Fatalf("canceled selection err=%v", err)
	}
	if calls.Load() != 0 || a.Agent.Model().ID != before.ID || a.Agent.Model().Provider != before.Provider || output.Len() != 0 {
		t.Fatal("invalid selection changed state or performed discovery")
	}
}

func TestSessionSetModelRejectsBusySession(t *testing.T) {
	a, _ := discoveryTestApp(t, func(w http.ResponseWriter, r *http.Request) { t.Error("busy selection performed discovery") })
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	req := Request{Type: "session_set_model", Provider: "openai-compatible", Model: "active-model"}
	// Cover the admitted RPC prompt window before the agent goroutine starts.
	srv.promptDone = make(chan struct{})
	if err := srv.handle(t.Context(), req); err == nil || rpcErrorCode(err) != "session_busy" {
		t.Fatalf("admitted prompt selection err=%v", err)
	}
	srv.promptDone = nil
	provider := &rpcQueueProvider{started: make(chan struct{}), release: make(chan struct{})}
	model := a.Agent.Model()
	model.Provider = provider.ID()
	if err := a.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Agent.Prompt(ctx, "busy") }()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("prompt did not start")
	}
	if err := srv.handle(t.Context(), req); err == nil || rpcErrorCode(err) != "session_busy" {
		t.Fatalf("running prompt selection err=%v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("prompt did not cancel")
	}
	if a.Agent.Model().Provider != provider.ID() {
		t.Fatal("busy selection changed agent")
	}
}

func TestSessionModelThinkingFallback(t *testing.T) {
	model := protocol.Model{SupportsThinking: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh}, DefaultThinking: protocol.ThinkingLow}
	for _, test := range []struct{ current, want protocol.ThinkingLevel }{
		{protocol.ThinkingHigh, protocol.ThinkingHigh},
		{protocol.ThinkingOff, protocol.ThinkingOff},
		{"", protocol.ThinkingOff},
		{protocol.ThinkingUltra, protocol.ThinkingLow},
	} {
		if got := sessionModelThinking(model, test.current); got != test.want {
			t.Fatalf("current=%s got=%s want=%s", test.current, got, test.want)
		}
	}
	model.DefaultThinking = "invalid"
	if got := sessionModelThinking(model, protocol.ThinkingUltra); got != protocol.ThinkingOff {
		t.Fatalf("invalid default survived: %s", got)
	}
	if !slices.Contains(protocol.KnownRPCCapabilities(), "session_model_selection") || !slices.Contains(protocol.KnownRPCCommands(), "session_set_model") {
		t.Fatal("session-only selection capability/command missing")
	}
}
