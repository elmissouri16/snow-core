package web

// Integration hooks (all Locked methods require r.mu):
//   - RuntimeSnapshot: Steer *RuntimeSteer `json:"steer"`,
//     SteerACK *RuntimeSteerACK `json:"steer_ack,omitempty"` (response-only).
//   - liveRuntime: steer runtimeSteerState; steerSupported bool.
//   - initialize steerSupported from the managed_steer capability.
//   - clone snapshot via s.Steer = s.Steer.clone(); copy SteerACK if nonnil.
//   - publishLocked calls refreshSteerLocked after refreshQueueLocked.
//   - after exact event/root validation, queue_updated calls
//     applySteerControlLocked(q), including events after provider failure.
//   - queue validator additionally accepts steer_accepted/discarded
//     (bounded nonempty ItemID; empty Text). Generic delivered retains its
//     existing durable source/text validation and normal source projection.
//   - Queue next admission/projection adds steerPendingUsageLocked() to its
//     native items/review capacity check. Do not add steering to Queue.Items.
//   - session replacement clears r.steer (root replacement must NOT clear it:
//     late receipts are reconciled only with their original private root).
//   - shell routes action "steer" to runtimeSteerAction after normal CSRF/body
//     parsing. Frontend SnowSteer API mirrors SnowQueue; include steer assets
//     and {{template "steer" .}} in the live surface, add [data-steer-open].
// Frontend init uses {root, session, request, validText, changed, writeDraft};
// render(snapshot, {connected,busy,unknown,stopping,editing,status}, present).
// Parent owns these shared integration points; this file intentionally declares
// no competing controller fields. Tests/builds stay paused for a serial slot.
