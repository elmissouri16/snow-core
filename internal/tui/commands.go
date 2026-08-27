package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// runCommand handles slash commands.
func (m *Model) runCommand(line string) (tea.Model, tea.Cmd) {
	return m.runCommandWithDisplay(line, line)
}

func (m *Model) runCommandWithDisplay(line, displayLine string) (tea.Model, tea.Cmd) {
	m.imagePasteGeneration++
	m.editor.Reset()
	m.pastedTexts = nil
	m.resetInputHistoryNavigation()
	parts := strings.Fields(line)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "/quit", "/q":
		return m, m.quitCmd()
	case "/help":
		return m.startHelp()
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
				if g.Status == protocol.GoalBlocked {
					m.pushLine(renderBlockedGoalTranscript(g))
				}
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
		displayMessage := strings.TrimSpace(strings.TrimPrefix(displayLine, "/plan"))
		m.nudgeDismissed[m.planNudgeScope()] = true
		if message == "" {
			if err := m.app.Agent.SetMode(protocol.ModePlan); err != nil {
				m.pushLine(styleError.Render(err.Error()))
			}
			return m, nil
		}
		if displayMessage == "" {
			displayMessage = message
		}
		m.pushLine(styleUser.Render("› " + displayMessage))
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
	case "/processes":
		if len(args) > 1 {
			m.pushLine(styleError.Render("usage: /processes [id | name]"))
			return m, nil
		}
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		return m, m.openProcessFleet(target)
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
		if len(args) == 0 {
			return m.startSkillsInfo()
		}
		if len(args) != 1 || args[0] != "clear" {
			m.pushLine(styleError.Render("usage: /skills [clear]"))
			return m, nil
		}
		cleared, err := m.app.Agent.ClearActiveSkills()
		if err != nil {
			m.pushLine(styleError.Render("skills: " + err.Error()))
			return m, nil
		}
		if cleared == 0 {
			m.pushLine(styleFooter.Render("no active skills to clear"))
		} else {
			m.pushLine(styleFooter.Render(fmt.Sprintf("cleared %d active skill(s)", cleared)))
		}
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

func (m *Model) startLogin(args []string) (tea.Model, tea.Cmd) {
	if m.authOperationPending() {
		m.pushLine(styleError.Render("login: wait for the current authentication operation to finish"))
		return m, nil
	}
	// Login reuses the composer textarea for single-line fields. Invalidate any
	// clipboard/image result that was requested by the prior composer state.
	m.imagePasteGeneration++
	m.clearLoginNavigation()
	m.oauthBackRequested = false
	if len(args) == 0 {
		m.providers = m.supportedProviders()
		m.provIndex = 0
		m.providerLogout = false
		m.pickProvider = true
		m.loginError = ""
		m.compVisible = false
		m.editor.Reset()
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
