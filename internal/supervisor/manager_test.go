package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestSupervisorHelperProcess(t *testing.T) {
	if os.Getenv("SNOW_SUPERVISOR_HELPER") != "1" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	_ = encoder.Encode(protocol.RPCReady{
		Type: protocol.RPCTypeReady, ProtocolVersion: protocol.RPCProtocolVersion,
		Capabilities:  []string{"permission_input", "prompt_completion", "session_info", "session_messages"},
		MaxInputBytes: protocol.RPCMaxInputBytes,
	})
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), protocol.RPCMaxInputBytes)
	activePrompt := ""
	messages := []protocol.Message{protocol.NewUserMessage("u-1", "", "prior task")}
	for scanner.Scan() {
		var request protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		response := func(data any) {
			_ = encoder.Encode(map[string]any{"id": request.ID, "type": "response", "command": request.Type, "success": true, "data": data})
		}
		switch request.Type {
		case "session_info":
			response(protocol.RPCSessionInfo{
				SessionID: os.Getenv("SNOW_HELPER_SESSION_ID"), Path: os.Getenv("SNOW_HELPER_SESSION_PATH"),
				CWD: os.Getenv("SNOW_HELPER_CWD"), Provider: "fake", Model: "fake-1", PermissionMode: "ask", Thinking: protocol.ThinkingOff,
			})
		case "session_messages":
			response(protocol.RPCSessionMessages{Messages: messages})
			if os.Getenv("SNOW_HELPER_SCENARIO") == "exit-after-hydrate" {
				os.Exit(3)
			}
		case "prompt":
			activePrompt = request.ID
			response(map[string]any{"accepted": true})
			_ = encoder.Encode(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "working"})
			_ = encoder.Encode(protocol.AgentEvent{Type: protocol.EvPermissionRequest, Permission: &protocol.Permission{Request: protocol.PermissionRequest{
				ID: "perm-helper-1", Tool: "bash", Args: json.RawMessage(`{"command":"true"}`), Risk: string(permission.RiskExec),
			}}})
			if os.Getenv("SNOW_HELPER_SCENARIO") == "crash-on-prompt" {
				os.Exit(4)
			}
		case "permission_reply", "permission_reject":
			response(nil)
			messages = append(messages, protocol.Message{ID: "a-1", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "done"}}})
			_ = encoder.Encode(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: activePrompt, Status: protocol.RPCPromptCompletedStatus})
			activePrompt = ""
		case "abort":
			response(nil)
			if activePrompt != "" {
				_ = encoder.Encode(protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: activePrompt, Status: protocol.RPCPromptCanceledStatus})
				activePrompt = ""
			}
		default:
			response(nil)
		}
	}
	os.Exit(0)
}

func TestManagerStartPermissionPromptAndStop(t *testing.T) {
	worktreePath, sessionPath, sessionID := createSupervisorSession(t)
	manager := newHelperManager(t, "")
	defer manager.Close(context.Background())
	request := StartRequest{
		WorkspaceID: "workspace-1", SessionID: sessionID, SessionPath: sessionPath,
		WorktreePath: worktreePath, Branch: "snow/test", Provider: "fake", Model: "fake-1", Thinking: protocol.ThinkingOff,
	}
	state, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if state.ProcessStatus != ProcessReady || len(state.Messages) != 1 {
		t.Fatalf("start state = %+v", state)
	}
	if _, err := manager.Start(context.Background(), request); err == nil {
		t.Fatal("duplicate managed session unexpectedly started")
	}
	secondPath := filepath.Join(worktreePath, "second.db")
	secondStore, err := session.NewSQLiteStore(secondPath, worktreePath, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	secondID := secondStore.ID()
	secondMessage := protocol.NewUserMessage("second-seed", "", "seed")
	if err := secondStore.Append(session.Entry{Type: session.EntryMessage, ID: secondMessage.ID, Message: &secondMessage}); err != nil {
		t.Fatal(err)
	}
	if err := secondStore.Close(); err != nil {
		t.Fatal(err)
	}
	secondRequest := request
	secondRequest.SessionID = secondID
	secondRequest.SessionPath = secondPath
	if _, err := manager.Start(context.Background(), secondRequest); err == nil {
		t.Fatal("second managed session in the same worktree unexpectedly started")
	}

	promptDone := make(chan error, 1)
	go func() { promptDone <- manager.Prompt(context.Background(), state.ID, "continue") }()
	var permissionRequest *protocol.PermissionRequest
	deadline := time.After(5 * time.Second)
	for permissionRequest == nil {
		select {
		case event := <-manager.Events():
			if event.Agent != nil && event.Agent.Permission != nil {
				request := event.Agent.Permission.Request
				permissionRequest = &request
			}
		case <-deadline:
			t.Fatal("timed out waiting for attributed worker permission")
		}
	}
	if permissionRequest.ID != "perm-helper-1" {
		t.Fatalf("permission ID = %q", permissionRequest.ID)
	}
	if err := manager.ReplyPermission(context.Background(), state.ID, permissionRequest.ID, permission.DecisionAllow); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-promptDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not complete")
	}
	settled, ok := manager.State(state.ID)
	if !ok || settled.TurnStatus != TurnIdle || len(settled.Messages) != 2 {
		t.Fatalf("settled state = %+v, ok=%t", settled, ok)
	}
	if err := manager.Stop(context.Background(), state.ID); err != nil {
		t.Fatal(err)
	}
	waitForWorkerStatus(t, manager, state.ID, ProcessStopped)
}

func TestClearInteractionDoesNotRegressCompletedTurn(t *testing.T) {
	manager, err := New(context.Background(), Options{MaxConcurrent: 1, CommandFactory: func(context.Context, StartRequest) *exec.Cmd { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	id := WorkerID("worker-complete")
	manager.workers[id] = &managedWorker{state: WorkerState{ID: id, ProcessGeneration: 1, ProcessStatus: ProcessReady, TurnStatus: TurnIdle}}
	manager.clearInteraction(id, "late-permission", true)
	state, _ := manager.State(id)
	if state.TurnStatus != TurnIdle {
		t.Fatalf("late interaction changed status to %s", state.TurnStatus)
	}
}

func TestManagerRejectsHandshakenWorkerWithWrongCWD(t *testing.T) {
	worktreePath, sessionPath, sessionID := createSupervisorSession(t)
	manager := newHelperManager(t, "wrong-cwd")
	defer manager.Close(context.Background())
	state, err := manager.Start(context.Background(), StartRequest{
		WorkspaceID: "workspace-wrong", SessionID: sessionID, SessionPath: sessionPath,
		WorktreePath: worktreePath, Provider: "fake", Model: "fake-1",
	})
	if err == nil {
		t.Fatal("worker with wrong CWD unexpectedly started")
	}
	if state.ProcessStatus != ProcessStopped || state.LastError == "" {
		t.Fatalf("failed start state = %+v", state)
	}
}

func TestManagerMarksReadyWorkerCrashAndOutcomeUnknown(t *testing.T) {
	worktreePath, sessionPath, sessionID := createSupervisorSession(t)
	manager := newHelperManager(t, "crash-on-prompt")
	defer manager.Close(context.Background())
	state, err := manager.Start(context.Background(), StartRequest{
		WorkspaceID: "workspace-crash", SessionID: sessionID, SessionPath: sessionPath,
		WorktreePath: worktreePath, Provider: "fake", Model: "fake-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Prompt(context.Background(), state.ID, "crash now"); err == nil {
		t.Fatal("crashed prompt unexpectedly succeeded")
	}
	waitForWorkerStatus(t, manager, state.ID, ProcessCrashed)
	crashed, _ := manager.State(state.ID)
	if !crashed.OutcomeUnknown || crashed.TurnStatus != TurnOutcomeUnknown {
		t.Fatalf("crashed state = %+v", crashed)
	}
}

func createSupervisorSession(t *testing.T) (string, string, string) {
	t.Helper()
	cwd := t.TempDir()
	path := filepath.Join(cwd, "worker.db")
	store, err := session.NewSQLiteStore(path, cwd, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	id := store.ID()
	message := protocol.NewUserMessage("seed", "", "seed")
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return cwd, path, id
}

func newHelperManager(t *testing.T, scenario string) *Manager {
	t.Helper()
	manager, err := New(context.Background(), Options{
		MaxConcurrent: 2, ShutdownTimeout: time.Second, StartupTimeout: 5 * time.Second,
		CommandFactory: func(ctx context.Context, request StartRequest) *exec.Cmd {
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestSupervisorHelperProcess", "--")
			helperCWD := request.WorktreePath
			if scenario == "wrong-cwd" {
				helperCWD = filepath.Dir(request.WorktreePath)
			}
			command.Env = append(os.Environ(),
				"SNOW_SUPERVISOR_HELPER=1",
				"SNOW_HELPER_SCENARIO="+scenario,
				"SNOW_HELPER_SESSION_ID="+request.SessionID,
				"SNOW_HELPER_SESSION_PATH="+request.SessionPath,
				"SNOW_HELPER_CWD="+helperCWD,
			)
			return command
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func waitForWorkerStatus(t *testing.T, manager *Manager, id WorkerID, status ProcessStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, ok := manager.State(id)
		if ok && state.ProcessStatus == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, _ := manager.State(id)
	t.Fatalf("worker status = %s, want %s (state=%s)", state.ProcessStatus, status, fmt.Sprintf("%+v", state))
}
