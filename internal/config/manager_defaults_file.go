package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Pin the existing OS parent before creating missing descendants. Ancestor
// aliases (for example macOS /var -> /private/var) are outside the selected
// config root; the selected root and every new descendant must not be symlinks.
func openManagerParent(path string, create bool) (*os.Root, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil || path == "" {
		return nil, "", ErrManagerUnavailable
	}
	parent := filepath.Dir(absolute)
	var missing []string
	var before os.FileInfo
	for {
		before, err = os.Lstat(parent)
		if !errors.Is(err, os.ErrNotExist) {
			break
		}
		if !create {
			return nil, "", os.ErrNotExist
		}
		missing = append(missing, filepath.Base(parent))
		next := filepath.Dir(parent)
		if next == parent {
			return nil, "", ErrManagerUnavailable
		}
		parent = next
	}
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, "", ErrManagerUnavailable
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, "", ErrManagerUnavailable
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, "", ErrManagerUnavailable
	}
	for i := len(missing) - 1; i >= 0; i-- {
		part := missing[i]
		if err = root.Mkdir(part, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			root.Close()
			return nil, "", ErrManagerUnavailable
		}
		before, err = root.Lstat(part)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, "", ErrManagerUnavailable
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, "", ErrManagerUnavailable
		}
		after, err = next.Stat(".")
		root.Close()
		if err != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, "", ErrManagerUnavailable
		}
		root = next
	}
	return root, filepath.Base(absolute), nil
}

func readManagerRoot(ctx context.Context, root *os.Root, base string) (managerObject, os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	before, err := root.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		return managerObject{}, nil, nil
	}
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() > MaxConfigFileBytes {
		return nil, nil, ErrManagerUnavailable
	}
	file, err := openManagerReadFile(root, base)
	if err != nil {
		return nil, nil, ErrManagerUnavailable
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, nil, ErrManagerUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxConfigFileBytes+1))
	if err != nil || len(data) > MaxConfigFileBytes {
		return nil, nil, ErrManagerUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	current, err := root.Lstat(base)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(after, current) {
		return nil, nil, ErrManagerUnavailable
	}
	var value managerObject
	if json.Unmarshal(data, &value) != nil || value == nil {
		return nil, nil, ErrManagerUnavailable
	}
	selections, err := managerObjectValue(value["project_selections"])
	if err != nil || len(selections) > MaxProjectSelections {
		return nil, nil, ErrManagerUnavailable
	}
	return value, current, nil
}

func readManagerDocument(ctx context.Context, path string) (managerObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, base, err := openManagerParent(path, false)
	if errors.Is(err, os.ErrNotExist) {
		return managerObject{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	value, _, err := readManagerRoot(ctx, root, base)
	return value, err
}

func managerNonce() string {
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	return hex.EncodeToString(nonce[:])
}

// Atomic replacement stays in the pinned parent. Its same-file precondition
// prevents overwriting a target replaced by an uncoordinated writer.
func writeManagerRoot(ctx context.Context, root *os.Root, base string, before os.FileInfo, data []byte) (err error) {
	if len(data) > MaxConfigFileBytes {
		return ErrManagerUnavailable
	}
	temp := ".snow-manager-" + managerNonce() + ".tmp"
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return ErrManagerUnavailable
	}
	defer func() { file.Close(); _ = root.Remove(temp) }()
	if file.Chmod(0o600) != nil {
		return ErrManagerUnavailable
	}
	if _, err = file.Write(data); err != nil {
		return ErrManagerUnavailable
	}
	if file.Sync() != nil || file.Close() != nil {
		return ErrManagerUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, statErr := root.Lstat(base)
	if before == nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			return ErrManagerUnavailable
		}
	} else if statErr != nil || !current.Mode().IsRegular() || !os.SameFile(before, current) || before.Size() != current.Size() || !before.ModTime().Equal(current.ModTime()) {
		return ErrManagerUnavailable
	}
	if root.Rename(temp, base) != nil {
		return ErrManagerUnavailable
	}
	directory, err := root.Open(".")
	if err != nil {
		return ErrManagerUnavailable
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return ErrManagerUnavailable
	}
	return nil
}
