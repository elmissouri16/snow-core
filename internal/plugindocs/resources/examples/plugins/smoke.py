#!/usr/bin/env python3
"""Exercise the installed Snow CLI and example plugins without a model account."""

import http.server
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading


EXAMPLES = Path(__file__).resolve().parent
CALLS = [
    ("plugin_project-context_brief", {}),
    ("plugin_todo-radar_scan", {"glob": "**/*.go", "kind": "fixme"}),
    ("plugin_git-review_status", {}),
    ("plugin_git-review_diff", {"scope": "unstaged"}),
    ("plugin_git-review_diff", {"scope": "staged", "stat": True}),
]


class Provider(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def send_body(self, data, content_type):
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        self.send_body(json.dumps({"data": [{"id": "plugin-smoke"}]}).encode(), "application/json")

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        if length <= 0 or length > 1024 * 1024:
            self.send_error(413)
            return
        body = json.loads(self.rfile.read(length))
        self.server.requests.append(body)
        if len(self.server.requests) == 1:
            events = [{"type": "response.output_item.done", "item": {
                "type": "function_call", "id": "item-%d" % i, "call_id": "call-%d" % i,
                "name": name, "arguments": json.dumps(args),
            }} for i, (name, args) in enumerate(CALLS)]
        else:
            events = [{"type": "response.output_text.delta", "delta": "Plugin example checks passed."}]
        events.append({"type": "response.completed", "response": {"status": "completed"}})
        self.send_body("".join("data: " + json.dumps(event) + "\n\n" for event in events).encode(), "text/event-stream")


def checked(args, cwd, env):
    result = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True, timeout=45)
    if result.returncode:
        raise RuntimeError("Command failed: %s\n%s\n%s" % (args[0], result.stdout, result.stderr))
    return result


def main():
    binary = shutil.which(os.environ.get("SNOW_BIN", "snow"))
    if not binary:
        raise RuntimeError("Snow is not on PATH. Install this branch or set SNOW_BIN to its binary.")
    if not shutil.which("git"):
        raise RuntimeError("Git is required for the git-review smoke test.")
    with tempfile.TemporaryDirectory(prefix="snow-plugin-examples-") as temporary:
        root = Path(temporary).resolve()
        workspace = root / "project"
        workspace.mkdir()
        env = {**os.environ, "SNOW_BIN": binary, "SNOW_HOME": str(root / "snow-home"),
               "SNOW_SESSIONS_DIR": str(root / "sessions"), "GIT_CONFIG_GLOBAL": os.devnull,
               "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_COUNT": "0"}
        (workspace / "README.md").write_text("# Plugin fixture\nInitial content.\n")
        (workspace / "go.mod").write_text("module example.invalid/fixture\n\ngo 1.27rc3\n")
        (workspace / "main.go").write_text("package main\n// FIXME: smoke-marker\n")
        for args in [["init", "-b", "main"], ["add", "."],
                     ["-c", "user.name=Plugin Test", "-c", "user.email=plugin@example.invalid",
                      "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + os.devnull,
                      "commit", "-m", "fixture"]]:
            checked(["git", *args], workspace, env)
        (workspace / "README.md").write_text("# Plugin fixture\nunstaged-smoke-marker\n")
        (workspace / "staged.txt").write_text("staged-smoke-marker\n")
        checked(["git", "add", "staged.txt"], workspace, env)
        for package in ["project-context", "todo-radar", "git-review"]:
            checked([binary, "--js-plugin", str(EXAMPLES / package), "plugin", "check", package], workspace, env)

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Provider)
        server.requests = []
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            # Permission allow is confined to this fixture run and is not persisted.
            result = checked([str(EXAMPLES / "try.sh"), "--provider", "openai-compatible",
                "--base-url", "http://127.0.0.1:%d/v1" % server.server_port,
                "--api-key", "local-smoke-fixture", "--model", "plugin-smoke",
                "--permission", "allow", "--thinking", "off", "--no-session", "--no-mcp",
                "--no-skills", "--no-subagents", "--no-debug", "--mode", "json",
                "-p", "Run the five example checks."], workspace, env)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

        frames = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
        if len(server.requests) != 2:
            raise RuntimeError("Expected one tool step followed by one final response")
        names = {tool.get("name") for tool in server.requests[0].get("tools", [])}
        if not {name for name, _ in CALLS}.issubset(names):
            raise RuntimeError("Example tools were not exposed to the provider")
        outputs = [item for item in server.requests[1]["input"] if item.get("type") == "function_call_output"]
        if len(outputs) != len(CALLS):
            raise RuntimeError("Unexpected nested or missing transcript results")
        output_by_call = {item["call_id"]: json.dumps(item["output"]) for item in outputs}
        for i, marker in enumerate(["example.invalid/fixture", "main.go:2: // FIXME: smoke-marker",
                                    "staged.txt", "+unstaged-smoke-marker", "1 file changed"]):
            if marker not in output_by_call.get("call-%d" % i, ""):
                raise RuntimeError("Example %s failed: %s" % (CALLS[i][0], output_by_call))
        print("PASS: all three plugins initialized and five tool calls executed through the installed CLI.")
        print("PASS: real file/search/Git results, five outer transcript results, %d valid JSON frames." % len(frames))
        print("Temporary project and isolated Snow configuration cleaned up; no model API used.")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.TimeoutExpired) as error:
        raise SystemExit(str(error))
