#!/usr/bin/python3
"""Dependency-free Snow protocol-v2 example for Python 3.9+.

stdout is reserved for JSON-RPC frames; write diagnostics to stderr.
"""

import asyncio
import json
import sys
from typing import Any, Dict

MANIFEST = {
    "id": "example-python",
    "name": "Snow Python example",
    "version": "1.0.0",
    "protocol_version": 2,
}

TOOLS = [
    {
        "name": "echo",
        "description": "Echo text after an optional delay",
        "parameters": {
            "type": "object",
            "properties": {
                "text": {"type": "string"},
                "delay_ms": {"type": "integer", "minimum": 0, "maximum": 5000},
            },
            "required": ["text"],
            "additionalProperties": False,
        },
        "risk": "read",
    }
]

write_lock: asyncio.Lock
active_requests: Dict[str, asyncio.Task[Any]] = {}
active_calls: Dict[str, asyncio.Task[Any]] = {}
closing = False


async def write_frame(frame: Dict[str, Any]) -> None:
    encoded = json.dumps(frame, separators=(",", ":"), ensure_ascii=False).encode("utf-8") + b"\n"
    async with write_lock:
        sys.stdout.buffer.write(encoded)
        sys.stdout.buffer.flush()


async def respond(request_id: str, result: Any) -> None:
    await write_frame({"jsonrpc": "2.0", "id": request_id, "result": result})


async def respond_error(request_id: Any, code: int, message: str) -> None:
    await write_frame(
        {"jsonrpc": "2.0", "id": request_id, "error": {"code": code, "message": message}}
    )


async def progress(call_id: str, message: str, done: bool = False, is_error: bool = False) -> None:
    if not call_id:
        raise ValueError("progress requires a non-empty call_id")
    await write_frame(
        {
            "jsonrpc": "2.0",
            "method": "notifications/progress",
            "params": {
                "call_id": call_id,
                "message": message,
                "done": done,
                "is_error": is_error,
            },
        }
    )


async def execute_echo(params: Dict[str, Any]) -> Dict[str, Any]:
    arguments = params.get("arguments") or {}
    await progress(str(params.get("call_id", "")), "Preparing echo")
    delay_ms = max(0, min(5000, int(arguments.get("delay_ms", 0))))
    if delay_ms:
        await asyncio.sleep(delay_ms / 1000)
    text = str(arguments.get("text", ""))
    return {
        "content": [{"type": "text", "text": text}],
        "details": {"runtime": "python", "length": len(text)},
        "is_error": False,
    }


async def call_tool(request: Dict[str, Any]) -> None:
    params = request.get("params") or {}
    if params.get("name") != "echo":
        await respond_error(request["id"], -32602, f"unknown tool {params.get('name')}")
        return
    call_id = str(params.get("call_id") or "")
    if not call_id:
        await respond_error(request["id"], -32602, "tools/call requires a non-empty call_id")
        return
    timeout_ms = int(params.get("timeout_ms") or 0)
    try:
        operation = execute_echo(params)
        result = (
            await asyncio.wait_for(operation, timeout_ms / 1000)
            if timeout_ms > 0
            else await operation
        )
        await respond(request["id"], result)
    except asyncio.CancelledError:
        # Snow has stopped waiting and will ignore a late response.
        raise
    except asyncio.TimeoutError:
        await respond_error(request["id"], -32000, "tool timed out")
    except Exception as error:  # bounded error text is returned; traceback stays private
        await respond_error(request["id"], -32000, str(error)[:4096])


async def run_request(request: Dict[str, Any]) -> None:
    request_id = str(request["id"])
    call_id = str((request.get("params") or {}).get("call_id") or "")
    task = asyncio.current_task()
    if task is not None:
        active_requests[request_id] = task
        if call_id:
            active_calls[call_id] = task
    try:
        if request.get("method") == "tools/call":
            await call_tool(request)
        else:
            await respond_error(request_id, -32601, f"unknown method {request.get('method')}")
    finally:
        active_requests.pop(request_id, None)
        if call_id:
            active_calls.pop(call_id, None)


async def serve() -> None:
    global write_lock, closing
    write_lock = asyncio.Lock()
    tasks = set()
    while not closing:
        line = await asyncio.to_thread(sys.stdin.buffer.readline)
        if not line:
            break
        if not line.strip():
            continue
        try:
            message = json.loads(line)
        except (UnicodeDecodeError, json.JSONDecodeError):
            await respond_error(None, -32700, "parse error")
            continue

        method = message.get("method")
        if method == "notifications/cancelled":
            params = message.get("params") or {}
            request_id = str(params.get("request_id") or "")
            call_id = str(params.get("call_id") or "")
            task = active_requests.get(request_id) or active_calls.get(call_id)
            if task is not None:
                task.cancel()
            continue
        if method == "notifications/event":
            continue
        if "id" not in message:
            continue
        if method == "initialize":
            await respond(
                message["id"],
                {"manifest": MANIFEST, "supported_events": ["tool_end"]},
            )
            continue
        if method == "tools/list":
            await respond(message["id"], {"tools": TOOLS})
            continue
        if method == "shutdown":
            closing = True
            for task in set(active_requests.values()):
                task.cancel()
            await respond(message["id"], {})
            break

        task = asyncio.create_task(run_request(message))
        tasks.add(task)
        task.add_done_callback(tasks.discard)

    for task in set(active_requests.values()):
        task.cancel()
    if tasks:
        await asyncio.gather(*tasks, return_exceptions=True)


if __name__ == "__main__":
    asyncio.run(serve())
