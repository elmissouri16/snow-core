//go:build darwin || linux

package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"
)

var (
	ErrOperationInvalid     = errors.New("Invalid project operation request")
	ErrOperationConflict    = errors.New("Project operation changed; reload and review")
	ErrOperationBusy        = errors.New("A project operation is already active")
	ErrOperationLimit       = errors.New("Project operation capacity reached; dismiss settled metadata")
	ErrOperationUnavailable = errors.New("Project operations are unavailable; no fallback was attempted")
	ErrOperationNotFound    = errors.New("Project operation not found")
)

// DirectoryIdentity records an opened host-user directory, not a sandbox grant.
type DirectoryIdentity struct {
	Path   string `json:"path"`
	Device string `json:"device"`
	Inode  string `json:"inode"`
}

type ProjectOperation struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	State     string            `json:"state"`
	Revision  int64             `json:"revision"`
	CreatedAt int64             `json:"created_at"`
	UpdatedAt int64             `json:"updated_at"`
	Parent    DirectoryIdentity `json:"parent"`
	Child     DirectoryIdentity `json:"child"`
	Remote    string            `json:"remote,omitempty"`
	Outcome   string            `json:"outcome"`
	ProjectID string            `json:"project_id,omitempty"`
	Error     string            `json:"error,omitempty"`
	// Fingerprint is not a browser credential and is never projected publicly.
	Fingerprint string `json:"fingerprint,omitempty"`
}

type ParentSelection struct {
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
	ExpiresAt   int64  `json:"expires_at"`
	Authority   string `json:"authority"`
}

type ProjectOperationRequest struct{ OperationID, Kind, Name, Remote string }

type ProjectOperationTerminal struct{ State, Outcome string }

// ProjectOperationWorker owns exactly one prepared destination. Start is the
// manager's durable identity ACK; clone network work MUST wait for it. Wait
// returns only after execution cleanup, never merely a clone-start ACK.
type ProjectOperationWorker interface {
	Prepare(context.Context, ProjectOperation) (DirectoryIdentity, error)
	Start(context.Context, ProjectOperation) error
	Wait(context.Context) (ProjectOperationTerminal, error)
	Cancel(context.Context) error
	Close() error
}
type ProjectOperationBackend interface {
	Open(context.Context, ProjectOperation) (ProjectOperationWorker, error)
}

type parentGrant struct {
	browser, instance string
	identity          DirectoryIdentity
	expires           time.Time
}
type operationRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// ProjectOperations owns grants and jobs for one manager instance. Browser
// disconnects do not cancel jobs; shutdown cancels and joins every worker.
type ProjectOperations struct {
	registry *Registry
	backend  ProjectOperationBackend
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	grants   map[string]parentGrant
	active   map[string]*operationRun
	instance string
	now      func() time.Time
	closed   bool
	wg       sync.WaitGroup
}

func NewProjectOperations(ctx context.Context, registry *Registry, backend ProjectOperationBackend) (*ProjectOperations, error) {
	if registry == nil || ctx == nil {
		return nil, ErrOperationUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &ProjectOperations{registry: registry, backend: backend, ctx: ctx, cancel: cancel, grants: make(map[string]parentGrant), active: make(map[string]*operationRun), instance: randomToken(), now: time.Now}
	if err := registry.interruptProjectOperations(ctx); err != nil {
		cancel()
		return nil, err
	}
	return m, nil
}
func (m *ProjectOperations) Close() error {
	m.mu.Lock()
	m.closed = true
	clear(m.grants)
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}

func openDirectoryIdentity(path string) (DirectoryIdentity, error) {
	if !validOperationPath(path) {
		return DirectoryIdentity{}, ErrOperationInvalid
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || !validOperationPath(canonical) {
		return DirectoryIdentity{}, ErrOperationInvalid
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return DirectoryIdentity{}, ErrOperationInvalid
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() {
		return DirectoryIdentity{}, ErrOperationInvalid
	}
	dev, ino, ok := registryIdentity(info)
	if !ok {
		return DirectoryIdentity{}, ErrOperationInvalid
	}
	identity := DirectoryIdentity{canonical, dev, ino}
	if !matchesDirectoryIdentity(identity) {
		return DirectoryIdentity{}, ErrOperationConflict
	}
	return identity, nil
}
func matchesDirectoryIdentity(d DirectoryIdentity) bool {
	if !validDirectoryIdentity(d) {
		return false
	}
	canonical, err := filepath.EvalSymlinks(d.Path)
	if err != nil || canonical != d.Path {
		return false
	}
	root, err := os.OpenRoot(d.Path)
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() {
		return false
	}
	dev, ino, ok := registryIdentity(info)
	if !ok || dev != d.Device || ino != d.Inode {
		return false
	}
	current, err := os.Lstat(d.Path)
	return err == nil && current.IsDir() && os.SameFile(info, current)
}
func validOperationPath(path string) bool {
	if len(path) == 0 || len(path) > 4096 || !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validDirectoryIdentity(d DirectoryIdentity) bool {
	if !validOperationPath(d.Path) {
		return false
	}
	for _, v := range []string{d.Device, d.Inode} {
		if len(v) == 0 || len(v) > 20 {
			return false
		}
		for _, r := range v {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
func validOperationName(name string) bool {
	return validProjectName(name) && strings.TrimSpace(name) == name && name != "." && name != ".." && !strings.ContainsAny(name, "/\\")
}

// reviewedOperationRemote deliberately accepts a small credential-free HTTPS
// locator grammar only. Rejected raw URLs are never persisted or returned.
func reviewedOperationRemote(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > 512 {
		return "", ErrOperationInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.RawPath != "" || u.Port() != "" || u.Hostname() == "" || u.Host != u.Hostname() {
		return "", ErrOperationInvalid
	}
	for _, r := range u.Host {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return "", ErrOperationInvalid
		}
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 || len(parts) > 8 {
		return "", ErrOperationInvalid
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || len(p) > 128 {
			return "", ErrOperationInvalid
		}
		for _, r := range p {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
				return "", ErrOperationInvalid
			}
		}
	}
	return u.String(), nil
}

func (m *ProjectOperations) SelectParent(ctx context.Context, browserID, path string) (ParentSelection, error) {
	if browserID == "" || len(browserID) > 128 {
		return ParentSelection{}, ErrOperationInvalid
	}
	if err := ctx.Err(); err != nil {
		return ParentSelection{}, err
	}
	identity, err := openDirectoryIdentity(path)
	if err != nil {
		return ParentSelection{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil || m.backend == nil {
		return ParentSelection{}, ErrOperationUnavailable
	}
	count := 0
	now := m.now()
	for id, g := range m.grants {
		if !now.Before(g.expires) {
			delete(m.grants, id)
		} else if g.browser == browserID {
			count++
		}
	}
	if count >= 32 || len(m.grants) >= 128 {
		return ParentSelection{}, ErrOperationLimit
	}
	id := uuid.New().String()
	expires := now.Add(5 * time.Minute)
	m.grants[id] = parentGrant{browserID, m.instance, identity, expires}
	return ParentSelection{id, identity.Path, expires.UnixMilli(), "host-user OS authority; not a filesystem sandbox"}, nil
}
func operationFingerprint(browser string, req ProjectOperationRequest) string {
	data, _ := json.Marshal([]string{browser, req.OperationID, req.Kind, req.Name, req.Remote})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (m *ProjectOperations) Admit(ctx context.Context, browserID string, req ProjectOperationRequest) (ProjectOperation, error) {
	if !validProjectID(req.OperationID) || !validOperationName(req.Name) || (req.Kind != "create" && req.Kind != "clone") {
		return ProjectOperation{}, ErrOperationInvalid
	}
	if req.Kind == "clone" {
		remote, err := reviewedOperationRemote(req.Remote)
		if err != nil {
			return ProjectOperation{}, err
		}
		req.Remote = remote
	} else if req.Remote != "" {
		return ProjectOperation{}, ErrOperationInvalid
	}
	fingerprint := operationFingerprint(browserID, req)
	m.mu.Lock()
	defer m.mu.Unlock()
	// Durable idempotency precedes grant lookup, but is bound to this browser ID.
	if old, err := m.registry.operationGet(ctx, req.OperationID); err == nil {
		if old.Fingerprint != fingerprint {
			return ProjectOperation{}, ErrOperationConflict
		}
		return publicOperation(old), nil
	} else if !errors.Is(err, ErrOperationNotFound) {
		return ProjectOperation{}, err
	}
	if m.closed || m.ctx.Err() != nil || m.backend == nil {
		return ProjectOperation{}, ErrOperationUnavailable
	}
	grant, ok := m.grants[req.OperationID]
	if !ok || grant.browser != browserID || grant.instance != m.instance || !m.now().Before(grant.expires) {
		return ProjectOperation{}, ErrOperationConflict
	}
	if len(m.active) != 0 {
		return ProjectOperation{}, ErrOperationBusy
	}
	if !matchesDirectoryIdentity(grant.identity) {
		delete(m.grants, req.OperationID)
		return ProjectOperation{}, ErrOperationConflict
	}
	childPath := filepath.Join(grant.identity.Path, req.Name)
	if !validOperationPath(childPath) {
		return ProjectOperation{}, ErrOperationInvalid
	}
	now := m.now().UnixMilli()
	op := ProjectOperation{ID: req.OperationID, Kind: req.Kind, Name: req.Name, State: "admitted", Revision: 1, CreatedAt: now, UpdatedAt: now, Parent: grant.identity, Remote: req.Remote, Outcome: "not_observed", Fingerprint: fingerprint}
	// Consume even when persistence has an uncertain failure; no rejected request
	// can acquire this ID again. A successful insert is recoverable by duplicate.
	delete(m.grants, req.OperationID)
	if err := m.registry.operationInsert(ctx, op); err != nil {
		return ProjectOperation{}, err
	}
	runCtx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
	run := &operationRun{cancel: cancel, done: make(chan struct{})}
	m.active[op.ID] = run
	m.wg.Go(func() {
		defer close(run.done)
		defer cancel()
		m.run(runCtx, op)
		m.mu.Lock()
		delete(m.active, op.ID)
		m.mu.Unlock()
	})
	return publicOperation(op), nil
}
func publicOperation(op ProjectOperation) ProjectOperation { op.Fingerprint = ""; return op }
func operationActive(state string) bool {
	return state == "admitted" || state == "creating" || state == "running" || state == "cancel_requested"
}
