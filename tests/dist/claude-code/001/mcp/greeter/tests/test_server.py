import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

import server  # noqa: E402


def call(method, params=None, req_id=1):
    return server.handle({"jsonrpc": "2.0", "id": req_id, "method": method, "params": params or {}})


class ServerTest(unittest.TestCase):
    def test_initialize(self):
        result = call("initialize", {"protocolVersion": "2024-11-05"})["result"]
        self.assertEqual(result["serverInfo"]["name"], "greeter")

    def test_lists_tools(self):
        names = [t["name"] for t in call("tools/list")["result"]["tools"]]
        self.assertIn("hello", names)

    def test_hello(self):
        result = call("tools/call", {"name": "hello", "arguments": {"name": "world"}})["result"]
        self.assertEqual(result["content"][0]["text"], "Hello, world!")
        self.assertFalse(result["isError"])

    def test_unknown_method(self):
        self.assertEqual(call("nope")["error"]["code"], -32601)


if __name__ == "__main__":
    unittest.main()
