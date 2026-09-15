package auth

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

func openHostKeyParent(path string, create bool) (*os.Root, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil || path == "" {
		return nil, "", ErrHostKeyUnavailable
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
			return nil, "", ErrHostKeyUnavailable
		}
		parent = next
	}
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, "", ErrHostKeyUnavailable
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, "", ErrHostKeyUnavailable
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, "", ErrHostKeyUnavailable
	}
	for i := len(missing) - 1; i >= 0; i-- {
		part := missing[i]
		if err = root.Mkdir(part, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			root.Close()
			return nil, "", ErrHostKeyUnavailable
		}
		before, err = root.Lstat(part)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, "", ErrHostKeyUnavailable
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, "", ErrHostKeyUnavailable
		}
		after, err = next.Stat(".")
		root.Close()
		if err != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, "", ErrHostKeyUnavailable
		}
		root = next
	}
	return root, filepath.Base(absolute), nil
}

func sameHostKeyFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Mode().IsRegular() && b.Mode().IsRegular() && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func verifyHostKeyFile(root *os.Root, name string, file *os.File) error {
	opened, err := file.Stat()
	if err != nil {
		return ErrHostKeyUnavailable
	}
	current, err := root.Lstat(name)
	if err != nil || !current.Mode().IsRegular() || !opened.Mode().IsRegular() || !os.SameFile(opened, current) {
		return ErrHostKeyUnavailable
	}
	return nil
}

func readHostKeySnapshot(ctx context.Context, root *os.Root, base string) (hostKeySnapshot, error) {
	var zero hostKeySnapshot
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	before, err := root.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		return hostKeySnapshot{document: hostKeyObject{}, revision: HostAuthMissingRevision}, nil
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() > MaxHostAuthFileBytes {
		return zero, ErrHostKeyUnavailable
	}
	file, err := openHostStatusFile(root, base)
	if err != nil {
		return zero, ErrHostKeyUnavailable
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !sameHostKeyFile(before, after) {
		return zero, ErrHostKeyUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxHostAuthFileBytes+1))
	if err != nil || len(data) > MaxHostAuthFileBytes {
		return zero, ErrHostKeyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	afterRead, err := file.Stat()
	if err != nil || !sameHostKeyFile(after, afterRead) {
		return zero, ErrHostKeyUnavailable
	}
	current, err := root.Lstat(base)
	if err != nil || !sameHostKeyFile(afterRead, current) {
		return zero, ErrHostKeyUnavailable
	}
	var document hostKeyObject
	if json.Unmarshal(data, &document) != nil || document == nil {
		return zero, ErrHostKeyUnavailable
	}
	// Fail closed on semantic corruption anywhere in the credential store;
	// preserve unknown fields, but never rewrite an unreadable credential map.
	for id := range document {
		if _, _, err := hostKeyCredential(document, id); err != nil {
			return zero, err
		}
	}
	revision, err := hostKeyRevision(current)
	if err != nil {
		return zero, err
	}
	return hostKeySnapshot{document: document, info: current, revision: revision}, nil
}

func hostKeyNonce() string {
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	return hex.EncodeToString(nonce[:])
}

func writeHostKeyRoot(ctx context.Context, root *os.Root, base string, before os.FileInfo, data []byte) (os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > MaxHostAuthFileBytes {
		return nil, ErrHostKeyUnavailable
	}
	temp := ".snow-auth-key-" + hostKeyNonce() + ".tmp"
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, ErrHostKeyUnavailable
	}
	defer func() { file.Close(); _ = root.Remove(temp) }()
	if file.Chmod(0o600) != nil {
		return nil, ErrHostKeyUnavailable
	}
	if _, err = file.Write(data); err != nil {
		return nil, ErrHostKeyUnavailable
	}
	if file.Sync() != nil {
		return nil, ErrHostKeyUnavailable
	}
	written, err := file.Stat()
	if err != nil || !written.Mode().IsRegular() {
		return nil, ErrHostKeyUnavailable
	}
	if file.Close() != nil {
		return nil, ErrHostKeyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, statErr := root.Lstat(base)
	if before == nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			return nil, ErrHostKeyUnavailable
		}
	} else if statErr != nil || !sameHostKeyFile(before, current) {
		return nil, ErrHostKeyUnavailable
	}
	if root.Rename(temp, base) != nil {
		return nil, ErrHostKeyUnavailable
	}
	directory, err := root.Open(".")
	if err != nil {
		return nil, ErrHostKeyUnavailable
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return nil, ErrHostKeyUnavailable
	}
	current, err = root.Lstat(base)
	if err != nil || !sameHostKeyFile(written, current) {
		return nil, ErrHostKeyUnavailable
	}
	return current, nil
}
