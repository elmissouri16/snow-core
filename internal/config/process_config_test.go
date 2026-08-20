package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessConfigDefaultsAndLoad(t *testing.T) {
	defaults := Default()
	if defaults.Processes != DefaultProcesses() {
		t.Fatalf("process defaults = %+v", defaults.Processes)
	}
	tests := []struct {
		name string
		raw  string
		want ProcessConfig
	}{
		{name: "omitted", raw: `{}`, want: DefaultProcesses()},
		{name: "empty", raw: `{"processes":{}}`, want: DefaultProcesses()},
		{name: "partial", raw: `{"processes":{"max_running":7}}`, want: ProcessConfig{MaxRunning: 7, MaxRecords: 32, RetainedOutputBytes: 1 << 20}},
		{name: "complete", raw: `{"processes":{"max_running":7,"max_records":40,"retained_output_bytes":2097152}}`, want: ProcessConfig{MaxRunning: 7, MaxRecords: 40, RetainedOutputBytes: 2 << 20}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(test.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Processes != test.want {
				t.Fatalf("loaded process config = %+v, want %+v", cfg.Processes, test.want)
			}
		})
	}
}

func TestProcessConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  ProcessConfig
		want string
	}{
		{name: "running", cfg: ProcessConfig{MaxRunning: 0, MaxRecords: 32, RetainedOutputBytes: 1 << 20}, want: "max_running"},
		{name: "records", cfg: ProcessConfig{MaxRunning: 4, MaxRecords: 3, RetainedOutputBytes: 1 << 20}, want: "max_records"},
		{name: "output", cfg: ProcessConfig{MaxRunning: 4, MaxRecords: 32, RetainedOutputBytes: 1024}, want: "retained_output_bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.cfg.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}
