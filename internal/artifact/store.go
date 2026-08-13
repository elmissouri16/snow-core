// Package artifact stores immutable, session-scoped private text artifacts.
// Artifacts live outside project roots and are addressed only by opaque IDs.
package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
)

const DefaultMaxBytes = 4 << 20

var artifactIDPattern = regexp.MustCompile(`^artifact-[a-f0-9]{32}$`)

// Ref identifies one immutable artifact in a session namespace.
type Ref struct {
	ID    string
	Bytes int
}

// Store is the private persistence used by tool-result spill and retrieval.
type Store interface {
	SaveText(context.Context, string, string, string) (Ref, error)
	ReadText(context.Context, string, string) (string, error)
	Close() error
}

// LocalStore stores artifacts beneath a pinned private root.
type LocalStore struct {
	mu       sync.RWMutex
	root     *os.Root
	maxBytes int
	closed   bool
}

// NewLocalStore opens or creates a private artifact root.
func NewLocalStore(path string, maxBytes int) (*LocalStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("artifact: root path is required")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, fmt.Errorf("artifact: create root: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return nil, fmt.Errorf("artifact: protect root: %w", err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("artifact: open root: %w", err)
	}
	return &LocalStore{root: root, maxBytes: maxBytes}, nil
}

func namespace(sessionID string) string {
	sum := sha256.Sum256([]byte("snow-artifact-session\x00" + sessionID))
	return "session-" + hex.EncodeToString(sum[:16])
}

func artifactID(sessionID, key, text string) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + key + "\x00" + text))
	return "artifact-" + hex.EncodeToString(sum[:16])
}

func validateID(id string) error {
	if !artifactIDPattern.MatchString(id) {
		return errors.New("artifact: invalid artifact ID")
	}
	return nil
}

func openVerifiedArtifact(root *os.Root, name string, maxBytes int) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() > int64(maxBytes) {
		return nil, errors.New("artifact: stored artifact is invalid")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("artifact: stored artifact changed while opening")
	}
	return file, nil
}

func openVerifiedNamespace(root *os.Root, name string, create bool) (*os.Root, error) {
	if create {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, errors.New("artifact: session namespace is not a real directory")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = child.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("artifact: session namespace changed while opening")
	}
	if err := child.Chmod(".", 0o700); err != nil {
		_ = child.Close()
		return nil, err
	}
	return child, nil
}

// SaveText atomically saves text. Repeating the same session/key/content is
// idempotent and returns the same opaque ID.
func (s *LocalStore) SaveText(ctx context.Context, sessionID, key, text string) (Ref, error) {
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	if sessionID == "" {
		return Ref{}, errors.New("artifact: session ID is required")
	}
	if len(text) > s.maxBytes {
		return Ref{}, fmt.Errorf("artifact: text exceeds %d byte limit", s.maxBytes)
	}
	id := artifactID(sessionID, key, text)
	name := id + ".txt"

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || s.root == nil {
		return Ref{}, errors.New("artifact: store is closed")
	}
	dir, err := openVerifiedNamespace(s.root, namespace(sessionID), true)
	if err != nil {
		return Ref{}, fmt.Errorf("artifact: create namespace: %w", err)
	}
	defer dir.Close()
	file, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, openErr := openVerifiedArtifact(dir, name, s.maxBytes)
		if openErr != nil {
			return Ref{}, openErr
		}
		data, readErr := io.ReadAll(io.LimitReader(existing, int64(s.maxBytes)+1))
		closeErr := existing.Close()
		if readErr != nil || closeErr != nil || string(data) != text {
			return Ref{}, errors.New("artifact: existing artifact does not match immutable content")
		}
		return Ref{ID: id, Bytes: len(text)}, nil
	}
	if err != nil {
		return Ref{}, fmt.Errorf("artifact: create: %w", err)
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = dir.Remove(name)
		}
	}()
	if _, err := io.WriteString(file, text); err != nil {
		return Ref{}, fmt.Errorf("artifact: write: %w", err)
	}
	if err := file.Sync(); err != nil {
		return Ref{}, fmt.Errorf("artifact: sync: %w", err)
	}
	if err := file.Close(); err != nil {
		return Ref{}, fmt.Errorf("artifact: close: %w", err)
	}
	ok = true
	return Ref{ID: id, Bytes: len(text)}, nil
}

// ReadText returns a complete artifact after validating session ownership.
func (s *LocalStore) ReadText(ctx context.Context, sessionID, id string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", errors.New("artifact: session ID is required")
	}
	if err := validateID(id); err != nil {
		return "", err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || s.root == nil {
		return "", errors.New("artifact: store is closed")
	}
	dir, err := openVerifiedNamespace(s.root, namespace(sessionID), false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("artifact: artifact not found in current session")
		}
		return "", fmt.Errorf("artifact: open namespace: %w", err)
	}
	defer dir.Close()
	file, err := openVerifiedArtifact(dir, id+".txt", s.maxBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("artifact: artifact not found in current session")
		}
		return "", fmt.Errorf("artifact: open: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(s.maxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("artifact: read: %w", err)
	}
	if len(data) > s.maxBytes {
		return "", errors.New("artifact: stored artifact exceeds limit")
	}
	return strings.ToValidUTF8(string(data), "�"), nil
}

func (s *LocalStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.root == nil {
		return nil
	}
	err := s.root.Close()
	s.root = nil
	return err
}
