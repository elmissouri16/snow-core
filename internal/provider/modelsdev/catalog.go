// Package modelsdev provides bounded access to the public models.dev provider
// metadata catalog. Provider adapters decide which provider/model records they
// trust and how to merge them with their authoritative availability source.
package modelsdev

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	DefaultURL       = "https://models.dev/api.json"
	maxResponseBytes = 16 << 20
	userAgent        = "snow-core-model-catalog/0.1"
)

// Catalog is the top-level models.dev provider map.
type Catalog map[string]Provider

// Provider contains the models published for one models.dev provider ID.
type Provider struct {
	Models map[string]Model `json:"models"`
}

// Model contains the provider metadata consumed by Snow's adapters.
type Model struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Reasoning        *bool             `json:"reasoning"`
	ReasoningOptions []ReasoningOption `json:"reasoning_options"`
	ToolCall         *bool             `json:"tool_call"`
	Limit            Limit             `json:"limit"`
	Cost             *Cost             `json:"cost,omitempty"`
	Modalities       Modalities        `json:"modalities"`
}

// ReasoningOption describes one provider-selectable reasoning control.
type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

// Limit contains the total context and maximum output token counts.
type Limit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

// Cost contains USD prices per million tokens.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

// Modalities lists supported input/output media types.
type Modalities struct {
	Input []string `json:"input"`
}

// FetchProvider retrieves one provider's metadata without credentials. The
// boolean is true only after a bounded successful HTTP/JSON response; a valid
// catalog with no matching provider returns an empty map and true.
func FetchProvider(ctx context.Context, client *http.Client, catalogURL, providerID string) (map[string]Model, bool) {
	if strings.TrimSpace(catalogURL) == "" || strings.TrimSpace(providerID) == "" {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return nil, false
	}
	var catalog Catalog
	if json.Unmarshal(data, &catalog) != nil {
		return nil, false
	}
	models := catalog[providerID].Models
	if models == nil {
		models = map[string]Model{}
	}
	return models, true
}

// ReasoningMetadata normalizes models.dev reasoning metadata into Snow's
// provider contract. Unknown future effort values are ignored until the public
// protocol supports them; off is implicit and never sent to the provider.
func ReasoningMetadata(model Model) (bool, []protocol.ThinkingLevel) {
	supports := model.Reasoning != nil && *model.Reasoning
	levels := make([]protocol.ThinkingLevel, 0)
	seen := make(map[protocol.ThinkingLevel]bool)
	for _, option := range model.ReasoningOptions {
		if !strings.EqualFold(strings.TrimSpace(option.Type), "effort") {
			continue
		}
		for _, value := range option.Values {
			level, err := protocol.ParseThinkingLevel(strings.ToLower(strings.TrimSpace(value)))
			if err != nil || level == protocol.ThinkingOff || seen[level] {
				continue
			}
			seen[level] = true
			levels = append(levels, level)
		}
	}
	return supports || len(levels) > 0, levels
}
