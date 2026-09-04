package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestSettingsUpdateRowsRequireExplicitInstallation(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startSettings()
	plain := stripANSI(m.renderSettings())
	for _, want := range []string{"Check for updates on startup", "Check for updates now", "Update now"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("settings missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "Auto update") {
		t.Fatalf("settings still expose automatic installation: %q", plain)
	}

	m.settingsIndex = settingsUpdateCheckOnStartup
	m.cycleSetting(1)
	if !m.app.Cfg.Updates.CheckOnStartup {
		t.Fatalf("startup update check was not enabled: %+v", m.app.Cfg.Updates)
	}
	m.cycleSetting(1)
	if m.app.Cfg.Updates.CheckOnStartup {
		t.Fatalf("startup update check was not disabled: %+v", m.app.Cfg.Updates)
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
	m.installUpdate = func(_ context.Context, _ app.UpdateStatus, report app.UpdateProgressFunc) (app.UpdateResult, error) {
		installs++
		report(app.UpdateProgress{Phase: app.UpdateProgressDownloading, DownloadedBytes: 1, TotalBytes: 2})
		report(app.UpdateProgress{Phase: app.UpdateProgressVerifying, DownloadedBytes: 2, TotalBytes: 2})
		report(app.UpdateProgress{Phase: app.UpdateProgressInstalling, DownloadedBytes: 2, TotalBytes: 2})
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
	runUpdateInstallCommands(t, m, installCmd)
	if checks != 2 || installs != 1 || m.updateInstalledVersion != "1.0.1" || !m.restartPromptPending {
		t.Fatalf("update state = checks:%d installs:%d version:%q prompt:%v", checks, installs, m.updateInstalledVersion, m.restartPromptPending)
	}
}

func TestStartupUpdateOfferRequiresInstallOrSkipDecision(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	status := app.UpdateStatus{CurrentVersion: "1.0.0", LatestVersion: "1.0.1", Available: true, Eligible: true}
	m.updateCheckRunning = true
	m.updateGeneration = 1
	if cmd := m.handleUpdateCheckDone(updateCheckDoneMsg{generation: 1, reason: updateCheckStartup, status: status}); cmd != nil {
		t.Fatal("startup check automatically scheduled installation")
	}
	if !m.updateOfferPending || m.updateOfferChoice != 1 || !m.updateOfferVisible() {
		t.Fatalf("startup offer state = pending:%v choice:%d visible:%v", m.updateOfferPending, m.updateOfferChoice, m.updateOfferVisible())
	}
	plain := stripANSI(m.renderUpdateOffer())
	for _, want := range []string{"Update available", "v1.0.0 → v1.0.1", "Install update", "Skip for now"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("update offer missing %q: %q", want, plain)
		}
	}

	_, cmd := m.handleUpdateOfferKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.updateOfferPending {
		t.Fatalf("default Skip choice scheduled install: cmd=%v pending=%v", cmd != nil, m.updateOfferPending)
	}

	m.updateOfferPending = true
	m.updateOfferChoice = 0
	m.installUpdate = func(context.Context, app.UpdateStatus, app.UpdateProgressFunc) (app.UpdateResult, error) {
		return app.UpdateResult{PreviousVersion: "1.0.0", InstalledVersion: "1.0.1"}, nil
	}
	_, cmd = m.handleUpdateOfferKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.updateInstallRunning || m.updateOfferPending {
		t.Fatalf("Install choice state = cmd:%v running:%v pending:%v", cmd != nil, m.updateInstallRunning, m.updateOfferPending)
	}
}

func TestUpdateInstallProgressIsVisible(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.updateGeneration = 3
	m.updateInstallRunning = true
	m.updateStatus = app.UpdateStatus{LatestVersion: "1.0.1"}
	updates := make(chan app.UpdateProgress)
	progress := app.UpdateProgress{
		Phase:           app.UpdateProgressDownloading,
		DownloadedBytes: 5 << 20,
		TotalBytes:      20 << 20,
	}
	cmd := m.handleUpdateInstallProgress(updateInstallProgressMsg{
		generation: 3,
		progress:   progress,
		updates:    updates,
		open:       true,
	})
	if got, want := m.lastStatus, "downloading v1.0.1  25% · 5.0 MiB / 20.0 MiB"; got != want {
		t.Fatalf("progress status = %q, want %q", got, want)
	}
	if cmd == nil {
		t.Fatal("progress handler did not continue subscription")
	}
	plain := stripANSI(m.renderUpdateInstallProgress())
	for _, want := range []string{"Installing update", "25%", "5.0 MiB / 20.0 MiB", "Download started only after confirmation", "████"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("progress modal missing %q: %q", want, plain)
		}
	}
	close(updates)
	if msg, ok := cmd().(updateInstallProgressMsg); !ok || msg.open {
		t.Fatalf("closed progress channel produced %#v", msg)
	}

	if got, want := formatUpdateProgress("v1.0.1", app.UpdateProgress{Phase: app.UpdateProgressVerifying}), "verifying v1.0.1 checksum and archive…"; got != want {
		t.Fatalf("verify status = %q, want %q", got, want)
	}
	if got, want := formatUpdateProgress("v1.0.1", app.UpdateProgress{Phase: app.UpdateProgressInstalling}), "installing verified v1.0.1…"; got != want {
		t.Fatalf("install status = %q, want %q", got, want)
	}
}

func TestUpdateOfferDefersBehindSettings(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.updateOfferPending = true
	m.updateStatus = app.UpdateStatus{CurrentVersion: "1.0.0", LatestVersion: "1.0.1", Available: true, Eligible: true}
	m.pickSettings = true
	if m.updateOfferVisible() {
		t.Fatal("update offer preempted settings")
	}
	m.pickSettings = false
	if !m.updateOfferVisible() {
		t.Fatal("deferred update offer did not become visible")
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

func runUpdateInstallCommands(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("install command did not return a two-command batch")
	}
	type commandResult struct{ msg tea.Msg }
	results := make(chan commandResult, 8)
	active := 0
	launch := func(next tea.Cmd) {
		active++
		go func() { results <- commandResult{msg: next()} }()
	}
	for _, next := range batch {
		launch(next)
	}
	var done *updateInstallDoneMsg
	progressCount := 0
	closed := false
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for active > 0 {
		select {
		case result := <-results:
			active--
			switch msg := result.msg.(type) {
			case updateInstallDoneMsg:
				done = &msg
			case updateInstallProgressMsg:
				if msg.open {
					progressCount++
				} else {
					closed = true
				}
				_, next := m.Update(msg)
				if next != nil {
					launch(next)
				}
			default:
				t.Fatalf("unexpected install command message %T", result.msg)
			}
		case <-deadline.C:
			t.Fatal("install progress command chain deadlocked")
		}
	}
	if done == nil {
		t.Fatal("install command did not complete")
	}
	if progressCount != 3 || !closed {
		t.Fatalf("progress events = %d, closed=%v", progressCount, closed)
	}
	_, _ = m.Update(*done)
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
