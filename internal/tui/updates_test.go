package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestSettingsUpdateRowsAndDependencyToggles(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startSettings()
	plain := stripANSI(m.renderSettings())
	for _, want := range []string{"Check for updates on startup", "Auto update", "Check for updates now", "Update now"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("settings missing %q: %q", want, plain)
		}
	}

	m.settingsIndex = settingsAutoUpdate
	m.cycleSetting(1)
	if !m.app.Cfg.Updates.AutoUpdate || !m.app.Cfg.Updates.CheckOnStartup {
		t.Fatalf("auto-update dependency not enabled: %+v", m.app.Cfg.Updates)
	}
	m.settingsIndex = settingsUpdateCheckOnStartup
	m.cycleSetting(1)
	if m.app.Cfg.Updates.AutoUpdate || m.app.Cfg.Updates.CheckOnStartup {
		t.Fatalf("check disable dependency not applied: %+v", m.app.Cfg.Updates)
	}
}

func TestManualUpdateActionsCheckThenInstall(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startSettings()
	checks, installs := 0, 0
	status := app.UpdateStatus{CurrentVersion: "1.0.0", LatestVersion: "1.0.1", Available: true, Eligible: true}
	m.checkForUpdate = func(context.Context) (app.UpdateStatus, error) {
		checks++
		return status, nil
	}
	m.installUpdate = func(context.Context, app.UpdateStatus) (app.UpdateResult, error) {
		installs++
		return app.UpdateResult{PreviousVersion: "1.0.0", InstalledVersion: "1.0.1"}, nil
	}

	m.settingsIndex = settingsCheckNow
	_, checkCmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if checkCmd == nil {
		t.Fatal("Check now did not schedule a command")
	}
	_, _ = m.Update(checkCmd())
	if checks != 1 || installs != 0 {
		t.Fatalf("manual check calls = checks:%d installs:%d", checks, installs)
	}

	m.settingsIndex = settingsUpdateNow
	_, beforeInstall := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if beforeInstall == nil {
		t.Fatal("Update now did not schedule a fresh check")
	}
	_, installCmd := m.Update(beforeInstall())
	if installCmd == nil {
		t.Fatal("fresh update check did not schedule installation")
	}
	_, _ = m.Update(installCmd())
	if checks != 2 || installs != 1 || m.updateInstalledVersion != "1.0.1" || !m.restartPromptPending {
		t.Fatalf("update state = checks:%d installs:%d version:%q prompt:%v", checks, installs, m.updateInstalledVersion, m.restartPromptPending)
	}
}

func TestUpdateStatusShowsCurrentAndLatestVersions(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.updateCheckRunning = true
	m.updateGeneration = 1
	status := app.UpdateStatus{
		CurrentVersion: "0.1.0-alpha.2",
		LatestVersion:  "0.1.0-alpha.2",
		Eligible:       true,
	}
	m.handleUpdateCheckDone(updateCheckDoneMsg{generation: 1, reason: updateCheckManual, status: status})
	if got, want := m.settingsStatus, "Current v0.1.0-alpha.2 · latest v0.1.0-alpha.2 · up to date"; got != want {
		t.Fatalf("settings status = %q, want %q", got, want)
	}
	if got, want := m.updateActionText(), "Update now  v0.1.0-alpha.2 is latest"; got != want {
		t.Fatalf("update action = %q, want %q", got, want)
	}

	m.updateStatus = app.UpdateStatus{
		CurrentVersion: "0.1.0-alpha.2",
		LatestVersion:  "0.1.0-alpha.3",
		Available:      true,
		Eligible:       true,
	}
	if got, want := m.updateActionText(), "Update now  v0.1.0-alpha.2 → v0.1.0-alpha.3"; got != want {
		t.Fatalf("available action = %q, want %q", got, want)
	}
}

func TestUpdateErrorsAndRestartChoices(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.updateCheckRunning = true
	m.updateGeneration = 2
	m.handleUpdateCheckDone(updateCheckDoneMsg{generation: 2, reason: updateCheckStartup, err: errors.New("offline")})
	if m.lastErr != nil || m.settingsError != "" || !strings.Contains(m.updateLastError, "offline") {
		t.Fatalf("startup update failure was fatal or visible as manual error: last=%v settings=%q update=%q", m.lastErr, m.settingsError, m.updateLastError)
	}

	m.updateInstalledVersion = "1.0.1"
	m.restartPromptPending = true
	if !m.restartPromptVisible() {
		t.Fatal("restart prompt not visible while idle")
	}
	_, _ = m.handleRestartPromptKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.restartPromptPending || m.restartRequested {
		t.Fatalf("Later/Esc requested restart: pending=%v requested=%v", m.restartPromptPending, m.restartRequested)
	}

	m.restartPromptPending = true
	m.restartChoice = 0
	_, quit := m.handleRestartPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.restartRequested || quit == nil {
		t.Fatalf("Restart now state = requested:%v cmd:%v", m.restartRequested, quit != nil)
	}
}

func TestRestartWarningUsesCurrentSessionDurability(t *testing.T) {
	testHome(t)
	a, err := app.New(t.Context(), app.Options{Provider: "fake", Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(t.Context(), app.Options{NoSession: true})
	m.app = a
	m.updateInstalledVersion = "1.0.1"
	m.restartPromptPending = true
	plain := stripANSI(m.renderRestartPrompt())
	if strings.Contains(plain, "fresh ephemeral session") {
		t.Fatalf("durable current session rendered ephemeral warning: %q", plain)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsActionRowsIgnoreHorizontalArrows(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startSettings()
	for _, index := range []int{settingsCheckNow, settingsUpdateNow} {
		m.settingsIndex = index
		_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
		if cmd != nil || m.settingsIndex != index || m.updateCheckRunning {
			t.Fatalf("action row %d reacted to horizontal arrow", index)
		}
	}
}

func TestDoneSchedulesStartupCheckOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(onOff(enabled), func(t *testing.T) {
			m := newModel(t.Context(), app.Options{})
			testHome(t)
			a, err := app.New(t.Context(), app.Options{
				Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
			})
			if err != nil {
				t.Fatal(err)
			}
			a.Cfg.Updates.CheckOnStartup = enabled
			m.checkForUpdate = func(context.Context) (app.UpdateStatus, error) {
				return app.UpdateStatus{}, nil
			}
			_, _ = m.Update(doneMsg{app: a})
			if m.updateCheckRunning != enabled {
				t.Fatalf("enabled=%v checkRunning=%v", enabled, m.updateCheckRunning)
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
