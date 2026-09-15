package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
)

func workerEnvironmentValue(env []string, key string) string {
	if env == nil {
		env = os.Environ()
	}
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, key+"="); ok {
			return value
		}
	}
	return ""
}
func TestWorkerEnvironmentAllManagerWorkersFreezeRelativeHome(t *testing.T) {
	launch := t.TempDir()
	t.Chdir(launch)
	t.Setenv("SNOW_HOME", "operator-home")
	t.Setenv("SNOW_SESSIONS_DIR", "old-relative-sessions")
	t.Setenv("SNOW_WORKER_ENV_CANARY", "preserved-non-secret")
	expectedHome, err := filepath.Abs("operator-home")
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := filepath.Abs("selected-sessions")
	if err != nil {
		t.Fatal(err)
	}
	managerDir := filepath.Join(expectedHome, "manager")
	control := NewWorkerControl("/operator/snow", managerDir, nil)
	runtime := NewRuntimeManager(t.Context(), "/operator/snow", sessions)
	defer runtime.Close()
	catalog := newWorkerCatalog("/operator/snow", sessions)
	environments := map[string][]string{"control": control.env, "runtime": runtime.env, "catalog": catalog.env}
	parent, err := openDirectoryIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewWorkerProjectOperations("/operator/snow", managerDir)
	starts := 0
	jobs.start = func(_ context.Context, options process.Options) (*process.Worker, error) {
		starts++
		environments["jobs"] = options.Env
		return nil, errors.New("fixture does not spawn")
	}
	// Future activation/job startup may occur after caller-side environment or
	// CWD changes. Every already-constructed surface must retain the same roots.
	t.Setenv("SNOW_HOME", "later-home")
	t.Setenv("SNOW_SESSIONS_DIR", "later-sessions")
	t.Chdir(parent.Path)
	_, _ = jobs.Open(t.Context(), ProjectOperation{Kind: "create", Parent: parent})
	if starts != 1 {
		t.Fatal("job fixture did not reach process boundary")
	}
	for name, env := range environments {
		if home := workerEnvironmentValue(env, "SNOW_HOME"); home != expectedHome {
			t.Errorf("%s SNOW_HOME not pinned to manager launch directory: got %q want %q", name, home, expectedHome)
		}
		if got := workerEnvironmentValue(env, "SNOW_WORKER_ENV_CANARY"); got != "preserved-non-secret" {
			t.Errorf("%s lost unrelated environment", name)
		}
	}
	for name, env := range map[string][]string{"runtime": runtime.env, "catalog": catalog.env} {
		if got := workerEnvironmentValue(env, "SNOW_SESSIONS_DIR"); got != sessions {
			t.Errorf("%s lost operator sessions root", name)
		}
	}
	// Constructors and the intercepted startup never open config/auth/session
	// files or create the nonexistent operator config directory.
	if _, err := os.Stat(expectedHome); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("environment construction touched configuration storage")
	}
}

func TestWorkerEnvironmentFallbacksAndSessionsPrecedence(t *testing.T) {
	for _, mode := range []string{"relative-snow-home", "absolute-snow-home", "relative-home-fallback", "missing-home-fallback", "relative-session-override", "absolute-session-override", "default-sessions"} {
		t.Run(mode, func(t *testing.T) {
			launch := t.TempDir()
			t.Chdir(launch)
			// All possible roots refer exclusively to nonexistent private fixture paths.
			t.Setenv("HOME", "operator-user-home")
			t.Setenv("SNOW_HOME", "operator-snow-home")
			t.Setenv("SNOW_SESSIONS_DIR", "environment-sessions")
			t.Setenv("SNOW_WORKER_ENV_CANARY", "unchanged")
			expectedHome, _ := filepath.Abs("operator-snow-home")
			expectedSessions, _ := filepath.Abs("environment-sessions")
			override := ""
			switch mode {
			case "absolute-snow-home":
				expectedHome = filepath.Join(launch, "absolute-home")
				t.Setenv("SNOW_HOME", expectedHome)
			case "relative-home-fallback":
				t.Setenv("SNOW_HOME", "")
				expectedHome, _ = filepath.Abs(filepath.Join("operator-user-home", ".snow"))
			case "missing-home-fallback":
				t.Setenv("SNOW_HOME", "")
				t.Setenv("HOME", "")
				expectedHome, _ = filepath.Abs(".snow")
			case "relative-session-override":
				override = "explicit-sessions"
				expectedSessions, _ = filepath.Abs(override)
			case "absolute-session-override":
				override = filepath.Join(launch, "explicit-sessions")
				expectedSessions = override
			case "default-sessions":
				t.Setenv("SNOW_SESSIONS_DIR", "")
				expectedSessions, _ = filepath.Abs(filepath.Join("operator-user-home", ".snow", "sessions"))
			}
			before := os.Environ()
			env, err := freezeWorkerEnvironment(override)
			if err != nil {
				t.Fatal(err)
			}
			if got := workerEnvironmentValue(env, "SNOW_HOME"); got != expectedHome {
				t.Fatalf("incorrect frozen home: got %q want %q", got, expectedHome)
			}
			if got := workerEnvironmentValue(env, "SNOW_SESSIONS_DIR"); got != expectedSessions {
				t.Fatalf("incorrect frozen sessions: got %q want %q", got, expectedSessions)
			}
			if !slices.Equal(before, os.Environ()) {
				t.Fatal("freezing mutated manager environment")
			}
			for _, entry := range before {
				if strings.HasPrefix(entry, "SNOW_HOME=") || strings.HasPrefix(entry, "SNOW_SESSIONS_DIR=") {
					continue
				}
				if !slices.Contains(env, entry) {
					t.Fatal("freezing dropped unrelated operator environment")
				}
			}
			for _, key := range []string{"SNOW_HOME", "SNOW_SESSIONS_DIR"} {
				count := 0
				for _, entry := range env {
					if strings.HasPrefix(entry, key+"=") {
						count++
					}
				}
				if count != 1 {
					t.Errorf("%s authoritative entries = %d", key, count)
				}
			}
			for _, root := range []string{expectedHome, expectedSessions} {
				if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("freezing touched operator storage")
				}
			}
		})
	}
}
