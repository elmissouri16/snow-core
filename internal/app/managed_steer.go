package app

import "github.com/elmissouri16/snow-core/pkg/protocol"

// ManagedSteer admits literal text to an exact active native user root. It does
// not run commands, expand skills, create a prompt, or schedule follow-up work.
// No app lifecycle lock is needed: identity and admission are one core queue
// transaction, which rejects a concurrent delivery rather than waiting for it.
func (a *App) ManagedSteer(p protocol.RPCManagedSteerParams) (protocol.RPCManagedSteerResult, error) {
	return a.Agent.ManagedSteer(p)
}
