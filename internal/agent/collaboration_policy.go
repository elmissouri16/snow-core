package agent

import (
	"fmt"

	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// collaborationToolAllowed is the shared collaboration-mode boundary used for
// provider schema exposure and final dispatch. Conditional tools must perform
// their argument- and runtime-specific checks inside their owning subsystem.
func collaborationToolAllowed(mode protocol.CollaborationMode, desc tools.DescriptorMetadata) bool {
	if mode != protocol.ModePlan {
		return true
	}
	switch desc.Effect {
	case tools.EffectReadOnly:
		return true
	case tools.EffectConditional:
		return desc.PlanGuarded
	default:
		return false
	}
}

func collaborationToolDeniedMessage(name string) string {
	return fmt.Sprintf("Error: %s is blocked in Plan mode because it may mutate state; switch to Default mode explicitly before running it", name)
}
