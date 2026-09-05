package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

func TestUpdateKeybindingsPreservesActionsAndMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keybindings.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nbindings:\n  submit: [ctrl+s]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := UpdateKeybindings(KeybindingWriteScope{Path: path, ConfinedRoot: root, Global: true}, func(file *KeybindingsFile) error {
		file.Bindings["models"] = []string{"alt+z"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Bindings["submit"][0] != "ctrl+s" || got.Bindings["models"][0] != "alt+z" {
		t.Fatalf("bindings = %+v", got.Bindings)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	reloaded, diagnostics := LoadKeybindings(root, "", false)
	if len(diagnostics) != 0 || reloaded.Bindings["models"][0] != "alt+z" {
		t.Fatalf("reloaded=%+v diagnostics=%+v", reloaded, diagnostics)
	}
}

func TestUpdateKeybindingsRejectsInheritedCollision(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keybindings.yaml")
	if _, err := UpdateKeybindings(KeybindingWriteScope{Path: path, ConfinedRoot: root, Global: true}, func(file *KeybindingsFile) error {
		file.Bindings["submit"] = []string{"ctrl+t"} // Built-in thinking owns ctrl+t.
		return nil
	}); err == nil {
		t.Fatal("inherited collision was persisted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("collision created file: %v", err)
	}
}

func TestUpdateProjectKeybindingsIsConfinedAndPreservesMode(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".snow", "keybindings.yaml")
	got, err := UpdateKeybindings(KeybindingWriteScope{Path: path, ConfinedRoot: project}, func(file *KeybindingsFile) error {
		file.Bindings["agents"] = []string{"ctrl+g"}
		return nil
	})
	if err != nil || got.Bindings["agents"][0] != "ctrl+g" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("new project mode = %o", info.Mode().Perm())
	}
	outside := filepath.Join(t.TempDir(), "keybindings.yaml")
	if _, err := UpdateKeybindings(KeybindingWriteScope{Path: outside, ConfinedRoot: project}, func(*KeybindingsFile) error { return nil }); err == nil {
		t.Fatal("project escape was accepted")
	}
}

func TestUpdateKeybindingsRejectsOversizeWithoutReplacing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keybindings.yaml")
	original := []byte("version: 1\nbindings:\n  models: [alt+z]\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateKeybindings(KeybindingWriteScope{Path: path, ConfinedRoot: root, Global: true}, func(file *KeybindingsFile) error {
		file.Bindings["submit"] = make([]string, AuxiliaryFileLimit)
		for i := range file.Bindings["submit"] {
			file.Bindings["submit"][i] = "x"
		}
		return nil
	}); err == nil {
		t.Fatal("oversize keybinding file was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(original) {
		t.Fatalf("target changed: len=%d err=%v", len(after), err)
	}
}

func TestUpdateKeybindingsRejectsMalformedAndSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keybindings.yaml")
	if err := os.WriteFile(path, []byte("version: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateKeybindings(KeybindingWriteScope{Path: path, ConfinedRoot: root, Global: true}, func(*KeybindingsFile) error { return nil }); err == nil {
		t.Fatal("malformed file was overwritten")
	}
	malformed, err := os.ReadFile(path)
	if err != nil || string(malformed) != "version: nope\n" {
		t.Fatalf("malformed file changed: %q err=%v", malformed, err)
	}

	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("version: 1\nbindings: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := UpdateKeybindings(KeybindingWriteScope{Path: path, ConfinedRoot: root, Global: true}, func(*KeybindingsFile) error { return nil }); err == nil {
		t.Fatal("symlink target was accepted")
	}
}

func TestUpdateProjectKeybindingsRejectsSymlinkedParent(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, ".snow")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(project, ".snow", "keybindings.yaml")
	if _, err := UpdateKeybindings(KeybindingWriteScope{Path: path, ConfinedRoot: project}, func(file *KeybindingsFile) error {
		file.Bindings["submit"] = []string{"ctrl+s"}
		return nil
	}); err == nil {
		t.Fatal("symlinked project parent was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "keybindings.yaml")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through symlink: %v", err)
	}
}

func TestUpdateKeybindingsHelperProcess(t *testing.T) {
	if os.Getenv("SNOW_KEYBINDINGS_UPDATE_HELPER") != "1" {
		return
	}
	root := os.Getenv("SNOW_KEYBINDINGS_UPDATE_ROOT")
	action := os.Getenv("SNOW_KEYBINDINGS_UPDATE_ACTION")
	value := os.Getenv("SNOW_KEYBINDINGS_UPDATE_VALUE")
	scope := KeybindingWriteScope{
		Path: filepath.Join(root, "keybindings.yaml"), ConfinedRoot: root, Global: true,
	}
	if project := os.Getenv("SNOW_KEYBINDINGS_UPDATE_PROJECT"); project != "" {
		globalPath := filepath.Join(root, "keybindings.yaml")
		scope = KeybindingWriteScope{
			Path: filepath.Join(project, ".snow", "keybindings.yaml"), ConfinedRoot: project,
			CoordinationRoot: root, CoordinationPath: globalPath,
		}
		if os.Getenv("SNOW_KEYBINDINGS_UPDATE_SCOPE") == "global" {
			scope.Path, scope.ConfinedRoot, scope.Global = globalPath, root, true
		}
		scope.Validate = func(candidate KeybindingsFile) error {
			return validateTestLayeredBindings(root, project, os.Getenv("SNOW_KEYBINDINGS_UPDATE_SCOPE"), candidate.Bindings)
		}
	}
	_, err := UpdateKeybindings(scope, func(file *KeybindingsFile) error {
		file.Bindings[action] = []string{value}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func validateTestLayeredBindings(globalDir, projectRoot, candidateScope string, candidate map[string][]string) error {
	var global, project map[string][]string
	global, project = map[string][]string{}, map[string][]string{}
	for _, scope := range mustLoadTestKeybindingScopes(globalDir, projectRoot) {
		if filepath.Clean(scope.Path) == filepath.Clean(filepath.Join(globalDir, "keybindings.yaml")) {
			global = scope.File.Bindings
		} else {
			project = scope.File.Bindings
		}
	}
	if candidateScope == "global" {
		global = candidate
	} else {
		project = candidate
	}
	effective := defaultAuxBindings()
	for action, values := range global {
		effective[action] = slices.Clone(values)
	}
	if err := validateEffectiveAuxBindings(effective); err != nil {
		return err
	}
	for action, values := range project {
		effective[action] = slices.Clone(values)
	}
	return validateEffectiveAuxBindings(effective)
}

func mustLoadTestKeybindingScopes(globalDir, projectRoot string) []KeybindingScope {
	scopes, _ := LoadKeybindingScopes(globalDir, projectRoot, true)
	return scopes
}

func TestUpdateKeybindingsSerializesAcrossProcesses(t *testing.T) {
	for range 16 {
		t.Run("fresh_lock", func(t *testing.T) {
			root := t.TempDir()
			var commands []*exec.Cmd
			outputs := make(map[*exec.Cmd]*bytes.Buffer)
			for action, value := range map[string]string{"models": "alt+z", "agents": "ctrl+g", "processes": "ctrl+p"} {
				cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateKeybindingsHelperProcess$")
				output := new(bytes.Buffer)
				cmd.Stdout, cmd.Stderr = output, output
				outputs[cmd] = output
				cmd.Env = append(os.Environ(),
					"SNOW_KEYBINDINGS_UPDATE_HELPER=1",
					"SNOW_KEYBINDINGS_UPDATE_ROOT="+root,
					"SNOW_KEYBINDINGS_UPDATE_ACTION="+action,
					"SNOW_KEYBINDINGS_UPDATE_VALUE="+value,
				)
				if err := cmd.Start(); err != nil {
					t.Error(err)
					break
				}
				commands = append(commands, cmd)
			}
			for _, cmd := range commands {
				if err := cmd.Wait(); err != nil {
					t.Errorf("helper failed: %v\n%s", err, outputs[cmd].String())
				}
			}
			if t.Failed() {
				return
			}
			got, diagnostics := LoadKeybindings(root, "", false)
			if len(diagnostics) != 0 || got.Bindings["models"][0] != "alt+z" || got.Bindings["agents"][0] != "ctrl+g" || got.Bindings["processes"][0] != "ctrl+p" {
				t.Fatalf("got=%+v diagnostics=%+v", got, diagnostics)
			}
		})
	}
}

func TestUpdateKeybindingsCoordinatesGlobalAndProjectValidationAcrossProcesses(t *testing.T) {
	global, project := t.TempDir(), t.TempDir()
	type mutation struct{ scope, action string }
	mutations := []mutation{{scope: "global", action: "thinking"}, {scope: "project", action: "submit"}}
	commands := make([]*exec.Cmd, 0, len(mutations))
	for _, mutation := range mutations {
		cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateKeybindingsHelperProcess$")
		cmd.Env = append(os.Environ(),
			"SNOW_KEYBINDINGS_UPDATE_HELPER=1",
			"SNOW_KEYBINDINGS_UPDATE_ROOT="+global,
			"SNOW_KEYBINDINGS_UPDATE_PROJECT="+project,
			"SNOW_KEYBINDINGS_UPDATE_SCOPE="+mutation.scope,
			"SNOW_KEYBINDINGS_UPDATE_ACTION="+mutation.action,
			"SNOW_KEYBINDINGS_UPDATE_VALUE=ctrl+x",
		)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	successes := 0
	for _, cmd := range commands {
		if err := cmd.Wait(); err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful conflicting writers = %d, want 1", successes)
	}
	_, diagnostics := LoadKeybindings(global, project, true)
	if len(diagnostics) != 0 {
		t.Fatalf("persisted invalid layered config: %+v", diagnostics)
	}
}

func TestUpdateKeybindingsSerializesConcurrentMutations(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keybindings.yaml")
	scope := KeybindingWriteScope{Path: path, ConfinedRoot: root, Global: true}
	var wg sync.WaitGroup
	for action, value := range map[string]string{"models": "alt+z", "agents": "ctrl+g"} {
		wg.Go(func() {
			if _, err := UpdateKeybindings(scope, func(file *KeybindingsFile) error {
				file.Bindings[action] = []string{value}
				return nil
			}); err != nil {
				t.Errorf("update %s: %v", action, err)
			}
		})
	}
	wg.Wait()
	got, diagnostics := LoadKeybindings(root, "", false)
	if len(diagnostics) != 0 || got.Bindings["models"][0] != "alt+z" || got.Bindings["agents"][0] != "ctrl+g" {
		t.Fatalf("got=%+v diagnostics=%+v", got, diagnostics)
	}
}
