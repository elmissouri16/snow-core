#!/usr/bin/env python3
"""Persistent Snow RPC example built on the checked-in asynchronous Python SDK."""

import argparse
import asyncio
import os
import shutil
import sys
from pathlib import Path

# Keep the repository example directly runnable before the package is published.
REPOSITORY_ROOT = Path(__file__).resolve().parents[3]
SDK_SOURCE = REPOSITORY_ROOT / "sdk" / "python" / "src"
if SDK_SOURCE.is_dir():
    sys.path.insert(0, str(SDK_SOURCE))

from snow_sdk import SnowClient, SnowError, SnowOptions  # noqa: E402


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


async def answer_user_input(request):
    answers = []
    for question in request["questions"]:
        options = question.get("options", [])
        answer = options[0]["label"] if options else "No additional constraints"
        answers.append({"id": question["id"], "answer": answer})
    return {"answers": answers}


async def run(args: argparse.Namespace) -> int:
    options = SnowOptions(
        command=(resolve_snow(args.snow),),
        provider=args.provider,
        model=args.model,
        base_url=args.base_url,
        session_path=args.session,
        request_timeout=args.timeout,
    )
    async with await SnowClient.start(options, user_input_handler=answer_user_input) as snow:
        info = await snow.session_info()
        if info["data"].get("provider") != args.provider:
            raise RuntimeError("session_info returned an unexpected provider")

        events = snow.events()
        prompt = asyncio.create_task(snow.prompt(args.prompt, timeout=args.timeout))
        saw_text = False
        event_error = None
        stream_error = None
        try:
            async for event in events:
                if event.type == "text_delta" and "agent" not in event.raw:
                    saw_text = True
                    print(event.get("text", ""), end="", flush=True)
                elif event.type == "error" and "agent" not in event.raw:
                    event_error = event.get("message", "agent turn failed")
                if event.type == "turn_done" and "agent" not in event.raw:
                    events.close()
        except Exception as exc:
            stream_error = exc
        finally:
            events.close()

        result = await prompt
        if stream_error is not None:
            raise stream_error
        if event_error is not None:
            raise RuntimeError(event_error)
        if result.status != "completed":
            raise RuntimeError(f"unexpected prompt status {result.status}")
        if saw_text:
            print()
        else:
            print("agent turn completed (the fake provider emits no text)")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snow", default="snow", help="snow binary or path (default: snow on PATH)")
    parser.add_argument(
        "--provider",
        default="fake",
        choices=("fake", "opencode-go", "openai-compatible", "chatgpt"),
    )
    parser.add_argument("--model", default="", help="model id (default: provider default)")
    parser.add_argument("--base-url", default="", help="API root for openai-compatible")
    parser.add_argument("--session", default="", help="SQLite session path (default: ephemeral)")
    parser.add_argument("--prompt", default="Summarize this repository.")
    parser.add_argument("--timeout", type=float, default=120, help="seconds to wait for prompt completion")
    args = parser.parse_args()
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    try:
        return asyncio.run(run(args))
    except (OSError, RuntimeError, SnowError, asyncio.TimeoutError) as exc:
        print(f"snow RPC example: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
