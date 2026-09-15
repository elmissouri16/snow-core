package permission

import (
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
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

func TestPublicRequestEffectFieldBounds(t *testing.T) {
	fields := []struct {
		name   string
		limit  int
		effect func(string) Effect
		value  func(protocol.PermissionEffect) string
	}{
		{"type", maxPublicFieldRunes, func(v string) Effect { return Effect{Type: v} }, func(e protocol.PermissionEffect) string { return e.Type }},
		{"capability", maxPublicFieldRunes, func(v string) Effect { return Effect{Capability: Capability(v)} }, func(e protocol.PermissionEffect) string { return e.Capability }},
		{"operation", maxPublicFieldRunes, func(v string) Effect { return Effect{Operation: v} }, func(e protocol.PermissionEffect) string { return e.Operation }},
		{"resource", maxPublicReasonRunes, func(v string) Effect { return Effect{Resource: v} }, func(e protocol.PermissionEffect) string { return e.Resource }},
		{"command", maxPublicFieldRunes, func(v string) Effect { return Effect{Command: v} }, func(e protocol.PermissionEffect) string { return e.Command }},
		{"reason", maxPublicReasonRunes, func(v string) Effect { return Effect{Reason: v} }, func(e protocol.PermissionEffect) string { return e.Reason }},
		{"confidence", maxPublicFieldRunes, func(v string) Effect { return Effect{Confidence: v} }, func(e protocol.PermissionEffect) string { return e.Confidence }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			for _, character := range []string{"x", "界"} {
				t.Run(character, func(t *testing.T) {
					for _, size := range []struct {
						name      string
						count     int
						truncated bool
					}{
						{"empty", 0, false},
						{"below", field.limit - 1, false},
						{"exact", field.limit, false},
						{"over", field.limit + 1, true},
					} {
						t.Run(size.name, func(t *testing.T) {
							input := strings.Repeat(character, size.count)
							effect := field.effect(input)
							effect.Dynamic = true
							// A later complete effect must not clear an earlier truncation.
							got := PublicRequest(Request{Effects: []Effect{effect, {Type: "x"}}})
							if got.EffectsTruncated != size.truncated {
								t.Errorf("EffectsTruncated = %v, want %v", got.EffectsTruncated, size.truncated)
							}
							if len(got.Effects) != 2 {
								t.Fatal("effect count changed")
							}
							want := input
							if size.truncated {
								want = strings.Repeat(character, field.limit) + "…"
							}
							if field.value(got.Effects[0]) != want {
								t.Error("effect field did not preserve the expected rune bound")
							}
							if !got.Effects[0].Dynamic || got.Effects[1].Type != "x" {
								t.Error("unrelated effect fields changed")
							}
							if got.CapabilitiesTruncated || got.PathsTruncated {
								t.Error("unrelated truncation flags changed")
							}
						})
					}
				})
			}
		})
	}
}
