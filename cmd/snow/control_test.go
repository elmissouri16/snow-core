package main

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// The helper executes the real root command and its actual flag parser, rather
// than reconstructing a smaller Cobra command that could miss startup wiring.
func TestControlCLIHelperProcess(t *testing.T) {
	if os.Getenv("SNOW_CONTROL_TEST_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	os.Args = append([]string{"snow"}, args...)
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

type controlCLIFixture struct{ root, home, snow, cwd, sessions string }

func newControlCLIFixture(t *testing.T) controlCLIFixture {
	t.Helper()
	root := t.TempDir()
	f := controlCLIFixture{root: root, home: filepath.Join(root, "home"), snow: filepath.Join(root, "snow"), cwd: filepath.Join(root, "project"), sessions: filepath.Join(root, "sessions")}
	for _, dir := range []string{f.home, f.snow, f.cwd} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f controlCLIFixture) run(t *testing.T, input string, args ...string) (string, string, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestControlCLIHelperProcess$", "--"}, args...)...)
	cmd.Dir = f.cwd
	// Deliberate allowlist: no inherited credentials, custom endpoints, proxies,
	// config overrides or real user HOME may reach this subprocess.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + f.home, "USERPROFILE=" + f.home, "SNOW_HOME=" + f.snow,
		"SNOW_SESSIONS_DIR=" + f.sessions, "SNOW_CONTROL_TEST_HELPER=1", "TMPDIR=" + f.root}
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("control subprocess did not terminate: %v", ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

func controlCLISnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := info.Mode().String()
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += fmt.Sprintf(" %d %s", info.ModTime().UnixNano(), data)
		}
		snapshot[relative] = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func controlCLIFrames(t *testing.T, output string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if len(line)+1 > protocol.RPCControlMaxOutputBytes {
			t.Fatal("unbounded output frame")
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("invalid control output: %v", err)
		}
		frames = append(frames, frame)
	}
	return frames
}

func TestControlCLIActualRootRuntimeFreeRead(t *testing.T) {
	f := newControlCLIFixture(t)
	var providerCalls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer provider.Close()
	marker := filepath.Join(f.root, "extension-started")
	configData, err := json.Marshal(map[string]any{
		"default_provider": "openai-compatible", "default_model": "control-fixture",
		"providers":   map[string]any{"openai-compatible": map[string]any{"base_url": provider.URL}},
		"mcp_servers": map[string]any{"control-marker": map[string]any{"command": "sh", "args": []string{"-c", `printf started > "$1"`, "sh", marker}}},
		"debug":       map[string]any{"enabled": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		filepath.Join(f.snow, "config.json"): configData,
		filepath.Join(f.snow, "auth.json"):   []byte(`{"openai-compatible":{"type":"api_key","key":"PRIVATE_CONTROL_KEY"}}`),
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// A project file must never be consulted by this cold host-control process.
	if err := os.Mkdir(filepath.Join(f.cwd, ".snow"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.cwd, ".snow", "config.json"), []byte("not valid JSON: PRIVATE_PROJECT_CONFIG"), 0600); err != nil {
		t.Fatal(err)
	}
	before := controlCLISnapshot(t, f.root)
	input := `{"id":"get","type":"defaults_get","params":{"scope":"global"}}` + "\n" +
		`{"id":"status","type":"provider_status_list"}` + "\n"
	for _, command := range []string{"prompt", "session_create", "sessions_list", "catalog_sessions", "models_discover", "auth_login_start", "auth_providers", "plugin_reload", "mcp_servers", "auth_refresh"} {
		input += `{"type":"` + command + `"}` + "\n"
	}
	output, stderr, err := f.run(t, input, "--mode", "rpc", "--rpc-startup", "control")
	if err != nil {
		t.Fatalf("control startup: %v, stderr=%s", err, stderr)
	}
	frames := controlCLIFrames(t, output)
	caps, ok := frames[0]["capabilities"].([]any)
	if len(frames) != 13 || !ok || len(caps) < 3 || !reflect.DeepEqual(caps[:3], []any{"runtime_free_control", "defaults_control", "provider_status"}) ||
		frames[0]["max_input_bytes"] != float64(protocol.RPCControlMaxInputBytes) {
		t.Fatalf("unexpected frames: %v", frames)
	}
	for _, frame := range frames[1:3] {
		if frame["success"] != true {
			t.Fatalf("read failed: %v", frame)
		}
	}
	for _, frame := range frames[3:] {
		if frame["success"] != false || frame["error_code"] != "unsupported" {
			t.Fatalf("legacy command accepted: %v", frame)
		}
	}
	if strings.Contains(output+stderr, "PRIVATE_") || strings.Contains(output+stderr, f.snow) {
		t.Fatal("private data leaked")
	}
	if providerCalls.Load() != 0 {
		t.Fatal("control contacted provider")
	}
	if !reflect.DeepEqual(before, controlCLISnapshot(t, f.root)) {
		t.Fatal("read-only control created session, extension, debug or other runtime files")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("extension initialized: %v", err)
	}
	if _, err := os.Stat(f.sessions); !os.IsNotExist(err) {
		t.Fatalf("session initialized: %v", err)
	}
}

func TestControlCLIRejectsRuntimeFlagsAndSubcommands(t *testing.T) {
	cases := [][]string{
		{"--rpc-startup", "control"},
		{"--mode", "rpc", "--rpc-startup", "control", "--provider", "fake"},
		{"--mode", "rpc", "--rpc-startup", "control", "--model", "fake"},
		{"--mode", "rpc", "--rpc-startup", "control", "--permission", "allow"},
		{"--mode", "rpc", "--rpc-startup", "control", "--no-session"},
		{"--mode", "rpc", "--rpc-startup", "control", "--no-plugins"},
		{"--mode", "rpc", "--rpc-startup", "control", "--config", "private-config"},
		{"--mode", "rpc", "--rpc-startup", "control", "-p", "never execute"},
		{"--mode", "rpc", "--rpc-startup", "control", "resume"},
		{"--mode", "rpc", "--rpc-startup", "control", "login", "chatgpt"},
		{"--mode", "rpc", "--rpc-startup", "control", "sessions"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := newControlCLIFixture(t)
			before := controlCLISnapshot(t, f.root)
			output, _, err := f.run(t, "", args...)
			if err == nil || strings.Contains(output, `"type":"rpc_ready"`) {
				t.Fatalf("invalid startup accepted: %v %s", err, output)
			}
			if !reflect.DeepEqual(before, controlCLISnapshot(t, f.root)) {
				t.Fatal("invalid startup mutated private storage")
			}
		})
	}
}

func TestControlCLIReadyDoesNotReadMalformedConfiguration(t *testing.T) {
	f := newControlCLIFixture(t)
	for _, name := range []string{"config.json", "auth.json"} {
		if err := os.WriteFile(filepath.Join(f.snow, name), []byte("PRIVATE_MALFORMED_CONTENT"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := controlCLISnapshot(t, f.root)
	output, stderr, err := f.run(t, `{"type":"defaults_get","params":{"scope":"global"}}`+"\n", "--mode", "rpc", "--rpc-startup", "control")
	if err != nil {
		t.Fatalf("cold startup loaded runtime config: %v %s", err, stderr)
	}
	frames := controlCLIFrames(t, output)
	if len(frames) != 2 || frames[0]["type"] != "rpc_ready" || frames[1]["success"] != false {
		t.Fatalf("frames=%v", frames)
	}
	if strings.Contains(output+stderr, "PRIVATE_") || strings.Contains(output+stderr, f.snow) {
		t.Fatal("raw parse error leaked")
	}
	if !reflect.DeepEqual(before, controlCLISnapshot(t, f.root)) {
		t.Fatal("failed read changed storage")
	}
}

func TestControlCLIWriteOnlyAPIKeyPrivateStorage(t *testing.T) {
	f := newControlCLIFixture(t)
	configPath := filepath.Join(f.snow, "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	configBefore, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	projectBefore := controlCLISnapshot(t, f.cwd)
	homeBefore := controlCLISnapshot(t, f.home)
	secret := "PRIVATE_CONTROL_KEY_" + strings.Repeat("x", 4096-len("PRIVATE_CONTROL_KEY_"))
	set, err := json.Marshal(map[string]any{"id": "set", "type": "api_key_set", "params": protocol.HostAPIKeySetRequest{
		ProviderID: "opencode-go", ExpectedRevision: "missing", Secret: secret, ConfirmReplace: false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := `{"id":"inspect","type":"api_key_inspect","params":{"provider_id":"opencode-go"}}` + "\n" + string(set) + "\n" +
		`{"id":"status","type":"provider_status_list"}` + "\n"
	output, stderr, err := f.run(t, input, "--mode", "rpc", "--rpc-startup", "control")
	if err != nil {
		t.Fatalf("key control failed: %v %s", err, stderr)
	}
	frames := controlCLIFrames(t, output)
	if len(frames) != 4 {
		t.Fatalf("frames=%v", frames)
	}
	for _, frame := range frames[1:] {
		if frame["success"] != true {
			t.Fatalf("key operation failed: %v", frame)
		}
	}
	if strings.Contains(output+stderr, "PRIVATE_CONTROL_KEY_") {
		t.Fatal("submitted key leaked")
	}
	authPath := filepath.Join(f.snow, "auth.json")
	authData, err := os.ReadFile(authPath)
	if err != nil || !bytes.Contains(authData, []byte(secret)) {
		t.Fatal("explicit key set was not persisted")
	}
	info, err := os.Stat(authPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential file mode is not 0600")
	}
	configAfter, err := os.Stat(configPath)
	if err != nil || !configBefore.ModTime().Equal(configAfter.ModTime()) {
		t.Fatal("key write changed config")
	}
	if !reflect.DeepEqual(projectBefore, controlCLISnapshot(t, f.cwd)) || !reflect.DeepEqual(homeBefore, controlCLISnapshot(t, f.home)) {
		t.Fatal("key write initialized project/user runtime")
	}
	if _, err := os.Stat(f.sessions); !os.IsNotExist(err) {
		t.Fatal("key write initialized sessions")
	}
}
