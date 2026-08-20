package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	internalsandbox "github.com/elmissouri16/snow-core/internal/sandbox"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// runCommand handles slash commands.
func (m *Model) runCommand(line string) (tea.Model, tea.Cmd) {
	m.editor.Reset()
	parts := strings.Fields(line)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "/quit", "/q":
		return m, m.quitCmd()
	case "/help":
		mouseHelp := "mouse: native terminal selection · F6 enables wheel + app drag-copy"
		if m.app.Cfg.TUI.Mouse {
			mouseHelp = "mouse: wheel + app drag-copy · F6 restores native terminal selection"
		}
		m.pushLine(styleFooter.Render(formatCommandListWithKeys(m.keys) + "\n(while working: submit queues steer, follow-up uses its configured binding)\n(" + mouseHelp + ")"))
	case "/goal":
		if len(args) == 0 {
			g, err := m.app.GoalState()
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else if g == nil {
				m.pushLine(styleFooter.Render("no thread goal"))
			} else {
				m.goal = g
				m.pushLine(styleFooter.Render(fmt.Sprintf("goal %s · %s · %ds\n%s", g.Status, formatGoalTokenUsage(g), g.SecondsUsed, g.Objective)))
			}
			return m, nil
		}
		sub := args[0]
		switch sub {
		case "pause":
			g, err := m.app.PauseGoal()
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				m.setRunIdle()
			}
		case "resume":
			g, err := m.app.ResumeGoal()
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				m.busy = true
				m.runStartedAt = m.currentTime()
			}
		case "clear":
			if err := m.app.ClearGoal(); err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = nil
				m.setRunIdle()
			}
		case "replace":
			objective := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "/goal")), "replace"))
			if objective == "" {
				m.pushLine(styleError.Render("usage: /goal replace <objective>"))
				return m, nil
			}
			g, err := m.app.CreateGoal(objective, nil, true)
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				m.busy = true
				m.runStartedAt = m.currentTime()
			}
		case "edit":
			objective := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "/goal")), "edit"))
			if objective == "" {
				if g, _ := m.app.GoalState(); g != nil {
					m.editor.SetValue("/goal edit " + g.Objective)
					m.editor.CursorEnd()
				}
				return m, nil
			}
			g, err := m.app.EditGoal(objective)
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				if g != nil && g.Status == protocol.GoalActive {
					m.busy = true
					m.runStartedAt = m.currentTime()
				}
			}
		default:
			objective := strings.TrimSpace(strings.TrimPrefix(line, "/goal"))
			var budget *int64
			if strings.HasPrefix(objective, "--budget ") {
				fields := strings.Fields(objective)
				if len(fields) < 3 {
					m.pushLine(styleError.Render("usage: /goal --budget <tokens> <objective>"))
					return m, nil
				}
				v, e := strconv.ParseInt(fields[1], 10, 64)
				if e != nil || v <= 0 {
					m.pushLine(styleError.Render("goal budget must be positive"))
					return m, nil
				}
				budget = &v
				objective = strings.Join(fields[2:], " ")
			}
			g, err := m.app.CreateGoal(objective, budget, false)
			if err != nil {
				if strings.Contains(err.Error(), "unfinished goal") {
					m.confirmGoalReplace = true
					m.pendingGoalObjective = objective
					m.pendingGoalBudget = budget
				} else {
					m.pushLine(styleError.Render(err.Error()))
				}
			} else {
				m.goal = g
				m.busy = true
				m.runStartedAt = m.currentTime()
			}
		}
		return m, nil
	case "/plan":
		if m.busy || m.app.Agent.IsRunning() {
			m.pushLine(styleError.Render("plan: wait for the current turn to finish"))
			return m, nil
		}
		message := strings.TrimSpace(strings.TrimPrefix(line, "/plan"))
		m.nudgeDismissed[m.planNudgeScope()] = true
		if message == "" {
			if err := m.app.Agent.SetMode(protocol.ModePlan); err != nil {
				m.pushLine(styleError.Render(err.Error()))
			}
			return m, nil
		}
		m.pushLine(styleUser.Render("› " + message))
		return m, m.startPromptWithMode(message, protocol.ModePlan)
	case "/default":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/default takes no arguments"))
			return m, nil
		}
		if err := m.app.Agent.SetMode(protocol.ModeDefault); err != nil {
			m.pushLine(styleError.Render(err.Error()))
		}
	case "/compact":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/compact takes no arguments"))
			return m, nil
		}
		return m, m.startCompact()
	case "/context":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/context takes no arguments"))
			return m, nil
		}
		a := m.app.Agent
		epoch := a.RootEpoch()
		return m, func() tea.Msg {
			report, err := a.ContextReport()
			return contextReportMsg{epoch: epoch, report: report, err: err}
		}
	case "/login":
		return m.startLogin(args)
	case "/logout":
		return m.doLogout(args)
	case "/agent":
		if len(args) > 0 && args[0] == "concurrency" {
			if len(args) == 1 {
				m.pushLine(styleFooter.Render(fmt.Sprintf("subagent concurrency: %d (use /agent concurrency <n>; restart to apply)", m.app.Cfg.Subagents.MaxConcurrentThreads)))
				return m, nil
			}
			if len(args) != 2 {
				m.pushLine(styleError.Render("usage: /agent concurrency <positive-number>"))
				return m, nil
			}
			limit, err := strconv.Atoi(args[1])
			if err != nil || limit < 1 {
				m.pushLine(styleError.Render("agent concurrency must be a positive number"))
				return m, nil
			}
			if err := m.setSubagentConcurrency(limit); err != nil {
				m.pushLine(styleError.Render(err.Error()))
				return m, nil
			}
			m.pushLine(styleFooter.Render(fmt.Sprintf("subagent concurrency saved as %d; restart Snow to apply", limit)))
			return m, nil
		}
		if m.app.Subagents == nil {
			m.pushLine(styleFooter.Render("subagents are disabled (enable them in /settings or start with --subagents)"))
			return m, nil
		}
		if len(args) == 0 {
			return m, m.openSubagentFleet("")
		}
		if len(args) > 1 {
			m.pushLine(styleError.Render("usage: /agent [path] | /agent concurrency <n>"))
			return m, nil
		}
		return m, m.openSubagentFleet(args[0])
	case "/mcp":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/mcp takes no arguments"))
			return m, nil
		}
		return m.startMCPInfo()
	case "/sandbox":
		return m.startSandboxCommand(args)
	case "/fork":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/fork takes no arguments"))
			return m, nil
		}
		return m.startForkPick()
	case "/new":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/new takes no arguments"))
			return m, nil
		}
		return m.startNewSession()
	case "/resume":
		if len(args) > 1 {
			m.pushLine(styleError.Render("usage: /resume [session-path]"))
			return m, nil
		}
		if len(args) == 1 {
			return m.openSession(args[0])
		}
		return m.startSessionPick()
	case "/sessions":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/sessions takes no arguments"))
			return m, nil
		}
		return m.startSessionPick()
	case "/tree":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/tree takes no arguments"))
			return m, nil
		}
		return m.startTreePick()

	case "/model":
		if m.compatibleLoginPending {
			m.lastStatus = "waiting for openai-compatible model discovery"
			m.pushLine(styleFooter.Render(m.lastStatus))
			return m, nil
		}
		if len(args) == 0 {
			// No arg: open the startup-cached interactive model picker.
			return m.startModelPick()
		}
		selected := protocol.Model{Provider: m.app.ProviderID, ID: args[0]}
		catalog := m.modelList
		if len(catalog) == 0 {
			catalog = uniquePickerModels(m.app.AllModels, m.app.ProviderID)
		}
		for _, cached := range catalog {
			if cached.Provider == selected.Provider && cached.ID == selected.ID {
				selected = cached
				break
			}
		}
		m.setModel(selected)
	case "/settings":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/settings takes no arguments"))
			return m, nil
		}
		return m.startSettings()
	case "/skills":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/skills takes no arguments"))
			return m, nil
		}
		return m.startSkillsInfo()
	case "/thinking":
		if len(args) == 0 {
			return m.startThinkingPick()
		}
		if len(args) > 1 {
			m.pushLine(styleError.Render("/thinking takes one level"))
			return m, nil
		}
		level, err := protocol.ParseThinkingLevel(args[0])
		if err != nil {
			m.pushLine(styleError.Render(err.Error()))
			return m, nil
		}
		if err := m.applyThinking(level); err != nil {
			m.pushLine(styleError.Render(err.Error()))
		}
	case "/allow", "/deny":
		// Respond to a pending permission request from the TUI asker. This is
		// also reachable while the interactive picker is open, so clear it.
		if m.asker == nil {
			m.pushLine(styleError.Render("no pending permission request"))
			return m, nil
		}
		m.permPending = false
		m.permRequest = nil
		m.permAgent = nil
		decision := permission.DecisionDeny
		if cmd == "/allow" {
			decision = permission.DecisionAllow
			if len(args) > 0 && args[0] == "always" {
				decision = permission.DecisionAllowAlways
			}
		}
		if err := m.asker.Respond(decision); err != nil {
			m.pushLine(styleError.Render(err.Error()))
		} else {
			m.pushLine(styleFooter.Render(cmd + " granted"))
		}
	case "/permissions":
		if len(args) == 0 {
			return m.startPermissionModePick()
		}
		if len(args) > 1 {
			m.pushLine(styleError.Render("/permissions takes one mode"))
			return m, nil
		}
		switch args[0] {
		case "ask", "allow", "deny":
			if err := m.setPermissionMode(permission.Mode(args[0]), true); err != nil {
				m.pushLine(styleError.Render(err.Error()))
			}
		default:
			m.pushLine(styleError.Render("invalid mode: " + args[0]))
		}
	case "/trust":
		if len(args) == 0 {
			if lvl, ok := m.app.Trust.Get(m.app.CWD()); ok {
				m.pushLine(styleFooter.Render("trust for " + m.app.CWD() + ": " + string(lvl)))
			} else {
				m.pushLine(styleFooter.Render("no trust decision for " + m.app.CWD()))
			}
			return m, nil
		}
		switch args[0] {
		case "allow", "deny":
			if err := m.app.Trust.Set(m.app.CWD(), trust.Level(args[0])); err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.pushLine(styleFooter.Render("trust " + args[0] + " saved for next launch · " + m.app.CWD()))
			}
		default:
			m.pushLine(styleError.Render("invalid trust level: " + args[0]))
		}
	default:
		m.pushLine(styleError.Render("unknown command: " + cmd + " (try /help)"))
	}
	return m, nil
}

func (m *Model) startSandboxCommand(args []string) (tea.Model, tea.Cmd) {
	if m.app == nil || m.app.Sandbox == nil {
		m.pushLine(styleError.Render("sandbox: runtime is unavailable"))
		return m, nil
	}
	if m.sandboxLoading {
		m.pushLine(styleFooter.Render("sandbox operation already in progress"))
		return m, nil
	}
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	var initOpts internalsandbox.InitOptions
	switch action {
	case "status", "start", "stop":
		if len(args) != 0 && len(args) != 1 {
			m.pushLine(styleError.Render("usage: /sandbox [status|init [--from] [--read-only] [--network] <source>|start|stop|delete confirm]"))
			return m, nil
		}
	case "init":
		for _, arg := range args[1:] {
			switch arg {
			case "--from":
				initOpts.SourceKind = internalsandbox.SourcePack
			case "--read-only":
				initOpts.ReadOnly = true
			case "--network":
				initOpts.Network = true
			default:
				if strings.HasPrefix(arg, "--") || initOpts.Source != "" {
					m.pushLine(styleError.Render("usage: /sandbox init [--from] [--read-only] [--network] <source>"))
					return m, nil
				}
				initOpts.Source = arg
			}
		}
		if initOpts.Source == "" {
			initOpts.Source = m.app.Cfg.Sandbox.DefaultImage
		}
		initOpts.CPUs = m.app.Cfg.Sandbox.CPUs
		initOpts.MemoryMiB = m.app.Cfg.Sandbox.MemoryMiB
		initOpts.StorageGiB = m.app.Cfg.Sandbox.StorageGiB
		initOpts.OverlayGiB = m.app.Cfg.Sandbox.OverlayGiB
		initOpts.StorageSet = true
		initOpts.OverlaySet = true
		initOpts.GuestCWD = m.app.Cfg.Sandbox.GuestCWD
		m.sandboxSetupProfileIndex = len(internalsandbox.Profiles())
		m.sandboxSetupCustomOpts = initOpts
		m.sandboxSetupOpts = initOpts
		m.sandboxSetupIndex = 0
		m.sandboxSetup = true
		return m, nil
	case "delete":
		if len(args) != 2 || args[1] != "confirm" {
			m.pushLine(styleError.Render("usage: /sandbox delete confirm (future Bash calls will run on the host)"))
			return m, nil
		}
	default:
		m.pushLine(styleError.Render("usage: /sandbox [status|init|start|stop|delete confirm]"))
		return m, nil
	}
	return m.startSandboxOperation(action, initOpts)
}

func (m *Model) startSandboxOperation(action string, initOpts internalsandbox.InitOptions) (tea.Model, tea.Cmd) {
	if action == "init" {
		detail := initOpts.Source
		if detail == "" {
			detail = "the configured default image"
		}
		m.pushLine(styleFooter.Render("sandbox init: ensuring smolvm " + internalsandbox.MinimumSmolVMVersion + " and creating " + detail + "…"))
	}
	m.sandboxLoading = true
	m.sandboxGeneration++
	generation := m.sandboxGeneration
	manager := m.app.Sandbox
	ctx := m.ctx
	return m, func() tea.Msg {
		if !m.beginSandboxOperation() {
			return sandboxDoneMsg{generation: generation, action: action, err: context.Canceled}
		}
		defer m.endSandboxOperation()
		var status internalsandbox.Status
		var err error
		switch action {
		case "status":
			status, err = manager.Status(ctx)
		case "init":
			status, err = manager.Init(ctx, initOpts)
		case "start":
			status, err = manager.Start(ctx)
		case "stop":
			status, err = manager.Stop(ctx)
		case "delete":
			err = manager.Delete(ctx)
			status = internalsandbox.Status{Initialized: false}
		}
		return sandboxDoneMsg{generation: generation, action: action, status: status, err: err}
	}
}

func (m *Model) handleSandboxSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	const fields = 7
	switch msg.Type {
	case tea.KeyEsc:
		m.sandboxSetup = false
		m.pushLine(styleFooter.Render("sandbox init canceled"))
		return m, nil
	case tea.KeyUp, tea.KeyShiftTab:
		m.sandboxSetupIndex = (m.sandboxSetupIndex + fields - 1) % fields
	case tea.KeyDown, tea.KeyTab:
		m.sandboxSetupIndex = (m.sandboxSetupIndex + 1) % fields
	case tea.KeyLeft:
		m.adjustSandboxSetup(-1)
	case tea.KeyRight:
		m.adjustSandboxSetup(1)
	case tea.KeySpace:
		if m.sandboxSetupIndex == 5 {
			m.sandboxSetupOpts.ReadOnly = !m.sandboxSetupOpts.ReadOnly
		} else if m.sandboxSetupIndex == 6 && m.sandboxSetupOpts.Profile == "" {
			m.sandboxSetupOpts.Network = !m.sandboxSetupOpts.Network
		}
	case tea.KeyEnter:
		opts := m.sandboxSetupOpts
		m.sandboxSetup = false
		return m.startSandboxOperation("init", opts)
	}
	return m, nil
}

func (m *Model) adjustSandboxSetup(direction int) {
	opts := &m.sandboxSetupOpts
	switch m.sandboxSetupIndex {
	case 0:
		m.selectSandboxProfile(direction)
	case 1:
		opts.CPUs = min(64, max(1, opts.CPUs+direction))
	case 2:
		opts.MemoryMiB = min(262144, max(128, opts.MemoryMiB+direction*1024))
	case 3:
		opts.StorageGiB = adjustSandboxDisk(opts.StorageGiB, direction)
	case 4:
		opts.OverlayGiB = adjustSandboxDisk(opts.OverlayGiB, direction)
	case 5:
		opts.ReadOnly = !opts.ReadOnly
	case 6:
		if opts.Profile == "" {
			opts.Network = !opts.Network
		}
	}
}

func (m *Model) selectSandboxProfile(direction int) {
	profiles := internalsandbox.Profiles()
	choices := len(profiles) + 1 // final choice preserves the configured/custom options
	if m.sandboxSetupProfileIndex == len(profiles) {
		m.sandboxSetupCustomOpts = m.sandboxSetupOpts
	}
	index := (m.sandboxSetupProfileIndex + direction) % choices
	if index < 0 {
		index += choices
	}
	m.sandboxSetupProfileIndex = index
	if index == len(profiles) {
		m.sandboxSetupOpts = m.sandboxSetupCustomOpts
		return
	}
	profile := profiles[index]
	m.sandboxSetupOpts.Profile = profile.ID
	m.sandboxSetupOpts.Source = profile.Source
	m.sandboxSetupOpts.SourceKind = internalsandbox.SourceImage
	m.sandboxSetupOpts.Network = profile.Network
	if profile.CPUs > 0 {
		m.sandboxSetupOpts.CPUs = profile.CPUs
	}
	if profile.MemoryMiB > 0 {
		m.sandboxSetupOpts.MemoryMiB = profile.MemoryMiB
	}
}

func adjustSandboxDisk(value, direction int) int {
	if direction < 0 {
		if value <= 5 {
			return 0
		}
		return value - 5
	}
	if value == 0 {
		return 5
	}
	return min(1048576, value+5)
}

func (m *Model) renderSandboxSetup() string {
	if !m.sandboxSetup {
		return ""
	}
	opts := m.sandboxSetupOpts
	profiles := internalsandbox.Profiles()
	profileLabel := "Custom/configured image"
	profileDescription := "operator-selected source"
	if m.sandboxSetupProfileIndex >= 0 && m.sandboxSetupProfileIndex < len(profiles) {
		profile := profiles[m.sandboxSetupProfileIndex]
		profileLabel = profile.Name
		profileDescription = profile.Description
	}
	source := opts.Source
	if source == "" {
		source = "configured default image"
	}
	storage := "smolvm default"
	if opts.StorageGiB > 0 {
		storage = fmt.Sprintf("%d GiB", opts.StorageGiB)
	}
	overlay := "smolvm default"
	if opts.OverlayGiB > 0 {
		overlay = fmt.Sprintf("%d GiB", opts.OverlayGiB)
	}
	mount := "read-write"
	if opts.ReadOnly {
		mount = "read-only"
	}
	network := "disabled"
	if opts.Network {
		network = "ENABLED (persistent guest egress)"
	}
	if opts.Profile != "" {
		network += " · required by profile"
	}
	rows := []string{
		fmt.Sprintf("Environment      %s", profileLabel),
		fmt.Sprintf("CPUs             %d", opts.CPUs),
		fmt.Sprintf("Memory           %d MiB", opts.MemoryMiB),
		fmt.Sprintf("Storage disk     %s", storage),
		fmt.Sprintf("Overlay disk     %s", overlay),
		fmt.Sprintf("Project mount    %s", mount),
		fmt.Sprintf("Guest network    %s", network),
	}
	lines := []string{
		styleHeader.Render("Sandbox setup"),
		styleHeaderDim.Render(profileDescription),
		styleHeaderDim.Render("Image: " + source),
	}
	for i, row := range rows {
		prefix := "  "
		style := styleCompletion
		if i == m.sandboxSetupIndex {
			prefix = "› "
			style = styleCompletionSelected
		}
		lines = append(lines, style.Render(prefix+row))
	}
	lines = append(lines, styleHeaderDim.Render("↑/↓ field · ←/→ change · Space toggle · Enter create · Esc cancel"))
	return strings.Join(lines, "\n")
}

func formatTUISandboxStatus(status internalsandbox.Status) string {
	if !status.Initialized {
		return "sandbox: not initialized · Bash runs on the host"
	}
	r := status.Record
	mount := "rw"
	if r.ReadOnly {
		mount = "ro"
	}
	network := "net off"
	if r.Network {
		network = "net on"
	}
	routing := "shell:vm"
	if r.Stopped {
		routing = "shell:host (sandbox stopped)"
	}
	runtimeStatus := strings.TrimSpace(status.Runtime)
	if runtimeStatus != "" {
		runtimeStatus = "\n" + runtimeStatus
	}
	diagnostic := strings.TrimSpace(status.Diagnostic)
	if diagnostic != "" {
		runtimeStatus += "\nstatus diagnostic: " + diagnostic
	}
	profile := ""
	if r.Profile != "" {
		profile = " · profile " + r.Profile
	}
	resources := fmt.Sprintf("%d CPU · %d MiB", r.CPUs, r.MemoryMiB)
	if r.StorageGiB > 0 {
		resources += fmt.Sprintf(" · %d GiB storage", r.StorageGiB)
	}
	if r.OverlayGiB > 0 {
		resources += fmt.Sprintf(" · %d GiB overlay", r.OverlayGiB)
	}
	return fmt.Sprintf("sandbox: %s · %s%s · %s · Bash mount %s → %s (%s) · guest %s%s · other tools remain host-side",
		r.Machine, routing, profile, resources, r.Project, r.GuestCWD, mount, network, runtimeStatus)
}

// startLogin handles /login. No args opens the provider picker. A direct
// openai-compatible login captures endpoint then optional key; other API-key
// providers enter masked capture directly.
func (m *Model) startLogin(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.providers = m.supportedProviders()
		m.provIndex = 0
		m.providerLogout = false
		m.pickProvider = true
		m.compVisible = false
		m.editor.Reset()
		m.pushLine(styleFooter.Render("select a login provider (↑/↓ navigate, Enter to pick, Esc to cancel)"))
		return m, nil
	}
	if len(args) > 2 {
		m.pushLine(styleError.Render("usage: /login [provider] [profile-name]"))
		return m, nil
	}
	provider := args[0]
	if provider == chatgpt.ProviderID {
		return m.startChatGPTAuthPick()
	}
	if !m.isSupportedProvider(provider) {
		m.pushLine(styleError.Render("login: unsupported provider " + provider +
			" (supported: " + strings.Join(m.supportedProviders(), ", ") + ")"))
		return m, nil
	}
	if provider == openaicompat.ProviderID {
		if len(args) == 2 {
			profileID := strings.TrimSpace(args[1])
			if err := config.ValidateProviderProfileID(profileID); err != nil {
				m.pushLine(styleError.Render("login: " + err.Error()))
				return m, nil
			}
			m.beginCompatibleEndpointCapture(profileID)
		} else {
			m.beginCompatibleProfileCapture()
		}
	} else if m.isOpenAICompatibleProfile(provider) {
		m.beginCompatibleEndpointCapture(provider)
	} else {
		m.beginKeyCapture(provider)
	}
	return m, nil
}
