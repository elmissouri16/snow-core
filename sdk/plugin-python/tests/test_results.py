import json
import unittest

from snow_plugin import error_result, result, text_result
from snow_plugin.errors import ToolError


class ResultBuilderTests(unittest.TestCase):
    def test_text_result(self):
        built = text_result("hello", details={"n": 1})
        self.assertEqual(
            built,
            {"content": [{"type": "text", "text": "hello"}], "details": {"n": 1}, "is_error": False},
        )

    def test_text_result_rejects_empty(self):
        with self.assertRaises(TypeError):
            text_result("")
        with self.assertRaises(TypeError):
            text_result("   ")

    def test_result_validates_blocks(self):
        built = result([{"type": "text", "text": "x"}])
        self.assertTrue(built["is_error"] is False)
        with self.assertRaises(TypeError):
            result([])
        with self.assertRaises(TypeError):
            result([{"no_type": True}])
        with self.assertRaises(TypeError):
            result(["not-a-dict"])

    def test_error_result_marks_is_error(self):
        built = error_result("boom")
        self.assertEqual(built["is_error"], True)
        self.assertEqual(built["content"][0]["text"], "boom")
        with self.assertRaises(ValueError):
            error_result("x" * 5000)

    def test_tool_error_type_and_message(self):
        err = ToolError("record missing")
        self.assertEqual(str(err), "record missing")
        with self.assertRaises(TypeError):
            ToolError("  ")


if __name__ == "__main__":
    unittest.main()
