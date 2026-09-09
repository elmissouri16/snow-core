package javascript

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func packageDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"test","name":"Test","version":"1","api_version":1,"entry":"main.js","host_tools":["read"]}`
	for name, data := range map[string]string{"snow-plugin.json": manifest, "main.js": "var value=1;"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestPackageImmutableAndConfigurationScoped(t *testing.T) {
	dir := packageDir(t)
	one, err := ReadPackage(t.Context(), dir, "", json.RawMessage(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := ReadPackage(t.Context(), dir, "", json.RawMessage(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if reordered.Fingerprint != one.Fingerprint {
		t.Fatal("configuration ordering changed scope")
	}
	two, err := ReadPackage(t.Context(), dir, "", json.RawMessage(`{"a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if two.Fingerprint == one.Fingerprint {
		t.Fatal("configuration did not change scope")
	}
	if err := os.WriteFile(filepath.Join(dir, "main.js"), []byte("var value=2;"), 0600); err != nil {
		t.Fatal(err)
	}
	three, err := ReadPackage(t.Context(), dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if three.Fingerprint == one.Fingerprint || string(one.Script) != "var value=1;" {
		t.Fatal("snapshot mutated or identity unchanged")
	}
}
func TestPackageRejectsUnsafeAndOversizedInputs(t *testing.T) {
	for _, kind := range []string{"entry_escape", "entry_symlink", "directory_symlink", "manifest_symlink", "oversized", "version", "host_tools", "outside_project"} {
		t.Run(kind, func(t *testing.T) {
			dir := packageDir(t)
			root := ""
			var m Manifest
			data, err := os.ReadFile(filepath.Join(dir, "snow-plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err = jsonv2.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "entry_escape":
				m.Entry = "../main.js"
			case "entry_symlink":
				if err = os.Symlink("main.js", filepath.Join(dir, "link.js")); err != nil {
					t.Fatal(err)
				}
				m.Entry = "link.js"
			case "directory_symlink":
				if err = os.Symlink(".", filepath.Join(dir, "linked")); err != nil {
					t.Fatal(err)
				}
				m.Entry = "linked/main.js"
			case "manifest_symlink":
				if err = os.Rename(filepath.Join(dir, "snow-plugin.json"), filepath.Join(dir, "other.json")); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink("other.json", filepath.Join(dir, "snow-plugin.json")); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err = os.WriteFile(filepath.Join(dir, "main.js"), []byte(strings.Repeat(" ", MaxScriptBytes+1)), 0600); err != nil {
					t.Fatal(err)
				}
			case "version":
				m.APIVersion = 99
			case "host_tools":
				m.HostTools = []string{"process_start"}
			case "outside_project":
				root = packageDir(t)
			}
			if kind != "manifest_symlink" {
				data, err = jsonv2.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, "snow-plugin.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = ReadPackage(t.Context(), dir, root, nil); err == nil {
				t.Fatal("unsafe package accepted")
			}
		})
	}
}
func TestDeclarationsPrecedenceAndDisabledShadow(t *testing.T) {
	global := map[string]plugin.JavaScriptSpec{"one": {Path: "global"}, "two": {Path: "global"}}
	project := map[string]plugin.JavaScriptSpec{"one": {Disabled: true}}
	explicit := map[string]plugin.JavaScriptSpec{"two": {Path: "explicit"}}
	out, err := Resolve(global, project, explicit, "/g", "/p", "/c")
	if err != nil {
		t.Fatal(err)
	}
	if !out[0].Disabled || out[0].Scope != "project" || out[1].Scope != "explicit" || out[1].Path != "/c/explicit" {
		t.Fatalf("declarations=%+v", out)
	}
}
