import asyncio
import os
import sys
import tempfile
import unittest
from pathlib import Path

from snow_sdk import (
    SnowClient,
    SnowClosedError,
    SnowCommandError,
    SnowOptions,
    SnowProcessError,
    SnowPromptError,
    SnowProtocolError,
    SnowSubscriptionOverflowError,
    SnowTimeoutError,
    SnowVersionError,
)


FIXTURE = Path(__file__).with_name("fake_snow.py")


def fixture_options(**changes):
    values = {
        "command": (sys.executable, str(FIXTURE)),
        "startup_timeout": 3.0,
        "close_timeout": 1.0,
        "request_timeout": 3.0,
    }
    values.update(changes)
    return SnowOptions(**values)


class SnowClientTests(unittest.IsolatedAsyncioTestCase):
    async def test_parity_wrapper_methods(self):
        async with await SnowClient.start(fixture_options()) as snow:
            compact = await snow.compact()
            self.assertEqual(compact["command"], "compact")
            self.assertEqual(compact["data"]["summarized_messages"], 3)

            branches = await snow.branches_list()
            self.assertEqual(branches["data"]["branches"][0]["id"], "branch-1")
            self.assertEqual((await snow.branch_select("branch-1"))["command"], "branch_select")
            renamed = await snow.branch_rename("branch-1", "feature")
            self.assertEqual(renamed["data"]["name"], "feature")
            self.assertEqual((await snow.branch_delete("branch-1"))["command"], "branch_delete")

            messages = await snow.messages_list()
            self.assertEqual(messages["data"]["messages"][0]["role"], "user")
            usage = await snow.usage()
            self.assertEqual(usage["data"]["total_tokens"], 150)

            pending = await snow.pending_inputs()
            self.assertEqual(pending["data"]["items"][0]["kind"], "steer")
            cleared = await snow.pending_inputs_clear()
            self.assertEqual(cleared["data"]["items"][0]["text"], "focus")

            diagnostics = await snow.configuration_diagnostics()
            self.assertEqual(diagnostics["command"], "diagnostics")
            self.assertEqual(diagnostics["data"]["diagnostics"][0]["path"], "tui.theme")
            self.assertEqual((await snow.set_reasoning_summary("concise"))["command"], "set_reasoning_summary")
            self.assertEqual((await snow.set_text_verbosity("high"))["command"], "set_text_verbosity")
            self.assertEqual((await snow.subagent_close("/root/reviewer"))["command"], "subagent_close")
            self.assertEqual((await snow.subagent_resume("/root/reviewer"))["command"], "subagent_resume")

            mcp = await snow.mcp_servers()
            self.assertEqual(mcp["command"], "mcp_servers")
            self.assertEqual(mcp["data"]["servers"][0]["id"], "mcp-1")
            skills = await snow.skills()
            self.assertEqual(skills["data"]["skills"][0]["name"], "caveman")
            self.assertEqual(skills["data"]["diagnostics"][0]["level"], "error")

            reply = await snow.reply_permission("perm-1", "allow")
            self.assertEqual(reply["command"], "permission_reply")
            reject = await snow.reject_permission("perm-1")
            self.assertEqual(reject["command"], "permission_reject")

    async def test_prompt_content_wire_and_result(self):
        async with await SnowClient.start(fixture_options()) as snow:
            result = await snow.prompt(content=[{"type": "image", "mime_type": "image/png", "data": "AAAA"}], mode="plan")
            self.assertEqual(result.status, "completed")
            self.assertIn("multimodal_prompts", snow.ready.capabilities)

    async def test_prompt_rejects_content_by_fixture(self):
        async with await SnowClient.start(fixture_options()) as snow:
            with self.assertRaises(SnowPromptError):
                await snow.prompt(content=[{"type": "text", "text": "hi"}])

    async def test_parity_response_dtos(self):
        from snow_sdk import (
            BranchesList,
            CompactionResult,
            DiagnosticsList,
            MCPServers,
            MessagesList,
            PendingInputs,
            Skills,
            UsageSnapshot,
        )

        async with await SnowClient.start(fixture_options()) as snow:
            result = CompactionResult.from_response(await snow.compact())
            self.assertEqual(result.summarized_messages, 3)
            self.assertTrue(result.used_fallback)

            branches = BranchesList.from_response(await snow.branches_list())
            self.assertEqual(branches.branches[0].name, "main")
            self.assertTrue(branches.branches[0].active)

            messages = MessagesList.from_response(await snow.messages_list())
            self.assertEqual(messages.messages[0]["id"], "msg-1")

            usage = UsageSnapshot.from_response(await snow.usage())
            self.assertEqual(usage.total_tokens, 150)

            pending = PendingInputs.from_response(await snow.pending_inputs())
            self.assertEqual(pending.items[0].kind, "steer")
            self.assertEqual(pending.items[0].order, 1)

            diagnostics = DiagnosticsList.from_response(await snow.configuration_diagnostics())
            self.assertEqual(diagnostics.diagnostics[0].path, "tui.theme")

            mcp = MCPServers.from_response(await snow.mcp_servers())
            self.assertEqual(mcp.servers[0].id, "mcp-1")
            self.assertTrue(mcp.servers[0].connected)
            skills = Skills.from_response(await snow.skills())
            self.assertEqual(skills.skills[0].name, "caveman")
            self.assertEqual(skills.diagnostics[0].level, "error")

    async def test_handshake_discovery_and_prompt_events(self):
        async with await SnowClient.start(fixture_options()) as snow:
            self.assertEqual(snow.ready.protocol_version, "1")
            self.assertIn("prompt_completion", snow.ready.capabilities)
            info = await snow.session_info()
            self.assertEqual(info["data"]["model"], "fake-1")
            self.assertEqual(snow.diagnostics[0]["kind"], "unknown_response")
            models = await snow.models()
            self.assertEqual(models["data"]["models"][0]["id"], "fake-1")

            events = snow.events()
            prompt = asyncio.create_task(snow.prompt("hello"))
            seen = []
            async for event in events:
                seen.append(event.type)
                if event.type == "turn_done":
                    events.close()
            result = await prompt
            self.assertEqual(result.status, "completed")
            self.assertIn("text_delta", seen)
            self.assertIn("turn_done", seen)

    async def test_event_subscriptions_receive_isolated_payloads(self):
        async with await SnowClient.start(fixture_options()) as snow:
            first = snow.events()
            second = snow.events()
            prompt = asyncio.create_task(snow.prompt("hello"))
            left = await first.__anext__()
            right = await second.__anext__()
            self.assertEqual(left.type, "text_delta")
            self.assertEqual(right.type, "text_delta")
            left.raw["text"] = "mutated"
            self.assertEqual(right.raw["text"], "fixture text")
            first.close()
            second.close()
            await prompt

    async def test_out_of_order_responses_are_correlated_by_id(self):
        async with await SnowClient.start(fixture_options()) as snow:
            held = asyncio.create_task(snow.request("hold"))
            await asyncio.sleep(0)
            released = await snow.request("release")
            self.assertEqual(released["command"], "release")
            self.assertEqual((await held)["command"], "hold")

    async def test_terminal_prompt_failure_is_not_turn_done_success(self):
        async with await SnowClient.start(fixture_options()) as snow:
            with self.assertRaises(SnowPromptError):
                await snow.prompt("fail")

    async def test_task_cancellation_aborts_and_consumes_terminal_status(self):
        async with await SnowClient.start(fixture_options()) as snow:
            prompt = asyncio.create_task(snow.prompt("wait"))
            await asyncio.sleep(0.05)
            prompt.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await prompt
            self.assertEqual((await snow.session_info())["data"]["model"], "fake-1")

    async def test_user_input_handler_replies_after_event_publication(self):
        handled = asyncio.Event()

        async def handler(request):
            self.assertEqual(request["id"], "ask-1")
            handled.set()
            return {"answers": [{"id": "choice", "answer": "A"}]}

        async with await SnowClient.start(fixture_options(), user_input_handler=handler) as snow:
            events = snow.events()
            prompt = asyncio.create_task(snow.prompt("ask"))
            event = await events.__anext__()
            while event.type != "user_input_request":
                event = await events.__anext__()
            await asyncio.wait_for(handled.wait(), 1.0)
            self.assertEqual((await prompt).status, "completed")
            events.close()

    async def test_permission_handler_replies_after_event_publication(self):
        release = asyncio.Event()
        handled = asyncio.Event()

        async def handler(request):
            self.assertEqual(request["id"], "perm-handler-1")
            self.assertEqual(request["tool"], "bash")
            handled.set()
            await release.wait()
            return "allow_session"

        async with await SnowClient.start(fixture_options(), permission_handler=handler) as snow:
            self.assertIn("permission_interaction", snow.ready.capabilities)
            events = snow.events()
            prompt = asyncio.create_task(snow.prompt("permission"))
            event = await events.__anext__()
            while event.type != "permission_request":
                event = await events.__anext__()
            self.assertEqual(event.raw["permission"]["request"]["id"], "perm-handler-1")
            await asyncio.wait_for(handled.wait(), 1.0)
            release.set()
            self.assertEqual((await prompt).status, "completed")
            events.close()

    async def test_version_protocol_process_and_overflow_errors_are_typed(self):
        with self.assertRaises(SnowVersionError):
            await SnowClient.start(fixture_options(environment={"FAKE_SNOW_SCENARIO": "bad_version"}))
        for scenario in ("bad_version_type", "bad_snow_type", "bad_caps_type", "bad_max_type"):
            with self.assertRaises(SnowProtocolError):
                await SnowClient.start(fixture_options(environment={"FAKE_SNOW_SCENARIO": scenario}))

        for scenario in ("malformed", "oversized"):
            snow = await SnowClient.start(
                fixture_options(
                    environment={"FAKE_SNOW_SCENARIO": scenario},
                    max_frame_bytes=1024,
                )
            )
            try:
                await asyncio.sleep(0.05)
                with self.assertRaises(SnowProtocolError):
                    await snow.session_info()
            finally:
                await snow.close()

        snow = await SnowClient.start(fixture_options(environment={"FAKE_SNOW_SCENARIO": "small_limit"}))
        try:
            self.assertEqual(snow.ready.max_input_bytes, 128)
            with self.assertRaises(SnowProtocolError):
                await snow.request("echo", message="x" * 256)
            self.assertEqual((await snow.session_info())["data"]["model"], "fake-1")
        finally:
            await snow.close()

        for scenario in ("stdout_closed", "stdin_closed"):
            snow = await SnowClient.start(fixture_options(environment={"FAKE_SNOW_SCENARIO": scenario}))
            try:
                await asyncio.sleep(0.05)
                with self.assertRaises(SnowProcessError):
                    await snow.session_info()
            finally:
                await snow.close()

        snow = await SnowClient.start(fixture_options())
        try:
            with self.assertRaises(SnowCommandError):
                await snow.request("fail_command")
            with self.assertRaises(SnowTimeoutError):
                await snow.request("hold", timeout=0.01)
            await snow.request("release")
            with self.assertRaises(SnowTimeoutError):
                await snow.prompt("wait", timeout=0.01)
            self.assertEqual((await snow.session_info())["data"]["model"], "fake-1")
        finally:
            await snow.close()
        with self.assertRaises(SnowClosedError):
            await snow.session_info()

        snow = await SnowClient.start(fixture_options())
        try:
            with self.assertRaises(SnowProcessError) as failure:
                await snow.request("crash")
            self.assertIn("fixture process failure", str(failure.exception))
        finally:
            await snow.close()

        snow = await SnowClient.start(fixture_options())
        try:
            events = snow.events(capacity=1)
            await snow.prompt("hello")
            with self.assertRaises(SnowSubscriptionOverflowError):
                await events.__anext__()
        finally:
            await snow.close()

    def test_options_are_safe_and_external_binary_only(self):
        args = SnowOptions(command=("/opt/snow",)).argv()
        self.assertEqual(args[0], "/opt/snow")
        for flag in ("--permission", "--thinking", "--no-session", "--no-plugins", "--no-mcp", "--no-skills", "--no-subagents"):
            self.assertIn(flag, args)
        self.assertNotIn("--api-key", args)


@unittest.skipUnless(os.environ.get("SNOW_TEST_BINARY"), "SNOW_TEST_BINARY is not set")
class SnowBinaryIntegrationTests(unittest.IsolatedAsyncioTestCase):
    async def test_real_fake_provider_lifecycle(self):
        binary = os.environ["SNOW_TEST_BINARY"]
        with tempfile.TemporaryDirectory() as directory:
            options = SnowOptions(
                command=(binary,),
                cwd=directory,
                environment={"SNOW_HOME": str(Path(directory) / "snow-home")},
                startup_timeout=10.0,
                request_timeout=30.0,
            )
            async with await SnowClient.start(options) as snow:
                self.assertEqual(snow.ready.protocol_version, "1")
                self.assertTrue(snow.ready.snow_version)
                info = await snow.session_info()
                self.assertEqual(info["data"]["provider"], "fake")
                models = await snow.models()
                self.assertTrue(models["data"]["models"])
                result = await snow.prompt("Python SDK integration smoke")
                self.assertEqual(result.status, "completed")

                if "multimodal_prompts" in snow.ready.capabilities:
                    image = {"type": "image", "mime_type": "image/png", "data": "AAAA"}
                    model = next(
                        (item for item in (await snow.models())["data"]["models"] if item["id"] == info["data"]["model"]),
                        {},
                    )
                    if model.get("supports_vision"):
                        result = await snow.prompt(content=[image], mode="plan")
                        self.assertEqual(result.status, "completed")
                    else:
                        with self.assertRaises(SnowCommandError):
                            await snow.prompt(content=[image], mode="plan")

                self.assertIn("compaction", snow.ready.capabilities)
                self.assertIn("response_controls", snow.ready.capabilities)
                await snow.set_reasoning_summary("concise")
                await snow.set_text_verbosity("high")
                info = await snow.session_info()
                self.assertEqual(info["data"]["reasoning_summary"], "concise")
                self.assertEqual(info["data"]["text_verbosity"], "high")

                messages = await snow.messages_list()
                self.assertGreaterEqual(len(messages["data"]["messages"]), 2)
                for message in messages["data"]["messages"]:
                    self.assertNotIn("provider_data", [block.get("type") for block in message["content"]])
                usage = (await snow.usage())["data"]
                self.assertIsInstance(usage["total_tokens"], int)
                self.assertIsInstance(usage["input"], int)
                self.assertEqual((await snow.pending_inputs())["data"]["items"], [])
                self.assertEqual((await snow.pending_inputs_clear())["data"]["items"], [])
                self.assertIsInstance((await snow.configuration_diagnostics())["data"]["diagnostics"], list)
                self.assertIsInstance((await snow.mcp_servers())["data"]["servers"], list)
                self.assertIsInstance((await snow.skills())["data"]["skills"], list)

                branches = (await snow.branches_list())["data"]["branches"]
                main = next(branch for branch in branches if branch["active"])
                child = (await snow.branch_fork(name="python-sdk-child"))["data"]
                renamed = await snow.branch_rename(child["id"], "python-sdk-renamed")
                self.assertEqual(renamed["data"]["name"], "python-sdk-renamed")
                await snow.branch_select(main["id"])
                await snow.branch_delete(child["id"])
                remaining = (await snow.branches_list())["data"]["branches"]
                self.assertNotIn(child["id"], [branch["id"] for branch in remaining])

                compacted = await snow.compact()
                self.assertIn("summarized_messages", compacted["data"])
                self.assertIn("retained_messages", compacted["data"])


if __name__ == "__main__":
    unittest.main()
