// Package diagnostics implements Snow's opt-in, bounded runtime event recorder.
// It never writes by itself; callers explicitly create a diagnostic dump.
package diagnostics

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	// MaxEventRecords bounds event metadata retained between clears.
	MaxEventRecords = 50_000
	// MaxEventBytes bounds encoded event payloads retained between clears.
	MaxEventBytes = 32 << 20
	queueCapacity = 2048
)

// EventRecord is one normalized AgentEvent as observed by the shared runtime.
type EventRecord struct {
	Sequence   uint64          `json:"sequence"`
	RecordedAt time.Time       `json:"recorded_at"`
	Event      json.RawMessage `json:"event"`
}

// Status is a cheap snapshot of the recorder state.
type Status struct {
	Enabled       bool      `json:"enabled"`
	StartedAt     time.Time `json:"started_at,omitzero"`
	EventCount    int       `json:"event_count"`
	RetainedBytes int       `json:"retained_bytes"`
	DroppedEvents uint64    `json:"dropped_events"`
	MaxEvents     int       `json:"max_events"`
	MaxBytes      int       `json:"max_bytes"`
}

type queuedEvent struct {
	recordedAt time.Time
	event      protocol.AgentEvent
}

type flushRequest chan struct{}

// Recorder accepts event callbacks without blocking the agent dispatcher.
type Recorder struct {
	enabled atomic.Bool
	dropped atomic.Uint64
	queue   chan any
	stop    chan struct{}
	done    chan struct{}
	closed  atomic.Bool

	admission   sync.RWMutex
	mu          sync.RWMutex
	startedAt   time.Time
	records     []EventRecord
	recordStart int
	bytes       int
	sequence    uint64
}

// New starts a recorder worker. Disabled recorders remain cheap and can be
// enabled later by the TUI, RPC, or SDK facade.
func New(enabled bool) *Recorder {
	r := &Recorder{queue: make(chan any, queueCapacity), stop: make(chan struct{}), done: make(chan struct{})}
	if enabled {
		r.enabled.Store(true)
		r.startedAt = time.Now().UTC()
	}
	go r.run()
	return r
}

// Record is suitable for agent.Agent.Subscribe. It intentionally drops on a
// saturated queue rather than delaying the serial event stream.
func (r *Recorder) Record(event protocol.AgentEvent) {
	if r == nil {
		return
	}
	r.admission.RLock()
	defer r.admission.RUnlock()
	if !r.enabled.Load() || r.closed.Load() {
		return
	}
	select {
	case r.queue <- queuedEvent{recordedAt: time.Now().UTC(), event: event}:
	default:
		r.dropped.Add(1)
	}
}

// SetEnabled changes capture immediately. Existing records are retained until
// Clear so a user can disable capture and still dump the incident.
func (r *Recorder) SetEnabled(enabled bool) {
	if r == nil {
		return
	}
	r.admission.RLock()
	defer r.admission.RUnlock()
	if r.closed.Load() {
		return
	}
	if enabled && !r.enabled.Swap(true) {
		r.mu.Lock()
		if r.startedAt.IsZero() {
			r.startedAt = time.Now().UTC()
		}
		r.mu.Unlock()
		return
	}
	if !enabled {
		r.enabled.Store(false)
	}
}

// Enabled reports whether new events are being captured.
func (r *Recorder) Enabled() bool { return r != nil && r.enabled.Load() && !r.closed.Load() }

func (r *Recorder) run() {
	defer close(r.done)
	handle := func(item any) {
		switch value := item.(type) {
		case queuedEvent:
			r.append(value)
		case flushRequest:
			close(value)
		}
	}
	for {
		select {
		case item := <-r.queue:
			handle(item)
		case <-r.stop:
			for {
				select {
				case item := <-r.queue:
					handle(item)
				default:
					return
				}
			}
		}
	}
}

func (r *Recorder) append(item queuedEvent) {
	data, err := json.Marshal(item.event)
	if err != nil {
		r.dropped.Add(1)
		return
	}
	if len(data) > MaxEventBytes {
		r.dropped.Add(1)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sequence++
	record := EventRecord{Sequence: r.sequence, RecordedAt: item.recordedAt, Event: data}
	r.records = append(r.records, record)
	r.bytes += len(data)
	for len(r.records)-r.recordStart > MaxEventRecords || r.bytes > MaxEventBytes {
		r.bytes -= len(r.records[r.recordStart].Event)
		r.records[r.recordStart] = EventRecord{}
		r.recordStart++
		r.dropped.Add(1)
	}
	// Compact only occasionally. This keeps sustained over-capacity capture
	// amortized O(1) rather than shifting as many as 50,000 records per event.
	if r.recordStart >= 1024 && r.recordStart*2 >= len(r.records) {
		retained := copy(r.records, r.records[r.recordStart:])
		for i := retained; i < len(r.records); i++ {
			r.records[i] = EventRecord{}
		}
		r.records = r.records[:retained]
		r.recordStart = 0
	}
}

// Flush waits until events already accepted by Record are materialized.
func (r *Recorder) Flush(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ack := make(flushRequest)
	r.admission.RLock()
	if r.closed.Load() {
		r.admission.RUnlock()
		return nil
	}
	select {
	case r.queue <- ack:
		r.admission.RUnlock()
	case <-ctx.Done():
		r.admission.RUnlock()
		return ctx.Err()
	case <-r.done:
		r.admission.RUnlock()
		return nil
	}
	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		return nil
	}
}

// Snapshot returns independent encoded event records and recorder status.
func (r *Recorder) Snapshot(ctx context.Context) ([]EventRecord, Status, error) {
	if r == nil {
		return nil, Status{MaxEvents: MaxEventRecords, MaxBytes: MaxEventBytes}, nil
	}
	if err := r.Flush(ctx); err != nil {
		return nil, Status{}, err
	}
	r.mu.RLock()
	retained := r.records[r.recordStart:]
	records := make([]EventRecord, len(retained))
	for i, record := range retained {
		records[i] = record
		records[i].Event = append(json.RawMessage(nil), record.Event...)
	}
	status := Status{Enabled: r.Enabled(), StartedAt: r.startedAt, EventCount: len(records), RetainedBytes: r.bytes, DroppedEvents: r.dropped.Load(), MaxEvents: MaxEventRecords, MaxBytes: MaxEventBytes}
	r.mu.RUnlock()
	return records, status, nil
}

// Status returns current counters. Events still queued may not be reflected in
// EventCount yet; Snapshot provides a flushed view.
func (r *Recorder) Status() Status {
	if r == nil {
		return Status{MaxEvents: MaxEventRecords, MaxBytes: MaxEventBytes}
	}
	r.mu.RLock()
	status := Status{Enabled: r.Enabled(), StartedAt: r.startedAt, EventCount: len(r.records) - r.recordStart, RetainedBytes: r.bytes, DroppedEvents: r.dropped.Load(), MaxEvents: MaxEventRecords, MaxBytes: MaxEventBytes}
	r.mu.RUnlock()
	return status
}

// Clear discards retained records and resets loss counters without changing
// whether capture is enabled.
func (r *Recorder) Clear(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if err := r.Flush(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	for i := range r.records {
		r.records[i] = EventRecord{}
	}
	r.records = nil
	r.recordStart = 0
	r.bytes = 0
	r.sequence = 0
	if r.Enabled() {
		r.startedAt = time.Now().UTC()
	} else {
		r.startedAt = time.Time{}
	}
	r.dropped.Store(0)
	r.mu.Unlock()
	return nil
}

// Close stops the worker after draining accepted events.
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.admission.Lock()
	if !r.closed.CompareAndSwap(false, true) {
		r.admission.Unlock()
		return
	}
	r.enabled.Store(false)
	close(r.stop)
	r.admission.Unlock()
	<-r.done
}
