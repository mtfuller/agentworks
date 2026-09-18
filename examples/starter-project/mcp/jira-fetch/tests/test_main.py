"""Tests for jira-fetch. Run with: python3 -m unittest discover -s tests

Requires `mcp` to be installed (pip install mcp), same as src/main.py
itself -- these tests simulate the Jira REST API rather than calling a
real instance, but they still import the real MCP SDK to exercise the
actual tool registration/invocation path, not just extract().
"""

import io
import json
import os
import pathlib
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent / "src"))

import main  # noqa: E402

FAKE_ISSUE = {
    "key": "PROJ-123",
    "fields": {
        "summary": "Add dark mode toggle",
        "description": "Users want a dark mode option in settings.",
        "status": {"name": "In Progress"},
        "issuelinks": [
            {
                "type": {"outward": "blocks", "inward": "is blocked by"},
                "outwardIssue": {
                    "key": "PROJ-124",
                    "fields": {"summary": "Design dark mode palette"},
                },
            }
        ],
        "customfield_10059": "Toggle persists across sessions; respects OS preference by default.",
    },
}


class TestConfig(unittest.TestCase):
    def test_missing_env_vars_raises(self):
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(main.ConfigError):
                main._config()

    def test_config_reads_env_and_strips_trailing_slash(self):
        env = {
            "JIRA_BASE_URL": "https://example.atlassian.net/",
            "JIRA_EMAIL": "a@b.com",
            "JIRA_API_TOKEN": "tok",
        }
        with patch.dict(os.environ, env, clear=True):
            base_url, email, token = main._config()
        self.assertEqual(base_url, "https://example.atlassian.net")
        self.assertEqual(email, "a@b.com")
        self.assertEqual(token, "tok")


class TestExtract(unittest.TestCase):
    def test_extract_pulls_expected_fields(self):
        with patch.dict(os.environ, {"JIRA_ACCEPTANCE_CRITERIA_FIELD": "customfield_10059"}):
            result = main.extract(FAKE_ISSUE)
        self.assertEqual(result["key"], "PROJ-123")
        self.assertEqual(result["title"], "Add dark mode toggle")
        self.assertEqual(result["status"], "In Progress")
        self.assertEqual(
            result["linked_issues"],
            [{"key": "PROJ-124", "summary": "Design dark mode palette", "relation": "blocks"}],
        )
        self.assertIn("Toggle persists", result["acceptance_criteria"])

    def test_extract_omits_acceptance_criteria_when_not_configured(self):
        with patch.dict(os.environ, {}, clear=True):
            result = main.extract(FAKE_ISSUE)
        self.assertNotIn("acceptance_criteria", result)

    def test_extract_handles_no_linked_issues(self):
        issue = {"key": "PROJ-1", "fields": {"summary": "x", "status": {"name": "Open"}}}
        with patch.dict(os.environ, {}, clear=True):
            result = main.extract(issue)
        self.assertEqual(result["linked_issues"], [])


class FakeHTTPResponse(io.BytesIO):
    """A minimal stand-in for the object urllib.request.urlopen() returns,
    supporting the `with ... as response:` usage in fetch_issue_json."""

    def __enter__(self):
        return self

    def __exit__(self, *exc_info):
        return False


class TestFetchIssueJSON(unittest.TestCase):
    def test_builds_authenticated_request_and_parses_response(self):
        env = {
            "JIRA_BASE_URL": "https://example.atlassian.net",
            "JIRA_EMAIL": "a@b.com",
            "JIRA_API_TOKEN": "tok",
        }
        fake_response = FakeHTTPResponse(json.dumps(FAKE_ISSUE).encode())

        with patch.dict(os.environ, env, clear=True):
            with patch("urllib.request.urlopen", return_value=fake_response) as mock_urlopen:
                result = main.fetch_issue_json("PROJ-123")

        self.assertEqual(result["key"], "PROJ-123")
        called_request = mock_urlopen.call_args[0][0]
        self.assertIn("PROJ-123", called_request.full_url)
        self.assertIn("example.atlassian.net", called_request.full_url)
        self.assertTrue(called_request.get_header("Authorization").startswith("Basic "))


class TestMCPToolRegistration(unittest.TestCase):
    def test_fetch_issue_is_registered_as_a_tool(self):
        import asyncio

        tools = asyncio.run(main.mcp.list_tools())
        names = [t.name for t in tools]
        self.assertIn("fetch_issue", names)


if __name__ == "__main__":
    unittest.main()
