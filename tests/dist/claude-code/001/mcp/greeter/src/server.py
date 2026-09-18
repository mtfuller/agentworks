#!/usr/bin/env python3
"""greeter: an MCP server over stdio (newline-delimited JSON-RPC 2.0).

No dependencies. Add tools by decorating a function with @tool -- its name,
description, and JSON Schema become what an agent sees in tools/list. Return
a string (or anything str()-able); raise to report an error to the agent.
"""

import json
import sys

SERVER_NAME = "greeter"
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
