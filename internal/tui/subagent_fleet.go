package tui

import (
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	maxFleetActivityLines = 128
	maxFleetActivityBytes = 32 * 1024
	fleetWideMinWidth     = 90
)

type subagentFleetListMsg struct {
	generation      uint64
	target          string
	list            protocol.SubagentList
	historicalCount int
	err             error
}

type subagentFleetDetailMsg struct {
	generation uint64
	target     string
	state      protocol.SubagentState
	messages   []protocol.Message
	messageErr error
	err        error
}

type subagentFleetLayout struct {
	innerWidth   int
	innerHeight  int
	bodyHeight   int
	detailWidth  int
	detailHeight int
	listWidth    int
	listHeight   int
	wide         bool
}
