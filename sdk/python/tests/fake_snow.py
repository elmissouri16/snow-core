#!/usr/bin/env python3
"""Deterministic JSONL RPC fixture for the Python SDK tests."""

import json
import os
import signal
import sys
import time


def emit(value):
    sys.stdout.write(json.dumps(value, separators=(",", ":")) + "\n")
    sys.stdout.flush()


scenario = os.environ.get("FAKE_SNOW_SCENARIO", "")

emit(
    {
        "type": "rpc_ready",
        "protocol_version": 1 if scenario == "bad_version_type" else ("2" if scenario == "bad_version" else "1"),
        "snow_version": 1 if scenario == "bad_snow_type" else "fixture",
        "capabilities": [1] if scenario == "bad_caps_type" else ["models_list", "multimodal_prompts", "permission_interaction", "prompt_completion", "session_info", "subagent_models", "user_input"],
        "max_input_bytes": "128" if scenario == "bad_max_type" else (128 if scenario == "small_limit" else 16777216),
    }
)
emit({"type": "mode_changed", "mode": {"mode": "default", "reasoning_effort": "off"}})
emit({"id": "unknown-fixture", "type": "response", "command": "fixture", "success": True})
if scenario == "malformed":
    sys.stdout.write("{not-json}\n")
    sys.stdout.flush()
if scenario == "oversized":
    sys.stdout.buffer.write(b"x" * 2048)
    sys.stdout.buffer.flush()
if scenario == "stdout_closed":
    sys.stdout.close()
if scenario == "stdin_closed":
    os.close(0)
    time.sleep(5)
    raise SystemExit(0)

held = None
asking_prompt = None
permission_prompt = None
waiting_prompt = None
for line in sys.stdin:
    if not line.strip():
        continue
    request = json.loads(line)
    request_id = request.get("id", "")
    command = request["type"]
    if command == "fatal_ignore_sigterm":
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        sys.stdout.write("{not-json}\n")
        sys.stdout.flush()
        continue
    if command == "crash":
        print("fixture process failure", file=sys.stderr, flush=True)
        raise SystemExit(7)
    if command == "fail_command":
        emit({"id": request_id, "type": "response", "command": command, "success": False, "error": "fixture command failure"})
        continue
    if command == "hold":
        held = request
        continue
    if command == "release":
        emit({"id": request_id, "type": "response", "command": command, "success": True})
        if held is not None:
            emit({"id": held.get("id", ""), "type": "response", "command": "hold", "success": True})
            held = None
        continue
    if command == "session_info":
        emit(
            {
                "id": request_id,
                "type": "response",
                "command": command,
                "success": True,
                "data": {"provider": "fake", "model": "fake-1"},
            }
        )
        continue
    if command in ("models_list", "subagent_models"):
        emit(
            {
                "id": request_id,
                "type": "response",
                "command": command,
                "success": True,
                "data": {"models": [{"provider": "fake", "id": "fake-1"}]},
            }
        )
        continue
    if command == "prompt":
        emit({"id": request_id, "type": "response", "command": command, "success": True})
        message = request.get("message", "")
        if request.get("content") is not None:
            blocks = request["content"]
            if any(block.get("type") != "image" for block in blocks):
                emit({"id": request_id, "type": "response", "command": command, "success": False, "error": "fixture only accepts image content"})
                emit({"type": "prompt_completed", "request_id": request_id, "status": "failed", "error": "fixture only accepts image content"})
                continue
            emit({"type": "text_delta", "text": "image received", "turn_id": "turn-img"})
            emit({"type": "turn_done", "turn_id": "turn-img"})
            emit({"type": "prompt_completed", "request_id": request_id, "status": "completed"})
            continue
        if message == "wait":
            waiting_prompt = request_id
            continue
        if message == "permission":
            permission_prompt = request_id
            emit(
                {
                    "type": "permission_request",
                    "permission": {
                        "request": {
                            "id": "perm-handler-1",
                            "tool": "bash",
                            "args": {"command": "echo ok"},
                            "paths": [],
                            "risk": "exec",
                            "reason": "fixture",
                        }
                    },
                }
            )
            continue
        if message == "ask":
            asking_prompt = request_id
            emit(
                {
                    "type": "user_input_request",
                    "user_input": {
                        "id": "ask-1",
                        "questions": [{"id": "choice", "header": "Choice", "question": "Choose?"}],
                    },
                }
            )
            continue
        emit({"type": "text_delta", "text": "fixture text", "turn_id": "turn-1"})
        emit({"type": "turn_done", "turn_id": "turn-1"})
        if message == "fail":
            emit({"id": request_id, "type": "response", "command": "prompt", "success": False, "error": "fixture failure"})
            emit({"type": "prompt_completed", "request_id": request_id, "status": "failed", "error": "fixture failure"})
        else:
            emit({"type": "prompt_completed", "request_id": request_id, "status": "completed"})
        continue
    if command == "compact":
        emit({"id": request_id, "type": "response", "command": command, "success": True, "data": {"summarized_messages": 3, "retained_messages": 5, "summary": "fixture summary", "used_fallback": True, "automatic": False}})
        continue
    if command == "branches_list":
        emit({"id": request_id, "type": "response", "command": command, "success": True, "data": {"branches": [{"id": "branch-1", "name": "main", "parent_branch_id": "", "forked_from_id": "", "tip_id": "entry-1", "messages": 2, "preview": "fixture", "created_at": 1, "updated_at": 2, "active": True}]}})
        continue
    if command in ("branch_select", "branch_delete"):
        emit({"id": request_id, "type": "response", "command": command, "success": True})
        continue
    if command == "branch_rename":
        emit({"id": request_id, "type": "response", "command": command, "success": True, "data": {"id": "branch-1", "name": request.get("params", {}).get("name", ""), "parent_branch_id": "", "forked_from_id": "", "tip_id": "entry-1", "messages": 2, "preview": "fixture", "created_at": 1, "updated_at": 2, "active": True}})
        continue
    if command == "messages_list":
        emit({"id": request_id, "type": "response", "command": command, "success": True, "data": {"messages": [{"id": "msg-1", "parent_id": "", "role": "user", "content": [{"type": "text", "text": "hi"}], "ts": 1}]}})
        continue
    if command == "usage":
        emit({"id": request_id, "type": "response", "command": command, "success": True, "data": {"input": 100, "output": 50, "reasoning": 10, "cache_read": 5, "cache_write": 3, "total_tokens": 150, "requests": 2}})
        continue
    if command in ("pending_inputs", "pending_inputs_clear"):
        emit({"id": request_id, "type": "response", "command": command, "success": True, "data": {"items": [{"id": "q-1", "kind": "steer", "text": "focus", "order": 1}]}})
        continue
    if command == "diagnostics":
        emit({"id": request_id, "type": "response", "command": command, "success": True, "data": {"diagnostics": [{"path": "tui.theme", "message": "missing theme"}]}})
        continue
    if command in ("set_reasoning_summary", "set_text_verbosity"):
        emit({"id": request_id, "type": "response", "command": command, "success": True})
        continue
    if command == "mcp_servers":
        emit({"id": request_id, "type": "response", "command": command, "success": True, "data": {"servers": [{"id": "mcp-1", "transport": "stdio", "connected": True, "tool_count": 2}]}})
        continue
    if command == "skills":
        emit({"id": request_id, "type": "response", "command": command, "success": True, "data": {"skills": [{"name": "caveman", "location": "/skills/caveman", "scope": "builtin", "source": "catalog", "enabled": True, "description": "compressed mode"}], "diagnostics": [{"path": "/skills/broken", "level": "error", "message": "shadowed"}]}})
        continue
    if command in ("permission_reply", "permission_reject"):
        emit({"id": request_id, "type": "response", "command": command, "success": True})
        if permission_prompt is not None:
            emit({"type": "turn_done", "turn_id": "turn-permission"})
            emit({"type": "prompt_completed", "request_id": permission_prompt, "status": "completed"})
            permission_prompt = None
        continue
    if command in ("user_input_reply", "user_input_reject"):
        emit({"id": request_id, "type": "response", "command": command, "success": True})
        if asking_prompt is not None:
            emit({"type": "turn_done", "turn_id": "turn-ask"})
            emit({"type": "prompt_completed", "request_id": asking_prompt, "status": "completed"})
            asking_prompt = None
        continue
    if command == "abort":
        emit({"id": request_id, "type": "response", "command": command, "success": True})
        if waiting_prompt is not None:
            emit({"type": "aborted", "turn_id": "turn-wait"})
            emit({"type": "turn_done", "turn_id": "turn-wait"})
            emit({"type": "prompt_completed", "request_id": waiting_prompt, "status": "canceled"})
            waiting_prompt = None
        continue
    emit({"id": request_id, "type": "response", "command": command, "success": True})
