package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var (
	ErrManagerInvalid     = errors.New("host control: invalid request")
	ErrManagerUnavailable = errors.New("host control: local configuration unavailable")
	ErrManagerConflict    = errors.New("host control: revision conflict")
)

const MaxManagerProviders = 128

type managerObject map[string]jsontext.Value

func managerObjectValue(raw jsontext.Value) (managerObject, error) {
	if len(raw) == 0 {
		return managerObject{}, nil
	}
	var value managerObject
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, ErrManagerUnavailable
	}
	return value, nil
}

// CanonicalManagerScope resolves an operator selection, not project settings.
func CanonicalManagerScope(request protocol.HostDefaultsRequest) (protocol.HostDefaultsRequest, error) {
	switch request.Scope {
	case "global":
		if request.CWD != "" {
			return request, ErrManagerInvalid
		}
	case "project":
		if request.CWD == "" || len(request.CWD) > 4096 {
			return request, ErrManagerInvalid
		}
		cwd, err := filepath.Abs(request.CWD)
		if err != nil {
			return request, ErrManagerInvalid
		}
		cwd, err = filepath.EvalSymlinks(cwd)
		if err != nil {
			return request, ErrManagerInvalid
		}
		info, err := os.Stat(cwd)
		if err != nil || !info.IsDir() || len(cwd) > 4096 {
			return request, ErrManagerInvalid
		}
		request.CWD = cwd
	default:
		return request, ErrManagerInvalid
	}
	return request, nil
}

func managerSafeString(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

func managerProviderID(id string) bool {
	switch id {
	case "chatgpt", "opencode-go", "openai-compatible":
		return true
	case "opencode-zen":
		// Read legacy selections so Web settings remain available for migration.
		// managerProviders intentionally omits Zen, preventing new selections.
		return true
	}
	return ValidateProviderProfileID(id) == nil
}

func managerProviders(root managerObject) ([]string, error) {
	providers, err := managerObjectValue(root["providers"])
	if err != nil {
		return nil, err
	}
	ids := []string{"chatgpt", "opencode-go", "openai-compatible"}
	for id, raw := range providers {
		if id == "opencode-zen" || slices.Contains(ids, id) {
			continue
		}
		// Only locally configured compatible profiles belong in the public list.
		if !managerProviderID(id) {
			return nil, ErrManagerUnavailable
		}
		var profile struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &profile) != nil {
			return nil, ErrManagerUnavailable
		}
		if profile.Type == ProviderTypeOpenAICompatible {
			ids = append(ids, id)
		}
		if len(ids) > MaxManagerProviders {
			return nil, ErrManagerUnavailable
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// ReadManagerProviderIDs reads only local configuration. It never constructs a
// provider or resolves a catalog, and errors intentionally carry no file data.
func ReadManagerProviderIDs(ctx context.Context, path string) ([]string, error) {
	root, err := readManagerDocument(ctx, path)
	if err != nil {
		return nil, err
	}
	return managerProviders(root)
}

func managerString(root managerObject, key string) (*string, error) {
	raw, ok := root[key]
	if !ok {
		return nil, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return nil, ErrManagerUnavailable
	}
	if value == "" {
		return nil, nil
	} // existing config omitempty semantics
	if !managerSafeString(value, 256) {
		return nil, ErrManagerUnavailable
	}
	return new(value), nil
}

func managerEnum(value *string, allowed ...string) error {
	if value != nil && !slices.Contains(allowed, *value) {
		return ErrManagerUnavailable
	}
	return nil
}

func managerStringDefault(root managerObject, key, fallback, source string, allowed ...string) (protocol.HostStringDefault, error) {
	value, err := managerString(root, key)
	if err != nil {
		return protocol.HostStringDefault{}, err
	}
	if err = managerEnum(value, allowed...); err != nil {
		return protocol.HostStringDefault{}, err
	}
	result := protocol.HostStringDefault{Explicit: value, Effective: fallback, Source: source}
	if value != nil {
		result.Effective = *value
		result.Source = "global"
	}
	return result, nil
}

func managerPair(root managerObject, providerKey, modelKey string) (*protocol.HostProviderModel, error) {
	provider, err := managerString(root, providerKey)
	if err != nil {
		return nil, err
	}
	model, err := managerString(root, modelKey)
	if err != nil {
		return nil, err
	}
	if provider == nil && model == nil {
		return nil, nil
	}
	pair := &protocol.HostProviderModel{}
	if provider != nil {
		if !managerProviderID(*provider) {
			return nil, ErrManagerUnavailable
		}
		pair.Provider = *provider
	}
	if model != nil {
		pair.Model = *model
	}
	return pair, nil
}

var managerThinking = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}

func managerSnapshot(root managerObject, request protocol.HostDefaultsRequest) (protocol.HostDefaultsResponse, error) {
	response := protocol.HostDefaultsResponse{Scope: request.Scope, CWD: request.CWD, AppliesTo: "future_runtime"}
	pair, err := managerPair(root, "default_provider", "default_model")
	if err != nil {
		return response, err
	}
	global := protocol.HostGlobalDefaults{ProviderModel: protocol.HostProviderModelDefault{Explicit: pair, Effective: protocol.HostProviderModel{Provider: "opencode-go"}, Source: "builtin"}}
	if pair != nil {
		global.ProviderModel.Source = "global"
		if pair.Provider != "" {
			global.ProviderModel.Effective.Provider = pair.Provider
		}
		global.ProviderModel.Effective.Model = pair.Model
	}
	global.Thinking, err = managerStringDefault(root, "thinking", "off", "builtin", managerThinking...)
	if err != nil {
		return response, err
	}
	global.ReasoningSummary, err = managerStringDefault(root, "reasoning_summary", "auto", "builtin", "off", "auto", "concise", "detailed")
	if err != nil {
		return response, err
	}
	global.TextVerbosity, err = managerStringDefault(root, "text_verbosity", "low", "builtin", "low", "medium", "high")
	if err != nil {
		return response, err
	}
	selections, err := managerObjectValue(root["project_selections"])
	if err != nil || len(selections) > MaxProjectSelections {
		return response, ErrManagerUnavailable
	}
	revisionRaw := root["_manager_defaults_revision"]
	if request.Scope == "global" {
		response.Global = &global
	} else {
		selection, err := managerObjectValue(selections[request.CWD])
		if err != nil {
			return response, err
		}
		pair, err := managerPair(selection, "provider", "model")
		if err != nil {
			return response, err
		}
		project := protocol.HostProjectDefaults{ProviderModel: global.ProviderModel, Thinking: global.Thinking}
		project.ProviderModel.Explicit = pair
		project.Thinking.Explicit = nil
		if pair != nil {
			project.ProviderModel.Source = "project"
			if pair.Provider != "" && pair.Provider != project.ProviderModel.Effective.Provider {
				project.ProviderModel.Effective.Model = ""
			}
			if pair.Provider != "" {
				project.ProviderModel.Effective.Provider = pair.Provider
			}
			if pair.Model != "" {
				project.ProviderModel.Effective.Model = pair.Model
			}
		}
		thinking, err := managerString(selection, "thinking")
		if err != nil {
			return response, err
		}
		if managerEnum(thinking, managerThinking...) != nil {
			return response, ErrManagerUnavailable
		}
		if thinking != nil {
			project.Thinking = protocol.HostStringDefault{Explicit: thinking, Effective: *thinking, Source: "project"}
		}
		response.Project = &project
		revisionRaw = selection["_manager_defaults_revision"]
	}
	// Hash only the public scoped projection, never the config, credentials or
	// unrelated fields. The opaque random mutation marker fences same-value ABA.
	var nonce string
	if len(revisionRaw) > 0 {
		if json.Unmarshal(revisionRaw, &nonce) != nil || len(nonce) != 32 {
			return response, ErrManagerUnavailable
		}
		if _, err := hex.DecodeString(nonce); err != nil {
			return response, ErrManagerUnavailable
		}
	}
	data, err := json.Marshal(struct {
		Snapshot protocol.HostDefaultsResponse
		Nonce    string
	}{response, nonce})
	if err != nil {
		return response, ErrManagerUnavailable
	}
	digest := sha256.Sum256(data)
	response.Revision = hex.EncodeToString(digest[:])
	return response, nil
}

// ReadManagerDefaults does not create directories, locks, or migrate files.
func ReadManagerDefaults(ctx context.Context, path string, request protocol.HostDefaultsRequest) (protocol.HostDefaultsResponse, error) {
	if err := ctx.Err(); err != nil {
		return protocol.HostDefaultsResponse{}, err
	}
	request, err := CanonicalManagerScope(request)
	if err != nil {
		return protocol.HostDefaultsResponse{}, err
	}
	root, err := readManagerDocument(ctx, path)
	if err != nil {
		return protocol.HostDefaultsResponse{}, err
	}
	return managerSnapshot(root, request)
}
