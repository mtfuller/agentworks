#!/bin/sh
# Fake stand-in for a real model call, so this example runs (in CI or
# locally) without network access or an API key -- the same "simulate
# rather than call the real thing" approach mcp/jira-fetch's tests use for
# its API. It speaks the json eval protocol, so the example can show the
# trace assertions too: the canned response reports one tool call and that
# this agent was activated, whatever the prompt.
#
# For a real behavior test point eval_runner at something that calls a model
# and reports what happened, e.g. examples/eval-runners/claude_code.py, and
# add a should_trigger: false case -- a canned runner can't tell prompts apart,
# so this example can't include one.
cat >/dev/null
cat <<'EOF2'
{"text": "Sources agree that primary documentation is more reliable than blog summaries. One source (vendor-blog.example) claims a 2x speedup; the original benchmark report doesn't confirm that figure, so it's flagged as unconfirmed rather than repeated as fact.", "tool_calls": [{"name": "web-search", "arguments": {"query": "library X 2x speedup benchmark report", "max_results": 5}}], "activated": ["researcher"], "usage": {"input_tokens": 240, "output_tokens": 62}}
EOF2
