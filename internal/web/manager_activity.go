package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const managerActivityMaxBytes = 256 << 10

// ManagerActivitySummary is a navigation-only projection. Never serialize a
// RuntimeSnapshot here: even its public fields include conversation text and
// local control capabilities that have no place in cross-project summaries.
// Counts overlap (a host run can be waiting for attention); they are not totals
// of mutually exclusive states. No browser connection is inferred from a worker.
type ManagerActivitySummary struct {
	UpdatedAt time.Time                `json:"updated_at"`
	Projects  []ManagerActivityProject `json:"projects"`
	Counts    ManagerActivityCounts    `json:"counts"`
}

type ManagerActivityCounts struct {
	Registered  int `json:"registered"`
	Running     int `json:"running"`
	Permissions int `json:"permissions"`
	Questions   int `json:"questions"`
	Failed      int `json:"failed"`
	Recovery    int `json:"recovery"`
	Queued      int `json:"queued"`
	Review      int `json:"review"`
}

type ManagerActivityProject struct {
	ProjectID     string        `json:"project_id"`
	Name          string        `json:"name"`
	ProjectURL    string        `json:"project_url"`
	SessionID     string        `json:"session_id"`
	SessionURL    string        `json:"session_url"`
	FolderState   string        `json:"folder_state"`
	RuntimeState  string        `json:"runtime_state"`
	HostRunning   bool          `json:"host_running"`
	Permissions   int           `json:"permissions"`
	Questions     int           `json:"questions"`
	Failed        bool          `json:"failed"`
	Recovery      bool          `json:"recovery"`
	RecoveryState RecoveryState `json:"recovery_state"`
	Queued        int           `json:"queued"`
	Review        int           `json:"review"`
	Unavailable   bool          `json:"unavailable"`
}

// managerActivity only observes registered projects, manager recovery metadata,
// and in-memory snapshots. It never touches catalog/session storage or activates
// a worker, and carries neither command authority nor reconnect replay state.
func (s *shell) managerActivity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Activity is read-only", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	if s.registry == nil {
		http.Error(w, "Project registry is unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	summary, err := s.managerActivitySummary(ctx)
	if err != nil {
		http.Error(w, "Activity could not be refreshed", http.StatusServiceUnavailable)
		return
	}
	// A browser may have been revoked while registry reads were in progress.
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	data, err := json.Marshal(summary)
	if err != nil || len(data) > managerActivityMaxBytes {
		http.Error(w, "Activity could not be refreshed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *shell) managerActivitySummary(ctx context.Context) (ManagerActivitySummary, error) {
	result := ManagerActivitySummary{Projects: make([]ManagerActivityProject, 0)}
	projects, err := s.registry.List(ctx)
	if err != nil {
		return result, err
	}
	for _, project := range projects[:min(len(projects), MaxProjects)] {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		hint, found, err := s.registry.LoadRecovery(ctx, project.ID)
		// Recovery metadata failure is local to this card, not evidence that another
		// project's worker failed. Runtime evidence is sampled after the slower read.
		item := managerActivityProject(project)
		item.Unavailable = err != nil
		var snapshot RuntimeSnapshot
		var live bool
		if s.runtimes != nil {
			snapshot, live = s.runtimes.Snapshot(project.ID)
		}
		if live && snapshot.ProjectID == project.ID {
			item.applyRuntime(snapshot)
		} else if live {
			item.RuntimeState, item.Unavailable = "unknown", true
		} else if found && err == nil {
			item.applyRecovery(hint)
		}
		result.Projects = append(result.Projects, item)
	}
	// A registration removed during a slow snapshot/read must not survive in the
	// completed result. No cached collection can resurrect a removed project.
	current, err := s.registry.List(ctx)
	if err != nil {
		return result, err
	}
	registered := make(map[string]bool, len(current))
	for _, project := range current {
		registered[project.ID] = true
	}
	retained := result.Projects[:0]
	for _, item := range result.Projects {
		if !registered[item.ProjectID] {
			continue
		}
		retained = append(retained, item)
		result.Counts.Registered++
		if item.HostRunning {
			result.Counts.Running++
		}
		result.Counts.Permissions += item.Permissions
		result.Counts.Questions += item.Questions
		if item.Failed {
			result.Counts.Failed++
		}
		if item.Recovery {
			result.Counts.Recovery++
		}
		result.Counts.Queued += item.Queued
		result.Counts.Review += item.Review
	}
	result.Projects = retained
	result.UpdatedAt = s.now().UTC()
	return result, ctx.Err()
}

func managerActivityProject(project Project) ManagerActivityProject {
	item := ManagerActivityProject{ProjectID: project.ID, Name: managerActivityName(project.Name), RuntimeState: "inactive", FolderState: "unavailable"}
	item.ProjectURL = "/?" + url.Values{"view": {"projects"}, "project": {project.ID}}.Encode()
	switch project.State {
	case "available", "missing", "changed":
		item.FolderState = project.State
	}
	return item
}

func managerActivityName(name string) string {
	// Registry names are already bounded; retain a defensive bound for future
	// backends without splitting UTF-8 or exposing control characters.
	name = name[:min(len(name), 128)]
	for !utf8.ValidString(name) && len(name) > 0 {
		name = name[:len(name)-1]
	}
	return strings.TrimSpace(safeVersion(name))
}

func (p *ManagerActivityProject) sessionLink(id string) {
	if !runtimeIdentifier(id) {
		return
	}
	p.SessionID = id
	p.SessionURL = "/?" + url.Values{"view": {"projects"}, "project": {p.ProjectID}, "session": {id}}.Encode()
}

func (p *ManagerActivityProject) applyRuntime(snapshot RuntimeSnapshot) {
	p.sessionLink(snapshot.SessionID)
	switch snapshot.Status {
	case "opening", "idle", "running", "permission", "input", "switching", "closing", "failed":
		p.RuntimeState = snapshot.Status
	default:
		p.RuntimeState, p.Unavailable = "unknown", true
	}
	p.HostRunning = snapshot.Status == "running" || snapshot.Status == "permission" || snapshot.Status == "input"
	// Stale request pointers on terminal/transitioning snapshots confer no live
	// attention. Only the authoritative waiting status contributes to the count.
	if snapshot.Status == "permission" {
		p.Permissions = 1
	}
	if snapshot.Status == "input" {
		p.Questions = 1
	}
	p.Failed = snapshot.Status == "failed"
	if snapshot.Recovery.SessionID == snapshot.SessionID && snapshot.Recovery.valid() {
		p.RecoveryState = snapshot.Recovery.State
		// A current admitted run is not interrupted simply because it has not ended.
		p.Recovery = p.Failed && managerActivityNeedsRecovery(snapshot.Recovery.State)
		if snapshot.Status == "idle" && snapshot.Recovery.State == RecoveryFailed {
			p.Failed = true
		}
	}
	if snapshot.Queue != nil {
		for _, item := range snapshot.Queue.Items[:min(len(snapshot.Queue.Items), protocol.RPCQueueMaxItems)] {
			switch item.State {
			case "pending", "starting":
				if p.HostRunning {
					p.Queued++
				} else {
					p.Review++
				}
			case "held", "uncertain":
				p.Review++
			}
		}
	}
}

func (p *ManagerActivityProject) applyRecovery(hint RecoveryHint) {
	if !hint.valid() {
		p.Unavailable = true
		return
	}
	p.sessionLink(hint.SessionID)
	p.RecoveryState = hint.State
	p.Recovery = managerActivityNeedsRecovery(hint.State)
	p.Failed = hint.State == RecoveryFailed
}

func managerActivityNeedsRecovery(state RecoveryState) bool {
	return state == RecoveryAdmissionUnknown || state == RecoveryAdmitted || state == RecoveryFailed
}
