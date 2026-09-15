package app

import "github.com/elmissouri16/snow-core/pkg/protocol"

func (a *App) QueueList(p protocol.RPCQueueListParams) (protocol.QueueControl, error) {
	return a.Agent.QueueControlSnapshot(p)
}
func (a *App) QueueEnqueue(p protocol.RPCQueueEnqueueParams) (protocol.QueueControl, error) {
	return a.Agent.MutateQueueControl("queue_enqueue", protocol.RPCQueueUpdateParams{SessionID: p.SessionID, TurnID: p.TurnID, Revision: p.Revision, Text: p.Text})
}
func (a *App) QueueUpdate(p protocol.RPCQueueUpdateParams) (protocol.QueueControl, error) {
	return a.Agent.MutateQueueControl("queue_update", p)
}
func (a *App) QueueRemove(p protocol.RPCQueueRemoveParams) (protocol.QueueControl, error) {
	return a.Agent.MutateQueueControl("queue_remove", protocol.RPCQueueUpdateParams{SessionID: p.SessionID, TurnID: p.TurnID, Revision: p.Revision, ItemID: p.ItemID})
}
