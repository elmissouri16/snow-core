//go:build darwin || linux

package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

const accessFile = "access.json"
const maxAccessBytes = 32 << 10

// accessStore shares Registry's pinned root and lifetime lease, but never its
// database schema. All calls hold access.mu, then acquire Registry's gate.
// Identity AND content checks reject stale writers and in-place replacement.
type accessStore struct {
	registry *Registry
	info     os.FileInfo
	digest   [32]byte
}

type storedBrowser struct {
	ID       string    `json:"id,omitempty"`
	Label    string    `json:"label,omitempty"`
	Hash     string    `json:"hash"`
	CSRF     string    `json:"csrf"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"last_used"`
	Expires  time.Time `json:"expires"`
}

type storedAccess struct {
	Version     int             `json:"version"`
	Key         string          `json:"key"`
	PairCode    string          `json:"pair_code"`
	PairExpires time.Time       `json:"pair_expires"`
	Window      time.Time       `json:"window"`
	Attempts    int             `json:"attempts"`
	Browsers    []storedBrowser `json:"browsers"`
}

// restoreAccess must run before serving or printing initialCode. A malformed,
// replaced, or non-private access file is fatal, never an invitation to reset
// credentials. Only the operator's private startup output may reprint the code.
func (s *shell) restoreAccess(ctx context.Context, registry *Registry) error {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	s.access.store = &accessStore{registry: registry}
	s.access.failed = true
	leave, err := registry.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	store := s.access.store
	data, info, err := store.read()
	if errors.Is(err, os.ErrNotExist) {
		if err := store.write(ctx, s.access.snapshot()); err != nil {
			return err
		}
	} else {
		if err != nil {
			return err
		}
		var state storedAccess
		if err := json.Unmarshal(data, &state, json.RejectUnknownMembers(true)); err != nil || !state.valid() {
			return ErrRegistryStorage
		}
		store.info, store.digest = info, sha256.Sum256(data)
		key, _ := hex.DecodeString(state.Key)
		copy(s.access.key[:], key)
		s.access.pairCode, s.access.pairExpires = state.PairCode, state.PairExpires
		s.access.pairHash = sha256.Sum256([]byte(state.PairCode))
		s.access.window, s.access.attempts = state.Window, state.Attempts
		s.access.sessions = make(map[[32]byte]browserSession)
		for _, browser := range state.Browsers {
			hash, _ := hex.DecodeString(browser.Hash)
			s.access.sessions[[32]byte(hash)] = browserSession{ID: browser.ID, Label: browser.Label, CSRF: browser.CSRF, Created: browser.Created, LastUsed: browser.LastUsed}
		}
		migrated := state.Version == 1
		if migrated {
			for hash, browser := range s.access.sessions {
				browser.ID, browser.Label = s.newBrowserIDLocked(), "Paired browser"
				s.access.sessions[hash] = browser
			}
		}
		before := len(s.access.sessions)
		s.expireSessionsLocked()
		expired := !s.now().Before(s.access.pairExpires)
		if expired {
			s.issuePairingLocked()
		}
		if migrated || expired || before != len(s.access.sessions) {
			if err := store.write(ctx, s.access.snapshot()); err != nil {
				return err
			}
		}
	}
	s.initialCode = s.access.pairCode
	s.access.failed = false
	return nil
}

func validAccessToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func (state storedAccess) valid() bool {
	if (state.Version != 1 && state.Version != 2) || !validAccessToken(state.Key) || !validAccessToken(state.PairCode) || state.PairExpires.IsZero() || state.Attempts < 0 || state.Attempts > 20 || (state.Attempts > 0 && state.Window.IsZero()) || len(state.Browsers) > maxBrowsers {
		return false
	}
	seen := make(map[string]bool)
	ids := make(map[string]bool)
	for _, browser := range state.Browsers {
		if !validAccessToken(browser.Hash) || !validAccessToken(browser.CSRF) || browser.Created.IsZero() || browser.LastUsed.Before(browser.Created) || !browser.Expires.Equal(browser.Created.Add(browserLifetime)) || seen[browser.Hash] {
			return false
		}
		if state.Version == 1 {
			if browser.ID != "" || browser.Label != "" {
				return false
			}
		} else if !validBrowserID(browser.ID) || !validBrowserLabel(browser.Label) || ids[browser.ID] {
			return false
		}
		ids[browser.ID] = true
		seen[browser.Hash] = true
	}
	return true
}

func (a *accessState) snapshot() storedAccess {
	state := storedAccess{Version: 2, Key: hex.EncodeToString(a.key[:]), PairCode: a.pairCode, PairExpires: a.pairExpires, Window: a.window, Attempts: a.attempts}
	for hash, browser := range a.sessions {
		state.Browsers = append(state.Browsers, storedBrowser{ID: browser.ID, Label: browser.Label, Hash: hex.EncodeToString(hash[:]), CSRF: browser.CSRF, Created: browser.Created, LastUsed: browser.LastUsed, Expires: browser.Created.Add(browserLifetime)})
	}
	return state
}

// read never follows links or creates missing storage; it also bounds reads
// after opening, in case another same-user process changes the file's size.
func (store *accessStore) read() ([]byte, os.FileInfo, error) {
	root := store.registry.root
	before, err := root.Lstat(accessFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, os.ErrNotExist
	}
	if err != nil || !privateRegistryFile(before) || before.Size() > maxAccessBytes {
		return nil, nil, ErrRegistryUnsafe
	}
	file, err := root.OpenFile(accessFile, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, ErrRegistryUnsafe
	}
	defer file.Close()
	after, err := file.Stat()
	current, currentErr := root.Lstat(accessFile)
	if err != nil || currentErr != nil || !privateRegistryFile(after) || !privateRegistryFile(current) || !os.SameFile(before, after) || !os.SameFile(after, current) {
		return nil, nil, ErrRegistryUnsafe
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAccessBytes+1))
	if err != nil || len(data) > maxAccessBytes {
		return nil, nil, ErrRegistryStorage
	}
	return data, after, nil
}

func (store *accessStore) check() error {
	data, info, err := store.read()
	if store.info == nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || store.info == nil || !os.SameFile(store.info, info) || sha256.Sum256(data) != store.digest {
		return ErrRegistryUnsafe
	}
	return nil
}

// write requires Registry's gate. Rename is the commit point; sync the parent
// directory before reporting success. Any ambiguous failure disables access for
// the lifetime of this shell, rather than granting from unsaved memory.
func (store *accessStore) write(ctx context.Context, state storedAccess) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !state.valid() {
		return ErrRegistryStorage
	}
	if err := store.check(); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > maxAccessBytes {
		return ErrRegistryStorage
	}
	root := store.registry.root
	name := ".access-" + randomToken() + ".tmp"
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return ErrRegistryStorage
	}
	defer root.Remove(name)
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	info, statErr := file.Stat()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || statErr != nil || closeErr != nil || !privateRegistryFile(info) {
		return ErrRegistryStorage
	}
	current, err := root.Lstat(name)
	if err != nil || !privateRegistryFile(current) || !os.SameFile(info, current) {
		return ErrRegistryUnsafe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.registry.checkStorage(); err != nil {
		return err
	}
	if err := store.check(); err != nil {
		return err
	}
	if err := root.Rename(name, accessFile); err != nil {
		return ErrRegistryStorage
	}
	dir, err := root.Open(".")
	if err != nil {
		return ErrRegistryStorage
	}
	syncErr = dir.Sync()
	closeErr = dir.Close()
	if syncErr != nil || closeErr != nil {
		return ErrRegistryStorage
	}
	store.info, store.digest = info, sha256.Sum256(data)
	return store.check()
}

// checkAccessLocked validates the durable backing before any authority is used.
func (s *shell) checkAccessLocked(ctx context.Context) bool {
	return s.accessOperationLocked(ctx, false)
}

func (s *shell) saveAccessLocked(ctx context.Context) bool {
	return s.accessOperationLocked(ctx, true)
}

func (s *shell) accessOperationLocked(ctx context.Context, write bool) bool {
	if s.access.failed {
		return false
	}
	if ctx.Err() != nil {
		if write {
			s.access.failed = true
		}
		return false
	}
	if s.access.store == nil {
		return true
	} // Explicit runtime-free shell mode.
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := s.access.store.registry.enter(ctx)
	if err == nil {
		defer leave()
		if write {
			err = s.access.store.write(ctx, s.access.snapshot())
		} else {
			err = s.access.store.check()
		}
	}
	if err != nil {
		s.access.failed = true
		return false
	}
	return true
}
