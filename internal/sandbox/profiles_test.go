package sandbox

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
)

func TestBuiltinProfilesAreUniqueDigestPinnedAndNetworked(t *testing.T) {
	profiles := Profiles()
	if len(profiles) != 4 {
		t.Fatalf("profile count = %d", len(profiles))
	}
	seen := map[string]bool{}
	for _, profile := range profiles {
		if profile.ID == "" || seen[profile.ID] {
			t.Fatalf("duplicate/empty profile ID %q", profile.ID)
		}
		seen[profile.ID] = true
		if !profile.Network {
			t.Fatalf("profile %s did not enable network", profile.ID)
		}
		ref, err := name.ParseReference(profile.Source, name.StrictValidation)
		if err != nil {
			t.Fatalf("profile %s source: %v", profile.ID, err)
		}
		if _, ok := ref.(name.Digest); !ok {
			t.Fatalf("profile %s source is not digest-pinned: %s", profile.ID, profile.Source)
		}
	}
	goProfile, ok := FindProfile("go")
	if !ok || goProfile.CPUs != 4 || goProfile.MemoryMiB != 6144 {
		t.Fatalf("Go profile resources = %+v, ok=%v", goProfile, ok)
	}
	for _, id := range []string{"ubuntu", "go", "node", "python"} {
		if !seen[id] {
			t.Errorf("missing profile %s", id)
		}
	}
}
