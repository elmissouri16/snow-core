package session

import (
	"context"
	"database/sql"
	json "encoding/json/v2"
	"errors"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	catalogMaxHistoryDecodeBytes = 8 << 20
	catalogMaxHistoryToolRows    = 256
)

type catalogHistoryRecord struct {
	depth      int
	message    protocol.Message
	ownerDepth int
}

// catalogHistoryBlock decodes only the fields appropriate for a public block.
// In particular, thinking text, tool arguments, images and provider data are
// skipped by the JSON decoder, not decoded into a protocol content block.
type catalogHistoryBlock struct {
	Type       protocol.ContentBlockType `json:"type"`
	Text       string                    `json:"text"`
	ToolCallID string                    `json:"tool_call_id"`
	Name       string                    `json:"name"`
}

func (b *catalogHistoryBlock) UnmarshalJSON(raw []byte) error {
	var kind struct {
		Type protocol.ContentBlockType `json:"type"`
	}
	if err := json.Unmarshal(raw, &kind); err != nil {
		return err
	}
	b.Type = kind.Type
	switch kind.Type {
	case protocol.BlockText, protocol.BlockPlan:
		var text struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		b.Text = text.Text
	case protocol.BlockToolCall:
		var call struct {
			ToolCallID string `json:"tool_call_id"`
			Name       string `json:"name"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return err
		}
		b.ToolCallID, b.Name = call.ToolCallID, call.Name
	}
	return nil
}

// catalogHistoryTools reads only results on this same verified branch whose
// nearest user/assistant boundary is one of the selected assistant owners.
// Compaction and custom entries do not cut ownership, but another branch does.
// SQL budgets bound both returned rows and raw JSON before decoding in Go.
func catalogHistoryTools(ctx context.Context, db *sql.DB, tip string, records []catalogHistoryRecord, budget int) (map[string][]protocol.RPCHistoryTool, bool, error) {
	var owners []string
	args := []any{tip, maxSessionQueryDepth}
	for _, record := range records {
		if record.message.Role == protocol.RoleAssistant {
			owners = append(owners, "?")
			args = append(args, record.depth)
		}
	}
	if len(owners) == 0 {
		return nil, false, nil
	}
	args = append(args, catalogMaxHistoryToolRows, catalogMaxMessageBytes, budget)
	query := catalogBranchSQL + `, labeled AS (
  SELECT b.id,b.depth,CASE WHEN b.entry_type='message' THEN json_extract(e.message,'$.role') END AS role,
   min(CASE WHEN b.entry_type='message' AND json_extract(e.message,'$.role') IN ('user','assistant') THEN b.depth END)
    OVER (ORDER BY b.depth ROWS BETWEEN 1 FOLLOWING AND UNBOUNDED FOLLOWING) AS owner_depth
  FROM branch b JOIN entries e ON e.id=b.id
 ), matching AS (
  SELECT id,depth,owner_depth FROM labeled WHERE role='tool_result' AND owner_depth IN (` + strings.Join(owners, ",") + `)
 ), candidates AS (
  SELECT id,depth,owner_depth FROM matching ORDER BY depth LIMIT ?
 ), sized AS (
  SELECT c.id,c.depth,c.owner_depth,length(CAST(e.message AS BLOB)) AS size,
   sum(length(CAST(e.message AS BLOB))) OVER (ORDER BY c.depth) AS total
  FROM candidates c JOIN entries e ON e.id=c.id
 ), stats AS (
  SELECT owner_depth,count(*) AS expected FROM matching GROUP BY owner_depth
 ) SELECT stats.owner_depth,stats.expected,COALESCE(s.id,''),COALESCE(s.depth,-1),
  CASE WHEN s.size<=? AND s.total<=? THEN e.message ELSE '' END
 FROM stats LEFT JOIN sized s ON s.owner_depth=stats.owner_depth LEFT JOIN entries e ON e.id=s.id
 ORDER BY s.depth`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, errors.New("catalog: cannot read public tool history")
	}
	defer rows.Close()
	omitted := false
	incomplete := make(map[int]bool)
	expected, seen := make(map[int]int), make(map[int]int)
	for rows.Next() {
		var record catalogHistoryRecord
		var raw []byte
		var ownerRows int
		if err := rows.Scan(&record.ownerDepth, &ownerRows, &record.message.ID, &record.depth, &raw); err != nil {
			return nil, false, errors.New("catalog: invalid tool history entry")
		}
		expected[record.ownerDepth] = ownerRows
		if record.depth < 0 {
			incomplete[record.ownerDepth] = true
			continue
		}
		seen[record.ownerDepth]++
		if len(raw) == 0 {
			incomplete[record.ownerDepth] = true
			continue
		}
		// Do not decode Content, ToolDisplay, plugin metadata, provider state, or
		// any historical fallback. Legacy results deliberately have no preview.
		var result struct {
			ToolName           catalogToolResultName       `json:"tool_name"`
			ToolCallID         string                      `json:"tool_call_id"`
			ToolOutcomeUnknown bool                        `json:"tool_outcome_unknown"`
			IsError            bool                        `json:"is_error"`
			PublicToolResult   *protocol.ToolResultPreview `json:"public_tool_result"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, false, errors.New("catalog: invalid tool history JSON")
		}
		if result.ToolName.invalid {
			incomplete[record.ownerDepth] = true
			continue
		}
		record.message.ToolName = result.ToolName.text
		record.message.Role = protocol.RoleTool
		record.message.ToolCallID, record.message.IsError = result.ToolCallID, result.IsError
		record.message.ToolOutcomeUnknown = result.ToolOutcomeUnknown
		record.message.PublicToolResult = result.PublicToolResult
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, false, errors.New("catalog: tool history read failed")
	}
	for owner, count := range expected {
		if seen[owner] != count {
			incomplete[owner] = true
		}
	}
	// Any skipped record may hide a duplicate or conflicting result. Suppress
	// all results in that ownership interval, not just the omitted record.
	records = slices.DeleteFunc(records, func(record catalogHistoryRecord) bool {
		return record.message.Role == protocol.RoleTool && incomplete[record.ownerDepth]
	})
	slices.SortFunc(records, func(a, b catalogHistoryRecord) int { return b.depth - a.depth })
	messages := make([]protocol.Message, len(records))
	for i := range records {
		messages[i] = records[i].message
	}
	tools, truncated := protocol.ProjectHistoryTools(messages)
	for _, record := range records {
		if record.message.Role == protocol.RoleAssistant && incomplete[record.depth] {
			omitted = true
			for i := range tools[record.message.ID] {
				tools[record.message.ID][i].Truncated = true
			}
		}
	}
	return tools, omitted || truncated, nil
}

// Reject oversized names without decoding an oversized string. Six encoded
// bytes per ASCII character is JSON's worst-case escape expansion. Empty
// names remain compatible with legacy persisted results; a nonempty name must
// match the owning call in the shared projector.
type catalogToolResultName struct {
	text    string
	invalid bool
}

func (n *catalogToolResultName) UnmarshalJSON(raw []byte) error {
	if len(raw) > 6*protocol.RPCHistoryMaxNameBytes+2 {
		n.invalid = true
		return nil
	}
	if err := json.Unmarshal(raw, &n.text); err != nil {
		return err
	}
	if len(n.text) > protocol.RPCHistoryMaxNameBytes {
		n.text = ""
		n.invalid = true
	}
	return nil
}
