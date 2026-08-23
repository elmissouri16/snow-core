package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderRetryEventJSONAndCloneIsolation(t *testing.T) {
	event := AgentEvent{Type: EvProviderRetry, ProviderRetry: &ProviderRetry{
		Provider: "test", Kind: "transient", Phase: "recovery", Attempt: 3, MaxAttempts: 12,
		DelayMS: 1500, ElapsedMS: 5000, MaxElapsedMS: 300000,
	}}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"type":"provider_retry"`, `"provider_retry"`, `"phase":"recovery"`, `"attempt":3`, `"delay_ms":1500`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("missing %s in %s", want, encoded)
		}
	}
	clone := event.Clone()
	clone.ProviderRetry.Attempt = 9
	if event.ProviderRetry.Attempt != 3 {
		t.Fatalf("clone mutated source: %+v", event.ProviderRetry)
	}
}
