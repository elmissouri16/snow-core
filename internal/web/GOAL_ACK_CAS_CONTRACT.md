# Chosen ACK + consent CAS contract (parent relay frontend now)

Child implementing `expected_revision` (required canonical positive uint64 form
field) with `RuntimeGoalRunInput` wrapper embedding RPCGoalRunParams plus
ExpectedRevision. RunGoal compares this EXACTLY to r.snapshot.Revision UNDER
shared control+mu BEFORE savePromptIntent or any mutation. Browser must capture
reviewed snapshot.Revision when dialog opens and include expected_revision on
start/resume; stale mode/provider/model/permission/read-update conflicts reject,
never silently refreshing or retrying. Inspect remains read-only/no revision CAS.

Parent please add existing RuntimeSnapshot field:

    GoalRunACK *RuntimeGoalRunACK `json:"goal_run_ack,omitempty"`

Child sets receipt ONLY on returned ACK snapshot clone, after buffered events
are replayed. It is never retained in r.snapshot, never future execution authority.
It remains present even when that returned snapshot is idle (fast completion).
Parent clone should deep-copy nonnil GoalRunACK if snapshot.clone ever receives
one (optional defensive ownership). No other runtime.go edits by child.

Receipt fields: goal_run_id, goal_id, session_id, branch_id (core accepted DTO).
ExpectedRevision is web-only; it MUST NOT enter core strict goal_run params.

Wrapper implementation and RuntimeGoalRunACK alias NOW EXIST; frontend can send expected_revision. Build waits only for parent-owned snapshot field.
