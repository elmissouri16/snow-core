"""Subprocess protocol-v2 conformance tests for snow-plugin (Python SDK)."""

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SDK_ROOT = Path(__file__).resolve().parents[1]
SRC = SDK_ROOT / "src"

PLUGIN = """
import sys
sys.path.insert(0, sys.argv[1])

from snow_plugin import Plugin, text_result, ToolError, error_result

plugin = Plugin(plugin_id="conformance-python", name="Conformance Python", version="1.0.0")


@plugin.tool(
    name="echo",
    description="Echo text",
    risk="read",
    parameters={
        "type": "object",
        "properties": {"text": {"type": "string"}},
        "required": ["text"],
        "additionalProperties": False,
    },
)
def echo(args, ctx):
    # The SDK runtime wraps calls with an async Task; this is a sync handler
    # and is supported. Common print diagnostics must go to stderr.
    print("handler diagnostic")
    return text_result(args["text"], details={"runtime": "python", "length": len(args["text"])})


@plugin.tool(
    name="fail",
    description="Expected failure",
    risk="read",
    parameters={"type": "object", "properties": {}, "additionalProperties": False},
)
def fail(args, ctx):
    raise ToolError("record not found")


@plugin.tool(
    name="boom",
    description="Unexpected failure",
    risk="read",
    parameters={"type": "object", "properties": {}, "additionalProperties": False},
)
def boom(args, ctx):
    raise RuntimeError("secret explosion")


plugin.run()
"""


class SnowPluginConformanceTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.script = self.root / "plugin.py"
        self.script.write_text(PLUGIN, encoding="utf-8")
        self.proc = subprocess.Popen(
            [sys.executable, "-u", str(self.script), str(SRC)],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        self.next_id = 1

    def tearDown(self):
        if self.proc.poll() is None:
            try:
                self.proc.stdin.write('{"jsonrpc":"2.0","id":"bye","method":"shutdown","params":{}}\\n')
                self.proc.stdin.flush()
            except (BrokenPipeError, OSError, ValueError):
                pass
        if self.proc.stdin is not None and not self.proc.stdin.closed:
            self.proc.stdin.close()
        try:
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5)
        for stream in (self.proc.stdout, self.proc.stderr):
            if stream is not None:
                stream.close()
        self._tmp.cleanup()

    def _send(self, payload):
        self.proc.stdin.write(json.dumps(payload) + "\n")
        self.proc.stdin.flush()

    def _read_frame(self, timeout=5.0):
        import select

        if hasattr(select, "poll"):
            poller = select.poll()
            poller.register(self.proc.stdout, select.POLLIN)
            if not poller.poll(int(timeout * 1000)):
                raise AssertionError("timed out waiting for stdout frame")
        line = self.proc.stdout.readline()
        if not line:
            raise AssertionError("subprocess closed stdout unexpectedly")
        return json.loads(line)

    def _request(self, method, params=None, rid=None):
        rid = rid or str(self.next_id)
        self.next_id += 1
        self._send({"jsonrpc": "2.0", "id": rid, "method": method, "params": params or {}})
        while True:
            frame = self._read_frame()
            if frame.get("method") in ("notifications/progress", "notifications/log"):
                continue
            if frame.get("id") == rid:
                return frame

    def test_protocol_v2_lifecycle(self):
        init = self._request("initialize", {"protocol_version": 2, "config": {}, "host_capabilities": []})
        self.assertEqual(init["result"]["manifest"]["id"], "conformance-python")
        self.assertEqual(init["result"]["manifest"]["protocol_version"], 2)

        tools = self._request("tools/list", {})
        names = {t["name"] for t in tools["result"]["tools"]}
        self.assertEqual(names, {"echo", "fail", "boom"})
        echo = next(t for t in tools["result"]["tools"] if t["name"] == "echo")
        self.assertEqual(echo["risk"], "read")

        import time; time.sleep(0.05)
        ok = self._request("tools/call", {"name": "echo", "call_id": "call-1", "arguments": {"text": "hi"}})
        self.assertEqual(ok["result"]["content"][0]["text"], "hi")
        self.assertEqual(ok["result"]["details"]["runtime"], "python")
        self.assertFalse(ok["result"]["is_error"])

        fail = self._request("tools/call", {"name": "fail", "call_id": "call-2", "arguments": {}})
        self.assertTrue(fail["result"]["is_error"])
        self.assertIn("record not found", fail["result"]["content"][0]["text"])

        boom = self._request("tools/call", {"name": "boom", "call_id": "call-3", "arguments": {}})
        self.assertEqual(boom["error"]["code"], -32000)
        self.assertEqual(boom["error"]["message"], "tool failed: RuntimeError")
        self.assertNotIn("secret explosion", json.dumps(boom))

        shutdown = self._request("shutdown", {}, rid="bye")
        self.assertEqual(shutdown["result"], {})


if __name__ == "__main__":
    unittest.main()
