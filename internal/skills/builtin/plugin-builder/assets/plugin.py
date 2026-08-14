#!/usr/bin/env python3
"""Dependency-free Snow protocol-v2 template. Replace PLUGIN_ID and echo."""

import json
import sys

MANIFEST = {
    "id": "PLUGIN_ID",
    "name": "PLUGIN_ID generated plugin",
    "version": "0.1.0",
    "protocol_version": 2,
}

TOOLS = [
    {
        "name": "echo",
        "description": "Replace this example with the reusable capability.",
        "parameters": {
            "type": "object",
            "properties": {"text": {"type": "string"}},
            "required": ["text"],
            "additionalProperties": False,
        },
        "risk": "read",
    }
]


def send(frame):
    sys.stdout.write(json.dumps(frame, separators=(",", ":")) + "\n")
    sys.stdout.flush()


def result(request_id, value):
    send({"jsonrpc": "2.0", "id": request_id, "result": value})


def error(request_id, code, message):
    send({"jsonrpc": "2.0", "id": request_id, "error": {"code": code, "message": str(message)[:4096]}})


def call_tool(request):
    params = request.get("params") or {}
    if params.get("name") != "echo":
        error(request.get("id"), -32602, "unknown tool")
        return
    arguments = params.get("arguments") or {}
    result(
        request["id"],
        {
            "content": [{"type": "text", "text": str(arguments.get("text", ""))}],
            "details": {"template": True},
            "is_error": False,
        },
    )


def main():
    for line in sys.stdin:
        if not line.strip():
            continue
        try:
            request = json.loads(line)
        except (UnicodeDecodeError, json.JSONDecodeError):
            error(None, -32700, "parse error")
            continue
        if "id" not in request:
            # Add notifications/cancelled handling here for long-running calls.
            continue
        method = request.get("method")
        if method == "initialize":
            result(request["id"], {"manifest": MANIFEST, "supported_events": []})
        elif method == "tools/list":
            result(request["id"], {"tools": TOOLS})
        elif method == "tools/call":
            call_tool(request)
        elif method == "shutdown":
            result(request["id"], {})
            return
        else:
            error(request["id"], -32601, "method not found")


if __name__ == "__main__":
    main()
