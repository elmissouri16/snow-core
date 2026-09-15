package web

import (
	"cmp"
	"errors"
	"slices"
)

type workspaceFrontendProject struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Available     bool   `json:"available"`
	Trusted       bool   `json:"trusted"`
	SkillsEnabled bool   `json:"skillsEnabled"`
}

type homeFrontendProps struct {
	Projects []workspaceFrontendProject `json:"projects"`
	Error    string                     `json:"error"`
}

type workspaceCatalogFrontendProps struct {
	homeFrontendProps
	CSRF                     string `json:"csrf"`
	RegistryEnabled          bool   `json:"registryEnabled"`
	ProjectOperationsEnabled bool   `json:"projectOperationsEnabled"`
}

type loginFrontendProps struct {
	CSRF  string `json:"csrf"`
	Error string `json:"error"`
}

type workspaceColdFrontendProps struct {
	loginFrontendProps
	Project         workspaceFrontendProject `json:"project"`
	SessionID       string                   `json:"sessionID"`
	SessionTitle    string                   `json:"sessionTitle"`
	RuntimeEnabled  bool                     `json:"runtimeEnabled"`
	HasHistory      bool                     `json:"hasHistory"`
	NextURL         string                   `json:"nextURL"`
	RecoveryMessage string                   `json:"recoveryMessage"`
	RecoveryURL     string                   `json:"recoveryURL"`
}

func workspaceProjectProjection(project Project) workspaceFrontendProject {
	return workspaceFrontendProject{ID: project.ID, Name: project.Name, Path: project.Path, Available: project.Available, Trusted: project.Trusted, SkillsEnabled: project.SkillsEnabled}
}

func homeProjection(projects []Project, publicError string) (homeFrontendProps, error) {
	props := homeFrontendProps{Error: publicError}
	if len(projects) > MaxProjects {
		return props, errors.New("React workspace project limit exceeded")
	}
	for _, project := range projects {
		props.Projects = append(props.Projects, workspaceProjectProjection(project))
	}
	return props, nil
}

func homeReactProps(projects []Project, publicError string) (string, error) {
	props, err := homeProjection(projects, publicError)
	if err != nil {
		return "", err
	}
	return marshalReactProps(props)
}

func workspaceCatalogReactProps(data pageData) (string, error) {
	projects, err := homeProjection(data.Projects, data.Error)
	if err != nil {
		return "", err
	}
	return marshalReactProps(workspaceCatalogFrontendProps{homeFrontendProps: projects, CSRF: data.CSRF, RegistryEnabled: data.RegistryEnabled, ProjectOperationsEnabled: data.ProjectOperationsEnabled})
}

func loginReactProps(csrf, publicError string) (string, error) {
	return marshalReactProps(loginFrontendProps{CSRF: csrf, Error: publicError})
}

func workspaceColdReactProps(data pageData) (string, error) {
	if data.Project == nil || data.Live != nil {
		return "", errors.New("React cold workspace requires an inactive project")
	}
	props := workspaceColdFrontendProps{CSRF: data.CSRF, Error: data.Error, Project: workspaceProjectProjection(*data.Project), SessionID: data.SessionID, SessionTitle: "Session", RuntimeEnabled: data.RuntimeEnabled, HasHistory: data.History != nil, NextURL: data.NextURL, RecoveryURL: data.RecoveryURL}
	if data.Sessions != nil {
		if i := slices.IndexFunc(data.Sessions.Sessions, func(session SessionSummary) bool { return session.ID == data.SessionID }); i >= 0 {
			props.SessionTitle = cmp.Or(data.Sessions.Sessions[i].Name, "Untitled session")
		}
	}
	if data.Recovery != nil {
		props.RecoveryMessage = data.Recovery.Message()
	}
	return marshalReactProps(props)
}
