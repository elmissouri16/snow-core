package protocol

import "testing"

func TestInputQueueCloneAndEventIsolation(t *testing.T) {
	queue := &InputQueue{Items: []QueuedInput{{ID: "q1", Kind: QueuedInputSteer, Text: "original", Order: 1}}}
	event := AgentEvent{Type: EvQueueUpdated, Queue: queue}
	clone := event.Clone()
	clone.Queue.Items[0].Text = "changed"
	clone.Queue.Items = append(clone.Queue.Items, QueuedInput{ID: "q2"})
	if queue.Items[0].Text != "original" || len(queue.Items) != 1 {
		t.Fatalf("queue clone aliased source: %+v", queue)
	}
	steering, followUps := queue.Counts()
	if steering != 1 || followUps != 0 {
		t.Fatalf("counts = %d/%d", steering, followUps)
	}
}
