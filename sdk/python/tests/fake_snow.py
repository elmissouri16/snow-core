#!/usr/bin/env python3
"""Deterministic JSONL RPC fixture for the Python SDK tests."""

import json
import os
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
        "capabilities": [1] if scenario == "bad_caps_type" else ["models_list", "permission_input", "prompt_completion", "session_info", "session_messages", "subagent_models", "user_input"],
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
waiting_prompt = None
for line in sys.stdin:
    if not line.strip():
        continue
    request = json.loads(line)
    request_id = request.get("id", "")
    command = request["type"]
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
    if command == "session_messages":
        emit(
            {
                "id": request_id,
                "type": "response",
                "command": command,
                "success": True,
                "data": {"messages": [{"id": "u-1", "role": "user", "content": [], "ts": 1}]},
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
        if message == "wait":
            waiting_prompt = request_id
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
