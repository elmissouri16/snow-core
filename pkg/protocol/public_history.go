package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	RPCHistoryMaxTools            = 64
	RPCHistoryMaxNameBytes        = 128
	RPCHistoryMaxOutputBytes      = 8 << 10
	RPCHistoryMaxTotalOutputBytes = 128 << 10
	RPCHistoryMaxIDBytes          = 4096
)

// RPCHistoryTool is a public, terminal-or-unresolved tool history item. ID is a
// stable presentation identity, not the provider's reusable tool-call ID.
// Output is untrusted text and only comes from an explicit PublicToolResult.
type RPCHistoryTool struct {
	ID              string `json:"id"`
	OwnerID         string `json:"owner_id"`
	ResultID        string `json:"result_id,omitempty"`
	Tool            string `json:"tool"`
	Status          string `json:"status"`
	Output          string `json:"output"`
	OutputAvailable bool   `json:"output_available"`
	Truncated       bool   `json:"truncated"`
}

// ProjectHistoryTools projects chronological parent-linked history without
// mutating it. Callers must supply a single branch, not a bag of session entries.
// Only the nearest preceding assistant's unique call IDs can own results; user
// and assistant messages both end the preceding ownership interval. Missing or
// ambiguous results and explicit unknown-outcome recovery records remain
// unresolved, never inferred running or canceled. Recovery records supply neither
// ResultID nor output: they are not observations of the actual tool outcome.
// Nonempty result tool names must agree with their call; empty legacy names
// remain compatible. Duplicate owner IDs, result IDs and call results never
// receive a falsely definitive projection. IDs hash the tool-call ordinal, so
// removing private non-tool blocks does not change presentation identities.
// The most recent 64 calls win, and output budgets favor recent owners/calls.
// The boolean reports omitted or truncated public content (not private data).
func ProjectHistoryTools(messages []Message) (map[string][]RPCHistoryTool, bool) {
	out := make(map[string][]RPCHistoryTool)
	// Count before selection: even an otherwise omitted assistant can make an
	// owner key ambiguous. Never let map overwrite order choose the owner.
	ownerCounts := make(map[string]int)
	for _, message := range messages {
		if message.Role == RoleAssistant && historyID(message.ID) {
			ownerCounts[message.ID]++
		}
	}
	omitted, count, remaining := false, 0, RPCHistoryMaxTotalOutputBytes
	end := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		owner := &messages[i]
		if owner.Role == RoleUser {
			end = i
			continue
		}
		if owner.Role != RoleAssistant {
			continue
		}
		boundary := end
		end = i
		// Keep only bounded selected IDs in memory, including duplicate counts.
		type selectedCall struct{ index, ordinal int }
		indices := make([]selectedCall, 0, RPCHistoryMaxTools-count)
		ordinal := 0
		for _, block := range owner.Content {
			if block.Type == BlockToolCall {
				ordinal++
			}
		}
		calls := make(map[string]int)
		for j := len(owner.Content) - 1; j >= 0; j-- {
			block := &owner.Content[j]
			if block.Type != BlockToolCall {
				continue
			}
			ordinal--
			if ownerCounts[owner.ID] != 1 || !historyID(owner.ID) || !historyID(block.ToolCallID) || count+len(indices) == RPCHistoryMaxTools {
				omitted = true
				continue
			}
			indices = append(indices, selectedCall{j, ordinal})
			calls[block.ToolCallID] = 0
		}
		for _, block := range owner.Content {
			if block.Type == BlockToolCall {
				if _, ok := calls[block.ToolCallID]; ok {
					calls[block.ToolCallID]++
				}
			}
		}
		results, ambiguous := historyResults(messages[i+1:boundary], calls)
		omitted = omitted || ambiguous
		tools := make([]RPCHistoryTool, len(indices))
		for j, selected := range indices {
			block := &owner.Content[selected.index]
			digest := sha256.Sum256([]byte(owner.ID + "\x00" + strconv.Itoa(selected.ordinal)))
			tool := RPCHistoryTool{ID: "history-" + hex.EncodeToString(digest[:]), OwnerID: owner.ID, Status: "unresolved"}
			tool.Tool, tool.Truncated = historyBoundText(block.Name, RPCHistoryMaxNameBytes)
			result := results[block.ToolCallID]
			if result != nil && result.ToolName != "" && (result.ToolName != block.Name || len(result.ToolName) > RPCHistoryMaxNameBytes) {
				omitted = true
				result = nil
			}
			if result != nil && !result.ToolOutcomeUnknown {
				tool.ResultID, tool.Status = result.ID, "completed"
				if result.IsError {
					tool.Status = "failed"
				}
				if preview := result.PublicToolResult; preview != nil {
					tool.OutputAvailable = true
					var clipped bool
					tool.Output, clipped = historyBoundText(preview.Text, min(RPCHistoryMaxOutputBytes, remaining))
					tool.Truncated = tool.Truncated || clipped || preview.Truncated
					remaining -= len(tool.Output)
				}
			}
			omitted = omitted || tool.Truncated
			tools[len(indices)-1-j] = tool
		}
		if len(tools) > 0 {
			out[owner.ID] = tools
			count += len(tools)
		}
	}
	return out, omitted
}

// Count before validating IDs or names: rejecting one malformed result must
// not hide ambiguity and turn another result for that call into a success.
func historyResults(interval []Message, calls map[string]int) (map[string]*Message, bool) {
	results := make(map[string]*Message, len(calls))
	counts := make(map[string]int, len(calls))
	ids := make(map[string]int, len(calls))
	ambiguous := false
	for i := range interval {
		result := &interval[i]
		if result.Role != RoleTool || calls[result.ToolCallID] != 1 {
			continue
		}
		counts[result.ToolCallID]++
		if counts[result.ToolCallID] == 1 {
			results[result.ToolCallID] = result
			if historyID(result.ID) {
				ids[result.ID] = 0
			}
		}
	}
	// A reused result message ID is ambiguous even across distinct call IDs.
	for _, result := range interval {
		if result.Role == RoleTool {
			if _, ok := ids[result.ID]; ok {
				ids[result.ID]++
			}
		}
	}
	for call, result := range results {
		if counts[call] != 1 || !historyID(result.ID) || ids[result.ID] != 1 {
			delete(results, call)
			ambiguous = true
		}
	}
	return results, ambiguous
}

func historyID(id string) bool {
	return id != "" && len(id) <= RPCHistoryMaxIDBytes && utf8.ValidString(id)
}

func historyBoundText(text string, limit int) (string, bool) {
	// Clip before repairing UTF-8 so malformed caller input cannot force an
	// allocation proportional to an otherwise discarded, unbounded string.
	truncated := len(text) > limit
	if truncated {
		end := limit
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		text = text[:end]
	}
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
		truncated = true
	}
	// Replacement characters can expand an invalid prefix beyond its budget.
	if len(text) > limit {
		for limit > 0 && !utf8.RuneStart(text[limit]) {
			limit--
		}
		text = text[:limit]
		truncated = true
	}
	return strings.Clone(text), truncated
}
