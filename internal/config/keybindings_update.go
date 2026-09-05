package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var keybindingsUpdateMu sync.Mutex

// KeybindingWriteScope identifies one global or trusted-project auxiliary file.
// ConfinedRoot must be the global directory or the pinned trusted project root.
type KeybindingWriteScope struct {
	Path         string
	ConfinedRoot string
	Global       bool
	// CoordinationRoot and CoordinationPath optionally select a shared lock.
	// The TUI points both global and project writes at the global lock so merged
	// validation cannot race across Snow instances.
	CoordinationRoot string
	CoordinationPath string
	// Inherited is the effective lower-precedence binding map. Global writes
	// default to Snow's built-ins; project writes provide the current global
	// effective map so the persisted scope cannot be invalid on reload.
	Inherited map[string][]string
	// Validate runs under the coordination lock after the mutation and local
	// validation, but before replacement. It may re-read layered scopes.
	Validate func(KeybindingsFile) error
}

// UpdateKeybindings atomically mutates the latest keybinding file without
// discarding unrelated actions written by another Snow instance.
func UpdateKeybindings(scope KeybindingWriteScope, mutate func(*KeybindingsFile) error) (KeybindingsFile, error) {
	if scope.Path == "" || scope.ConfinedRoot == "" {
		return KeybindingsFile{}, errors.New("keybindings: empty path or confined root")
	}
	if mutate == nil {
		return KeybindingsFile{}, errors.New("keybindings: update mutation is nil")
	}
	rel, err := filepath.Rel(scope.ConfinedRoot, scope.Path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return KeybindingsFile{}, errors.New("keybindings: path escapes confined root")
	}

	keybindingsUpdateMu.Lock()
	defer keybindingsUpdateMu.Unlock()

	if scope.Global {
		if err := os.MkdirAll(scope.ConfinedRoot, 0o700); err != nil {
			return KeybindingsFile{}, fmt.Errorf("keybindings: create global root: %w", err)
		}
	}
	root, err := os.OpenRoot(scope.ConfinedRoot)
	if err != nil {
		return KeybindingsFile{}, fmt.Errorf("keybindings: open root: %w", err)
	}
	defer root.Close()
	dirRoot, base, err := openPinnedKeybindingDir(root, rel)
	if err != nil {
		return KeybindingsFile{}, err
	}
	defer dirRoot.Close()

	lock, closeLockRoot, err := openKeybindingCoordinationLock(scope, dirRoot, base)
	if err != nil {
		return KeybindingsFile{}, err
	}
	defer closeLockRoot()
	defer lock.Close()
	defer unlockConfigFile(lock)

	current, mode, err := readKeybindingsForUpdate(dirRoot, base, scope.Global)
	if err != nil {
		return KeybindingsFile{}, err
	}
	if err := mutate(&current); err != nil {
		return KeybindingsFile{}, err
	}
	if current.Bindings == nil {
		current.Bindings = map[string][]string{}
	}
	current.Version = 1
	if err := validateKeybindingFile(current); err != nil {
		return KeybindingsFile{}, err
	}
	if scope.Validate != nil {
		if err := scope.Validate(current); err != nil {
			return KeybindingsFile{}, err
		}
	} else {
		inherited := defaultAuxBindings()
		for action, values := range scope.Inherited {
			inherited[action] = slices.Clone(values)
		}
		effective := cloneBindings(inherited)
		for action, values := range current.Bindings {
			effective[action] = slices.Clone(values)
		}
		effective["abort"] = appendUnique(effective["abort"], "ctrl+c")
		effective["abort"] = appendUnique(effective["abort"], "esc")
		effective["quit"] = appendUnique(effective["quit"], "ctrl+c")
		effective["close"] = appendUnique(effective["close"], "esc")
		if err := validateEffectiveAuxBindings(effective); err != nil {
			return KeybindingsFile{}, err
		}
	}
	encoded, err := yaml.Marshal(current)
	if err != nil {
		return KeybindingsFile{}, fmt.Errorf("keybindings: encode: %w", err)
	}
	if len(encoded) > AuxiliaryFileLimit {
		return KeybindingsFile{}, fmt.Errorf("keybindings: encoded file exceeds %d bytes", AuxiliaryFileLimit)
	}
	if err := atomicWriteKeybindingsRoot(dirRoot, base, encoded, mode); err != nil {
		return KeybindingsFile{}, err
	}
	return current, nil
}

func openPinnedKeybindingDir(root *os.Root, rel string) (*os.Root, string, error) {
	dir := filepath.Dir(rel)
	if err := root.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("keybindings: create directory: %w", err)
	}
	if err := validateAuxRootPath(root, dir); err != nil {
		return nil, "", fmt.Errorf("keybindings: %w", err)
	}
	before, err := root.Lstat(dir)
	if err != nil {
		return nil, "", fmt.Errorf("keybindings: inspect directory: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, "", errors.New("keybindings: destination parent must be a non-symlink directory")
	}
	dirRoot, err := root.OpenRoot(dir)
	if err != nil {
		return nil, "", fmt.Errorf("keybindings: pin directory: %w", err)
	}
	after, err := dirRoot.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		dirRoot.Close()
		return nil, "", errors.New("keybindings: destination directory changed while opening")
	}
	base := filepath.Base(rel)
	if err := validateAuxRootPath(dirRoot, base); err != nil {
		dirRoot.Close()
		return nil, "", fmt.Errorf("keybindings: %w", err)
	}
	return dirRoot, base, nil
}

func openKeybindingCoordinationLock(scope KeybindingWriteScope, targetDir *os.Root, targetBase string) (*os.File, func(), error) {
	lockRoot := targetDir
	lockName := targetBase + ".lock"
	closeRoot := func() {}
	if scope.CoordinationRoot != "" || scope.CoordinationPath != "" {
		if scope.CoordinationRoot == "" || scope.CoordinationPath == "" {
			return nil, closeRoot, errors.New("keybindings: incomplete coordination lock path")
		}
		if err := os.MkdirAll(scope.CoordinationRoot, 0o700); err != nil {
			return nil, closeRoot, fmt.Errorf("keybindings: create coordination root: %w", err)
		}
		root, err := os.OpenRoot(scope.CoordinationRoot)
		if err != nil {
			return nil, closeRoot, fmt.Errorf("keybindings: open coordination root: %w", err)
		}
		rel, err := filepath.Rel(scope.CoordinationRoot, scope.CoordinationPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			root.Close()
			return nil, closeRoot, errors.New("keybindings: coordination lock escapes its root")
		}
		lockRoot, lockName, err = openPinnedKeybindingDir(root, rel)
		root.Close()
		if err != nil {
			return nil, closeRoot, err
		}
		lockName += ".lock"
		closeRoot = func() { _ = lockRoot.Close() }
	}
	lock, err := openPinnedKeybindingLock(lockRoot, lockName)
	if err != nil {
		closeRoot()
		return nil, func() {}, err
	}
	if err := lockConfigFile(lock); err != nil {
		lock.Close()
		closeRoot()
		return nil, func() {}, fmt.Errorf("keybindings: lock update: %w", err)
	}
	if err := verifyPinnedKeybindingFile(lockRoot, lockName, lock); err != nil {
		unlockConfigFile(lock)
		lock.Close()
		closeRoot()
		return nil, func() {}, err
	}
	return lock, closeRoot, nil
}

func openPinnedKeybindingLock(root *os.Root, name string) (*os.File, error) {
	if err := validateAuxRootPath(root, name); err != nil {
		return nil, fmt.Errorf("keybindings: %w", err)
	}
	// Exclusive creation avoids intermittent ENOENT from concurrent macOS
	// O_CREATE opens. Other writers reopen the winner's stable lock file.
	lock, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, fs.ErrExist) {
		lock, err = root.OpenFile(name, os.O_RDWR, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("keybindings: open update lock: %w", err)
	}
	if err := verifyPinnedKeybindingFile(root, name, lock); err != nil {
		lock.Close()
		return nil, err
	}
	if err := lock.Chmod(0o600); err != nil {
		lock.Close()
		return nil, fmt.Errorf("keybindings: chmod update lock: %w", err)
	}
	return lock, nil
}

func verifyPinnedKeybindingFile(root *os.Root, name string, file *os.File) error {
	opened, statErr := file.Stat()
	current, lstatErr := root.Lstat(name)
	if statErr != nil || lstatErr != nil || !opened.Mode().IsRegular() || !current.Mode().IsRegular() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, current) {
		return errors.New("keybindings: update lock changed while opening")
	}
	return nil
}

func readKeybindingsForUpdate(root *os.Root, rel string, global bool) (KeybindingsFile, os.FileMode, error) {
	mode := os.FileMode(0o644)
	if global {
		mode = 0o600
	}
	before, err := root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return KeybindingsFile{Version: 1, Bindings: map[string][]string{}}, mode, nil
	}
	if err != nil {
		return KeybindingsFile{}, 0, fmt.Errorf("keybindings: inspect: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return KeybindingsFile{}, 0, errors.New("keybindings: target must be a regular non-symlink file")
	}
	if !global {
		mode = before.Mode().Perm() & 0o666
		if mode == 0 {
			mode = 0o644
		}
	}
	file, err := root.Open(rel)
	if err != nil {
		return KeybindingsFile{}, 0, fmt.Errorf("keybindings: open: %w", err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return KeybindingsFile{}, 0, errors.New("keybindings: target changed while opening")
	}
	var current KeybindingsFile
	if err := decodeAuxFile(file, after, &current); err != nil {
		return KeybindingsFile{}, 0, fmt.Errorf("keybindings: %w", err)
	}
	if current.Version != 1 || current.Bindings == nil {
		return KeybindingsFile{}, 0, errors.New("keybindings: file requires version: 1 and bindings")
	}
	if err := validateKeybindingFile(current); err != nil {
		return KeybindingsFile{}, 0, fmt.Errorf("keybindings: %w", err)
	}
	return current, mode, nil
}

func atomicWriteKeybindingsRoot(root *os.Root, rel string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(rel)
	tmp := filepath.Join(dir, fmt.Sprintf(".snow-keybindings-%d-%d.tmp", os.Getpid(), time.Now().UnixNano()))
	file, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("keybindings: create temporary file: %w", err)
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		if err != nil {
			_ = root.Remove(tmp)
		}
	}()
	if err = file.Chmod(mode); err != nil {
		return fmt.Errorf("keybindings: chmod temporary file: %w", err)
	}
	if _, err = file.Write(data); err != nil {
		return fmt.Errorf("keybindings: write temporary file: %w", err)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("keybindings: sync temporary file: %w", err)
	}
	if err = file.Close(); err != nil {
		file = nil
		return fmt.Errorf("keybindings: close temporary file: %w", err)
	}
	file = nil
	if info, statErr := root.Lstat(rel); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("keybindings: target became a non-regular file")
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("keybindings: inspect destination: %w", statErr)
	}
	if err = root.Rename(tmp, rel); err != nil {
		return fmt.Errorf("keybindings: replace: %w", err)
	}
	return nil
}
