package config

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func managerPut(root managerObject, key string, value any) { root[key], _ = json.Marshal(value) }

func managerStringOp(root managerObject, key string, op *protocol.HostStringOperation, allowed []string) error {
	if op == nil {
		return nil
	}
	switch op.Op {
	case "reset":
		if op.Value != nil {
			return ErrManagerInvalid
		}
		delete(root, key)
	case "set":
		if op.Value == nil || !slices.Contains(allowed, *op.Value) {
			return ErrManagerInvalid
		}
		managerPut(root, key, *op.Value)
	default:
		return ErrManagerInvalid
	}
	return nil
}

func managerPairOp(root managerObject, providerKey, modelKey string, op *protocol.HostProviderModelOperation, ids []string) error {
	if op == nil {
		return nil
	}
	switch op.Op {
	case "reset":
		if op.Value != nil {
			return ErrManagerInvalid
		}
		delete(root, providerKey)
		delete(root, modelKey)
	case "set":
		if op.Value == nil || !slices.Contains(ids, op.Value.Provider) || !managerSafeString(op.Value.Model, 256) {
			return ErrManagerInvalid
		}
		managerPut(root, providerKey, op.Value.Provider)
		// Set carries a complete provider/model pair; reset removes both.
		delete(root, modelKey)
		if op.Value.Model != "" {
			managerPut(root, modelKey, op.Value.Model)
		}
	default:
		return ErrManagerInvalid
	}
	return nil
}

func validateManagerUpdate(request protocol.HostDefaultsUpdateRequest) error {
	if len(request.Revision) != 64 {
		return ErrManagerInvalid
	}
	if request.Scope == "global" {
		if request.Global == nil || request.Project != nil {
			return ErrManagerInvalid
		}
	} else if request.Scope == "project" {
		if request.Project == nil || request.Global != nil {
			return ErrManagerInvalid
		}
	} else {
		return ErrManagerInvalid
	}
	return nil
}

// UpdateManagerDefaults shares config.json.lock with existing config writers.
// The lock is never removed or stolen based on age. It is cancellable while
// waiting, and all reading, validation and CAS happen after lock acquisition.
func UpdateManagerDefaults(ctx context.Context, path string, request protocol.HostDefaultsUpdateRequest) (protocol.HostDefaultsResponse, error) {
	var zero protocol.HostDefaultsResponse
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := validateManagerUpdate(request); err != nil {
		return zero, err
	}
	scope, err := CanonicalManagerScope(protocol.HostDefaultsRequest{Scope: request.Scope, CWD: request.CWD})
	if err != nil {
		return zero, err
	}
	root, base, err := openManagerParent(path, true)
	if err != nil {
		return zero, err
	}
	defer root.Close()
	lock, err := openPinnedKeybindingLock(root, base+".lock")
	if err != nil {
		return zero, ErrManagerUnavailable
	}
	defer lock.Close()
	if err = lockManagerFile(ctx, lock); err != nil {
		return zero, err
	}
	defer unlockConfigFile(lock)
	if verifyPinnedKeybindingFile(root, base+".lock", lock) != nil {
		return zero, ErrManagerUnavailable
	}
	document, before, err := readManagerRoot(ctx, root, base)
	if err != nil {
		return zero, err
	}
	current, err := managerSnapshot(document, scope)
	if err != nil {
		return zero, err
	}
	if request.Revision != current.Revision {
		return zero, ErrManagerConflict
	}
	ids, err := managerProviders(document)
	if err != nil {
		return zero, err
	}
	target := document
	var selections managerObject
	if scope.Scope == "project" {
		selections, err = managerObjectValue(document["project_selections"])
		if err != nil {
			return zero, err
		}
		target, err = managerObjectValue(selections[scope.CWD])
		if err != nil {
			return zero, err
		}
	}
	// Work on a raw section, preserving unknown and unrelated JSON fields.
	if request.Global != nil {
		patch := request.Global
		if err = managerPairOp(target, "default_provider", "default_model", patch.ProviderModel, ids); err != nil {
			return zero, err
		}
		if err = managerStringOp(target, "thinking", patch.Thinking, managerThinking); err != nil {
			return zero, err
		}
		if err = managerStringOp(target, "reasoning_summary", patch.ReasoningSummary, []string{"off", "auto", "concise", "detailed"}); err != nil {
			return zero, err
		}
		if err = managerStringOp(target, "text_verbosity", patch.TextVerbosity, []string{"low", "medium", "high"}); err != nil {
			return zero, err
		}
	} else {
		patch := request.Project
		if err = managerPairOp(target, "provider", "model", patch.ProviderModel, ids); err != nil {
			return zero, err
		}
		if err = managerStringOp(target, "thinking", patch.Thinking, managerThinking); err != nil {
			return zero, err
		}
	}
	managerPut(target, "_manager_defaults_revision", managerNonce())
	if scope.Scope == "project" {
		managerPut(selections, scope.CWD, target)
		if len(selections) > MaxProjectSelections {
			return zero, ErrManagerInvalid
		}
		managerPut(document, "project_selections", selections)
	}
	next, err := managerSnapshot(document, scope)
	if err != nil {
		return zero, err
	}
	data, err := json.Marshal(document, jsontext.WithIndent("  "))
	if err != nil {
		return zero, ErrManagerUnavailable
	}
	data = append(data, '\n')
	if err = writeManagerRoot(ctx, root, base, before, data); err != nil {
		return zero, err
	}
	return next, nil
}
