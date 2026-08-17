package agent

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/snow-core/snow/pkg/protocol"
)

// ContextCategory is one estimated contributor to a provider request's input.
// Text tokens use Snow's provider-neutral UTF-8 bytes/4 heuristic; images use
// a bounded dimension-based vision estimate. Provider-reported aggregate input
// remains authoritative.
type ContextCategory struct {
	Name            string
	Bytes           int
	EstimatedTokens int
	Items           int
}

// ContextReport describes either the latest provider request or, before the
// current runtime has sent one on the active branch, what Snow would send next.
type ContextReport struct {
	LatestRequest        bool
	Categories           []ContextCategory
	EstimatedInputTokens int
	MessageCount         int
	ToolCount            int
	ContextWindow        int
	Usage                *protocol.Usage
}

// Clone returns an independent report suitable for another surface.
func (r ContextReport) Clone() ContextReport {
	r.Categories = append([]ContextCategory(nil), r.Categories...)
	r.Usage = r.Usage.Clone()
	return r
}

// ContextReport returns a category estimate for the latest provider request.
// Before this runtime has made a request on the branch, it reports the
// currently projected context instead. The report retains counts only, never
// prompt contents.
func (a *Agent) ContextReport() (ContextReport, error) {
	a.mu.RLock()
	if a.latestContextReport != nil {
		report := a.latestContextReport.Clone()
		a.mu.RUnlock()
		return report, nil
	}
	a.mu.RUnlock()

	// The initial projected scan is slower than the cached latest-request path.
	// Serialize it with prompt/session/branch admission, then check the cache a
	// second time so a request cannot land between the first check and snapshot.
	unlock := a.LockAdmission()
	defer unlock()
	a.mu.RLock()
	if a.latestContextReport != nil {
		report := a.latestContextReport.Clone()
		a.mu.RUnlock()
		return report, nil
	}
	a.mu.RUnlock()

	messages, err := a.contextMessagesCurrent()
	if err != nil {
		return ContextReport{}, err
	}
	req := protocol.ChatRequest{
		Model:    a.Model(),
		Messages: messages,
		Tools:    a.requestToolSchemas(),
		System:   a.requestSystemPrompt(),
	}
	return buildContextReport(req, false), nil
}

func buildContextReport(req protocol.ChatRequest, latestRequest bool) ContextReport {
	const (
		categorySystem        = "System prompt"
		categoryToolSchemas   = "Tool schemas"
		categoryInternal      = "Internal steering"
		categoryUser          = "User messages"
		categoryAssistant     = "Assistant responses"
		categoryToolCalls     = "Tool calls"
		categoryToolResults   = "Tool results"
		categoryAgent         = "Agent messages"
		categoryImages        = "Images"
		categoryProviderState = "Provider state"
		categoryOther         = "Other messages"
	)

	order := []string{
		categorySystem,
		categoryToolSchemas,
		categoryInternal,
		categoryUser,
		categoryAssistant,
		categoryToolCalls,
		categoryToolResults,
		categoryAgent,
		categoryImages,
		categoryProviderState,
		categoryOther,
	}
	categories := make(map[string]*ContextCategory, len(order))
	add := func(name string, bytes, items int) {
		if bytes <= 0 && items <= 0 {
			return
		}
		category := categories[name]
		if category == nil {
			category = &ContextCategory{Name: name}
			categories[name] = category
		}
		category.Bytes += max(0, bytes)
		category.Items += max(0, items)
	}

	add(categorySystem, len(req.System), boolCount(req.System != ""))
	add(categoryToolSchemas, providerSchemaBytes(req.Tools), len(req.Tools))
	for _, fragment := range req.InternalContext {
		// Every current adapter wraps an internal fragment in the same sealed
		// compatibility envelope. JSON framing and tokenizer effects remain in
		// the provider-vs-estimate delta shown by the TUI.
		bytes := len(fragment.Text) + len(fragment.Source) + len("<snow_internal_context source=\"\">\n\n</snow_internal_context>")
		add(categoryInternal, bytes, 1)
	}

	for _, message := range req.Messages {
		// Current adapters do not replay durable system-role messages or raw
		// thinking blocks. Count only content represented in provider input.
		if message.Role == protocol.RoleSystem {
			continue
		}
		baseCategory := messageBaseCategory(message)
		metadataBytes := len(message.Role) + len(message.ToolName) + len(message.ToolCallID)
		add(baseCategory, metadataBytes, 1)
		for _, block := range message.Content {
			if block.Type == protocol.BlockThinking {
				continue
			}
			if block.Type == protocol.BlockProviderData &&
				(req.Model.Provider == "" || message.Provider != req.Model.Provider ||
					message.StopReason == protocol.StopError || message.StopReason == protocol.StopAborted) {
				continue
			}
			if block.Type == protocol.BlockImage {
				// Compressed file size has almost no relationship to multimodal
				// context cost. Convert a bounded, dimension-based vision estimate
				// back to synthetic bytes so it participates in the same category
				// calibration without allowing a large PNG/JPEG to dominate.
				add(categoryImages, estimateImageTokens(block.Data)*4, 1)
				continue
			}
			category := baseCategory
			if message.Role == protocol.RoleAssistant {
				switch block.Type {
				case protocol.BlockToolCall:
					category = categoryToolCalls
				case protocol.BlockProviderData:
					category = categoryProviderState
				default:
					category = categoryAssistant
				}
			} else if block.Type == protocol.BlockProviderData {
				category = categoryProviderState
			}
			bytes := len(block.Type) + len(block.Text) + len(block.MIMEType) + len(block.Data) +
				len(block.Name) + len(block.ToolCallID) + len(block.Arguments)
			add(category, bytes, 0)
		}
	}

	report := ContextReport{
		LatestRequest: latestRequest,
		MessageCount:  len(req.Messages),
		ToolCount:     len(req.Tools),
		ContextWindow: req.Model.ContextWindow,
	}
	totalBytes := 0
	for _, name := range order {
		category := categories[name]
		if category == nil || category.Bytes == 0 {
			continue
		}
		category.EstimatedTokens = estimatedTokensForBytes(category.Bytes)
		report.Categories = append(report.Categories, *category)
		totalBytes += category.Bytes
	}
	report.EstimatedInputTokens = estimatedTokensForBytes(totalBytes)
	return report
}

func messageBaseCategory(message protocol.Message) string {
	switch message.Role {
	case protocol.RoleUser:
		return "User messages"
	case protocol.RoleAssistant:
		return "Assistant responses"
	case protocol.RoleTool:
		return "Tool results"
	case protocol.RoleAgent:
		return "Agent messages"
	default:
		return "Other messages"
	}
}

func estimateImageTokens(data []byte) int {
	const (
		minImageTokens     = 85
		maxImageTokens     = 1536
		unknownImageTokens = 1024
		patchSize          = 32
	)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return unknownImageTokens
	}
	wide := (cfg.Width + patchSize - 1) / patchSize
	high := (cfg.Height + patchSize - 1) / patchSize
	if wide > maxImageTokens || high > maxImageTokens || wide*high > maxImageTokens {
		return maxImageTokens
	}
	patches := wide * high
	return max(patches, minImageTokens)
}

func estimatedTokensForBytes(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
