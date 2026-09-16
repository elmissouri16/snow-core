package web

import (
	"encoding/json/v2"
	"errors"
	"strings"
)

const maxReactPropsBytes = 1 << 20

// React bootstrap data is an explicit presentation projection, never pageData,
// a runtime snapshot, or a registry record serialized wholesale. These helpers
// return ordinary strings so html/template remains the attribute escape owner.
type activityFrontendProps struct {
	RegistryEnabled bool   `json:"registryEnabled"`
	Error           string `json:"error"`
}

type organizationFrontendProps struct {
	CSRF         string                    `json:"csrf"`
	Error        string                    `json:"error"`
	Organization *organizationFrontendView `json:"organization"`
}

type organizationFrontendView struct {
	Projects        []organizationFrontendProject `json:"projects"`
	Archived        []organizationFrontendProject `json:"archived"`
	Project         *organizationFrontendProject  `json:"project"`
	Sessions        []organizationFrontendSession `json:"sessions"`
	Offset          int                           `json:"offset"`
	NextURL         string                        `json:"nextURL"`
	ArchivedNextURL string                        `json:"archivedNextURL"`
	Live            bool                          `json:"live"`
}

type organizationFrontendProject struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
	State     string `json:"state"`
	Issue     string `json:"issue"`
	Pinned    bool   `json:"pinned"`
}

type organizationFrontendSession struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Updated  string `json:"updated"`
	Pinned   bool   `json:"pinned"`
	Archived bool   `json:"archived"`
}

type hostSettingsFrontendProps struct {
	CSRF     string                        `json:"csrf"`
	Enabled  bool                          `json:"enabled"`
	Projects []hostSettingsFrontendProject `json:"projects"`
}

type hostSettingsFrontendProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type browserInventoryFrontendProps struct {
	CSRF string `json:"csrf"`
}

type inspectionFrontendProps struct {
	Project inspectionFrontendProject `json:"project"`
	CSRF    string                    `json:"csrf"`
	Live    *inspectionFrontendLive   `json:"live,omitzero"`
}

type inspectionFrontendProject struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
}

type inspectionFrontendLive struct {
	SessionID string `json:"session_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
}

func inspectionReactProps(project *Project, csrf string, live *RuntimeSnapshot) (string, error) {
	props := inspectionFrontendProps{CSRF: csrf}
	if project != nil {
		props.Project = inspectionFrontendProject{ID: project.ID, Name: project.Name, Path: project.Path, Available: project.Available}
	}
	if live != nil {
		props.Live = &inspectionFrontendLive{SessionID: live.SessionID, Provider: live.Provider, Model: live.Model}
	}
	return marshalReactProps(props)
}

func hostSettingsReactProps(csrf string, enabled bool, projects []Project) (string, error) {
	props := hostSettingsFrontendProps{CSRF: csrf, Enabled: enabled}
	for _, project := range projects {
		if project.Available {
			props.Projects = append(props.Projects, hostSettingsFrontendProject{ID: project.ID, Name: project.Name})
		}
	}
	return marshalReactProps(props)
}

func browserInventoryReactProps(csrf string) (string, error) {
	return marshalReactProps(browserInventoryFrontendProps{CSRF: csrf})
}

func activityReactProps(registryEnabled bool, publicError string) (string, error) {
	return marshalReactProps(activityFrontendProps{RegistryEnabled: registryEnabled, Error: publicError})
}

func organizationReactProps(csrf, publicError string, view *OrganizationView) (string, error) {
	props := organizationFrontendProps{CSRF: csrf, Error: publicError}
	if view != nil {
		project := func(p Project) organizationFrontendProject {
			return organizationFrontendProject{
				ID: p.ID, Name: p.Name, Path: p.Path, Available: p.Available,
				State: p.State, Issue: p.Issue, Pinned: p.Pinned,
			}
		}
		out := &organizationFrontendView{
			Offset: view.Offset, NextURL: view.NextURL,
			ArchivedNextURL: view.ArchivedNextURL, Live: view.Live,
		}
		for _, p := range view.Projects {
			out.Projects = append(out.Projects, project(p))
		}
		for _, p := range view.Archived.Projects {
			out.Archived = append(out.Archived, project(p))
		}
		if view.Project != nil {
			out.Project = new(project(*view.Project))
		}
		for _, session := range view.Sessions {
			out.Sessions = append(out.Sessions, organizationFrontendSession{
				ID: session.ID, Name: session.Name, Updated: session.Updated,
				Pinned: session.Pinned, Archived: session.Archived,
			})
		}
		props.Organization = out
	}
	return marshalReactProps(props)
}

// The bounded writer prevents a large projection from growing the page buffer.
// Rendering is buffered separately, so an oversized bootstrap fails the entire
// render rather than emitting partial JSON or a partially usable page.
type reactPropsWriter struct{ strings.Builder }

func (w *reactPropsWriter) Write(p []byte) (int, error) {
	if len(p) > maxReactPropsBytes-w.Len() {
		return 0, errors.New("React page data exceeds its size limit")
	}
	return w.Builder.Write(p)
}

func marshalReactProps(props any) (string, error) {
	var out reactPropsWriter
	if err := json.MarshalWrite(&out, props); err != nil {
		return "", err
	}
	return out.String(), nil
}
