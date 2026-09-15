package hostcontrol

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestHostAPIKeyLocalCapabilitiesAndNoInspectionWrites(t *testing.T) {
	service, dir, path := hostFixture(t)
	if _, err := service.InspectAPIKey(t.Context(), protocol.HostAPIKeyInspectRequest{ProviderID: "opencode-go"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing config authorized key capability")
	}
	writeHostFile(t, path, `{"providers":{"profile":{"type":"openai-compatible","base_url":"https://endpoint-canary.invalid"}}}`)
	for _, id := range []string{"opencode-go", "opencode-zen", "openai-compatible", "profile", "chatgpt"} {
		status, err := service.InspectAPIKey(t.Context(), protocol.HostAPIKeyInspectRequest{ProviderID: id})
		if err != nil {
			t.Fatal(err)
		}
		if status.ProviderID != id || !status.CheckedLocally || status.Revision != "missing" || status.APIKeySupported != (id != "chatgpt") || status.ReplaceRequired {
			t.Fatal("unexpected local capability")
		}
	}
	for _, id := range []string{"unconfigured", "UPPER", strings.Repeat("a", 65)} {
		if _, err := service.InspectAPIKey(t.Context(), protocol.HostAPIKeyInspectRequest{ProviderID: id}); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("unknown provider accepted")
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".snow"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatal("inspection created auth or lock files")
	}
}

func TestHostAPIKeySetWriteOnlyAndExplicitReplacement(t *testing.T) {
	service, _, path := hostFixture(t)
	writeHostFile(t, path, `{}`)
	request := protocol.HostAPIKeySetRequest{ProviderID: "opencode-go", ExpectedRevision: "missing", Secret: "submitted-secret-canary"}
	response, err := service.SetAPIKey(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status.State != "configured" || response.Status.Reason != "credential_present" || !response.CheckedLocally || !response.ReplaceRequired || response.AppliesTo != "future_runtime" || len(response.Revision) != 64 {
		t.Fatal("incorrect local set response")
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"submitted-secret-canary", "account", "email", "refresh", "expires", "headers", "endpoint", "summary"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("response exposed %s", forbidden)
		}
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, request), "submitted-secret-canary") {
			t.Fatal("printf-style request disclosure")
		}
	}
	inspected, err := service.InspectAPIKey(t.Context(), protocol.HostAPIKeyInspectRequest{ProviderID: "opencode-go"})
	if err != nil || inspected.Revision != response.Revision {
		t.Fatal("GET revision unstable")
	}
	request.ExpectedRevision = response.Revision
	request.Secret = "replacement-secret-canary"
	if _, err := service.SetAPIKey(t.Context(), request); !errors.Is(err, ErrReplaceConfirmationRequired) {
		t.Fatal("replacement bypassed consent")
	}
	request.ConfirmReplace = true
	replaced, err := service.SetAPIKey(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Revision == response.Revision {
		t.Fatal("replacement did not advance metadata revision")
	}
	if _, err := service.SetAPIKey(t.Context(), request); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale replacement accepted")
	}
}

func TestHostAPIKeyRejectsOAuthAndUnconfiguredProviders(t *testing.T) {
	service, dir, path := hostFixture(t)
	writeHostFile(t, path, `{}`)
	for _, id := range []string{"chatgpt", "unconfigured", "profile"} {
		_, err := service.SetAPIKey(t.Context(), protocol.HostAPIKeySetRequest{ProviderID: id, ExpectedRevision: "missing", Secret: "new-canary", ConfirmReplace: true})
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("unsupported API-key provider accepted")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".snow", "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected set created auth")
	}
}

func TestHostAPIKeyMissingOrMalformedConfigDoesNotWrite(t *testing.T) {
	service, dir, path := hostFixture(t)
	request := protocol.HostAPIKeySetRequest{ProviderID: "opencode-go", ExpectedRevision: "missing", Secret: "submitted-canary"}
	for _, data := range []string{"", `{"secret":"config-canary",`} {
		if data != "" {
			writeHostFile(t, path, data)
		}
		if _, err := service.SetAPIKey(t.Context(), request); !errors.Is(err, ErrUnavailable) {
			t.Fatal("unavailable config authorized mutation")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".snow", "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("config failure created auth")
	}
}

func TestHostAPIKeyEnvConsentAndCancellation(t *testing.T) {
	service, _, path := hostFixture(t)
	writeHostFile(t, path, `{}`)
	t.Setenv("OPENCODE_API_KEY", "environment-secret-canary")
	inspected, err := service.InspectAPIKey(t.Context(), protocol.HostAPIKeyInspectRequest{ProviderID: "opencode-go"})
	if err != nil {
		t.Fatal(err)
	}
	if !inspected.ReplaceRequired || inspected.Status.Reason != "credential_present" {
		t.Fatal("effective existing credential lacks consent")
	}
	request := protocol.HostAPIKeySetRequest{ProviderID: "opencode-go", ExpectedRevision: inspected.Revision, Secret: "submitted-canary"}
	if _, err := service.SetAPIKey(t.Context(), request); !errors.Is(err, ErrReplaceConfirmationRequired) {
		t.Fatal("environment override bypassed consent")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request.ConfirmReplace = true
	if _, err := service.SetAPIKey(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
	if _, err := service.InspectAPIKey(ctx, protocol.HostAPIKeyInspectRequest{ProviderID: "opencode-go"}); !errors.Is(err, context.Canceled) {
		t.Fatal("inspection cancellation lost")
	}
}
