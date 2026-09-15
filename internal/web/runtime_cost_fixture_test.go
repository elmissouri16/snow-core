package web

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"os"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func init() {
	if os.Getenv("SNOW_WEB_COST_TEST_CHILD") == "1" {
		os.Exit(runtimeCostFixture())
	}
}

func runtimeCostFixture() int {
	emit := func(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }
	ready := protocol.NewRPCReady("cost-fixture")
	ready.Capabilities = []string{"session_management", "session_info", "prompt_completion", "permission_interaction", "user_input", "messages_page", "usage"}
	emit(ready)
	cwd, _ := os.Getwd()
	active := "created"
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var request protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return 1
		}
		response := protocol.RPCResponse{Type: "response", ID: request.ID, Command: request.Type, Success: true}
		switch request.Type {
		case "session_create", "session_open":
			if request.Type == "session_open" {
				var params struct {
					SessionID string `json:"session_id"`
				}
				_ = json.Unmarshal(request.Params, &params)
				active = params.SessionID
			}
			response.Data = protocol.RPCSessionSummary{SessionID: active, Active: true}
		case "session_info":
			response.Data = protocol.RPCSessionInfo{SessionID: active, CWD: cwd, Path: "/private/session.db", Provider: "host-provider", Model: "host-model", PermissionMode: "ask"}
		case "messages_page":
			response.Data = protocol.RPCMessagesPage{}
		case "sessions_list":
			response.Data = protocol.RPCSessionList{Sessions: []protocol.RPCSessionSummary{{SessionID: "created", Active: active == "created"}, {SessionID: "saved", Active: active == "saved"}, {SessionID: "unknown", Active: active == "unknown"}, {SessionID: "conflict", Active: active == "conflict"}}}
		case "usage":
			cost := &protocol.Cost{Currency: "USD", Total: 1.25}
			if active == "saved" {
				cost = &protocol.Cost{Currency: "EUR", Total: 2.5}
			}
			if active == "unknown" || active == "conflict" {
				cost = nil
			}
			response.Data = protocol.Usage{Input: 100, Output: 20, Total: 120, Requests: 2, Cost: cost, CostCurrencyConflict: active == "conflict"}
		case "abort":
		default:
			response.Success = false
		}
		emit(response)
	}
	return 0
}
