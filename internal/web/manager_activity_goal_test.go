package web

import (
	"encoding/json/v2"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestManagerActivityGoalRunHTTPCountsExcludeGoalContentAndAuthority(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project := managerActivityAdd(t, s, "Goal project")
	const publicProjectID = "00000000-0000-4000-8000-000000008765"
	if _, err := s.registry.db.ExecContext(t.Context(), `UPDATE projects SET id=? WHERE id=?`, publicProjectID, project.ID); err != nil {
		t.Fatal(err)
	}
	project.ID = publicProjectID
	publicTime := time.Date(2026, time.April, 5, 6, 7, 8, 876_500_000, time.UTC)
	s.now = func() time.Time { return publicTime }
	snapshot := RuntimeSnapshot{
		ProjectID: project.ID, SessionID: "goal-session", Status: "running",
		Goal:     &RuntimeGoal{SessionID: "goal-session", GoalID: "private-goal-id", BranchID: "private-branch-id", TipID: "private-tip-id", Objective: strings.Repeat("private-objective", 10000), BlockedReason: "private-blocked-reason", GoalRunID: "private-run-authority", Running: true, TokenBudget: new(int64(918_273_645)), TokensUsed: 8765},
		Recovery: RecoveryHint{SessionID: "goal-session", State: RecoveryAdmitted, UpdatedAt: publicTime},
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
			assertManagerActivityPublicSchema(t, response.Body.Bytes())
			result := managerActivityDecode(t, response)
			running, permissions, questions, failed := 1, 0, 0, 0
			recoveryState := RecoveryAdmitted
			switch status {
			case "permission":
				permissions = 1
			case "input":
				questions = 1
			case "idle":
				running = 0
				failed = 1
				recoveryState = RecoveryFailed
			}
			wantCounts := ManagerActivityCounts{Registered: 1, Running: running, Permissions: permissions, Questions: questions, Failed: failed}
			if result.Counts != wantCounts {
				t.Fatalf("goal activity counts = %+v, want %+v", result.Counts, wantCounts)
			}
			wantProject := ManagerActivityProject{
				ProjectID: publicProjectID, Name: "Goal project", ProjectURL: "/?project=" + publicProjectID + "&view=projects",
				SessionID: "goal-session", SessionURL: "/?project=" + publicProjectID + "&session=goal-session&view=projects",
				FolderState: "available", RuntimeState: status, HostRunning: running == 1,
				Permissions: permissions, Questions: questions, Failed: failed == 1, RecoveryState: recoveryState,
			}
			if len(result.Projects) != 1 || result.Projects[0] != wantProject || !result.UpdatedAt.Equal(publicTime) {
				t.Fatalf("goal activity projection = %+v at %s, want %+v at %s", result.Projects, result.UpdatedAt, wantProject, publicTime)
			}
			for _, forbidden := range []string{"private-", "objective", "goal_id", "goal_run_id", "branch_id", "tip_id", "token_budget", "tokens_used", "918273645"} {
				if strings.Contains(response.Body.String(), forbidden) {
					t.Fatalf("Activity exposed goal field/content %q: %s", forbidden, response.Body.String())
				}
			}
			// The private usage value deliberately collides with a public project ID
			// suffix and timestamp fraction. Exactly four occurrences belong to those
			// public fields; an exposed goal count would add another occurrence.
			if got := strings.Count(response.Body.String(), "8765"); got != 4 {
				t.Fatalf("Activity numeric canary occurrences = %d, want 4: %s", got, response.Body.String())
			}
		})
	}
	if catalog.calls != 0 || len(backend.calls) != 0 {
		t.Fatal("Activity observation activated or controlled a goal/catalog")
	}
}

func assertManagerActivityPublicSchema(t *testing.T, body []byte) {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	assertManagerActivityKeys(t, "Activity", document, []string{"counts", "projects", "updated_at"})
	counts, ok := document["counts"].(map[string]any)
	if !ok {
		t.Fatalf("Activity counts are not an object: %s", body)
	}
	assertManagerActivityKeys(t, "Activity counts", counts, []string{"failed", "permissions", "questions", "queued", "recovery", "registered", "review", "running"})
	projects, ok := document["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Fatalf("Activity projects are not one-item array: %s", body)
	}
	project, ok := projects[0].(map[string]any)
	if !ok {
		t.Fatalf("Activity project is not an object: %s", body)
	}
	assertManagerActivityKeys(t, "Activity project", project, []string{"failed", "folder_state", "host_running", "name", "permissions", "project_id", "project_url", "questions", "queued", "recovery", "recovery_state", "review", "runtime_state", "session_id", "session_url", "unavailable"})
}

func assertManagerActivityKeys(t *testing.T, label string, object map[string]any, want []string) {
	t.Helper()
	if got := slices.Sorted(maps.Keys(object)); !slices.Equal(got, want) {
		t.Fatalf("%s fields = %v, want %v", label, got, want)
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
