package session

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// MetaPluginWorkflow stores append-only, provider-excluded workflow changes.
// It uses ordinary metadata entries and does not require a schema migration.
const MetaPluginWorkflow = "plugin_workflow_v1"

const (
	MaxWorkflowKeyBytes       = 128
	MaxWorkflowValueBytes     = 64 << 10
	MaxWorkflowStateBytes     = 1 << 20
	MaxWorkflowKeys           = 1024
	MaxWorkflowMutations      = 64
	MaxWorkflowUpdateBytes    = 128 << 10
	MaxWorkflowHistoryBytes   = 16 << 20
	MaxWorkflowHistoryRecords = 16384
	MaxWorkflowToolNames      = 512
)

// WorkflowStateStore is optional: custom Store implementations need not support
// plugin workflows. Updates compare both the active branch and its exact tip.
type WorkflowStateStore interface {
	WorkflowState(context.Context, string) (WorkflowState, error)
	ApplyWorkflowState(context.Context, string, string, string, WorkflowUpdate) (WorkflowState, error)
}

// ToolRestriction is one owner's restriction, not the effective tool policy.
// A nil Allow imposes no allowlist; a non-nil empty Allow permits no tools.
type ToolRestriction struct {
	Allow *[]string `json:"allow,omitzero"`
	Deny  []string  `json:"deny,omitempty"`
}

// WorkflowState is a defensive projection of one owner's complete ancestry.
// Values includes explicitly stored JSON null; absent keys are not in the map.
type WorkflowState struct {
	Values      map[string]json.RawMessage `json:"values"`
	Restriction *ToolRestriction           `json:"restriction,omitzero"`
	BranchID    string                     `json:"branch_id"`
	TipID       string                     `json:"tip_id"`
}

// WorkflowUpdate atomically replaces/deletes values and optionally replaces or
// clears the owner's restriction. Omitted restriction fields preserve policy.
// Set/Delete overlap, duplicate deletes, and empty updates are invalid.
type WorkflowUpdate struct {
	Set              map[string]json.RawMessage `json:"set,omitempty"`
	Delete           []string                   `json:"delete,omitempty"`
	Restriction      *ToolRestriction           `json:"restriction,omitzero"`
	ClearRestriction bool                       `json:"clear_restriction,omitzero"`
}

type workflowRecord struct {
	Version  int            `json:"version"`
	PluginID string         `json:"plugin_id"`
	Update   WorkflowUpdate `json:"update"`
}

var (
	workflowOwnerRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	workflowToolRE  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,127}$`)
)

func validateWorkflowOwner(id string) error {
	if !workflowOwnerRE.MatchString(id) {
		return errors.New("session: invalid workflow plugin id")
	}
	return nil
}

func validateWorkflowKey(key string) error {
	if len(key) == 0 || len(key) > MaxWorkflowKeyBytes || strings.ContainsRune(key, 0) || !utf8.ValidString(key) {
		return errors.New("session: workflow key must be valid UTF-8, 1..128 bytes, without NUL")
	}
	return nil
}

func validateWorkflowUpdate(update WorkflowUpdate) error {
	mutations := len(update.Set) + len(update.Delete)
	if update.Restriction != nil || update.ClearRestriction {
		mutations++
	}
	if mutations == 0 || mutations > MaxWorkflowMutations {
		return errors.New("session: workflow update must contain 1..64 mutations")
	}
	if update.Restriction != nil && update.ClearRestriction {
		return errors.New("session: cannot replace and clear a workflow restriction together")
	}
	for key, value := range update.Set {
		if err := validateWorkflowKey(key); err != nil {
			return err
		}
		if len(value) == 0 || len(value) > MaxWorkflowValueBytes {
			return errors.New("session: workflow value exceeds 64 KiB or is empty")
		}
		var decoded any
		if err := jsonv2.Unmarshal(value, &decoded); err != nil {
			return errors.New("session: invalid workflow JSON value")
		}
	}
	deleted := make(map[string]bool, len(update.Delete))
	for _, key := range update.Delete {
		if err := validateWorkflowKey(key); err != nil {
			return err
		}
		if _, ok := update.Set[key]; ok {
			return errors.New("session: workflow key cannot be set and deleted together")
		}
		if deleted[key] {
			return errors.New("session: duplicate workflow deletion")
		}
		deleted[key] = true
	}
	if r := update.Restriction; r != nil {
		lists := [][]string{r.Deny}
		if r.Allow != nil {
			lists = append(lists, *r.Allow)
		}
		for _, names := range lists {
			if len(names) > MaxWorkflowToolNames {
				return errors.New("session: workflow restriction exceeds 512 tool names")
			}
			seen := make(map[string]bool, len(names))
			for _, name := range names {
				if !workflowToolRE.MatchString(name) || seen[name] {
					return errors.New("session: invalid or duplicate workflow tool name")
				}
				seen[name] = true
			}
		}
	}
	return nil
}

func encodeWorkflowUpdate(pluginID string, update WorkflowUpdate) (string, WorkflowUpdate, error) {
	if err := validateWorkflowOwner(pluginID); err != nil {
		return "", WorkflowUpdate{}, err
	}
	if err := validateWorkflowUpdate(update); err != nil {
		return "", WorkflowUpdate{}, err
	}
	raw, err := jsonv2.Marshal(workflowRecord{Version: 1, PluginID: pluginID, Update: update}, jsonv2.Deterministic(true))
	if err != nil {
		return "", WorkflowUpdate{}, fmt.Errorf("session: encode workflow: %w", err)
	}
	if len(raw) > MaxWorkflowUpdateBytes {
		return "", WorkflowUpdate{}, errors.New("session: workflow update exceeds 128 KiB")
	}
	// Decoding the serialized representation both detaches caller-owned buffers
	// and normalizes JSON whitespace before applying the live-state quota.
	record, err := decodeWorkflowRecord(string(raw))
	return string(raw), record.Update, err
}

func decodeWorkflowRecord(raw string) (workflowRecord, error) {
	var record workflowRecord
	if len(raw) > MaxWorkflowUpdateBytes {
		return record, errors.New("session: workflow record exceeds 128 KiB")
	}
	if err := jsonv2.Unmarshal([]byte(raw), &record, jsonv2.RejectUnknownMembers(true)); err != nil {
		return record, errors.New("session: malformed workflow record")
	}
	if record.Version != 1 {
		return record, errors.New("session: unsupported workflow record version")
	}
	if err := validateWorkflowOwner(record.PluginID); err != nil {
		return record, err
	}
	if err := validateWorkflowUpdate(record.Update); err != nil {
		return record, err
	}
	return record, nil
}

func cloneToolRestriction(r *ToolRestriction) *ToolRestriction {
	if r == nil {
		return nil
	}
	out := &ToolRestriction{Deny: slices.Clone(r.Deny)}
	if r.Allow != nil {
		out.Allow = new(slices.Clone(*r.Allow))
	}
	return out
}

func applyWorkflowUpdate(state *WorkflowState, update WorkflowUpdate) error {
	for key, raw := range update.Set {
		state.Values[key] = bytes.Clone(raw)
	}
	for _, key := range update.Delete {
		delete(state.Values, key)
	}
	if update.ClearRestriction {
		state.Restriction = nil
	}
	if update.Restriction != nil {
		state.Restriction = cloneToolRestriction(update.Restriction)
	}
	if len(state.Values) > MaxWorkflowKeys {
		return errors.New("session: workflow state exceeds 1024 keys")
	}
	size := 0
	for key, value := range state.Values {
		size += len(key) + len(value)
	}
	if size > MaxWorkflowStateBytes {
		return errors.New("session: workflow state exceeds 1 MiB")
	}
	return nil
}

func projectWorkflow(ctx context.Context, pluginID, branchID, tipID string, records []string) (WorkflowState, error) {
	state := WorkflowState{Values: make(map[string]json.RawMessage), BranchID: branchID, TipID: tipID}
	for _, raw := range records {
		if err := ctx.Err(); err != nil {
			return WorkflowState{}, err
		}
		record, err := decodeWorkflowRecord(raw)
		if err != nil {
			return WorkflowState{}, err
		}
		if record.PluginID == pluginID {
			if err := applyWorkflowUpdate(&state, record.Update); err != nil {
				return WorkflowState{}, err
			}
		}
	}
	return state, ctx.Err()
}

func checkWorkflowHistory(records, size, addedBytes int) error {
	if addedBytes > 0 {
		records++
	}
	if records > MaxWorkflowHistoryRecords || size > MaxWorkflowHistoryBytes-addedBytes {
		return errors.New("session: workflow history quota exceeded")
	}
	return nil
}
