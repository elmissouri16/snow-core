package permission

import (
	"strings"
	"testing"
)

func TestPublicRequestProjectsAndBoundsAnalysis(t *testing.T) {
	long := strings.Repeat("x", maxPublicReasonRunes+50)
	paths := make([]string, maxPublicPaths+1)
	effects := make([]Effect, maxPublicEffects+1)
	capabilities := make([]Capability, maxPublicCapabilities+1)
	for i := range paths {
		paths[i] = long
	}
	for i := range effects {
		effects[i] = Effect{Type: "filesystem", Capability: CapabilityFilesystemReadExternal, Operation: "read", Resource: long, Confidence: "high"}
	}
	for i := range capabilities {
		capabilities[i] = CapabilityFilesystemReadExternal
	}
	req := Request{
		Tool: "bash", Paths: paths, Risk: RiskExec, Reason: long, Effects: effects,
		Capabilities: capabilities, Unknown: true, Rememberable: false,
		ScopeKey: "private-hash", ScopeLabel: "dynamic effects",
	}
	got := PublicRequest(req)
	if got.Tool != "bash" || len(got.Effects) != maxPublicEffects || len(got.Capabilities) != maxPublicCapabilities || !got.Unknown || got.Rememberable {
		t.Fatalf("public request = %+v", got)
	}
	if !got.EffectsTruncated || !got.CapabilitiesTruncated || !got.PathsTruncated {
		t.Fatalf("truncation flags missing: %+v", got)
	}
	if len([]rune(got.Reason)) > maxPublicReasonRunes+1 || len([]rune(got.Paths[0])) > maxPublicReasonRunes+1 || len([]rune(got.Effects[0].Resource)) > maxPublicReasonRunes+1 {
		t.Fatalf("public fields were not bounded: %+v", got)
	}
}
