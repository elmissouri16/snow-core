package web

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// freezeWorkerEnvironment binds operator-owned storage to the manager's launch
// CWD before a child changes to a registered project or reviewed job parent.
// It reads only process environment/CWD, never config/auth/session files, and
// leaves the manager environment unchanged. Every manager worker uses it.
// A caller must disable startup on error, never fall back to an inherited Env.
func freezeWorkerEnvironment(sessionsRoot string) ([]string, error) {
	env := os.Environ()
	userHome, homeErr := os.UserHomeDir()
	home := os.Getenv("SNOW_HOME")
	if home == "" {
		home = ".snow"
		if homeErr == nil {
			home = filepath.Join(userHome, ".snow")
		}
	}
	if sessionsRoot == "" {
		sessionsRoot = os.Getenv("SNOW_SESSIONS_DIR")
		if sessionsRoot == "" {
			// Match the public worker's default: sessions are HOME/.snow/sessions,
			// not SNOW_HOME/sessions, unless SNOW_SESSIONS_DIR explicitly overrides.
			sessionsRoot = filepath.Join(".snow", "sessions")
			if homeErr == nil {
				sessionsRoot = filepath.Join(userHome, ".snow", "sessions")
			}
		}
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, errors.New("web: operator storage directory is unavailable")
	}
	sessionsRoot, err = filepath.Abs(sessionsRoot)
	if err != nil {
		return nil, errors.New("web: operator storage directory is unavailable")
	}
	env = slices.DeleteFunc(env, func(value string) bool {
		return strings.HasPrefix(value, "SNOW_HOME=") || strings.HasPrefix(value, "SNOW_SESSIONS_DIR=")
	})
	return append(env, "SNOW_HOME="+home, "SNOW_SESSIONS_DIR="+sessionsRoot), nil
}
