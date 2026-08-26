// Package process owns bounded background subprocesses for one Snow runtime.
// Handles are opaque and valid only in the bound session and current process.
package process

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	DefaultMaxRunning          = 4
	DefaultMaxRecords          = 32
	DefaultRetainedOutputBytes = 1 << 20
	DefaultLogReadBytes        = 32 << 10
	DefaultReadinessTimeout    = 30 * time.Second
	MaxReadinessTimeout        = 120 * time.Second
	DefaultLogWait             = 0
	MaxLogWait                 = 30 * time.Second
	DefaultStopGrace           = 2 * time.Second
	MaxStopGrace               = 30 * time.Second
	DefaultShutdownTimeout     = 10 * time.Second
)

var processNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type Options struct {
	CWD                 string
	MaxRunning          int
	MaxRecords          int
	RetainedOutputBytes int
	MaxLogReadBytes     int
}

type State struct {
	ProcessID  string `json:"process_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Signal     string `json:"signal,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Ready      bool   `json:"ready,omitempty"`
}

type StartRequest struct {
	Command   string
	Name      string
	Readiness *ReadinessRequest
}

type ReadinessRequest struct {
	Type      string `json:"type"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	URL       string `json:"url,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type LogsRequest struct {
	ProcessID string
	Cursor    *int64
	MaxBytes  int
	Wait      time.Duration
}

type LogsResult struct {
	ProcessID  string `json:"process_id"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	NextCursor int64  `json:"next_cursor"`
	Omitted    int64  `json:"omitted_bytes,omitempty"`
	EOF        bool   `json:"eof"`
}

type Manager struct {
	mu        sync.Mutex
	opts      Options
	sessionID string
	records   map[string]*runtimeProcess
	order     []string
	rebinding bool
	closed    bool
	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func NewManager(opts Options) *Manager {
	if opts.MaxRunning <= 0 {
		opts.MaxRunning = DefaultMaxRunning
	}
	if opts.MaxRecords < opts.MaxRunning {
		opts.MaxRecords = max(DefaultMaxRecords, opts.MaxRunning)
	}
	if opts.RetainedOutputBytes <= 0 {
		opts.RetainedOutputBytes = DefaultRetainedOutputBytes
	}
	if opts.MaxLogReadBytes <= 0 {
		opts.MaxLogReadBytes = DefaultLogReadBytes
	}
	return &Manager{opts: opts, records: make(map[string]*runtimeProcess), closeDone: make(chan struct{})}
}

func (m *Manager) BindSession(sessionID string) error {
	if sessionID == "" {
		return errors.New("process manager: session id is empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("process manager is closed")
	}
	if m.rebinding {
		return errors.New("process manager: session switch already in progress")
	}
	if m.hasRunningLocked() {
		return errors.New("process manager: cannot bind session while processes are running")
	}
	m.bindSessionLocked(sessionID)
	return nil
}

// RebindSession stops every running process owned by the current session,
// clears its runtime-only inventory, and binds the manager to sessionID.
func (m *Manager) RebindSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errors.New("process manager: session id is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("process manager is closed")
	}
	if m.rebinding {
		m.mu.Unlock()
		return errors.New("process manager: session switch already in progress")
	}
	m.rebinding = true
	records := make([]*runtimeProcess, 0, len(m.records))
	for _, record := range m.records {
		if record.hasLiveGroup() {
			records = append(records, record)
		}
	}
	m.mu.Unlock()

	err := stopProcessRecords(ctx, records, DefaultStopGrace, "session_switch")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rebinding = false
	if err != nil {
		return fmt.Errorf("process manager: stop processes for session switch: %w", err)
	}
	if m.closed {
		return errors.New("process manager is closed")
	}
	m.bindSessionLocked(sessionID)
	return nil
}

func (m *Manager) bindSessionLocked(sessionID string) {
	m.sessionID = sessionID
	m.records = make(map[string]*runtimeProcess)
	m.order = nil
}

func (m *Manager) HasRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hasRunningLocked()
}

// HasRecords reports whether the active session retains any managed-process
// lifecycle state, including exited processes whose logs remain readable.
func (m *Manager) HasRecords() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records) > 0
}

func (m *Manager) hasRunningLocked() bool {
	for _, record := range m.records {
		if record.hasLiveGroup() {
			return true
		}
	}
	return false
}

func (m *Manager) Start(ctx context.Context, req StartRequest, progress func(string)) (State, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateStartRequest(req); err != nil {
		return State{}, err
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	id, err := newProcessID()
	if err != nil {
		return State{}, fmt.Errorf("process start: generate id: %w", err)
	}
	name := req.Name
	if name == "" {
		name = "process-" + id[len(id)-8:]
	}
	record := newRuntimeProcess(id, name, m.opts.CWD, req.Command, m.opts.RetainedOutputBytes)

	if progress != nil {
		progress("starting process")
	}
	// Keep manager admission locked through cmd.Start so Close cannot observe an
	// admitted record whose OS process has not been installed yet.
	m.mu.Lock()
	if err := m.admitLocked(record); err != nil {
		m.mu.Unlock()
		return State{}, err
	}
	if err := record.start(); err != nil {
		m.removeLocked(id)
		m.mu.Unlock()
		return State{}, fmt.Errorf("process start: %w", err)
	}
	m.mu.Unlock()
	if req.Readiness != nil {
		if progress != nil {
			progress("waiting for readiness")
		}
		if err := waitForReadiness(ctx, record, *req.Readiness); err != nil {
			stopErr := record.stop(context.Background(), DefaultStopGrace, "readiness_failed")
			state := record.state()
			tail := sanitizeUTF8(record.output.tail(4096), 4096)
			readinessErr := fmt.Errorf("process readiness: %w", err)
			if tail != "" {
				readinessErr = fmt.Errorf("%w; recent output: %s", readinessErr, tail)
			}
			if stopErr != nil {
				stopErr = fmt.Errorf("process readiness cleanup: %w", stopErr)
			}
			return state, errors.Join(readinessErr, stopErr)
		}
		record.markReady()
		if progress != nil {
			progress("process ready")
		}
	}
	if err := ctx.Err(); err != nil {
		stopErr := record.stop(context.Background(), DefaultStopGrace, "start_cancelled")
		if stopErr != nil {
			stopErr = fmt.Errorf("process start cancellation cleanup: %w", stopErr)
		}
		return record.state(), errors.Join(err, stopErr)
	}
	m.evictTerminal()
	return record.state(), nil
}

func validateStartRequest(req StartRequest) error {
	if req.Command == "" {
		return errors.New("process start: command is required")
	}
	if len(req.Command) > 64<<10 {
		return errors.New("process start: command exceeds 64 KiB")
	}
	if req.Name != "" && !processNameRE.MatchString(req.Name) {
		return errors.New("process start: name must match [a-z][a-z0-9_-]{0,63}")
	}
	return validateReadiness(req.Readiness)
}

func (m *Manager) admitLocked(record *runtimeProcess) error {
	if m.closed {
		return errors.New("process manager is closed")
	}
	if m.rebinding {
		return errors.New("process manager: session switch in progress")
	}
	if m.sessionID == "" {
		return errors.New("process manager is not bound to a session")
	}
	running := 0
	for _, existing := range m.records {
		if existing.hasLiveGroup() {
			running++
			if existing.name == record.name {
				return fmt.Errorf("process start: running process name %q already exists", record.name)
			}
		}
	}
	if running >= m.opts.MaxRunning {
		return fmt.Errorf("process start: running process limit %d reached", m.opts.MaxRunning)
	}
	record.sessionID = m.sessionID
	m.records[record.id] = record
	m.order = append(m.order, record.id)
	m.evictTerminalLocked()
	return nil
}

func (m *Manager) Status(processID string) (State, error) {
	record, err := m.lookup(processID)
	if err != nil {
		return State{}, err
	}
	return record.state(), nil
}

func (m *Manager) List() []State {
	m.mu.Lock()
	states := make([]State, 0, len(m.records))
	for _, record := range m.records {
		if record.sessionID == m.sessionID {
			states = append(states, record.state())
		}
	}
	m.mu.Unlock()
	sort.Slice(states, func(i, j int) bool {
		iRunning := states[i].Status == "running"
		jRunning := states[j].Status == "running"
		if iRunning != jRunning {
			return iRunning
		}
		return states[i].StartedAt > states[j].StartedAt
	})
	return states
}

func (m *Manager) Logs(ctx context.Context, req LogsRequest) (LogsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	record, err := m.lookup(req.ProcessID)
	if err != nil {
		return LogsResult{}, err
	}
	maxBytes := req.MaxBytes
	if maxBytes > 0 && maxBytes < utf8.UTFMax {
		return LogsResult{}, fmt.Errorf("process logs: max_bytes must be at least %d", utf8.UTFMax)
	}
	if maxBytes <= 0 {
		maxBytes = min(DefaultLogReadBytes, m.opts.MaxLogReadBytes)
	}
	if maxBytes > m.opts.MaxLogReadBytes {
		maxBytes = m.opts.MaxLogReadBytes
	}
	wait := req.Wait
	if wait < 0 {
		return LogsResult{}, errors.New("process logs: wait must not be negative")
	}
	if wait > MaxLogWait {
		wait = MaxLogWait
	}
	deadline := time.NewTimer(wait)
	if wait == 0 {
		if !deadline.Stop() {
			<-deadline.C
		}
	}
	for {
		state := record.state()
		terminal := state.Status != "running"
		read, err := record.output.read(req.Cursor, maxBytes, terminal)
		if err != nil {
			return LogsResult{}, err
		}
		if read.hasOutput || terminal || wait == 0 {
			return LogsResult{ProcessID: record.id, Status: state.Status, Output: sanitizeUTF8(read.data, maxBytes), NextCursor: read.next, Omitted: read.omitted, EOF: terminal && read.next == read.end}, nil
		}
		select {
		case <-ctx.Done():
			return LogsResult{}, ctx.Err()
		case <-record.done:
		case <-read.notify:
		case <-deadline.C:
			wait = 0
		}
	}
}

func (m *Manager) Stop(ctx context.Context, processID string, grace time.Duration) (State, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	record, err := m.lookup(processID)
	if err != nil {
		return State{}, err
	}
	if grace <= 0 {
		grace = DefaultStopGrace
	}
	if grace > MaxStopGrace {
		grace = MaxStopGrace
	}
	err = record.stop(ctx, grace, "stop_requested")
	return record.state(), err
}

func (m *Manager) lookup(processID string) (*runtimeProcess, error) {
	if processID == "" {
		return nil, errors.New("process id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[processID]
	if !ok || record.sessionID != m.sessionID {
		return nil, errors.New("process not found in this runtime/session")
	}
	return record, nil
}

func (m *Manager) removeLocked(id string) {
	delete(m.records, id)
	for i, existing := range m.order {
		if existing == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
}

func (m *Manager) evictTerminal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evictTerminalLocked()
}

func (m *Manager) evictTerminalLocked() {
	for len(m.records) > m.opts.MaxRecords {
		removed := false
		for i, id := range m.order {
			record := m.records[id]
			if record != nil && !record.hasLiveGroup() {
				delete(m.records, id)
				m.order = append(m.order[:i], m.order[i+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
}

func stopProcessRecords(ctx context.Context, records []*runtimeProcess, grace time.Duration, reason string) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(records))
	for _, record := range records {
		wg.Add(1)
		go func(record *runtimeProcess) {
			defer wg.Done()
			if err := record.stop(ctx, grace, reason); err != nil {
				errs <- err
			}
		}(record)
	}
	wg.Wait()
	close(errs)
	var joined []error
	for err := range errs {
		joined = append(joined, err)
	}
	return errors.Join(joined...)
}

func (m *Manager) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		records := make([]*runtimeProcess, 0, len(m.records))
		for _, record := range m.records {
			if record.hasLiveGroup() {
				records = append(records, record)
			}
		}
		m.mu.Unlock()
		go func() {
			err := stopProcessRecords(context.Background(), records, DefaultStopGrace, "shutdown")
			m.mu.Lock()
			m.closeErr = err
			m.mu.Unlock()
			close(m.closeDone)
		}()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.closeDone:
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.closeErr
	}
}

func newProcessID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "proc_" + hex.EncodeToString(raw[:]), nil
}
