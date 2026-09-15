package session

import (
	"context"
	"database/sql"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func validImageID(id string) bool {
	return id != "" && len(id) <= catalogMaxFieldBytes && utf8.ValidString(id) && !strings.ContainsRune(id, 0)
}

// Image reads one explicit raster from an inactive, project-owned current branch.
// It uses exactly the catalog's pinned-root, immutable SQLite and lifetime-lease
// guards. No URLs, private blocks, provider state or filenames are projected.
func (c *Catalog) Image(ctx context.Context, p protocol.RPCCatalogImageParams) (protocol.RPCCatalogImage, error) {
	var result protocol.RPCCatalogImage
	if !validImageID(p.SessionID) || !validImageID(p.MessageID) || (p.Index < 0 || p.Index > 10000) {
		return result, errors.New("catalog: invalid image selector")
	}
	ctx, cancel := context.WithTimeout(ctx, catalogQueryTimeout)
	defer cancel()
	entries, err := c.entries(ctx)
	if err != nil {
		return result, err
	}
	var selected *catalogEntry
	for i := range entries {
		if entries[i].SessionID == p.SessionID {
			if selected != nil {
				return result, errors.New("catalog: ambiguous session_id")
			}
			selected = &entries[i]
		}
	}
	if selected == nil {
		return result, ErrNotFound
	}
	root, err := os.OpenRoot(c.root)
	if err != nil {
		return result, errors.New("catalog: cannot open sessions root")
	}
	defer root.Close()
	db, cleanup, _, err := c.open(ctx, root, selected.path)
	if err != nil {
		return result, errors.New("catalog: session unavailable")
	}
	defer cleanup()
	current, err := c.inspect(ctx, db, selected.path, selected.UpdatedAt)
	if err != nil || current.SessionID != p.SessionID {
		return result, ErrNotFound
	}
	if current.MessagesCapped {
		return result, errors.New("catalog: history exceeds traversal limit")
	}
	// Prove the bounded path reaches the verified session root; a broken/cyclic
	// chain cannot authorize an otherwise matching row.
	var rooted bool
	err = db.QueryRowContext(ctx, catalogBranchSQL+`SELECT EXISTS(SELECT 1 FROM branch WHERE id='root' AND parent_id='' AND entry_type='meta')`, current.tip, maxSessionQueryDepth).Scan(&rooted)
	if err != nil || !rooted {
		return result, ErrNotFound
	}
	var raw []byte
	path := fmt.Sprintf("$.content[%d]", p.Index)
	err = db.QueryRowContext(ctx, catalogBranchSQL+`SELECT CASE WHEN length(CAST(json_extract(e.message, ?) AS BLOB))<=? THEN json_extract(e.message, ?) ELSE '' END
 FROM branch b JOIN entries e ON e.id=b.id WHERE b.entry_type='message' AND e.id=? AND json_extract(e.message,'$.role')='user' LIMIT 1`, current.tip, maxSessionQueryDepth, path, catalogMaxMessageBytes, path, p.MessageID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil || len(raw) == 0 {
		return result, errors.New("catalog: image block unavailable or oversized")
	}
	var kind struct {
		Type     protocol.ContentBlockType `json:"type"`
		MIMEType string                    `json:"mime_type"`
	}
	if err := json.Unmarshal(raw, &kind); err != nil || kind.Type != protocol.BlockImage || protocol.MessageImageMIME(kind.MIMEType) == "" {
		return result, errors.New("catalog: unsupported image block")
	}
	var image struct {
		Data []byte `json:"data"`
	}
	if err := json.Unmarshal(raw, &image); err != nil {
		return result, errors.New("catalog: invalid image data")
	}
	if err := validateMessageImage(kind.MIMEType, image.Data); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return protocol.RPCCatalogImage{SessionID: p.SessionID, MessageID: p.MessageID, Index: p.Index, MIMEType: kind.MIMEType, Data: image.Data}, nil
}
