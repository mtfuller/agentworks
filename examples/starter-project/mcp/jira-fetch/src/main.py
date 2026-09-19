#!/usr/bin/env python3
"""jira-fetch: an MCP server exposing one tool, fetch_issue, that fetches
a Jira Cloud issue and returns the fields useful for implementing it.

No dependencies: it speaks MCP over stdio itself (newline-delimited JSON-RPC),
so `agentworks test` passes on a fresh clone.
Reads JIRA_BASE_URL, JIRA_EMAIL, and JIRA_API_TOKEN from the environment
(see this tool's ../mcp.md `auth:` list -- when exported to claude-code
or github-copilot, these are wired through as ${VAR} references in the
generated MCP server config, never as literal secrets).

Acceptance criteria live in a custom field in most Jira instances, so
there's no universal field name for them -- set
JIRA_ACCEPTANCE_CRITERIA_FIELD (e.g. "customfield_10059") if your
instance has one; it's simply omitted from the result otherwise.
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.request
from base64 import b64encode

SERVER_NAME = "jira-fetch"
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


class ConfigError(RuntimeError):
    """Raised when required environment variables are missing."""


def _config() -> tuple[str, str, str]:
    base_url = os.environ.get("JIRA_BASE_URL", "").rstrip("/")
    email = os.environ.get("JIRA_EMAIL", "")
    token = os.environ.get("JIRA_API_TOKEN", "")
    missing = [
        name
        for name, value in (
            ("JIRA_BASE_URL", base_url),
            ("JIRA_EMAIL", email),
            ("JIRA_API_TOKEN", token),
        )
        if not value
    ]
    if missing:
        raise ConfigError(f"missing required environment variable(s): {', '.join(missing)}")
    return base_url, email, token


def fetch_issue_json(issue_key: str) -> dict:
    """Fetch the raw Jira API response for one issue.

    Uses API v2 (not v3) so `description` comes back as a plain string
    rather than Atlassian Document Format's nested JSON.
    """
    base_url, email, token = _config()
    fields = ["summary", "description", "status", "issuelinks"]
    acceptance_field = os.environ.get("JIRA_ACCEPTANCE_CRITERIA_FIELD")
    if acceptance_field:
        fields.append(acceptance_field)

    url = f"{base_url}/rest/api/2/issue/{issue_key}?fields={','.join(fields)}"
    credentials = b64encode(f"{email}:{token}".encode()).decode()
    request = urllib.request.Request(
        url,
        headers={"Authorization": f"Basic {credentials}", "Accept": "application/json"},
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)
    except urllib.error.HTTPError as e:
        body = e.read().decode(errors="replace")
        raise RuntimeError(f"Jira API returned {e.code} for {issue_key}: {body}") from e


def extract(issue: dict) -> dict:
    """Pull the fields useful for implementing a ticket out of a raw Jira
    issue response."""
    fields = issue.get("fields", {})
    acceptance_field = os.environ.get("JIRA_ACCEPTANCE_CRITERIA_FIELD")

    linked = []
    for link in fields.get("issuelinks", []):
        other = link.get("outwardIssue") or link.get("inwardIssue")
        if not other:
            continue
        relation_key = "outward" if "outwardIssue" in link else "inward"
        relation = link.get("type", {}).get(relation_key, "relates to")
        linked.append(
            {
                "key": other.get("key"),
                "summary": other.get("fields", {}).get("summary"),
                "relation": relation,
            }
        )

    result = {
        "key": issue.get("key"),
        "title": fields.get("summary"),
        "status": fields.get("status", {}).get("name"),
        "description": fields.get("description"),
        "linked_issues": linked,
    }
    if acceptance_field and acceptance_field in fields:
        result["acceptance_criteria"] = fields[acceptance_field]
    return result


@tool(
    "fetch_issue",
    "Fetch a Jira ticket and return its title, description, status, linked issues, "
    "and acceptance criteria (if configured) as structured data ready for an agent to act on.",
    {"issue_key": {"type": "string", "description": "The issue key, e.g. PROJ-123"}},
    ["issue_key"],
)
def fetch_issue(args):
    return json.dumps(extract(fetch_issue_json(args["issue_key"])), indent=2)


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
