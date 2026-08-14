package agent

import (
	"strings"

	planpkg "github.com/snow-core/snow/internal/plan"
	"github.com/snow-core/snow/pkg/protocol"
)

type planStreamCollector struct {
	enabled   bool
	parser    planpkg.Parser
	itemID    string
	blocks    []protocol.ContentBlock
	blockText []*strings.Builder
	planText  strings.Builder
	started   bool
	closed    bool
	completed bool
	publish   func(protocol.AgentEvent)
	onStart   func()
}

func newPlanStreamCollector(enabled bool, itemID string, publish func(protocol.AgentEvent), onStart func()) *planStreamCollector {
	return &planStreamCollector{enabled: enabled, itemID: itemID, publish: publish, onStart: onStart}
}

func (c *planStreamCollector) Push(text string) {
	if !c.enabled {
		c.append(protocol.BlockText, text)
		if text != "" {
			c.publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: text})
		}
		return
	}
	c.consume(c.parser.Push(text))
}

func (c *planStreamCollector) Finish() {
	if !c.enabled {
		return
	}
	c.consume(c.parser.Finish())
	if c.started && c.closed && !c.completed && strings.TrimSpace(c.planText.String()) != "" {
		c.completed = true
		for i := range c.blocks {
			if c.blocks[i].Type == protocol.BlockPlan {
				c.blocks[i].PlanComplete = true
			}
		}
	}
}

// PublishCompleted emits the authoritative lifecycle event only after the
// assistant message containing the completed BlockPlan is durable.
func (c *planStreamCollector) PublishCompleted() {
	if c.completed {
		c.publish(protocol.AgentEvent{Type: protocol.EvPlanCompleted, Plan: &protocol.PlanItem{ID: c.itemID, Text: c.planText.String()}})
	}
}

func (c *planStreamCollector) Interrupt() {
	if c.enabled {
		c.consume(c.parser.Interrupt())
	}
}

func (c *planStreamCollector) consume(segments []planpkg.Segment) {
	for _, segment := range segments {
		switch segment.Kind {
		case planpkg.Normal:
			c.append(protocol.BlockText, segment.Text)
			if segment.Text != "" {
				c.publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: segment.Text})
			}
		case planpkg.ProposedPlanStart:
			c.started = true
			if c.onStart != nil {
				c.onStart()
			}
			c.publish(protocol.AgentEvent{Type: protocol.EvPlanStarted, Plan: &protocol.PlanItem{ID: c.itemID}})
		case planpkg.ProposedPlanDelta:
			c.planText.WriteString(segment.Text)
			c.append(protocol.BlockPlan, segment.Text)
			c.publish(protocol.AgentEvent{Type: protocol.EvPlanDelta, Text: segment.Text, Plan: &protocol.PlanItem{ID: c.itemID}})
		case planpkg.ProposedPlanEnd:
			c.closed = true
		}
	}
}

func (c *planStreamCollector) append(kind protocol.ContentBlockType, text string) {
	if text == "" {
		return
	}
	if len(c.blocks) > 0 && c.blocks[len(c.blocks)-1].Type == kind {
		c.blockText[len(c.blockText)-1].WriteString(text)
		return
	}
	var builder strings.Builder
	builder.WriteString(text)
	c.blocks = append(c.blocks, protocol.ContentBlock{Type: kind})
	c.blockText = append(c.blockText, &builder)
}

func (c *planStreamCollector) Blocks() []protocol.ContentBlock {
	out := make([]protocol.ContentBlock, len(c.blocks))
	copy(out, c.blocks)
	for i := range out {
		out[i].Text = c.blockText[i].String()
	}
	return out
}
