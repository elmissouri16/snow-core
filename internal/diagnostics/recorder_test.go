package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestStatusJSONOmitsZeroStart(t *testing.T) {
	data, err := json.Marshal(Status{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"started_at"`) {
		t.Fatalf("zero status unexpectedly includes started_at: %s", data)
	}
}

func TestRecorderIsOptInAndRetainsDisabledCapture(t *testing.T) {
	recorder := New(false)
	defer recorder.Close()
	recorder.Record(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "ignored"})
	if _, status, err := recorder.Snapshot(context.Background()); err != nil || status.EventCount != 0 || status.Enabled {
		t.Fatalf("disabled snapshot status=%+v err=%v", status, err)
	}

	recorder.SetEnabled(true)
	recorder.Record(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "captured"})
	recorder.SetEnabled(false)
	records, status, err := recorder.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Enabled || status.EventCount != 1 || len(records) != 1 || !strings.Contains(string(records[0].Event), "captured") {
		t.Fatalf("retained snapshot records=%s status=%+v", records[0].Event, status)
	}
	if err := recorder.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Status(); got.EventCount != 0 || got.DroppedEvents != 0 {
		t.Fatalf("cleared status=%+v", got)
	}
}

func TestSanitizeJSONRemovesProviderDataAndKnownCredentials(t *testing.T) {
	input := json.RawMessage(`{"content":[{"type":"text","text":"api_key=super-secret-value auth={\"key\":\"raw-auth-key\",\"access\":\"raw-access-token\",\"refresh\":\"raw-refresh-token\"}"},{"type":"provider_data","data":"cHJpdmF0ZQ=="}],"authorization":"Bearer top-secret-token","nested":{"password":"hunter2","token":"oauth-value","x-api-key":"gateway-value","cookie":"session=cookie-value","headers":{"X-Custom-Secret":"header-value"},"env":{"SERVICE_PASSWORD":"environment-value"}},"error":"failed with unlabeled-known-secret"}`)
	output, removed, err := SanitizeJSONWithSecrets(input, []string{"unlabeled-known-secret"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if removed != 1 {
		t.Fatalf("removed=%d output=%s", removed, text)
	}
	for _, forbidden := range []string{"provider_data", "super-secret-value", "top-secret-token", "hunter2", "cHJpdmF0ZQ==", "raw-auth-key", "raw-access-token", "raw-refresh-token", "oauth-value", "gateway-value", "cookie-value", "header-value", "environment-value", "unlabeled-known-secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sanitized output contains %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, redactedValue) {
		t.Fatalf("sanitized output lacks marker: %s", text)
	}
}

func TestRedactTextRemovesKnownTokenAndPrivateKeyFormats(t *testing.T) {
	input := "JWT eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature1234\n" +
		"-----BEGIN ENCRYPTED PRIVATE KEY-----\nprivate-material\n-----END ENCRYPTED PRIVATE KEY-----"
	output := RedactText(input)
	for _, forbidden := range []string{"eyJhbGci", "private-material", "BEGIN ENCRYPTED PRIVATE KEY"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("redaction leaked %q: %s", forbidden, output)
		}
	}
}

func TestRecorderEvictsInOrderWithoutShiftingEveryEvent(t *testing.T) {
	r := New(false)
	defer r.Close()
	for i := range MaxEventRecords + 2048 {
		r.append(queuedEvent{recordedAt: time.Now(), event: protocol.AgentEvent{Type: protocol.EvTextDelta, Text: fmt.Sprintf("event-%06d", i)}})
	}
	records, status, err := r.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != MaxEventRecords || status.EventCount != MaxEventRecords || status.DroppedEvents != 2048 {
		t.Fatalf("records=%d status=%+v", len(records), status)
	}
	if records[0].Sequence != 2049 || records[len(records)-1].Sequence != MaxEventRecords+2048 {
		t.Fatalf("sequence range=%d..%d", records[0].Sequence, records[len(records)-1].Sequence)
	}
}

func TestWriteJSONContextCancellationPreservesDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dump.json")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := WriteJSONContext(ctx, path, map[string]string{"secret": "replace"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatalf("destination=%q err=%v", data, err)
	}
}

func TestWriteJSONUsesPrivateRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dump.json")
	if err := WriteJSON(path, map[string]string{"value": "ok"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), `"value": "ok"`) {
		t.Fatalf("data=%q err=%v", data, err)
	}

	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(link, map[string]string{"value": "overwrite"}); err == nil {
		t.Fatal("symlink destination unexpectedly accepted")
	}
	if data, _ := os.ReadFile(target); string(data) != "keep" {
		t.Fatalf("symlink target changed: %q", data)
	}
}
