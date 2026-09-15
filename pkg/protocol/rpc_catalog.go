package protocol

const (
	// Catalog pagination and display limits apply independently of live RPC.
	RPCCatalogDefaultLimit   = 32
	RPCCatalogMaxLimit       = 50
	RPCCatalogMaxTextBytes   = 256 * 1024
	RPCCatalogMaxOutputBytes = 1024 * 1024
)

// RPCCatalogSessionsParams selects a bounded page of inactive project sessions.
// Offset is zero-based; a zero limit selects RPCCatalogDefaultLimit.
type RPCCatalogSessionsParams struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// RPCCatalogMessagesParams selects saved, active-branch display history without
// opening a runtime or activating the selected session.
type RPCCatalogMessagesParams struct {
	SessionID    string `json:"session_id"`
	Offset       int    `json:"offset"`
	Limit        int    `json:"limit"`
	IncludeTools bool   `json:"include_tools,omitzero"`
}

// RPCCatalogSessionsPage is a path-free inventory. Pagination is best effort:
// concurrent session changes may reorder subsequent offset-based pages.
type RPCCatalogSessionsPage struct {
	Sessions   []RPCSessionSummary `json:"sessions"`
	Offset     int                 `json:"offset"`
	NextOffset int                 `json:"next_offset"`
	HasMore    bool                `json:"has_more"`
}

// RPCCatalogMessage deliberately is not Message: only user/assistant text and
// opted-in public tool previews and image metadata cross this boundary. Arguments, image bytes, thinking,
// continuity and private metadata cannot.
// Text is untrusted display data, never markup or execution authority.
type RPCCatalogMessage struct {
	ID        string            `json:"id"`
	Role      string            `json:"role"`
	Text      string            `json:"text"`
	Timestamp int64             `json:"timestamp"`
	Truncated bool              `json:"truncated"`
	Tools     []RPCHistoryTool  `json:"tools,omitempty"`
	Images    []RPCMessageImage `json:"images,omitempty"`
}

// RPCCatalogMessagesPage contains chronological saved branch history. Offsets
// count user/assistant entries only, including entries with no display text.
type RPCCatalogMessagesPage struct {
	Messages       []RPCCatalogMessage `json:"messages"`
	ToolsTruncated bool                `json:"tools_truncated,omitzero"`
	Offset         int                 `json:"offset"`
	NextOffset     int                 `json:"next_offset"`
	HasMore        bool                `json:"has_more"`
}

// NewRPCCatalogReady advertises only the dedicated runtime-free catalog surface.
func NewRPCCatalogReady(version string) RPCReady {
	ready := NewRPCReady(version)
	ready.Capabilities = []string{"runtime_free_catalog", "catalog_sessions", "catalog_messages", "catalog_public_tools", "catalog_image", "history_images"}
	return ready
}

// RPCMessageImage identifies an original user content-block index, not an image
// ordinal. Metadata contains no bytes, filenames, URLs or private block fields.
// An empty MIMEType means an unsupported declared type; retrieval fails closed.
type RPCMessageImage struct {
	Index    int    `json:"index"`
	MIMEType string `json:"mime_type"`
}

const (
	RPCMessageImageMaxBytes       = 2 << 20
	RPCMessageImageMaxOutputBytes = 4 << 20
	RPCMessageImageMaxCount       = 8
)

// RPCCatalogImageParams selects an exact current-branch saved user image.
type RPCCatalogImageParams struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
	Index     int    `json:"index"`
}

// RPCMessageImageParams selects a live worker's own durable user image. Exactly
// one of MessageID or TurnID is required; TurnID identifies a persisted user
// turn marker, never a display position or client-generated alias.
type RPCMessageImageParams struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
	Index     int    `json:"index"`
}

// RPCCatalogImage is the dedicated bounded raster response. Data is base64 in
// JSON and is never included in a public history page or general snapshot.
type RPCCatalogImage struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
	Index     int    `json:"index"`
	MIMEType  string `json:"mime_type"`
	Data      []byte `json:"data"`
}

// MessageImageMIME restricts public metadata to known raster labels.
func MessageImageMIME(mime string) string {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return mime
	}
	return ""
}

// ProjectMessageImages keeps only metadata from original user image blocks.
func ProjectMessageImages(message Message) []RPCMessageImage {
	if message.Role != RoleUser {
		return nil
	}
	var images []RPCMessageImage
	for index, block := range message.Content {
		if index > 10000 {
			break
		}
		if block.Type == BlockImage {
			images = append(images, RPCMessageImage{Index: index, MIMEType: MessageImageMIME(block.MIMEType)})
			if len(images) == RPCMessageImageMaxCount {
				break
			}
		}
	}
	return images
}

// RPCMessageImageResult is the live-worker name for the shared raster response.
type RPCMessageImageResult = RPCCatalogImage
