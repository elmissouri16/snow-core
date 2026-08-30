package pluginsdk

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSDKAssetsMatchCanonicalSources(t *testing.T) {
	cases := []struct {
		runtime Runtime
		root    string
		include func(string) bool
	}{
		{
			runtime: RuntimePython,
			root:    filepath.Join("..", "..", "sdk", "plugin-python"),
			include: func(name string) bool {
				return name == "LICENSE" || strings.HasPrefix(name, "src/snow_plugin/") && !strings.Contains(name, "__pycache__") && !strings.HasSuffix(name, ".pyc")
			},
		},
		{
			runtime: RuntimeJavaScript,
			root:    filepath.Join("..", "..", "sdk", "plugin-javascript"),
			include: func(name string) bool {
				return name == "LICENSE" || name == "package.json" || strings.HasPrefix(name, "src/")
			},
		},
	}
	for _, tc := range cases {
		t.Run(string(tc.runtime), func(t *testing.T) {
			expected := make(map[string][]byte)
			err := filepath.WalkDir(tc.root, func(name string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				relative, err := filepath.Rel(tc.root, name)
				if err != nil {
					return err
				}
				relative = filepath.ToSlash(relative)
				if !tc.include(relative) {
					return nil
				}
				assetName := relative
				if tc.runtime == RuntimePython {
					assetName = strings.TrimPrefix(relative, "src/")
				}
				data, err := os.ReadFile(name)
				if err != nil {
					return err
				}
				expected[assetName] = data
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			assetRoot := path.Join("assets", string(tc.runtime))
			actual := make(map[string][]byte)
			err = fs.WalkDir(sdkAssets, assetRoot, func(name string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				data, err := fs.ReadFile(sdkAssets, name)
				if err != nil {
					return err
				}
				actual[strings.TrimPrefix(name, assetRoot+"/")] = data
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(actual) != len(expected) {
				t.Fatalf("embedded files=%d canonical files=%d\nembedded=%v\ncanonical=%v", len(actual), len(expected), mapKeys(actual), mapKeys(expected))
			}
			for name, want := range expected {
				got, ok := actual[name]
				if !ok || !bytes.Equal(got, want) {
					t.Fatalf("embedded SDK file %q differs from canonical source", name)
				}
			}
		})
	}
}

func TestVendorStagesMetadataAndRequiresExplicitReplace(t *testing.T) {
	destination := t.TempDir()
	receipt, err := Vendor(Options{
		Runtime: RuntimePython, Destination: destination, HostVersion: "test-version",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Runtime != RuntimePython || receipt.Replaced || receipt.SDKVersion != runtimeVersions[RuntimePython] || len(receipt.Files) < 2 {
		t.Fatalf("receipt = %+v", receipt)
	}
	metadataPath := filepath.Join(destination, "vendor", "python", "snow-sdk.json")
	metadataData, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata vendorMetadata
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Runtime != RuntimePython || metadata.HostVersion != "test-version" || len(metadata.Files) != len(receipt.Files)-1 {
		t.Fatalf("metadata = %+v", metadata)
	}
	licenseData, err := os.ReadFile(filepath.Join(destination, "vendor", "python", "LICENSE"))
	if err != nil || !bytes.Contains(licenseData, []byte("MIT License")) {
		t.Fatalf("vendored license missing or invalid: %q, %v", licenseData, err)
	}
	licenseReceipted := false
	for _, file := range metadata.Files {
		licenseReceipted = licenseReceipted || file.Path == "LICENSE"
	}
	if !licenseReceipted {
		t.Fatalf("vendored license missing from metadata: %+v", metadata.Files)
	}

	initPath := filepath.Join(destination, "vendor", "python", "snow_plugin", "__init__.py")
	if err := os.WriteFile(initPath, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Vendor(Options{Runtime: RuntimePython, Destination: destination}); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("second vendor error = %v", err)
	}
	receipt, err = Vendor(Options{Runtime: RuntimePython, Destination: destination, Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Replaced {
		t.Fatalf("replacement receipt = %+v", receipt)
	}
	data, err := os.ReadFile(initPath)
	if err != nil || string(data) == "modified" {
		t.Fatalf("replacement data=%q err=%v", data, err)
	}
	matches, err := filepath.Glob(filepath.Join(destination, "vendor", ".python.*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("staging leftovers = %v, %v", matches, err)
	}
}

func TestVendorRejectsSymlinkedVendorDirectory(t *testing.T) {
	destination := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(destination, "vendor")); err != nil {
		t.Fatal(err)
	}
	if _, err := Vendor(Options{Runtime: RuntimeJavaScript, Destination: destination}); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("vendor error = %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside entries = %v, %v", entries, err)
	}
}

func TestVendorValidatesRuntimeAndExistingDestination(t *testing.T) {
	if _, err := ParseRuntime("js"); err == nil || !strings.Contains(err.Error(), "python or javascript") {
		t.Fatalf("runtime error = %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := Vendor(Options{Runtime: RuntimePython, Destination: missing}); err == nil || !strings.Contains(err.Error(), "existing directory") {
		t.Fatalf("destination error = %v", err)
	}
}

func mapKeys(values map[string][]byte) []string {
	keys := slices.Sorted(maps.Keys(values))
	return keys
}
