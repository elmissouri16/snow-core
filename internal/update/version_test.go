package update

import "testing"

func TestParseAndCompareVersions(t *testing.T) {
	t.Parallel()
	valid := []struct {
		input string
		want  string
	}{
		{"0.1.0", "0.1.0"},
		{"v1.2.3", "1.2.3"},
		{"v0.1.0-alpha.12", "0.1.0-alpha.12"},
	}
	for _, tc := range valid {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseVersion(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Fatalf("String() = %q, want %q", got.String(), tc.want)
			}
		})
	}
	invalid := []string{"", " 1.2.3", "1.2", "1.2.3-dev", "1.2.3-beta.1", "1.2.3-alpha.0", "1.2.3-alpha.01", "01.2.3", "1.2.3+meta", "v", "1.-2.3", "18446744073709551616.0.0"}
	for _, value := range invalid {
		t.Run("invalid_"+value, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseVersion(value); err == nil {
				t.Fatalf("ParseVersion(%q) unexpectedly succeeded", value)
			}
		})
	}
	alpha9, _ := ParseVersion("1.0.0-alpha.9")
	alpha10, _ := ParseVersion("1.0.0-alpha.10")
	stable, _ := ParseVersion("1.0.0")
	if Compare(alpha10, alpha9) <= 0 || Compare(stable, alpha10) <= 0 || Compare(alpha9, alpha9) != 0 {
		t.Fatal("version comparison does not follow numeric alpha/stable ordering")
	}
}
