"""Tests for the reference eval runners. They need no network and no `claude`:
the parsing is checked against captured-shape fixtures, and the failure modes the
runners exist to guard against (an auth error scored as an answer) are pinned.

    python3 -m unittest discover -s examples/eval-runners
"""

import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))

import claude_code  # noqa: E402
import claude_judge  # noqa: E402


def stream(*events):
    return [json.dumps(e) for e in events]


INIT = {"type": "system", "subtype": "init", "tools": ["Bash", "Skill"], "skills": ["csv-analyzer"]}


class ParseStreamTest(unittest.TestCase):
    def test_reports_text_tools_activation_and_usage(self):
        lines = stream(
            INIT,
            {"type": "assistant", "message": {"content": [
                {"type": "text", "text": "Let me look."},
                {"type": "tool_use", "name": "Skill", "input": {"skill": "csv-analyzer"}},
                {"type": "tool_use", "name": "Read", "input": {"file_path": "/tmp/data.csv"}},
                {"type": "tool_use", "name": "mcp__jira__fetch_issue", "input": {"issue_key": "ABC-1"}},
            ]}},
            {"type": "result", "subtype": "success", "is_error": False, "result": "Three rows.",
             "usage": {"input_tokens": 120, "output_tokens": 30, "cache_read_input_tokens": 5}},
        )
        out = claude_code.parse_stream(lines)
        self.assertEqual(out["text"], "Three rows.")
        self.assertEqual(out["activated"], ["csv-analyzer"])
        self.assertEqual([c["name"] for c in out["tool_calls"]], ["Read", "mcp__jira__fetch_issue"])
        self.assertEqual(out["tool_calls"][0]["arguments"], {"file_path": "/tmp/data.csv"})
        self.assertEqual(out["usage"], {"input_tokens": 120, "output_tokens": 30})

    def test_a_skill_activation_is_not_also_a_tool_call(self):
        out = claude_code.parse_stream(stream(
            {"type": "assistant", "message": {"content": [{"type": "tool_use", "name": "Skill", "input": {"command": "pdf"}}]}},
            {"type": "result", "is_error": False, "result": "ok"},
        ))
        self.assertEqual(out["activated"], ["pdf"])
        self.assertEqual(out["tool_calls"], [])

    def test_no_skill_and_no_tools_reports_empty_lists_not_missing_keys(self):
        out = claude_code.parse_stream(stream(INIT, {"type": "result", "is_error": False, "result": "4"}))
        self.assertEqual(out["activated"], [])
        self.assertEqual(out["tool_calls"], [])

    def test_an_authentication_failure_is_an_error_not_an_answer(self):
        # This is what `claude -p` really emitted when the session was not authenticated:
        # an assistant text block and a result with is_error true.
        lines = stream(
            INIT,
            {"type": "assistant", "message": {"content": [{"type": "text", "text": "Failed to authenticate: OAuth session expired"}]}},
            {"type": "result", "subtype": "success", "is_error": True, "result": "Failed to authenticate: OAuth session expired"},
        )
        with self.assertRaises(RuntimeError) as ctx:
            claude_code.parse_stream(lines)
        self.assertIn("Failed to authenticate", str(ctx.exception))

    def test_no_result_event_is_an_error(self):
        with self.assertRaises(RuntimeError):
            claude_code.parse_stream(stream(INIT))

    def test_tolerates_noise(self):
        lines = ["", "not json at all", "[1, 2]", json.dumps({"type": "result", "is_error": False, "result": "fine"})]
        self.assertEqual(claude_code.parse_stream(lines)["text"], "fine")

    def test_falls_back_to_assistant_text_when_result_has_none(self):
        out = claude_code.parse_stream(stream(
            {"type": "assistant", "message": {"content": [{"type": "text", "text": "part one"}, {"type": "text", "text": "part two"}]}},
            {"type": "result", "is_error": False},
        ))
        self.assertEqual(out["text"], "part one\npart two")

    def test_duplicate_activations_are_listed_once(self):
        block = {"type": "tool_use", "name": "Skill", "input": {"skill": "a"}}
        out = claude_code.parse_stream(stream(
            {"type": "assistant", "message": {"content": [block, block]}},
            {"type": "result", "is_error": False, "result": "x"},
        ))
        self.assertEqual(out["activated"], ["a"])


class BuildCommandTest(unittest.TestCase):
    def test_defaults(self):
        cmd = claude_code.build_command("hello", {})
        self.assertEqual(cmd[:3], ["claude", "-p", "hello"])
        self.assertIn("stream-json", cmd)
        self.assertIn("--verbose", cmd)  # stream-json requires it with --print

    def test_options(self):
        cmd = claude_code.build_command("hi", {
            "EVAL_CLAUDE_BIN": "/opt/claude", "EVAL_CLAUDE_PLUGIN_DIR": "/tmp/plug",
            "EVAL_CLAUDE_MODEL": "sonnet", "EVAL_CLAUDE_EXTRA_ARGS": "--max-turns 3 --foo 'a b'",
        })
        self.assertEqual(cmd[0], "/opt/claude")
        self.assertEqual(cmd[cmd.index("--plugin-dir") + 1], "/tmp/plug")
        self.assertEqual(cmd[cmd.index("--model") + 1], "sonnet")
        self.assertEqual(cmd[-4:], ["--max-turns", "3", "--foo", "a b"])


class ExtractVerdictTest(unittest.TestCase):
    def test_plain(self):
        self.assertEqual(claude_judge.extract_verdict('{"pass": true, "reason": "ok"}'), {"pass": True, "reason": "ok"})

    def test_inside_prose_and_a_code_fence(self):
        text = 'Sure! ```json\n{"pass": false, "reason": "no source"}\n``` Hope that helps.'
        self.assertEqual(claude_judge.extract_verdict(text), {"pass": False, "reason": "no source"})

    def test_skips_objects_without_a_boolean_pass(self):
        text = '{"note": 1} then {"pass": "yes"} then {"pass": true, "reason": "r"}'
        self.assertEqual(claude_judge.extract_verdict(text), {"pass": True, "reason": "r"})

    def test_unreadable_is_an_error_not_a_guess(self):
        with self.assertRaises(ValueError):
            claude_judge.extract_verdict("It looks good to me.")


if __name__ == "__main__":
    unittest.main()
