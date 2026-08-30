package snowsdk

import (
	"context"
	"time"
)

// DebugStatus describes the bounded shared event recorder. A disabled recorder
// can still retain earlier events until ClearDebugEvents is called.
type DebugStatus struct {
	Enabled       bool      `json:"enabled"`
	StartedAt     time.Time `json:"started_at,omitzero"`
	EventCount    int       `json:"event_count"`
	RetainedBytes int       `json:"retained_bytes"`
	DroppedEvents uint64    `json:"dropped_events"`
	MaxEvents     int       `json:"max_events"`
	MaxBytes      int       `json:"max_bytes"`
}

// DebugStatus returns current shared diagnostics capture state.
func (s *Session) DebugStatus() (DebugStatus, error) {
	a, err := s.activeApp()
	if err != nil {
		return DebugStatus{}, err
	}
	status := a.DebugStatus().Status
	return DebugStatus{Enabled: status.Enabled, StartedAt: status.StartedAt, EventCount: status.EventCount, RetainedBytes: status.RetainedBytes, DroppedEvents: status.DroppedEvents, MaxEvents: status.MaxEvents, MaxBytes: status.MaxBytes}, nil
}

// SetDebugEnabled toggles capture for this runtime only. It deliberately does
// not mutate the operator's persisted config from an embedding application.
func (s *Session) SetDebugEnabled(enabled bool) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	a.SetDebugEnabled(enabled)
	return nil
}

// ClearDebugEvents discards retained normalized runtime events without
// changing whether future events are captured.
func (s *Session) ClearDebugEvents(ctx context.Context) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	return a.ClearDebugEvents(ctx)
}

// CreateDebugDump writes a mode-0600 JSON snapshot containing full session
// content, runtime events, tool activity, and errors. Known credentials and
// provider-private continuity data are always excluded. An empty path selects
// a unique file under $SNOW_HOME/diagnostics.
func (s *Session) CreateDebugDump(ctx context.Context, path string) (string, error) {
	a, err := s.activeApp()
	if err != nil {
		return "", err
	}
	return a.CreateDebugDump(ctx, path)
}
