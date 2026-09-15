package rpc

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// splitFrame keeps CR in the payload for accurate byte bounds and requires LF,
// unlike ScanLines which accepts an unterminated final frame and strips CR.
func splitFrame(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) != 0 {
		return 0, nil, ErrMalformedFrame
	}
	return 0, nil, nil
}

func (c *Client) readLoop(handshake chan struct{}) {
	defer close(c.readDone)
	defer close(c.events)
	defer c.stopContext()
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, min(64*1024, c.opts.MaxFrameBytes+1)), c.opts.MaxFrameBytes+1)
	scanner.Split(splitFrame)
	ready := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if len(line) > c.opts.MaxFrameBytes {
			c.fail(ErrFrameTooLarge)
			return
		}
		// json/v2 rejects duplicate keys, invalid UTF-8, and trailing values by
		// default. Do not expose decoder diagnostics that can quote peer payloads.
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &header); err != nil || header.Type == "" {
			c.fail(ErrMalformedFrame)
			return
		}
		if !ready {
			if header.Type != protocol.RPCTypeReady {
				c.fail(ErrMalformedFrame)
				return
			}
			var r protocol.RPCReady
			if err := json.Unmarshal(line, &r); err != nil {
				c.fail(ErrMalformedFrame)
				return
			}
			if r.ProtocolVersion != protocol.RPCProtocolVersion {
				c.fail(ErrProtocolVersion)
				return
			}
			if r.MaxInputBytes <= 0 {
				c.fail(ErrMalformedFrame)
				return
			}
			if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
				c.fail(err)
				return
			}
			c.ready = r
			ready = true
			close(handshake)
			continue
		}
		if err := c.dispatch(header.Type, line); err != nil {
			c.fail(err)
			return
		}
	}
	err := scanner.Err()
	if errors.Is(err, bufio.ErrTooLong) {
		err = ErrFrameTooLarge
	}
	if err == nil {
		err = io.EOF
	}
	c.fail(err)
}

func (c *Client) dispatch(kind string, line []byte) error {
	switch kind {
	case protocol.RPCTypeReady:
		return ErrMalformedFrame
	case "response":
		// Presence matters: an omitted/null success must not become a rejection.
		var required struct {
			Success *bool `json:"success"`
		}
		var response protocol.RPCResponse
		if json.Unmarshal(line, &required) != nil || required.Success == nil || json.Unmarshal(line, &response) != nil || response.ID == "" || response.Command == "" {
			return ErrMalformedFrame
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if p := c.pending[response.ID]; p != nil {
			if p.command != response.Command {
				return ErrMalformedFrame
			}
			// Keep the slot until Call exits, bounding received-but-unconsumed replies.
			// A second same-ID legacy prompt failure must not replace the first ack.
			select {
			case p.response <- response:
			default:
			}
		}
		return nil
	case protocol.RPCTypeProjectCompleted:
		var completed protocol.RPCProjectCompleted
		if json.Unmarshal(line, &completed, json.RejectUnknownMembers(true)) != nil || !validProjectCompletion(completed) {
			return ErrMalformedFrame
		}
		return c.publish(Event{ProjectCompleted: &completed})
	case protocol.RPCTypeCompactionCompleted:
		var completed protocol.RPCCompactionCompleted
		var counts struct {
			SummarizedMessages *int  `json:"summarized_messages"`
			RetainedMessages   *int  `json:"retained_messages"`
			UsedFallback       *bool `json:"used_fallback"`
		}
		if json.Unmarshal(line, &completed, json.RejectUnknownMembers(true)) != nil || !validCompactionCompletion(completed) || json.Unmarshal(line, &counts) != nil || counts.SummarizedMessages == nil || counts.RetainedMessages == nil || counts.UsedFallback == nil {
			return ErrMalformedFrame
		}
		return c.publish(Event{CompactionCompleted: &completed})
	case protocol.RPCTypeGoalRunCompleted:
		var completed protocol.RPCGoalRunCompleted
		if json.Unmarshal(line, &completed) != nil || completed.RequestID == "" || completed.GoalRunID == "" || completed.GoalID == "" {
			return ErrMalformedFrame
		}
		switch completed.Status {
		case "finished", "failed", "canceled":
		default:
			return ErrMalformedFrame
		}
		return c.publish(Event{GoalRunCompleted: &completed})
	case protocol.RPCTypePromptCompleted:
		var completed protocol.RPCPromptCompleted
		if json.Unmarshal(line, &completed) != nil || completed.RequestID == "" {
			return ErrMalformedFrame
		}
		switch completed.Status {
		case protocol.RPCPromptCompletedStatus, protocol.RPCPromptFailedStatus, protocol.RPCPromptCanceledStatus:
		default:
			return ErrMalformedFrame
		}
		return c.publish(Event{PromptCompleted: &completed})
	default:
		var event protocol.AgentEvent
		if json.Unmarshal(line, &event) != nil {
			return ErrMalformedFrame
		}
		return c.publish(Event{AgentEvent: &event})
	}
}

func (c *Client) publish(event Event) error {
	select {
	case <-c.done:
		return c.Err()
	default:
	}
	select {
	case c.events <- event:
		return nil
	default:
		return ErrEventOverflow
	}
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, ErrFrameTooLarge
	}
	return b.Buffer.Write(p)
}

func encodeRequest(req protocol.RPCRequest, limit int) ([]byte, error) {
	b := &boundedBuffer{limit: limit}
	if err := json.MarshalWrite(b, req); err != nil {
		if errors.Is(err, ErrFrameTooLarge) {
			return nil, ErrFrameTooLarge
		}
		return nil, errors.New("rpc client: request JSON encoding failed")
	}
	// Keep the delimiter outside the payload bound.
	_ = b.WriteByte('\n')
	return b.Bytes(), nil
}

// Terminal compaction frames contain only public identity and counts. Reject
// malformed identities/statuses and accidental private/error payload members.
func validCompactionCompletion(c protocol.RPCCompactionCompleted) bool {
	for _, id := range []string{c.RequestID, c.CompactionID, c.SessionID, c.BranchID, c.TurnID} {
		if id == "" || len(id) > 256 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\x00\r\n\t") {
			return false
		}
	}
	if c.CompactionID != c.TurnID || c.TurnOrigin != "compact" || c.RootEpoch == 0 || c.TurnSequence == 0 || c.SummarizedMessages < 0 || c.RetainedMessages < 0 {
		return false
	}
	switch c.Status {
	case "completed", "noop", "fallback", "canceled", "failed":
		return true
	default:
		return false
	}
}

// Project operations expose only bounded prepared identity and a fixed status;
// never accept arbitrary Git output, raw errors or private process metadata.
func validProjectCompletion(c protocol.RPCProjectCompleted) bool {
	for _, id := range []string{c.RequestID, c.OperationID} {
		if id == "" || len(id) > 256 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\x00\r\n\t") {
			return false
		}
	}
	if c.Child.Path == "" || len(c.Child.Path) > 4096 || !strings.HasPrefix(c.Child.Path, "/") || path.Clean(c.Child.Path) != c.Child.Path || strings.ContainsRune(c.Child.Path, 0) {
		return false
	}
	for _, id := range []string{c.Child.Device, c.Child.Inode} {
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != id {
			return false
		}
	}
	switch c.Status {
	case protocol.HostOperationSucceeded, protocol.HostOperationFailed, protocol.HostOperationCanceled, protocol.HostOperationTimedOut, protocol.HostOperationOutputLimit, protocol.HostOperationCleanupFailed:
		return true
	default:
		return false
	}
}
