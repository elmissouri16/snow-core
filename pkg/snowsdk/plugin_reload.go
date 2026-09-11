package snowsdk

import (
	"context"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ReloadPlugin atomically replaces one already-loaded, enabled JavaScript
// plugin while idle. Applied results may include post-commit diagnostics.
func (s *Session) ReloadPlugin(ctx context.Context, id string) (protocol.PluginReloadResult, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.PluginReloadResult{}, err
	}
	return a.ReloadPlugin(ctx, id)
}
