package session

import (
	"context"
	"errors"
	"reflect"
	"slices"
)

const (
	messageEditMaxEntries      = 100000
	messageEditMaxHistoryBytes = 32 << 20
)

var errMessageEditHistoryBounds = errors.New("message edit history exceeds safe traversal or byte bounds")

// Only stores with a bounded, cancellation-aware implementation support edits.
// There is deliberately no fallback to the unbounded BranchEntries contract.
type messageEditHistoryStore interface {
	messageEditEntries(context.Context) ([]Entry, error)
}

func (s *MemoryStore) messageEditEntries(ctx context.Context) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return s.boundedVersionEntriesLocked(ctx, s.tip)
}

func (s *MemoryStore) boundedVersionEntriesLocked(ctx context.Context, tip string) ([]Entry, error) {
	// First inspect original references; never clone payloads until the complete
	// path has passed both limits. Index storage itself is bounded by entry count.
	indexes := make([]int, 0)
	remaining := messageEditMaxHistoryBytes
	for id := tip; id != ""; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(indexes) == messageEditMaxEntries {
			return nil, errMessageEditHistoryBounds
		}
		index, ok := s.byID[id]
		if !ok {
			return nil, errors.New("message edit history has a missing ancestor")
		}
		if err := chargeMessageEditValue(ctx, reflect.ValueOf(s.entries[index]), &remaining, 0); err != nil {
			return nil, err
		}
		indexes = append(indexes, index)
		id = s.entries[index].ParentID
	}
	slices.Reverse(indexes)
	entries := make([]Entry, 0, len(indexes))
	for _, index := range indexes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries = append(entries, cloneEntry(s.entries[index]))
	}
	return entries, nil
}

// Charge the decoded footprint without serializing or copying it. Reflection
// keeps private/plugin payloads and future protocol fields inside the bound too.
// Struct/slice overhead is conservatively overcounted; nesting and cycles fail
// closed before recursive Clone methods can consume an unbounded stack.
func chargeMessageEditValue(ctx context.Context, v reflect.Value, remaining *int, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !v.IsValid() {
		return nil
	}
	if depth > 128 || v.Type().Size() > uintptr(*remaining) {
		return errMessageEditHistoryBounds
	}
	*remaining -= int(v.Type().Size())
	switch v.Kind() {
	case reflect.String:
		if v.Len() > *remaining {
			return errMessageEditHistoryBounds
		}
		*remaining -= v.Len()
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return chargeMessageEditValue(ctx, v.Elem(), remaining, depth+1)
		}
	case reflect.Slice, reflect.Array:
		if uint64(v.Len())*uint64(v.Type().Elem().Size()) > uint64(*remaining) {
			return errMessageEditHistoryBounds
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			*remaining -= v.Len()
			return nil
		}
		for i := range v.Len() {
			if err := chargeMessageEditValue(ctx, v.Index(i), remaining, depth+1); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if err := chargeMessageEditValue(ctx, v.Field(i), remaining, depth+1); err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if err := chargeMessageEditValue(ctx, iter.Key(), remaining, depth+1); err != nil {
				return err
			}
			if err := chargeMessageEditValue(ctx, iter.Value(), remaining, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
