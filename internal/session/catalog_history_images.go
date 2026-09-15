package session

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Metadata is selected independently of text/raw-message decode caps. Even a
// message with several large images gets thumbnails, without transferring any
// image bytes or untrusted labels from SQLite into the history DTO.
func catalogHistoryImages(ctx context.Context, db *sql.DB, tip string, messages []protocol.RPCCatalogMessage) error {
	var owners []string
	args := []any{tip, maxSessionQueryDepth}
	byID := map[string]int{}
	for i, message := range messages {
		if message.Role == string(protocol.RoleUser) {
			owners = append(owners, "?")
			args = append(args, message.ID)
			byID[message.ID] = i
		}
	}
	if len(owners) == 0 {
		return nil
	}
	args = append(args, protocol.RPCMessageImageMaxCount)
	rows, err := db.QueryContext(ctx, catalogBranchSQL+`, images AS (
 SELECT e.id,CAST(j.key AS INTEGER) AS idx,
 CASE json_extract(j.value,'$.mime_type') WHEN 'image/png' THEN 'image/png' WHEN 'image/jpeg' THEN 'image/jpeg' WHEN 'image/gif' THEN 'image/gif' WHEN 'image/webp' THEN 'image/webp' ELSE '' END AS mime,
 row_number() OVER (PARTITION BY e.id ORDER BY CAST(j.key AS INTEGER)) AS ordinal
 FROM branch b JOIN entries e ON e.id=b.id JOIN json_each(e.message,'$.content') j
 WHERE b.entry_type='message' AND e.id IN (`+strings.Join(owners, ",")+`) AND json_extract(e.message,'$.role')='user' AND json_extract(j.value,'$.type')='image'
 ) SELECT id,idx,mime FROM images WHERE ordinal<=? ORDER BY id,idx`, args...)
	if err != nil {
		return errors.New("catalog: cannot read image metadata")
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var image protocol.RPCMessageImage
		if err := rows.Scan(&id, &image.Index, &image.MIMEType); err != nil {
			return errors.New("catalog: invalid image metadata")
		}
		if image.Index < 0 || image.Index > 10000 {
			continue
		}
		i, ok := byID[id]
		if !ok {
			return errors.New("catalog: unexpected image owner")
		}
		messages[i].Images = append(messages[i].Images, image)
	}
	return rows.Err()
}
