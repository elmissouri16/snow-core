import unittest

from snow_plugin import Plugin, ToolContext
from snow_plugin.errors import ToolError


class PluginDefinitionTests(unittest.TestCase):
    async def _async_echo(self, arguments, context):
        return {"text": str(arguments.get("text", ""))}

    def test_decorator_registration_and_descriptor(self):
        plugin = Plugin("audit-python", "Audit", "1.0.0")

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
            capabilities=["text"],
            discovery={"mode": "always"},
        )
        async def echo(arguments, context):
            return {"text": str(arguments.get("text", ""))}

        descriptor = plugin.get_tool("echo").descriptor()
        self.assertEqual(descriptor["name"], "echo")
        self.assertEqual(descriptor["risk"], "read")
        self.assertEqual(descriptor["parameters"]["type"], "object")
        self.assertEqual(descriptor["capabilities"], ["text"])
        self.assertEqual(descriptor["discovery"]["mode"], "always")
        self.assertEqual(plugin.manifest()["protocol_version"], 2)

    def test_validation_rejects_bad_definitions(self):
        plugin = Plugin("audit-python", "Audit", "1.0.0")
        with self.assertRaises(ValueError):
            plugin.tool(name="UPPERCASE", description="x")(lambda *a: None)
        with self.assertRaises(ValueError):
            plugin.tool(name="ok", description="x", risk="banana")(lambda *a: None)
        with self.assertRaises(ValueError):
            plugin.tool(name="ok", description="")(lambda *a: None)

    def test_duplicate_tool_names_rejected(self):
        plugin = Plugin("audit-python", "Audit", "1.0.0")
        plugin.tool(name="echo", description="first")(lambda *a: None)
        with self.assertRaises(ValueError):
            plugin.tool(name="echo", description="second")(lambda *a: None)

    def test_manifest_validation(self):
        with self.assertRaises(ValueError):
            Plugin("Bad_Id", "Nope", "1.0.0")
        with self.assertRaises(ValueError):
            Plugin("ok", "", "1.0.0")
        with self.assertRaises(ValueError):
            Plugin("ok", "Ok", "")

    def test_allowed_tools_and_capabilities_are_strings(self):
        plugin = Plugin("audit-python", "Audit", "1.0.0", capabilities=["x"], allowed_tools=["echo"])
        self.assertEqual(plugin.capabilities, ["x"])
        self.assertEqual(plugin.allowed_tools, ["echo"])


if __name__ == "__main__":
    unittest.main()
