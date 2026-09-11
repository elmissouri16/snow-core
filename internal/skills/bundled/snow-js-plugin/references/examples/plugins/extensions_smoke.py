#!/usr/bin/env python3
"""Exercise the API 2 example pack through the installed CLI and JSONL RPC.

Uses isolated configuration, a temporary project, and fake/local providers.
No model account, user configuration, or network service is used.
"""

import http.server
import json
import os
from pathlib import Path
import queue
import shutil
import subprocess
import tempfile
import threading


EXAMPLES = Path(__file__).resolve().parent
PACKAGES = ["workspace-dashboard", "review-team", "project-helper-v2", "ui-studio",
            "workspace-notes", "prompt-recipes", "session-pilot"]


class RPC:
    def __init__(self, binary, workspace, env, provider=None):
        self.frames = []
        self.queue = queue.Queue()
        self.counter = 0
        self.process = subprocess.Popen(
            [binary, "--mode", "rpc", "--no-mcp", "--no-skills", "--no-debug",
             "--permission", "allow", "--thinking", "off", "--subagents",
             *(provider or ["--provider", "fake"])],
            cwd=workspace, env=env, text=True, stdin=subprocess.PIPE,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.reader = threading.Thread(target=self.read, daemon=True)
        self.reader.start()

    def read(self):
        try:
            for line in self.process.stdout:
                self.queue.put(json.loads(line))
        except Exception as error:
            self.queue.put(error)
        finally:
            self.queue.put(None)

    def send(self, kind, params=None):
        self.counter += 1
        request = {"id": str(self.counter), "type": kind}
        if params is not None:
            request["params"] = params
        self.process.stdin.write(json.dumps(request) + "\n")
        self.process.stdin.flush()
        return request["id"]

    def request(self, kind, params=None, answers=None, failure=False):
        request_id = self.send(kind, params)
        while True:
            frame = self.queue.get(timeout=45)
            if frame is None:
                raise RuntimeError("Snow exited: " + self.process.stderr.read())
            if isinstance(frame, Exception):
                raise frame
            self.frames.append(frame)
            # Agent events are normalized versioned JSON objects.
            event = frame.get("event", frame)
            if event.get("type") == "user_input_request":
                if answers is None:
                    raise AssertionError("Unexpected user input: " + str(event))
                request = event["user_input"]
                self.send("user_input_reply", {"request_id": request["id"], "answers": [
                    {"id": q["id"], "answer": str(answers[q["id"]])} for q in request["questions"]]})
            if frame.get("id") == request_id and frame.get("type") == "response":
                if failure:
                    assert not frame["success"], frame
                    return frame
                assert frame["success"], frame
                return frame.get("data")

    def command(self, command, text="", **kwargs):
        result = self.request("plugin_command_run", {"command": command, "input": text}, **kwargs)
        if kwargs.get("failure"):
            return result
        assert not result.get("is_error"), result
        return "\n".join(block.get("text", "") for block in result.get("content", []))

    def close(self):
        self.process.stdin.close()
        try:
            self.process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait()
            raise
        self.reader.join(timeout=2)
        error = self.process.stderr.read()
        self.process.stdout.close()
        self.process.stderr.close()
        assert self.process.returncode == 0, error


class Provider(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_GET(self):
        body = json.dumps({"data": [{"id": "plugin-smoke"}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.server.requests.append(body)
        items = body.get("input", [])
        last_user = max((i for i, item in enumerate(items) if item.get("role") == "user" and "<snow_internal_context" not in json.dumps(item)), default=-1)
        prompt = json.dumps(items[last_user] if last_user >= 0 else items)
        outputs = [item for item in items[last_user + 1:] if item.get("type") == "function_call_output"]
        name, args = None, {}
        if not outputs:
            if "hook-read-fixture" in prompt:
                name, args = "read", {"path": "sample.txt", "limit": 300}
            elif "card-fixture" in prompt or "Use plugin_project-helper-v2_files to discover" in prompt:
                name = "plugin_project-helper-v2_files"
        if name:
            events = [{"type": "response.output_item.done", "item": {
                "type": "function_call", "id": "item", "call_id": "call-" + str(len(self.server.requests)),
                "name": name, "arguments": json.dumps(args)}}]
        else:
            events = [{"type": "response.output_text.delta", "delta": "Fixture inspected main.go."}]
        events.append({"type": "response.completed", "response": {"status": "completed"}})
        encoded = "".join("data: " + json.dumps(event) + "\n\n" for event in events).encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)


def check_management(binary, workspace, env):
    rpc = RPC(binary, workspace, env)
    try:
        statuses = rpc.request("plugin_statuses")
        assert {p["id"] for p in statuses} == set(PACKAGES), statuses
        assert all(p["enabled"] and p["loaded"] and p["can_toggle"]
                   and not p["restart_required"] for p in statuses), statuses
        status = rpc.request("plugin_disable", {"id": "workspace-notes"})
        assert not status["enabled"] and status["loaded"] and status["restart_required"], status
        assert "No notes yet" in rpc.command("workspace-notes:open"), "Running catalog changed before restart"
    finally:
        rpc.close()
    rpc = RPC(binary, workspace, env)
    try:
        statuses = rpc.request("plugin_statuses")
        for status in statuses:
            expected = status["id"] != "workspace-notes"
            assert status["enabled"] == expected and status["loaded"] == expected, status
            assert not status["restart_required"], status
        assert all(c["plugin_id"] != "workspace-notes" for c in rpc.request("plugin_commands"))
        rpc.command("workspace-notes:open", failure=True)
        status = rpc.request("plugin_enable", {"id": "workspace-notes"})
        assert status["enabled"] and not status["loaded"] and status["restart_required"], status
    finally:
        rpc.close()
    print("PASS: individual plugin enable/disable, disabled inventory, persistence, and restart boundaries.")


def check_controls(binary, workspace, env):
    rpc = RPC(binary, workspace, env)
    try:
        infos = rpc.request("plugins_list")
        assert {p["id"] for p in infos} == set(PACKAGES), infos
        commands = rpc.request("plugin_commands")
        aliases = {c.get("alias") for c in commands}
        assert {"notes", "ui-studio", "recipe", "pilot", "source-scout", "review-team", "dashboard"} <= aliases
        assert "Saved note 1" in rpc.command("workspace-notes:add", "remember smoke-marker")
        assert "smoke-marker" in rpc.command("workspace-notes:open")
        assert "No active goal" in rpc.command("session-pilot:goal", "status")
        assert "UI Studio ready" in rpc.command("ui-studio:open")
        rpc.command("ui-studio:form", answers={"task": "Verify plugin forms", "steps": "7", "tests": "true"})
        rpc.command("ui-studio:show")
        views = rpc.request("plugin_views")
        focus = next(v for v in views if v["id"] == "ui-studio:focus")
        assert "7 steps" in json.dumps(focus) and "include tests" in json.dumps(focus), focus
        rpc.command("ui-studio:hide")
        rpc.command("ui-studio:form", answers={"task": "Invalid steps", "steps": "-1", "tests": "false"}, failure=True)
        assert "7 steps" in rpc.command("ui-studio:draft")
        assert "#review plugin fixtures" == rpc.command("prompt-recipes:draft", "review plugin fixtures")
        assert "hooks off" in rpc.command("prompt-recipes:mode")
        assert "hooks on" in rpc.command("prompt-recipes:mode", "concise")
        assert "Model: fake-1" in rpc.command("session-pilot:status")
        rpc.command("session-pilot:model", "fake-1")
        rpc.command("session-pilot:rename", "Extension smoke")
        rpc.command("session-pilot:branch", "fork smoke-branch")
        assert "smoke-branch" in rpc.command("session-pilot:branch", "list")
        assert "Goal not started" in rpc.command("session-pilot:goal", "create Fixture objective", answers={"answer": "No"})
        rpc.command("session-pilot:run", "#explain plugin fixture")
        messages = rpc.request("messages_list")["messages"]
        assert "Explain the following using the actual code" in json.dumps(messages), messages
        rpc.command("review-team:run", "Review this fixture")
        agents = [a for a in rpc.request("subagent_list")["agents"] if a["agent"]["path"] != "/root"]
        assert len(agents) == 3 and all(a["status"] == "closed" for a in agents), agents
    finally:
        rpc.close()
    rpc = RPC(binary, workspace, env)
    try:
        assert "smoke-marker" in rpc.command("workspace-notes:open"), "Project storage did not survive restart"
        assert "7 steps" in rpc.command("ui-studio:draft"), "Typed form did not survive restart"
        assert "hooks off" in rpc.command("prompt-recipes:mode"), "Run-only hooks leaked across restart"
        assert "Notes kept" in rpc.command("workspace-notes:clear", answers={"answer": "No"})
        rpc.command("workspace-notes:remove", "1")
        assert "No notes yet" in rpc.command("workspace-notes:open")
    finally:
        rpc.close()
    print("PASS: normal registration loading, commands, typed forms, views, persistent notes, branches, model control, review cleanup.")


def check_provider(binary, workspace, env):
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Provider)
    server.requests = []
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    rpc = RPC(binary, workspace, env, ["--provider", "openai-compatible", "--base-url",
        "http://127.0.0.1:%d/v1" % server.server_port, "--api-key", "local-fixture", "--model", "plugin-smoke"])
    try:
        rpc.command("prompt-recipes:mode", "concise")
        rpc.command("session-pilot:run", "#review hook-read-fixture")
        messages = rpc.request("messages_list")["messages"]
        serialized = json.dumps(messages)
        assert '"limit": 120' in serialized and "bounded read" in serialized, serialized
        assert "Prompt Recipes concise mode" in json.dumps(server.requests), "Request hook missing"
        rpc.command("prompt-recipes:mode", "off")
        rpc.command("session-pilot:run", "card-fixture")
        messages = rpc.request("messages_list")["messages"]
        assert "Project sources" in json.dumps(messages), "Custom tool card not persisted"
        result = rpc.command("project-helper-v2:scout", "Where is main.go?")
        assert "main.go" in result, result
        agents = [a for a in rpc.request("subagent_list")["agents"] if a["agent"]["path"] != "/root"]
        assert len(agents) == 1 and agents[0]["status"] == "closed", agents
        assert "plugin_project-helper-v2_files" in agents[0]["plugin_tools"], agents
        child_requests = [r for r in server.requests if "Use plugin_project-helper-v2_files to discover" in json.dumps(r.get("input"))]
        assert child_requests and any("main.go" in json.dumps(r.get("input")) for r in child_requests)
        assert any(t.get("name") == "plugin_project-helper-v2_files" for t in child_requests[0]["tools"])
    finally:
        rpc.close()
        server.shutdown()
        server.server_close()
        thread.join()
    print("PASS: four hook phases, real bounded file reads, persisted tool cards, and selected child plugin execution.")


def main():
    binary = shutil.which(os.environ.get("SNOW_BIN", "snow"))
    if not binary:
        raise RuntimeError("Install Snow or set SNOW_BIN.")
    with tempfile.TemporaryDirectory(prefix="snow-extension-pack-") as temporary:
        root = Path(temporary).resolve()
        workspace = root / "project"
        workspace.mkdir()
        (workspace / "main.go").write_text("package main\n// plugin fixture\n")
        (workspace / "sample.txt").write_text("".join("sample line %d\n" % n for n in range(1, 401)))
        env = {**os.environ, "SNOW_HOME": str(root / "home"), "SNOW_SESSIONS_DIR": str(root / "sessions")}
        for package in PACKAGES:
            subprocess.run([binary, "plugin", "add", str(EXAMPLES / package)], cwd=workspace,
                           env=env, check=True, capture_output=True, text=True, timeout=15)
            subprocess.run([binary, "plugin", "check", package], cwd=workspace,
                           env=env, check=True, capture_output=True, text=True, timeout=15)
        config_path = Path(env["SNOW_HOME"]) / "config.json"
        config = json.loads(config_path.read_text())
        config["subagents"] = {"roles": {"plugin_scout": {"description": "Read-only plugin scout",
            "tools": ["read", "grep", "glob", "plugin_project-helper-v2_files"]}}}
        config_path.write_text(json.dumps(config))
        check_management(binary, workspace, env)
        check_controls(binary, workspace, env)
        check_provider(binary, workspace, env)
    print("Temporary configuration and fixture cleaned up. No model API used.")


if __name__ == "__main__":
    main()
