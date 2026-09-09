package plugin

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"math"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ValidateNode bounds both traversal and serialized presentation data.
func ValidateNode(node protocol.PluginNode) error {
	count := 0
	var visit func(protocol.PluginNode, int) error
	visit = func(n protocol.PluginNode, depth int) error {
		count++
		if count > 256 || depth > 16 {
			return errors.New("plugin view exceeds component limit")
		}
		if !slices.Contains([]string{"row", "column", "text", "markdown", "table", "list", "progress", "button", "input", "select", "checkbox"}, n.Type) {
			return errors.New("unknown plugin component")
		}
		if n.Tone != "" && !slices.Contains([]string{"accent", "muted", "foreground", "warning", "error", "success"}, n.Tone) {
			return errors.New("unknown component tone")
		}
		if math.IsNaN(n.Value) || math.IsInf(n.Value, 0) || len(n.Rows) > 256 || len(n.Columns) > 16 {
			return errors.New("invalid component data")
		}
		for _, row := range n.Rows {
			if len(row) > 16 {
				return errors.New("too many table columns")
			}
		}
		for _, child := range n.Children {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(node, 0); err != nil {
		return err
	}
	raw, err := jsonv2.Marshal(node)
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 {
		return errors.New("plugin view exceeds 64 KiB")
	}
	return nil
}
