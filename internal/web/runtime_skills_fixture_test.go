package web

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"os"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func init() {
	if os.Getenv("SNOW_WEB_SKILLS_TEST_CHILD") == "1" {
		os.Exit(runtimeSkillsFixture())
	}
}

func runtimeSkillsFixture() int {
	for _, flag := range []string{"--no-session", "--managed-explicit-goals", "--no-plugins", "--subagents", "--no-debug"} {
		if !slices.Contains(os.Args, flag) {
			return 10
		}
	}
	if slices.Contains(os.Args, "--no-mcp") || slices.Contains(os.Args, "--no-subagents") || slices.Contains(os.Args, "--no-skills") || slices.Contains(os.Args, "--permission") || slices.Contains(os.Args, "--trust-project") {
		return 11
	}
	tools := "read,glob,grep,write,edit,bash,ask_user,get_goal,create_goal,update_goal,process_start,process_status,process_logs,process_stop,process_list,activate_skill,deactivate_skill,read_skill_resource"
	for flag, want := range map[string]string{"--mode": "rpc", "--rpc-startup": "eager", "--tools": tools} {
		i := slices.Index(os.Args, flag)
		if i < 0 || i+1 >= len(os.Args) || os.Args[i+1] != want {
			return 12
		}
	}
	log, err := os.OpenFile(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 13
	}
	defer log.Close()
	emit := func(v any) { data, _ := json.Marshal(v); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("skills-fixture")
	ready.Capabilities = []string{"session_management", "session_info", "prompt_completion", "permission_interaction", "user_input", "messages_page", "skills"}
	emit(ready)
	cwd, _ := os.Getwd()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			return 14
		}
		fmt.Fprintln(log, req.Type)
		res := protocol.RPCResponse{Type: "response", ID: req.ID, Command: req.Type, Success: true}
		switch req.Type {
		case "session_create", "session_open":
			res.Data = protocol.RPCSessionSummary{SessionID: "skills-session"}
		case "session_info":
			res.Data = protocol.RPCSessionInfo{SessionID: "skills-session", CWD: cwd, Path: "/private/session.db", Provider: "host-provider", Model: "host-model", PermissionMode: "ask"}
		case "messages_page":
			res.Data = protocol.RPCMessagesPage{}
		case "abort":
		case "skills":
			if req.Params != nil {
				return 15
			}
			res.Data = protocol.RPCSkillsList{Skills: []protocol.RPCSkill{
				{Name: "review", Description: "Review changes", Enabled: true, Location: "/PRIVATE/SKILL.md", Metadata: map[string]string{"secret": "PRIVATE-CONFIG"}},
				{Name: "deploy", Description: "Deploy changes", Enabled: false, DisabledBy: "disabled by project named skill policy", Source: "PRIVATE-SOURCE"},
			}, Diagnostics: []protocol.RPCSkillDiagnostic{{Path: "/PRIVATE/broken/SKILL.md", Message: "PRIVATE-DIAGNOSTIC"}}}
		default:
			return 16
		}
		emit(res)
	}
	return 0
}
