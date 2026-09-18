package scaffold

import "strings"

// Starter sources for an mcp scaffold. Each is a complete, working MCP
// server (newline-delimited JSON-RPC over stdio: initialize, tools/list,
// tools/call, ping) with no third-party dependencies, so a freshly scaffolded
// artifact passes `agentworks test` and opens in `agentworks run` before the
// author has written a line. They deliberately don't pull in an MCP SDK:
// once a server outgrows this, swapping in the official SDK is a small,
// local change to the file that owns the protocol loop.

func mcpSource(src, name string) string {
	return strings.ReplaceAll(src, "__NAME__", name)
}

const pythonMCPServer = `#!/usr/bin/env python3
"""__NAME__: an MCP server over stdio (newline-delimited JSON-RPC 2.0).

No dependencies. Add tools by decorating a function with @tool -- its name,
description, and JSON Schema become what an agent sees in tools/list. Return
a string (or anything str()-able); raise to report an error to the agent.
"""

import json
import sys

SERVER_NAME = "__NAME__"
SERVER_VERSION = "0.1.0"
DEFAULT_PROTOCOL = "2024-11-05"

TOOLS = {}


def tool(name, description, properties=None, required=()):
    """Register fn as an MCP tool."""

    def register(fn):
        TOOLS[name] = {
            "fn": fn,
            "spec": {
                "name": name,
                "description": description,
                "inputSchema": {
                    "type": "object",
                    "properties": properties or {},
                    "required": list(required),
                },
            },
        }
        return fn

    return register


# --- tools ---
@tool(
    "hello",
    "Greet someone by name. Replace this with your first real tool.",
    {"name": {"type": "string", "description": "Who to greet"}},
    ["name"],
)
def hello(args):
    return f"Hello, {args['name']}!"
# --- end tools ---


def handle(request):
    """Turn one JSON-RPC request into a response dict, or None for a notification."""
    method = request.get("method")
    req_id = request.get("id")
    params = request.get("params") or {}

    if req_id is None:  # notification (e.g. notifications/initialized): no reply
        return None

    def ok(result):
        return {"jsonrpc": "2.0", "id": req_id, "result": result}

    def err(code, message):
        return {"jsonrpc": "2.0", "id": req_id, "error": {"code": code, "message": message}}

    if method == "initialize":
        return ok(
            {
                "protocolVersion": params.get("protocolVersion", DEFAULT_PROTOCOL),
                "capabilities": {"tools": {}},
                "serverInfo": {"name": SERVER_NAME, "version": SERVER_VERSION},
            }
        )
    if method == "ping":
        return ok({})
    if method == "tools/list":
        return ok({"tools": [t["spec"] for t in TOOLS.values()]})
    if method == "tools/call":
        entry = TOOLS.get(params.get("name"))
        if entry is None:
            return err(-32602, f"unknown tool: {params.get('name')}")
        try:
            text = str(entry["fn"](params.get("arguments") or {}))
            return ok({"content": [{"type": "text", "text": text}], "isError": False})
        except Exception as exc:  # report tool failures to the agent, don't crash
            return ok({"content": [{"type": "text", "text": str(exc)}], "isError": True})
    return err(-32601, f"method not found: {method}")


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            response = handle(json.loads(line))
        except json.JSONDecodeError:
            response = {"jsonrpc": "2.0", "id": None, "error": {"code": -32700, "message": "parse error"}}
        if response is not None:
            sys.stdout.write(json.dumps(response) + "\n")
            sys.stdout.flush()


if __name__ == "__main__":
    main()
`

const pythonMCPTest = `import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

import server  # noqa: E402


def call(method, params=None, req_id=1):
    return server.handle({"jsonrpc": "2.0", "id": req_id, "method": method, "params": params or {}})


class ServerTest(unittest.TestCase):
    def test_initialize(self):
        result = call("initialize", {"protocolVersion": "2024-11-05"})["result"]
        self.assertEqual(result["serverInfo"]["name"], "__NAME__")

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
`

const nodeMCPServer = `#!/usr/bin/env node
// __NAME__: an MCP server over stdio (newline-delimited JSON-RPC 2.0).
//
// No dependencies. Add a tool by adding an entry to TOOLS -- its name,
// description, and JSON Schema become what an agent sees in tools/list. The
// handler returns a string; throw to report an error to the agent.

import { createInterface } from "node:readline";
import { pathToFileURL } from "node:url";

const SERVER_NAME = "__NAME__";
const SERVER_VERSION = "0.1.0";
const DEFAULT_PROTOCOL = "2024-11-05";

export const TOOLS = {
  hello: {
    description: "Greet someone by name. Replace this with your first real tool.",
    inputSchema: {
      type: "object",
      properties: { name: { type: "string", description: "Who to greet" } },
      required: ["name"],
    },
    handler: (args) => ` + "`Hello, ${args.name}!`" + `,
  },
};

export function handle(request) {
  const { id, method, params = {} } = request;
  if (id === undefined || id === null) return null; // notification: no reply

  const ok = (result) => ({ jsonrpc: "2.0", id, result });
  const err = (code, message) => ({ jsonrpc: "2.0", id, error: { code, message } });

  switch (method) {
    case "initialize":
      return ok({
        protocolVersion: params.protocolVersion ?? DEFAULT_PROTOCOL,
        capabilities: { tools: {} },
        serverInfo: { name: SERVER_NAME, version: SERVER_VERSION },
      });
    case "ping":
      return ok({});
    case "tools/list":
      return ok({
        tools: Object.entries(TOOLS).map(([name, t]) => ({
          name,
          description: t.description,
          inputSchema: t.inputSchema,
        })),
      });
    case "tools/call": {
      const tool = TOOLS[params.name];
      if (!tool) return err(-32602, ` + "`unknown tool: ${params.name}`" + `);
      try {
        const text = String(tool.handler(params.arguments ?? {}));
        return ok({ content: [{ type: "text", text }], isError: false });
      } catch (e) {
        return ok({ content: [{ type: "text", text: String(e?.message ?? e) }], isError: true });
      }
    }
    default:
      return err(-32601, ` + "`method not found: ${method}`" + `);
  }
}

function main() {
  const rl = createInterface({ input: process.stdin });
  rl.on("line", (line) => {
    if (!line.trim()) return;
    let response;
    try {
      response = handle(JSON.parse(line));
    } catch {
      response = { jsonrpc: "2.0", id: null, error: { code: -32700, message: "parse error" } };
    }
    if (response) process.stdout.write(JSON.stringify(response) + "\n");
  });
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
`

const nodeMCPTest = `import { test } from "node:test";
import assert from "node:assert/strict";

import { handle } from "../src/index.js";

const call = (method, params = {}) => handle({ jsonrpc: "2.0", id: 1, method, params });

test("initialize reports the server name", () => {
  assert.equal(call("initialize", { protocolVersion: "2024-11-05" }).result.serverInfo.name, "__NAME__");
});

test("tools/list includes hello", () => {
  const names = call("tools/list").result.tools.map((t) => t.name);
  assert.ok(names.includes("hello"));
});

test("tools/call hello", () => {
  const { result } = call("tools/call", { name: "hello", arguments: { name: "world" } });
  assert.equal(result.content[0].text, "Hello, world!");
  assert.equal(result.isError, false);
});

test("unknown method is an error", () => {
  assert.equal(call("nope").error.code, -32601);
});
`

const tsMCPServer = `// __NAME__: an MCP server over stdio (newline-delimited JSON-RPC 2.0).
//
// No runtime dependencies. Add a tool by adding an entry to TOOLS -- its name,
// description, and JSON Schema become what an agent sees in tools/list. The
// handler returns a string; throw to report an error to the agent.

import { createInterface } from "node:readline";
import { pathToFileURL } from "node:url";

const SERVER_NAME = "__NAME__";
const SERVER_VERSION = "0.1.0";
const DEFAULT_PROTOCOL = "2024-11-05";

type Json = Record<string, unknown>;

interface Request {
  id?: number | string | null;
  method: string;
  params?: Json;
}

interface Response {
  jsonrpc: "2.0";
  id: number | string | null;
  result?: unknown;
  error?: { code: number; message: string };
}

interface Tool {
  description: string;
  inputSchema: Json;
  handler: (args: Json) => string;
}

export const TOOLS: Record<string, Tool> = {
  hello: {
    description: "Greet someone by name. Replace this with your first real tool.",
    inputSchema: {
      type: "object",
      properties: { name: { type: "string", description: "Who to greet" } },
      required: ["name"],
    },
    handler: (args) => ` + "`Hello, ${String(args.name)}!`" + `,
  },
};

export function handle(request: Request): Response | null {
  const { id, method, params = {} } = request;
  if (id === undefined || id === null) return null; // notification: no reply

  const ok = (result: unknown): Response => ({ jsonrpc: "2.0", id, result });
  const err = (code: number, message: string): Response => ({ jsonrpc: "2.0", id, error: { code, message } });

  switch (method) {
    case "initialize":
      return ok({
        protocolVersion: (params.protocolVersion as string | undefined) ?? DEFAULT_PROTOCOL,
        capabilities: { tools: {} },
        serverInfo: { name: SERVER_NAME, version: SERVER_VERSION },
      });
    case "ping":
      return ok({});
    case "tools/list":
      return ok({
        tools: Object.entries(TOOLS).map(([name, t]) => ({
          name,
          description: t.description,
          inputSchema: t.inputSchema,
        })),
      });
    case "tools/call": {
      const tool = TOOLS[params.name as string];
      if (!tool) return err(-32602, ` + "`unknown tool: ${String(params.name)}`" + `);
      try {
        const text = String(tool.handler((params.arguments as Json | undefined) ?? {}));
        return ok({ content: [{ type: "text", text }], isError: false });
      } catch (e) {
        return ok({ content: [{ type: "text", text: e instanceof Error ? e.message : String(e) }], isError: true });
      }
    }
    default:
      return err(-32601, ` + "`method not found: ${method}`" + `);
  }
}

function main(): void {
  const rl = createInterface({ input: process.stdin });
  rl.on("line", (line) => {
    if (!line.trim()) return;
    let response: Response | null;
    try {
      response = handle(JSON.parse(line) as Request);
    } catch {
      response = { jsonrpc: "2.0", id: null, error: { code: -32700, message: "parse error" } };
    }
    if (response) process.stdout.write(JSON.stringify(response) + "\n");
  });
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
`

const tsMCPTest = `import { test } from "node:test";
import assert from "node:assert/strict";

import { handle } from "../src/index.ts";

const call = (method: string, params: Record<string, unknown> = {}) =>
  handle({ jsonrpc: "2.0", id: 1, method, params } as never);

test("initialize reports the server name", () => {
  const res = call("initialize", { protocolVersion: "2024-11-05" });
  assert.equal((res?.result as { serverInfo: { name: string } }).serverInfo.name, "__NAME__");
});

test("tools/call hello", () => {
  const res = call("tools/call", { name: "hello", arguments: { name: "world" } });
  const result = res?.result as { content: { text: string }[]; isError: boolean };
  assert.equal(result.content[0].text, "Hello, world!");
  assert.equal(result.isError, false);
});

test("unknown method is an error", () => {
  assert.equal(call("nope")?.error?.code, -32601);
});
`

// pythonMCPServerWithTools returns the Python server source with its example
// tool region (between the "# --- tools ---" markers) replaced by tools, so
// the api-wrapper/cli-wrapper templates share one protocol loop.
func pythonMCPServerWithTools(name, tools string) string {
	src := pythonMCPServer
	start := strings.Index(src, "# --- tools ---\n")
	end := strings.Index(src, "# --- end tools ---\n")
	src = src[:start] + tools + src[end+len("# --- end tools ---\n"):]
	return mcpSource(src, name)
}

const pythonAPITools = `import json
import os
import urllib.request


@tool(
    "get",
    "GET a path on the wrapped API and return the response body.",
    {"path": {"type": "string", "description": "Path to request, e.g. /issues/42"}},
    ["path"],
)
def get(args):
    # Read config at call time (not import time) so the server starts, lists
    # its tools, and passes its tests without credentials set.
    base = os.environ["API_BASE_URL"].rstrip("/")
    request = urllib.request.Request(
        base + args["path"],
        headers={"Authorization": "Bearer " + os.environ["API_TOKEN"]},
    )
    with urllib.request.urlopen(request, timeout=30) as resp:
        return json.dumps(json.load(resp), indent=2)
`

const pythonCLITools = `import subprocess

# The one binary this server is allowed to run. Change it to the CLI you are
# wrapping -- the agent supplies only arguments, never the program.
WRAPPED_COMMAND = "echo"


@tool(
    "run",
    "Run the wrapped command-line tool with the given arguments and return its output.",
    {"args": {"type": "array", "items": {"type": "string"}, "description": "Arguments to pass"}},
)
def run(args):
    argv = [WRAPPED_COMMAND, *[str(a) for a in args.get("args", [])]]
    result = subprocess.run(argv, capture_output=True, text=True, timeout=60)
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or f"{WRAPPED_COMMAND} exited {result.returncode}")
    return result.stdout
`

const pythonMCPTestGeneric = `import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

import server  # noqa: E402


def call(method, params=None, req_id=1):
    return server.handle({"jsonrpc": "2.0", "id": req_id, "method": method, "params": params or {}})


class ServerTest(unittest.TestCase):
    def test_initialize(self):
        result = call("initialize", {"protocolVersion": "2024-11-05"})["result"]
        self.assertEqual(result["serverInfo"]["name"], "__NAME__")

    def test_lists_at_least_one_tool(self):
        self.assertTrue(call("tools/list")["result"]["tools"])

    def test_unknown_method(self):
        self.assertEqual(call("nope")["error"]["code"], -32601)


if __name__ == "__main__":
    unittest.main()
`
