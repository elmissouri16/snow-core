package rpc

import (
	"strings"
	"testing"
)

func TestProcessControlStrictParameters(t *testing.T) {
	id := "proc_" + strings.Repeat("a", 32)
	for _, tc := range []struct {
		command, params string
		valid           bool
	}{
		{"process_control_list", `{"session_id":"session"}`, true},
		{"process_control_logs", `{"session_id":"session","process_id":"` + id + `","cursor":0,"max_bytes":32768}`, true},
		{"process_control_stop", `{"session_id":"session","process_id":"` + id + `","grace_ms":5000}`, true},
		{"process_control_list", `{}`, false},
		{"process_control_list", `{"session_id":"session","session_id":"other"}`, false},
		{"process_control_list", `{"session_id":"session","command":"secret"}`, false},
		{"process_control_logs", `{"session_id":"session","process_id":"` + id + `","cursor":null}`, false},
		{"process_control_logs", `{"session_id":"session","process_id":"` + id + `","cursor":-1}`, false},
		{"process_control_logs", `{"session_id":"session","process_id":"` + id + `","max_bytes":32769}`, false},
		{"process_control_stop", `{"session_id":"session","process_id":"1234"}`, false},
		{"process_control_stop", `{"session_id":"session","process_id":"` + id + `","grace_ms":5001}`, false},
		{"process_control_start", `{"session_id":"session"}`, false},
	} {
		t.Run(tc.command+tc.params, func(t *testing.T) {
			err := validateProcessControlParams(Request{Type: tc.command, Params: []byte(tc.params)})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
