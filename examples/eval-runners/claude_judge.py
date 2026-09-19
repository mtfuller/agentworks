#!/usr/bin/env python3
"""An `agentworks eval` judge that grades a `rubric` assertion with Claude Code.

    # agentworks.yaml
    eval:
      judge_runner: python3 examples/eval-runners/claude_judge.py

It reads {"subject", "prompt", "response", "rubric"} as JSON on stdin and prints
{"pass": true|false, "reason": "..."} on stdout. It asks `claude -p` to answer with
only that JSON object, then extracts the object from whatever came back. If the
answer can't be understood it exits 1 rather than guessing, so an unreadable
verdict never silently passes or fails a case.

Environment (optional): EVAL_CLAUDE_MODEL, EVAL_CLAUDE_BIN, as for claude_code.py.
"""

import json
import os
import subprocess
import sys

INSTRUCTIONS = """You are grading a response against a rubric. Be strict and literal.

Reply with ONLY a JSON object, no prose and no code fence:
{"pass": true or false, "reason": "one sentence"}

Subject under test: %(subject)s

Prompt given to it:
%(prompt)s

Its response:
%(response)s

Rubric the response must satisfy:
%(rubric)s
"""


def extract_verdict(text):
    """Find the first JSON object with a boolean "pass" in text, or raise ValueError."""
    decoder = json.JSONDecoder()
    for start, ch in enumerate(text):
        if ch != "{":
            continue
        try:
            obj, _ = decoder.raw_decode(text[start:])
        except json.JSONDecodeError:
            continue
        if isinstance(obj, dict) and isinstance(obj.get("pass"), bool):
            return {"pass": obj["pass"], "reason": str(obj.get("reason", ""))}
    raise ValueError("no JSON object with a boolean \"pass\" in the judge's answer: %r" % text[:300])


def main():
    try:
        request = json.load(sys.stdin)
    except json.JSONDecodeError as err:
        print("claude_judge.py: stdin is not a JSON request: %s" % err, file=sys.stderr)
        return 2

    prompt = INSTRUCTIONS % {k: request.get(k, "") for k in ("subject", "prompt", "response", "rubric")}
    cmd = [os.environ.get("EVAL_CLAUDE_BIN", "claude"), "-p", prompt, "--output-format", "json"]
    if os.environ.get("EVAL_CLAUDE_MODEL"):
        cmd += ["--model", os.environ["EVAL_CLAUDE_MODEL"]]

    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.stderr:
        sys.stderr.write(proc.stderr)
    try:
        envelope = json.loads(proc.stdout)
    except json.JSONDecodeError:
        print("claude_judge.py: claude did not print JSON: %r" % proc.stdout[:300], file=sys.stderr)
        return 1
    if proc.returncode != 0 or envelope.get("is_error"):
        print("claude_judge.py: claude reported an error: %s" % envelope.get("result"), file=sys.stderr)
        return 1

    try:
        verdict = extract_verdict(str(envelope.get("result", "")))
    except ValueError as err:
        print("claude_judge.py: %s" % err, file=sys.stderr)
        return 1
    json.dump(verdict, sys.stdout)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
