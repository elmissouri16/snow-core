import asyncio
import unittest

from snow_plugin import ToolContext, error_result


class ToolContextTests(unittest.IsolatedAsyncioTestCase):
    async def test_progress_and_log_payloads(self):
        emitted = []

        async def write(payload):
            emitted.append(payload)

        context = ToolContext(
            call_id="call-1",
            request_id="req-1",
            cwd="/tmp",
            session_id="s",
            deadline=None,
            config=None,
            _write=write,
        )
        await context.progress("working", done=True)
        await context.log("info", "hello")
        self.assertEqual(emitted[0]["method"], "notifications/progress")
        self.assertEqual(emitted[0]["params"]["call_id"], "call-1")
        self.assertEqual(emitted[0]["params"]["done"], True)
        self.assertEqual(emitted[1]["method"], "notifications/log")
        self.assertEqual(emitted[1]["params"]["severity"], "info")

    def test_context_rejects_bad_progress(self):
        context = ToolContext(
            call_id="",
            request_id="req-1",
            cwd="/tmp",
            session_id="s",
            deadline=None,
            config=None,
        )
        with self.assertRaises(ValueError):
            asyncio.run(context.progress("no call id"))

    def test_error_result(self):
        built = error_result("boom")
        self.assertEqual(built["is_error"], True)
        self.assertEqual(built["content"][0]["text"], "boom")


if __name__ == "__main__":
    unittest.main()
