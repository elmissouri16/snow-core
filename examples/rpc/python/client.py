#!/usr/bin/env python3
"""Minimal persistent Snow JSONL RPC client using only Python's standard library."""

import argparse
import json
import os
import queue
import shutil
import subprocess
import sys
import threading
import time
from pathlib import Path


def resolve_snow(command: str) -> str:
    if os.sep in command or (os.altsep and os.altsep in command):
        path = Path(command).expanduser().resolve()
        if not path.is_file():
            raise FileNotFoundError(f"snow binary not found: {path}")
        return str(path)
    resolved = shutil.which(command)
    if resolved is None:
        raise FileNotFoundError(f"snow binary not found on PATH: {command}")
    return resolved


def send(proc: subprocess.Popen, message: dict) -> None:
    if proc.stdin is None:
        raise RuntimeError("RPC stdin is closed")
    proc.stdin.write(json.dumps(message, separators=(",", ":")) + "\n")
    proc.stdin.flush()


def read_lines(stream, output: queue.Queue) -> None:
    try:
        for line in stream:
            output.put(line)
    finally:
        output.put(None)


def run(args: argparse.Namespace) -> int:
    snow = resolve_snow(args.snow)
    command = [
        snow,
        "--mode",
        "rpc",
        "--provider",
        args.provider,
        "--permission",
        "deny",
        "--no-session",
        "--no-plugins",
        "--no-mcp",
        "--no-skills",
        "--no-subagents",
    ]
    proc = subprocess.Popen(
        command,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=None,
        text=True,
        bufsize=1,
    )
    if proc.stdout is None:
        raise RuntimeError("failed to open RPC stdout")

    lines: queue.Queue = queue.Queue()
    reader = threading.Thread(target=read_lines, args=(proc.stdout, lines), daemon=True)
    reader.start()

    deadline = time.monotonic() + args.timeout
    saw_prompt_ack = False
    saw_text = False
    saw_turn_done = False

    try:
        send(proc, {"id": "prompt-1", "type": "prompt", "message": args.prompt})
        while not saw_turn_done:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("timed out waiting for turn_done")
            try:
                line = lines.get(timeout=remaining)
            except queue.Empty as exc:
                raise TimeoutError("timed out waiting for RPC output") from exc
            if line is None:
                raise RuntimeError("RPC process closed stdout before turn_done")

            message = json.loads(line)
            kind = message.get("type")
            if kind == "response":
                if not message.get("success"):
                    raise RuntimeError(message.get("error", "RPC command failed"))
                if message.get("id") == "prompt-1":
                    saw_prompt_ack = True
                continue

            if kind == "text_delta" and "agent" not in message:
                saw_text = True
                print(message.get("text", ""), end="", flush=True)
                continue

            if kind == "error" and "agent" not in message:
                raise RuntimeError(message.get("message", "agent turn failed"))

            if kind == "user_input_request":
                request = message["user_input"]
                answers = []
                for question in request["questions"]:
                    options = question.get("options", [])
                    answer = options[0]["label"] if options else "No additional constraints"
                    answers.append({"id": question["id"], "answer": answer})
                send(
                    proc,
                    {
                        "id": f"input-{request['id']}",
                        "type": "user_input_reply",
                        "params": {"request_id": request["id"], "answers": answers},
                    },
                )
                continue

            if kind == "turn_done" and "agent" not in message:
                saw_turn_done = True

        if not saw_prompt_ack:
            raise RuntimeError("turn completed before the prompt acknowledgement was observed")
        if saw_text:
            print()
        else:
            print("prompt completed (the fake provider emits no text)")
    finally:
        if proc.stdin is not None and not proc.stdin.closed:
            try:
                proc.stdin.close()
            except BrokenPipeError:
                pass
        try:
            return_code = proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.terminate()
            try:
                return_code = proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                return_code = proc.wait(timeout=5)
        reader.join(timeout=1)

    if return_code != 0:
        raise RuntimeError(f"snow RPC process exited with status {return_code}")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snow", default="snow", help="snow binary or path (default: snow on PATH)")
    parser.add_argument("--provider", default="fake", choices=("fake", "opencode-go", "chatgpt"))
    parser.add_argument("--prompt", default="Summarize this repository.")
    parser.add_argument("--timeout", type=float, default=120, help="seconds to wait for turn_done")
    args = parser.parse_args()
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    try:
        return run(args)
    except (OSError, RuntimeError, TimeoutError, json.JSONDecodeError) as exc:
        print(f"snow RPC example: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
