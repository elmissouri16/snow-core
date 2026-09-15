//go:build darwin || linux

package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"
)

type operationFakeBackend struct {
	registry     *Registry
	opened       atomic.Int32
	prepared     atomic.Int32
	started      atomic.Int32
	closed       atomic.Int32
	prepareGate  chan struct{}
	terminal     chan ProjectOperationTerminal
	prepareError bool
	loseWait     bool
	onStart      func(ProjectOperation) error
}

func (b *operationFakeBackend) Open(ctx context.Context, op ProjectOperation) (ProjectOperationWorker, error) {
	b.opened.Add(1)
	return &operationFakeWorker{backend: b}, nil
}

type operationFakeWorker struct {
	backend   *operationFakeBackend
	id        string
	closeOnce sync.Once
}

func (w *operationFakeWorker) Prepare(ctx context.Context, op ProjectOperation) (DirectoryIdentity, error) {
	w.id = op.ID
	stored, err := w.backend.registry.operationGet(ctx, op.ID)
	if err != nil || stored.State != "creating" {
		return DirectoryIdentity{}, errors.New("creating must be durable")
	}
	w.backend.prepared.Add(1)
	if w.backend.prepareGate != nil {
		select {
		case <-w.backend.prepareGate:
		case <-ctx.Done():
			return DirectoryIdentity{}, ctx.Err()
		}
	}
	if w.backend.prepareError {
		return DirectoryIdentity{}, errors.New("lost prepare acknowledgement")
	}
	path := filepath.Join(op.Parent.Path, op.Name)
	if err := os.Mkdir(path, 0o700); err != nil {
		return DirectoryIdentity{}, err
	}
	return openDirectoryIdentity(path)
}
func (w *operationFakeWorker) Start(ctx context.Context, op ProjectOperation) error {
	stored, err := w.backend.registry.operationGet(ctx, op.ID)
	if err != nil || stored.State != "running" || stored.Child != op.Child || !validDirectoryIdentity(stored.Child) {
		return errors.New("child must be durable before ACK")
	}
	w.backend.started.Add(1)
	if w.backend.onStart != nil {
		return w.backend.onStart(op)
	}
	return nil
}
func (w *operationFakeWorker) Wait(ctx context.Context) (ProjectOperationTerminal, error) {
	if w.backend.loseWait {
		return ProjectOperationTerminal{}, errors.New("worker lost")
	}
	select {
	case result := <-w.backend.terminal:
		return result, nil
	case <-ctx.Done():
		return ProjectOperationTerminal{}, ctx.Err()
	}
}
func (w *operationFakeWorker) Cancel(context.Context) error {
	if !w.backend.loseWait {
		select {
		case w.backend.terminal <- ProjectOperationTerminal{"canceled", "observed"}:
		default:
		}
	}
	return nil
}
func (w *operationFakeWorker) Close() error {
	w.closeOnce.Do(func() { w.backend.closed.Add(1) })
	return nil
}

func newOperationTestManager(t *testing.T) (*ProjectOperations, *operationFakeBackend, string) {
	t.Helper()
	base := t.TempDir()
	parent := registryTestDirectory(t, base, "parent")
	r := registryTestOpen(t, filepath.Join(base, "manager"))
	backend := &operationFakeBackend{registry: r, terminal: make(chan ProjectOperationTerminal, 4)}
	m, err := NewProjectOperations(t.Context(), r, backend)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m, backend, parent
}
func selectOperation(t *testing.T, m *ProjectOperations, parent, name, kind string) ProjectOperationRequest {
	t.Helper()
	grant, err := m.SelectParent(t.Context(), "browser-one", parent)
	if err != nil {
		t.Fatal(err)
	}
	req := ProjectOperationRequest{OperationID: grant.OperationID, Name: name, Kind: kind}
	if kind == "clone" {
		req.Remote = "https://example.com/owner/repository.git"
	}
	return req
}
func awaitOperation(t *testing.T, m *ProjectOperations, id string, state string) ProjectOperation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		op, err := m.Get(t.Context(), id)
		if err == nil && op.State == state {
			return op
		}
		time.Sleep(time.Millisecond)
	}
	op, err := m.Get(t.Context(), id)
	t.Fatalf("operation never reached %s: %+v, %v", state, op, err)
	return ProjectOperation{}
}
func TestProjectOperationsDurableCreateAndDuplicate(t *testing.T) {
	m, b, parent := newOperationTestManager(t)
	req := selectOperation(t, m, parent, "new-project", "create")
	op, err := m.Admit(t.Context(), "browser-one", req)
	if err != nil || op.State != "admitted" || op.Fingerprint != "" {
		t.Fatalf("admit: %+v %v", op, err)
	}
	awaitOperation(t, m, op.ID, "running")
	deadlineACK := time.Now().Add(time.Second)
	for b.started.Load() != 1 && time.Now().Before(deadlineACK) {
		time.Sleep(time.Millisecond)
	}
	if b.started.Load() != 1 {
		t.Fatal("durable identity ACK not sent")
	}
	duplicate, err := m.Admit(t.Context(), "browser-one", req)
	if err != nil || duplicate.ID != op.ID {
		t.Fatalf("duplicate %+v %v", duplicate, err)
	}
	req.Name = "other"
	if _, err := m.Admit(t.Context(), "browser-one", req); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("conflicting duplicate %v", err)
	}
	req.Name = "new-project"
	if _, err := m.Admit(t.Context(), "browser-two", req); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("wrong browser duplicate %v", err)
	}
	b.terminal <- ProjectOperationTerminal{"succeeded", "observed"}
	retained := awaitOperation(t, m, op.ID, "awaiting_registration")
	if b.closed.Load() != 1 || b.opened.Load() != 1 || retained.ProjectID != "" || retained.Outcome != "observed" {
		t.Fatalf("bad retained terminal %+v", retained)
	}
	assertOperationRegistryEmpty(t, m.registry)
	settled, err := m.Register(t.Context(), retained.ID, retained.Revision, false)
	if err != nil || settled.State != "succeeded" || settled.ProjectID == "" {
		t.Fatalf("explicit registration %+v %v", settled, err)
	}
	project, err := m.registry.Lookup(t.Context(), settled.ProjectID)
	if err != nil || !project.Available || project.Path != settled.Child.Path {
		t.Fatalf("registration %+v %v", project, err)
	}
	// Dismissal removes metadata only, and consumed IDs cannot replay afterward.
	deadline := time.Now().Add(time.Second)
	for {
		err = m.Dismiss(t.Context(), settled.ID, settled.Revision)
		if !errors.Is(err, ErrOperationBusy) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(project.Path); err != nil {
		t.Fatal("dismiss touched files", err)
	}
	if _, err := m.Admit(t.Context(), "browser-one", req); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("dismiss replay %v", err)
	}
}
func TestProjectOperationGrantsBoundedExpiredAndIdentityPinned(t *testing.T) {
	m, _, parent := newOperationTestManager(t)
	now := time.Now()
	m.now = func() time.Time { return now }
	first := selectOperation(t, m, parent, "one", "create")
	if _, err := m.Admit(t.Context(), "browser-two", first); !errors.Is(err, ErrOperationConflict) {
		t.Fatal(err)
	}
	for range 31 {
		selectOperation(t, m, parent, "unused", "create")
	}
	if _, err := m.SelectParent(t.Context(), "browser-one", parent); !errors.Is(err, ErrOperationLimit) {
		t.Fatalf("browser cap %v", err)
	}
	for _, browser := range []string{"two", "three", "four"} {
		for range 32 {
			if _, err := m.SelectParent(t.Context(), browser, parent); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := m.SelectParent(t.Context(), "five", parent); !errors.Is(err, ErrOperationLimit) {
		t.Fatalf("global cap %v", err)
	}
	now = now.Add(5 * time.Minute)
	if _, err := m.Admit(t.Context(), "browser-one", first); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("expired grant %v", err)
	}
	req := selectOperation(t, m, parent, "replacement", "create")
	if err := os.Rename(parent, parent+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Admit(t.Context(), "browser-one", req); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("identity replacement %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, req.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replacement was mutated")
	}
}
func TestProjectOperationOneGlobalNoQueueAndCancelCAS(t *testing.T) {
	m, b, parent := newOperationTestManager(t)
	first := selectOperation(t, m, parent, "one", "clone")
	second := selectOperation(t, m, parent, "two", "create")
	op, err := m.Admit(t.Context(), "browser-one", first)
	if err != nil {
		t.Fatal(err)
	}
	running := awaitOperation(t, m, op.ID, "running")
	if _, err := m.Admit(t.Context(), "browser-one", second); !errors.Is(err, ErrOperationBusy) {
		t.Fatalf("second active %v", err)
	}
	if _, err := m.Cancel(t.Context(), op.ID, op.Revision); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("stale cancel %v", err)
	}
	canceled, err := m.Cancel(t.Context(), op.ID, running.Revision)
	if err != nil || canceled.State != "cancel_requested" {
		t.Fatalf("cancel %+v %v", canceled, err)
	}
	terminal := awaitOperation(t, m, op.ID, "canceled")
	if terminal.ProjectID != "" || b.closed.Load() != 1 {
		t.Fatalf("early terminal %+v", terminal)
	}
	if _, err := os.Stat(filepath.Join(parent, "one")); err != nil {
		t.Fatal("cancel removed retained destination", err)
	}
}
func TestProjectOperationLostWorkerAndExplicitReviewRegistration(t *testing.T) {
	m, b, parent := newOperationTestManager(t)
	b.loseWait = true
	req := selectOperation(t, m, parent, "retained", "clone")
	op, err := m.Admit(t.Context(), "browser-one", req)
	if err != nil {
		t.Fatal(err)
	}
	retained := awaitOperation(t, m, op.ID, "needs_review")
	if retained.Outcome != "unknown" || retained.ProjectID != "" {
		t.Fatalf("lost worker %+v", retained)
	}
	if _, err := m.Register(t.Context(), retained.ID, retained.Revision, false); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("unreviewed register %v", err)
	}
	reviewed, err := m.Register(t.Context(), retained.ID, retained.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.State != "needs_review" || reviewed.Outcome != "unknown" || reviewed.ProjectID == "" {
		t.Fatalf("retroactive success %+v", reviewed)
	}
}
func TestProjectOperationRegistrationNeverAdoptsReplacement(t *testing.T) {
	m, b, parent := newOperationTestManager(t)
	b.onStart = func(op ProjectOperation) error {
		if err := os.Rename(op.Child.Path, op.Child.Path+"-owned"); err != nil {
			return err
		}
		return os.Mkdir(op.Child.Path, 0o700)
	}
	req := selectOperation(t, m, parent, "replace", "clone")
	op, err := m.Admit(t.Context(), "browser-one", req)
	if err != nil {
		t.Fatal(err)
	}
	awaitOperation(t, m, op.ID, "running")
	b.terminal <- ProjectOperationTerminal{"succeeded", "observed"}
	retained := awaitOperation(t, m, op.ID, "awaiting_registration")
	assertOperationRegistryEmpty(t, m.registry)
	if _, err := m.Register(t.Context(), retained.ID, retained.Revision, false); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("explicit registration adopted replacement: %v", err)
	}
	retained, err = m.Get(t.Context(), op.ID)
	if err != nil || retained.State != "awaiting_registration" || retained.ProjectID != "" || retained.Error != "registration_failed" {
		t.Fatalf("replacement registration %+v %v", retained, err)
	}
	projects, err := m.registry.List(t.Context())
	if err != nil || len(projects) != 0 {
		t.Fatalf("adopted replacement %+v %v", projects, err)
	}
	reconciled, err := m.Reconcile(t.Context(), op.ID, retained.Revision)
	if err != nil || reconciled.State != "needs_review" || reconciled.Outcome != "unknown" {
		t.Fatalf("reconcile %+v %v", reconciled, err)
	}
	if b.opened.Load() != 1 || b.started.Load() != 1 {
		t.Fatal("reconcile reexecuted")
	}
}
func TestProjectOperationRestartInterruptsWithoutExecution(t *testing.T) {
	base := t.TempDir()
	r := registryTestOpen(t, filepath.Join(base, "manager"))
	parent := registryTestDirectory(t, base, "parent")
	identity, err := openDirectoryIdentity(parent)
	if err != nil {
		t.Fatal(err)
	}
	op := ProjectOperation{ID: uuid.New().String(), Kind: "clone", Name: "never-created", State: "creating", Revision: 1, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(), Parent: identity, Remote: "https://example.com/owner/repo", Outcome: "not_observed", Fingerprint: operationFingerprint("owner", ProjectOperationRequest{})}
	if err := r.operationInsert(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	b := &operationFakeBackend{registry: r, terminal: make(chan ProjectOperationTerminal, 1)}
	m, err := NewProjectOperations(t.Context(), r, b)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	interrupted, err := m.Get(t.Context(), op.ID)
	if err != nil || interrupted.State != "interrupted" || interrupted.Outcome != "unknown" {
		t.Fatalf("restart %+v %v", interrupted, err)
	}
	if b.opened.Load() != 0 {
		t.Fatal("startup started worker")
	}
	if _, err := os.Stat(filepath.Join(parent, op.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("startup created directory")
	}
	reconciled, err := m.Reconcile(t.Context(), op.ID, interrupted.Revision)
	if err != nil || reconciled.State != "needs_review" {
		t.Fatalf("reconcile %+v %v", reconciled, err)
	}
	if b.opened.Load() != 0 {
		t.Fatal("reconcile started worker")
	}
}
func TestProjectOperationValidation(t *testing.T) {
	for _, name := range []string{"", ".", "..", " trim", "trim ", "a/b", "a\\b", "a\n", "a\x00"} {
		if validOperationName(name) {
			t.Errorf("accepted leaf %q", name)
		}
	}
	for _, raw := range []string{"https://user:secret@example.com/a/b", "https://example.com/a/b?token=secret", "https://example.com/a/b#secret", "ssh://example.com/a/b", "git@example.com:a/b", "file:///tmp/repo", "https://example.com/a/%62", "https://example.com/a/../b"} {
		if _, err := reviewedOperationRemote(raw); err == nil {
			t.Errorf("accepted locator %q", raw)
		}
	}
	if _, err := reviewedOperationRemote("https://example.com/team/repo.git"); err != nil {
		t.Fatal(err)
	}
}
