//go:build linux || darwin

package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestUpdateKeybindingsRejectsNonRegularLock(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "keybindings.yaml.lock")
	if err := syscall.Mkfifo(lockPath, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	path := filepath.Join(root, "keybindings.yaml")
	if _, err := UpdateKeybindings(KeybindingWriteScope{Path: path, ConfinedRoot: root, Global: true}, func(file *KeybindingsFile) error {
		file.Bindings["models"] = []string{"alt+z"}
		return nil
	}); err == nil {
		t.Fatal("FIFO coordination lock was accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected lock still wrote target: %v", err)
	}
}
