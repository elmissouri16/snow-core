package web

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestManagerActivityGoalRunHTTPCountsExcludeGoalContentAndAuthority(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project := managerActivityAdd(t, s, "Goal project")
	snapshot := RuntimeSnapshot{
		ProjectID: project.ID, SessionID: "goal-session", Status: "running",
		Goal:     &RuntimeGoal{SessionID: "goal-session", GoalID: "private-goal-id", BranchID: "private-branch-id", TipID: "private-tip-id", Objective: strings.Repeat("private-objective", 10000), BlockedReason: "private-blocked-reason", GoalRunID: "private-run-authority", Running: true, TokenBudget: new(int64(9000)), TokensUsed: 8765},
		Recovery: RecoveryHint{SessionID: "goal-session", State: RecoveryAdmitted, UpdatedAt: time.Now()},
	}
	backend := &managerActivityBackend{snapshots: map[string]RuntimeSnapshot{project.ID: snapshot}}
	s.runtimes = backend
	for _, status := range []string{"running", "permission", "input", "idle"} {
		t.Run(status, func(t *testing.T) {
			snapshot.Status = status
			// A leftover goal Running flag is not stronger evidence than current runtime
			// status; completed runs must not retain attention or host-running counts.
			if status == "idle" {
				snapshot.Recovery.State = RecoveryFailed
			}
			backend.snapshots[project.ID] = snapshot
			response := request(t, s, "GET", "/activity", nil, cookie)
			result := managerActivityDecode(t, response)
			running, permissions, questions, failed := 1, 0, 0, 0
			switch status {
			case "permission":
				permissions = 1
			case "input":
				questions = 1
			case "idle":
				running = 0
				failed = 1
			}
			if result.Counts.Running != running || result.Counts.Permissions != permissions || result.Counts.Questions != questions || result.Counts.Failed != failed {
				t.Fatalf("goal activity counts = %+v", result.Counts)
			}
			for _, forbidden := range []string{"private-", "objective", "goal_id", "goal_run_id", "branch_id", "tip_id", "token_budget", "tokens_used", "8765"} {
				if strings.Contains(response.Body.String(), forbidden) {
					t.Fatalf("Activity exposed goal field/content %q", forbidden)
				}
			}
			if len(result.Projects) != 1 || result.Projects[0].SessionID != "goal-session" {
				t.Fatal("goal run lost exact-session navigation")
			}
		})
	}
	if catalog.calls != 0 || len(backend.calls) != 0 {
		t.Fatal("Activity observation activated or controlled a goal/catalog")
	}
}

func TestManagerActivityStaleCardHTTPNavigationCannotExposeNewSessionControls(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project := managerActivityAdd(t, s, "Session ownership")
	backend := &managerActivityBackend{snapshots: map[string]RuntimeSnapshot{project.ID: {ProjectID: project.ID, SessionID: "original-session", InstanceID: "original-instance", Status: "permission"}}}
	s.runtimes = backend
	result := managerActivityDecode(t, request(t, s, "GET", "/activity", nil, cookie))
	if len(result.Projects) != 1 {
		t.Fatal("Activity card missing")
	}
	link := result.Projects[0].SessionURL
	backend.snapshots[project.ID] = RuntimeSnapshot{ProjectID: project.ID, SessionID: "replacement-session", InstanceID: "replacement-instance", Status: "input"}
	response := request(t, s, "GET", link, nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Another session owns this project") {
		t.Fatalf("stale Activity target = %d %s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{`data-runtime="true"`, `data-runtime-open`, `replacement-instance`} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("stale Activity card exposed new-session authority %q", forbidden)
		}
	}
	if catalog.calls != 0 || len(backend.calls) != 0 {
		t.Fatal("stale Activity navigation activated or queried a foreign session")
	}
}
