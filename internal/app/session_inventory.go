package app

import (
	"cmp"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/internal/session"
)

// ListSessions returns the durable sessions that belong to the app's working
// directory. FileIndex performs the pinned-root and stored-CWD validation.
func (a *App) ListSessions() ([]session.SessionInfo, error) {
	infos, err := session.NewFileIndex(session.DefaultSessionsRoot()).List(a.cwd)
	if err != nil {
		return nil, err
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.Session == nil || a.Session.Path() == "" {
		if infos == nil {
			infos = []session.SessionInfo{}
		}
		return infos, nil
	}
	for _, info := range infos {
		if info.ID == a.Session.ID() {
			return infos, nil
		}
	}
	infos = append(infos, sessionInfoFromStore(a.Session))
	slices.SortFunc(infos, func(a, b session.SessionInfo) int {
		return cmp.Or(cmp.Compare(b.UpdatedAt, a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	})
	return infos, nil
}

// CreateSession creates a durable session in the app's working directory and
// makes it active through SetSession so every session-bound subsystem is
// rebound consistently.
func (a *App) CreateSession() (session.SessionInfo, error) {
	index := session.NewFileIndex(session.DefaultSessionsRoot())
	st, err := index.Create(a.cwd)
	if err != nil {
		return session.SessionInfo{}, err
	}
	if err := a.SetSession(st); err != nil {
		return session.SessionInfo{}, errors.Join(err, st.Close())
	}
	return sessionInfoFromStore(st), nil
}

// OpenSession switches to a durable project session selected by immutable ID.
// Clients never supply a database path, and the opened identity is rechecked
// before SetSession receives the store.
func (a *App) OpenSession(sessionID string) (session.SessionInfo, error) {
	index := session.NewFileIndex(session.DefaultSessionsRoot())
	info, err := a.sessionInfoByID(sessionID)
	if err != nil {
		return session.SessionInfo{}, err
	}
	activeID, _, err := a.Agent.SessionIdentity()
	if err != nil {
		return session.SessionInfo{}, err
	}
	if activeID == sessionID {
		return info, nil
	}
	st, err := index.Open(info.Path)
	if err != nil {
		return session.SessionInfo{}, err
	}
	if st.ID() != sessionID {
		_ = st.Close()
		return session.SessionInfo{}, session.ErrNotFound
	}
	if err := a.SetSession(st); err != nil {
		return session.SessionInfo{}, errors.Join(err, st.Close())
	}
	return a.sessionInfoByID(sessionID)
}

// DeleteSessionByID permanently deletes an inactive project session selected
// by immutable ID. DeleteSession retains the active-session and cleanup checks.
func (a *App) DeleteSessionByID(sessionID string) error {
	info, err := a.sessionInfoByID(sessionID)
	if err != nil {
		return err
	}
	return a.DeleteSession(info.Path, sessionID)
}

// RenameSessionByID renames either the active or an inactive project session
// selected by immutable ID. Inactive stores are opened only after FileIndex
// confirms project membership, then their identity is checked again.
func (a *App) RenameSessionByID(sessionID, title string) error {
	activeID, _, err := a.Agent.SessionIdentity()
	if err != nil {
		return err
	}
	if activeID == sessionID {
		return a.RenameSession(title)
	}

	index := session.NewFileIndex(session.DefaultSessionsRoot())
	info, err := a.sessionInfoByID(sessionID)
	if err != nil {
		return err
	}

	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	activeID, _, running, err := a.Agent.SessionIdentityAdmitted()
	if err != nil {
		return err
	}
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot rename session while subagents are active")
	}
	if activeID == sessionID {
		if running {
			return errors.New("app: cannot rename session while a turn is running")
		}
		return a.Agent.RenameSessionAdmitted(title)
	}

	st, err := index.Open(info.Path)
	if err != nil {
		return err
	}
	if st.ID() != sessionID {
		_ = st.Close()
		return session.ErrNotFound
	}
	titles, ok := st.(session.TitleStore)
	if !ok {
		_ = st.Close()
		return errors.New("session: store does not support titles")
	}
	return errors.Join(titles.RenameSession(title), st.Close())
}

func (a *App) sessionInfoByID(sessionID string) (session.SessionInfo, error) {
	if strings.TrimSpace(sessionID) == "" {
		return session.SessionInfo{}, session.ErrNotFound
	}
	infos, err := a.ListSessions()
	if err != nil {
		return session.SessionInfo{}, err
	}
	for _, info := range infos {
		if info.ID == sessionID {
			return info, nil
		}
	}
	return session.SessionInfo{}, session.ErrNotFound
}

func sessionInfoFromStore(st session.Store) session.SessionInfo {
	header := st.Header()
	updatedAt := header.CreatedAt
	if info, err := os.Stat(st.Path()); err == nil {
		updatedAt = info.ModTime().UnixMilli()
	}
	return session.SessionInfo{
		Path: st.Path(), ID: st.ID(), CWD: header.CWD, Name: header.Name,
		CreatedAt: header.CreatedAt, UpdatedAt: updatedAt,
	}
}
