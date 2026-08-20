package tui

import (
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
)

const (
	processFleetRefreshInterval = 750 * time.Millisecond
	processFleetOutputLimit     = 128 << 10
	processFleetLogReadBytes    = 32 << 10
	processFleetLogBatchChunks  = 4
)

type processFleetListMsg struct {
	generation uint64
	target     string
	list       []app.ManagedProcessState
	err        error
}

type processFleetLogsMsg struct {
	generation uint64
	target     string
	logs       app.ManagedProcessLogs
	err        error
}

type processFleetTickMsg struct {
	generation uint64
	tick       uint64
}

type processFleetLayout struct {
	innerWidth   int
	innerHeight  int
	bodyHeight   int
	listWidth    int
	listHeight   int
	detailWidth  int
	detailHeight int
	wide         bool
}
