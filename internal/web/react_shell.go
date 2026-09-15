package web

import (
	"cmp"
	"errors"
)

type shellFrontendProject struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Path            string `json:"path"`
	Available       bool   `json:"available"`
	TrustRemembered bool   `json:"trustRemembered"`
	SkillsEnabled   bool   `json:"skillsEnabled"`
	Pinned          bool   `json:"pinned"`
}

type shellFrontendSession struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
}

type shellFrontendLive struct {
	Project         string `json:"project"`
	Session         string `json:"session"`
	Instance        string `json:"instance"`
	Title           string `json:"title"`
	RenameAvailable bool   `json:"renameAvailable"`
	RenameDisabled  bool   `json:"renameDisabled"`
	NewDisabled     bool   `json:"newDisabled"`
}

type shellFrontendProps struct {
	CSRF                string                 `json:"csrf"`
	Version             string                 `json:"version"`
	View                string                 `json:"view"`
	Project             string                 `json:"project"`
	Session             string                 `json:"session"`
	HostSettingsEnabled bool                   `json:"hostSettingsEnabled"`
	APIKeyEnabled       bool                   `json:"apiKeyEnabled"`
	TLS                 bool                   `json:"tls"`
	PairingCode         string                 `json:"pairingCode"`
	Projects            []shellFrontendProject `json:"projects"`
	Sessions            []shellFrontendSession `json:"sessions"`
	Live                *shellFrontendLive     `json:"live"`
}

func shellReactProps(data pageData) (string, error) {
	if len(data.PairingCode) > 128 {
		return "", errors.New("React shell pairing code limit exceeded")
	}
	if len(data.Projects) > MaxProjects {
		return "", errors.New("React shell project limit exceeded")
	}
	props := shellFrontendProps{CSRF: data.CSRF, Version: data.Version, View: data.View, Session: data.SessionID, HostSettingsEnabled: data.HostSettingsEnabled, APIKeyEnabled: data.HostAPIKeyEnabled, TLS: data.TLS, PairingCode: data.PairingCode}
	for _, project := range data.Projects {
		props.Projects = append(props.Projects, shellFrontendProject{ID: project.ID, Name: project.Name, Path: project.Path, Available: project.Available, TrustRemembered: project.TrustRemembered, SkillsEnabled: project.SkillsEnabled, Pinned: project.Pinned})
	}
	if data.Project != nil {
		props.Project = data.Project.ID
		if data.Sessions != nil {
			for _, session := range data.Sessions.Sessions[:min(len(data.Sessions.Sessions), 100)] {
				props.Sessions = append(props.Sessions, shellFrontendSession{SessionID: session.ID, Name: session.Name})
			}
		}
		if data.Live != nil {
			props.Session = data.Live.SessionID
			title := cmp.Or(data.Live.SessionName, "New conversation")
			// Initial controls have no verified browser admission state yet.
			props.Live = &shellFrontendLive{Project: data.Project.ID, Session: data.Live.SessionID, Instance: data.Live.InstanceID, Title: title, RenameAvailable: data.WorkflowEnabled, RenameDisabled: true, NewDisabled: true}
		}
	}
	return marshalReactProps(props)
}
