package session

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	hydrationFlagUserVisible uint64 = 1 << iota
	hydrationFlagThinkingVisible
	hydrationFlagStopVisible
	hydrationFlagToolIsError
	hydrationFlagToolOutputPresent
	hydrationFlagToolDisplayPresent
	hydrationFlagToolWasDispatched
	hydrationFlagCompactionActive
)

func marshalHydrationProjection(record entryHydrationRecord) ([]byte, error) {
	summary := record.summary
	var usageJSON []byte
	var err error
	if summary.Usage != nil {
		usageJSON, err = json.Marshal(summary.Usage)
		if err != nil {
			return nil, fmt.Errorf("session: marshal hydration usage: %w", err)
		}
	}
	flags := uint64(0)
	if summary.UserVisible {
		flags |= hydrationFlagUserVisible
	}
	if summary.ThinkingVisible {
		flags |= hydrationFlagThinkingVisible
	}
	if summary.StopVisible {
		flags |= hydrationFlagStopVisible
	}
	if summary.ToolIsError {
		flags |= hydrationFlagToolIsError
	}
	if summary.ToolOutputPresent {
		flags |= hydrationFlagToolOutputPresent
	}
	if summary.ToolDisplayPresent {
		flags |= hydrationFlagToolDisplayPresent
	}
	if summary.ToolWasDispatched {
		flags |= hydrationFlagToolWasDispatched
	}
	if summary.CompactionActive {
		flags |= hydrationFlagCompactionActive
	}

	projection := make([]byte, 0, 64+len(summary.CompactedThrough)+len(usageJSON)+
		len(summary.ToolCallID)+len(summary.ToolName)+len(summary.ToolCallIDs)*16+
		len(record.userInput)+len(record.latestPlan))
	projection = appendHydrationBytes(projection, []byte(summary.Type))
	projection = appendHydrationBytes(projection, []byte(summary.Role))
	projection = binary.AppendUvarint(projection, flags)
	projection = binary.AppendUvarint(projection, uint64(summary.BodyRows))
	projection = binary.AppendUvarint(projection, uint64(summary.ContextChars))
	projection = binary.AppendUvarint(projection, uint64(summary.CompactionCheckpointChars))
	projection = appendHydrationBytes(projection, []byte(summary.CompactedThrough))
	projection = appendHydrationBytes(projection, usageJSON)
	projection = appendHydrationBytes(projection, []byte(summary.ToolCallID))
	projection = appendHydrationBytes(projection, []byte(summary.ToolName))
	projection = binary.AppendUvarint(projection, uint64(len(summary.ToolCallIDs)))
	for _, callID := range summary.ToolCallIDs {
		projection = appendHydrationBytes(projection, []byte(callID))
	}
	projection = appendHydrationBytes(projection, []byte(record.userInput))
	projection = appendHydrationBytes(projection, []byte(record.latestPlan))
	return projection, nil
}

func appendHydrationBytes(dst, value []byte) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(value)))
	return append(dst, value...)
}

type hydrationProjectionReader struct {
	data []byte
}

func (r *hydrationProjectionReader) bytes() ([]byte, error) {
	length, n := binary.Uvarint(r.data)
	if n <= 0 {
		return nil, errors.New("invalid length")
	}
	r.data = r.data[n:]
	if length > uint64(len(r.data)) {
		return nil, errors.New("truncated value")
	}
	value := r.data[:int(length)]
	r.data = r.data[int(length):]
	return value, nil
}

func (r *hydrationProjectionReader) integer() (int, error) {
	value, n := binary.Uvarint(r.data)
	if n <= 0 {
		return 0, errors.New("invalid integer")
	}
	r.data = r.data[n:]
	maxInt := uint64(^uint(0) >> 1)
	if value > maxInt {
		return 0, errors.New("integer overflow")
	}
	return int(value), nil
}

func unmarshalHydrationProjection(data []byte) (entryHydrationRecord, error) {
	reader := hydrationProjectionReader{data: data}
	typeBytes, err := reader.bytes()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("entry type: %w", err)
	}
	roleBytes, err := reader.bytes()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("role: %w", err)
	}
	flags, n := binary.Uvarint(reader.data)
	if n <= 0 {
		return entryHydrationRecord{}, errors.New("flags: invalid integer")
	}
	reader.data = reader.data[n:]
	bodyRows, err := reader.integer()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("body rows: %w", err)
	}
	contextChars, err := reader.integer()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("context chars: %w", err)
	}
	checkpointChars, err := reader.integer()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("checkpoint chars: %w", err)
	}
	compactedThrough, err := reader.bytes()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("compacted through: %w", err)
	}
	usageJSON, err := reader.bytes()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("usage: %w", err)
	}
	toolCallID, err := reader.bytes()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("tool call id: %w", err)
	}
	toolName, err := reader.bytes()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("tool name: %w", err)
	}
	callCount, err := reader.integer()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("tool call count: %w", err)
	}
	// Every encoded call ID consumes at least one length byte. Bound the
	// allocation by the bytes that actually remain instead of an unrelated UI
	// page size; valid large assistant turns remain decodable.
	if callCount > len(reader.data) {
		return entryHydrationRecord{}, errors.New("tool call count exceeds projection bytes")
	}

	summary := BranchEntrySummary{
		Type:                      knownHydrationEntryType(typeBytes),
		Role:                      knownHydrationRole(roleBytes),
		UserVisible:               flags&hydrationFlagUserVisible != 0,
		ThinkingVisible:           flags&hydrationFlagThinkingVisible != 0,
		BodyRows:                  bodyRows,
		StopVisible:               flags&hydrationFlagStopVisible != 0,
		ContextChars:              contextChars,
		ToolIsError:               flags&hydrationFlagToolIsError != 0,
		ToolOutputPresent:         flags&hydrationFlagToolOutputPresent != 0,
		ToolDisplayPresent:        flags&hydrationFlagToolDisplayPresent != 0,
		ToolWasDispatched:         flags&hydrationFlagToolWasDispatched != 0,
		CompactionActive:          flags&hydrationFlagCompactionActive != 0,
		CompactionCheckpointChars: checkpointChars,
	}
	if len(compactedThrough) > 0 {
		summary.CompactedThrough = string(compactedThrough)
	}
	if len(usageJSON) > 0 {
		summary.Usage = new(protocol.Usage)
		if err := json.Unmarshal(usageJSON, summary.Usage); err != nil {
			return entryHydrationRecord{}, fmt.Errorf("usage JSON: %w", err)
		}
	}
	if len(toolCallID) > 0 {
		summary.ToolCallID = string(toolCallID)
	}
	if len(toolName) > 0 {
		summary.ToolName = string(toolName)
	}
	if callCount > 0 {
		summary.ToolCallIDs = make([]string, 0, callCount)
		for range callCount {
			callID, err := reader.bytes()
			if err != nil {
				return entryHydrationRecord{}, fmt.Errorf("tool call: %w", err)
			}
			summary.ToolCallIDs = append(summary.ToolCallIDs, string(callID))
		}
	}
	userInput, err := reader.bytes()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("user input: %w", err)
	}
	latestPlan, err := reader.bytes()
	if err != nil {
		return entryHydrationRecord{}, fmt.Errorf("latest plan: %w", err)
	}
	if len(reader.data) != 0 {
		return entryHydrationRecord{}, errors.New("trailing projection bytes")
	}
	return entryHydrationRecord{
		summary: summary, userInput: string(userInput), latestPlan: string(latestPlan),
	}, nil
}

func knownHydrationEntryType(value []byte) EntryType {
	switch {
	case bytes.Equal(value, []byte(EntryMessage)):
		return EntryMessage
	case bytes.Equal(value, []byte(EntryCompaction)):
		return EntryCompaction
	case bytes.Equal(value, []byte(EntryMeta)):
		return EntryMeta
	default:
		return EntryType(string(value))
	}
}

func knownHydrationRole(value []byte) protocol.Role {
	switch {
	case bytes.Equal(value, []byte(protocol.RoleUser)):
		return protocol.RoleUser
	case bytes.Equal(value, []byte(protocol.RoleAssistant)):
		return protocol.RoleAssistant
	case bytes.Equal(value, []byte(protocol.RoleTool)):
		return protocol.RoleTool
	case bytes.Equal(value, []byte(protocol.RoleSystem)):
		return protocol.RoleSystem
	case bytes.Equal(value, []byte(protocol.RoleCustom)):
		return protocol.RoleCustom
	case bytes.Equal(value, []byte(protocol.RoleAgent)):
		return protocol.RoleAgent
	default:
		return protocol.Role(string(value))
	}
}
