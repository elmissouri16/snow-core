import asyncio
import json
import os
import subprocess
import sys
import tempfile
import textwrap
import unittest

SRC = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "src"))
if SRC not in sys.path:
    sys.path.insert(0, SRC)

from snow_plugin import Plugin, text_result, error_result


class RuntimeUnitTests(unittest.TestCase):
    def test_manifest_and_tools_list(self):
        plugin = Plugin("ping-python", "Ping", "1.0.0")

        @plugin.tool(name="ping", description="Ping pong", risk="read",
                     parameters={"type": "object"})
        async def ping(arguments, context):
            return {"text": "pong"}

        self.assertEqual(plugin.manifest()["id"], "ping-python")
        self.assertEqual(plugin.manifest()["protocol_version"], 2)
        self.assertEqual(plugin.get_tool("ping").descriptor()["risk"], "read")
        self.assertEqual(plugin.tools_list()["tools"][0]["name"], "ping")

    def test_error_result(self):
        err = error_result("boom")
        self.assertEqual(err["is_error"], True)
        self.assertEqual(err["content"][0]["text"], "boom")

    def test_oversized_result_becomes_bounded_error(self):
        from snow_plugin.runtime import _bounded_result

        bounded = _bounded_result(text_result("x" * (300 * 1024)))
        self.assertTrue(bounded["is_error"])
        self.assertIn("output limit", bounded["content"][0]["text"])

    def test_invalid_handler_results_become_structured_errors(self):
        from snow_plugin.runtime import _validate_result

        for value in (None, "", {}, {"content": []}, {"text": "ok", "details": []}):
            with self.subTest(value=value):
                normalized = _validate_result(value)
                self.assertTrue(normalized["is_error"])
                self.assertTrue(normalized["content"][0]["text"])


class SubprocessConformanceTests(unittest.TestCase):
    """Drive a real plugin subprocess over JSON-RPC JSONL."""

    SCRIPT = textwrap.dedent(
        '''
        import asyncio, json, sys
        sys.path.insert(0, {src!r})
        from snow_plugin import Plugin, ToolContext, text_result, error_result

        plugin = Plugin("demo-python", "Demo", "1.0.0")

        @plugin.tool(
            name="echo",
            description="Echo text with optional delay",
            risk="read",
            parameters={{
                "type": "object",
                "properties": {{
                    "text": {{"type": "string"}},
                    "delay_ms": {{"type": "integer", "minimum": 0, "maximum": 5000}},
                }},
                "required": ["text"],
                "additionalProperties": False,
            }},
            discovery={{"mode": "deferred"}},
        )
        async def echo(arguments, context):
            await context.progress("Preparing echo")
            delay = max(0, min(5000, int(arguments.get("delay_ms", 0))))
            if delay:
                await asyncio.sleep(delay / 1000.0)
            return text_result(str(arguments["text"]), details={{'runtime': 'python'}})

        @plugin.tool(name="boom", description="Raise an expected error", risk="read",
                     parameters={{"type": "object"}})
        async def boom(arguments, context):
            raise error_result("boom-expected") if False else __import__("snow_plugin").ToolError("boom-expected")

        @plugin.tool(name="crash", description="Raise an unexpected error", risk="read",
                     parameters={{"type": "object"}})
        async def crash(arguments, context):
            raise RuntimeError("token=super-secret")

        if __name__ == "__main__":
            plugin.run()
        '''
    )

    @classmethod
    def setUpClass(cls):
        with tempfile.NamedTemporaryFile("w", suffix=".py", delete=False) as handle:
            handle.write(SubprocessConformanceTests.SCRIPT.format(src=SRC))
            cls.script_path = handle.name

    @classmethod
    def tearDownClass(cls):
        os.unlink(cls.script_path)

    def _start(self):
        process = subprocess.Popen(
            [sys.executable, "-u", self.script_path],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        assert process.stdin is not None and process.stdout is not None
        return process

    def _send(self, process, frame):
        assert process.stdin is not None
        process.stdin.write(json.dumps(frame) + "\n")
        process.stdin.flush()

    def _read_until(self, process, until_id):
        assert process.stdout is not None
        frames = []
        while True:
            line = process.stdout.readline()
            if not line:
                break
            frame = json.loads(line)
            frames.append(frame)
            if frame.get("id") == until_id:
                break
        return frames

    def _finish(self, process):
        assert process.stdin is not None
        process.stdin.close()
        stderr = process.stderr.read() if process.stderr else ""
        process.wait(timeout=15)
        if process.stdout is not None:
            process.stdout.close()
        if process.stderr is not None:
            process.stderr.close()
        return stderr

    def test_initialize_tools_list_and_echo(self):
        process = self._start()
        self._send(process, {"jsonrpc": "2.0", "id": "1", "method": "initialize",
                             "params": {"protocol_version": 2, "cwd": "/tmp", "config": {}}})
        init = self._read_until(process, "1")[0]
        manifest = init["result"]["manifest"]
        self.assertEqual(manifest["id"], "demo-python")
        self.assertEqual(manifest["protocol_version"], 2)

        self._send(process, {"jsonrpc": "2.0", "id": "2", "method": "tools/list", "params": {}})
        tools = self._read_until(process, "2")[0]["result"]["tools"]
        self.assertEqual(tools[0]["name"], "echo")
        self.assertEqual(tools[0]["risk"], "read")

        self._send(process, {"jsonrpc": "2.0", "id": "3", "method": "tools/call",
                             "params": {"name": "echo", "call_id": "call-1",
                                        "arguments": {"text": "hello"}, "timeout_ms": 0}})
        frames = self._read_until(process, "3")
        call = next(frame for frame in frames if frame.get("id") == "3")
        self.assertEqual(call["result"]["content"][0]["text"], "hello")
        self.assertEqual(call["result"]["details"]["runtime"], "python")
        self.assertIn("notifications/progress", {frame.get("method") for frame in frames})

        self._send(process, {"jsonrpc": "2.0", "id": "shutdown", "method": "shutdown", "params": {}})
        shut = self._read_until(process, "shutdown")[0]
        self.assertEqual(shut["result"], {})
        stderr = self._finish(process)
        self.assertEqual(stderr.strip(), "")

    def test_expected_tool_error_is_structured_result(self):
        process = self._start()
        self._send(process, {"jsonrpc": "2.0", "id": "1", "method": "initialize",
                             "params": {"protocol_version": 2, "config": {}}})
        self._read_until(process, "1")
        self._send(process, {"jsonrpc": "2.0", "id": "2", "method": "tools/call",
                             "params": {"name": "boom", "call_id": "c1", "arguments": {}}})
        frames = self._read_until(process, "2")
        call = next(frame for frame in frames if frame.get("id") == "2")
        # Structured tool error: successful JSON-RPC request with is_error result.
        self.assertIn("result", call)
        self.assertEqual(call["result"]["is_error"], True)
        self.assertEqual(call["result"]["content"][0]["text"], "boom-expected")
        self._send(process, {"jsonrpc": "2.0", "id": "shutdown", "method": "shutdown", "params": {}})
        self._read_until(process, "shutdown")
        self._finish(process)

    def test_unexpected_tool_error_does_not_expose_exception_message(self):
        process = self._start()
        self._send(process, {"jsonrpc": "2.0", "id": "1", "method": "initialize",
                             "params": {"protocol_version": 2, "config": {}}})
        self._read_until(process, "1")
        self._send(process, {"jsonrpc": "2.0", "id": "2", "method": "tools/call",
                             "params": {"name": "crash", "call_id": "c1", "arguments": {}}})
        call = self._read_until(process, "2")[0]
        self.assertEqual(call["error"]["code"], -32000)
        self.assertEqual(call["error"]["message"], "tool failed: RuntimeError")
        self.assertNotIn("super-secret", json.dumps(call))
        self._send(process, {"jsonrpc": "2.0", "id": "shutdown", "method": "shutdown", "params": {}})
        self._read_until(process, "shutdown")
        self._finish(process)

    def test_unknown_tool_and_unknown_method_errors(self):
        process = self._start()
        self._send(process, {"jsonrpc": "2.0", "id": "1", "method": "initialize",
                             "params": {"protocol_version": 2, "config": {}}})
        self._read_until(process, "1")
        self._send(process, {"jsonrpc": "2.0", "id": "2", "method": "tools/call",
                             "params": {"name": "nope", "call_id": "c1", "arguments": {}}})
        err = self._read_until(process, "2")[0]
        self.assertEqual(err["error"]["code"], -32602)
        self._send(process, {"jsonrpc": "2.0", "id": "3", "method": "bogus", "params": {}})
        err2 = self._read_until(process, "3")[0]
        self.assertEqual(err2["error"]["code"], -32601)
        self._send(process, {"jsonrpc": "2.0", "id": "shutdown", "method": "shutdown", "params": {}})
        self._read_until(process, "shutdown")
        self._finish(process)

    def test_timeout(self):
        plugin = Plugin("slow-python", "Slow", "1.0.0")

        @plugin.tool(name="slow", description="Sleepy tool", risk="read",
                     parameters={"type": "object"})
        async def slow(arguments, context):
            await asyncio.sleep(5)
            return text_result("too late")

        async def drive():
            return await self._drive_local(plugin)

        frames = asyncio.run(drive())
        timeout_frame = next(
            frame for frame in frames if frame.get("error", {}).get("code") == -32000
        )
        self.assertIn("timed out", timeout_frame["error"]["message"])

    def test_cancellation_by_request_and_call_id(self):
        plugin = Plugin("cancel-python", "Cancel", "1.0.0")

        async def hang(arguments, context):
            try:
                await asyncio.sleep(5)
                return text_result("too late")
            except asyncio.CancelledError:
                raise

        plugin.tool(name="hang", description="Hang", risk="read",
                    parameters={"type": "object"})(hang)

        async def drive(self, plugin):
            from snow_plugin.runtime import _Runtime

            class Writer:
                def __init__(self):
                    self.frames = []

                async def write(self, payload):
                    self.frames.append(json.loads(json.dumps(payload)))

            runtime = _Runtime(plugin)
            runtime.writer = Writer()
            await runtime._handle_request(
                {"jsonrpc": "2.0", "id": "init", "method": "initialize",
                 "params": {"protocol_version": 2, "config": {}}}
            )
            await runtime._handle_request(
                {"jsonrpc": "2.0", "id": "call", "method": "tools/call",
                 "params": {"name": "hang", "call_id": "c1", "arguments": {}}}
            )
            # Send a cancellation notification by request id and by call id.
            await runtime._handle_request(
                {"jsonrpc": "2.0", "method": "notifications/cancelled",
                 "params": {"call_id": "c1", "request_id": "call", "reason": "ctx cancel"}}
            )
            await asyncio.gather(*runtime.active_tasks, return_exceptions=True)
            return runtime.writer.frames

        import inspect

        bound = drive.__code__
        frames = asyncio.run(drive(self, plugin))
        # No response was written for the cancelled request; active work drained.
        self.assertNotIn("call", {frame.get("id") for frame in frames})

    async def _drive_local(self, plugin):
        from snow_plugin.runtime import _Runtime

        class Writer:
            def __init__(self):
                self.frames = []

            async def write(self, payload):
                self.frames.append(json.loads(json.dumps(payload)))

        runtime = _Runtime(plugin)
        runtime.writer = Writer()
        await runtime._handle_request(
            {"jsonrpc": "2.0", "id": "init", "method": "initialize",
             "params": {"protocol_version": 2, "config": {}}}
        )
        await runtime._handle_request(
            {"jsonrpc": "2.0", "id": "call", "method": "tools/call",
             "params": {"name": "slow", "call_id": "c1", "arguments": {},
                        "timeout_ms": 50}}
        )
        if runtime.active_tasks:
            await asyncio.gather(*runtime.active_tasks, return_exceptions=True)
        return runtime.writer.frames


if __name__ == "__main__":
    unittest.main()
