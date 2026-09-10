package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	transcriptSelectionAutoScrollInterval = 8 * time.Millisecond
	transcriptSelectionMultiClickInterval = 400 * time.Millisecond
)

type transcriptSelectionGranularity uint8

const (
	transcriptSelectionCharacter transcriptSelectionGranularity = iota
	transcriptSelectionWord
	transcriptSelectionLine
)

type transcriptSelectionPoint struct {
	row      int
	col      int
	boundary bool
}

type transcriptSelectionRange struct {
	start transcriptSelectionPoint
	end   transcriptSelectionPoint
}

type transcriptSelectionClick struct {
	at        time.Time
	count     int
	row       int
	wordStart int
	wordEnd   int
}

type transcriptSelectionContextMenu struct {
	open         bool
	x            int
	y            int
	width        int
	height       int
	selectedText string
}

type transcriptSelectionState struct {
	anchor          *transcriptSelectionPoint
	focus           *transcriptSelectionPoint
	initial         *transcriptSelectionRange
	granularity     transcriptSelectionGranularity
	pressActive     bool
	dragged         bool
	lastClick       *transcriptSelectionClick
	autoScroll      int
	autoScrollStep  int
	autoScrollTicks int
	autoScrollX     int
	autoScrollY     int
	autoScrollID    uint64
}

type transcriptSelectionAutoScrollMsg uint64

type transcriptSelectionCopiedMsg struct {
	characters    int
	terminalWrite tea.Cmd
	err           error
}

type transcriptWordSegment struct {
	start int
	end   int
	kind  uint8
}
