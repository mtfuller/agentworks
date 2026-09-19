#!/usr/bin/env python3
"""An `agentworks eval` runner that asks Claude Code (`claude -p`) and reports the
trace: the response text, the tools it called, and which skills it activated.

Use it as a json-protocol runner:

    # agentworks.yaml
    eval:
      default_runner: python3 examples/eval-runners/claude_code.py
      protocol: json

It reads the prompt on stdin and prints ONE JSON object on stdout (everything else
goes to stderr): {"text", "tool_calls", "activated", "usage"}.

Configuration, all optional, through the environment:

  EVAL_CLAUDE_PLUGIN_DIR   a plugin directory to load for the run (--plugin-dir).
                           A skill is only *activated* if it is installed, so to test
                           whether a skill triggers, export your project
                           (`agentworks export`) and point this at the plugin.
  EVAL_CLAUDE_MODEL        model alias or name (--model).
  EVAL_CLAUDE_EXTRA_ARGS   extra arguments, shell-quoted, appended to the command.
  EVAL_CLAUDE_BIN          the claude executable (default: claude).

`agentworks eval` also sets AGENTWORKS_ARTIFACT_NAME/KIND/DIR and
AGENTWORKS_EVAL_CASE, which this runner ignores except to label errors.

It fails (exit 1, message on stderr) if `claude` errors -- including an
authentication failure, which `claude -p` reports as an ordinary-looking result --
so a broken setup can never be scored as if it were the model's answer.

The stream-json parsing is deliberately tolerant: it looks for `tool_use` content
blocks in assistant messages and a final `result` event, and ignores anything else.
It was written against the event envelope `claude -p --output-format stream-json
--verbose` emits (system/init, assistant, result) and the Messages API content-block
shape; check it against your installed version if a field moves.
"""

import json
import os
import shlex
import subprocess
import sys

# The tool Claude Code uses to activate a skill; the skill's name is in the input.
SKILL_TOOL = "Skill"
SKILL_NAME_KEYS = ("skill", "command", "name", "skill_name")


def parse_stream(lines):
    """Turn stream-json lines into the eval protocol's dict.

    Raises RuntimeError if the run reported an error (e.g. not authenticated) or
    never produced a result.
    """
    text_parts = []
    tool_calls = []
    activated = []
    result = None

    for raw in lines:
        raw = raw.strip()
        if not raw:
            continue
        try:
            event = json.loads(raw)
        except json.JSONDecodeError:
            continue  # not an event (a stray log line)
        if not isinstance(event, dict):
            continue

        kind = event.get("type")
        if kind == "assistant":
            message = event.get("message") or {}
            for block in message.get("content") or []:
                if not isinstance(block, dict):
                    continue
                if block.get("type") == "text" and block.get("text"):
                    text_parts.append(block["text"])
                elif block.get("type") == "tool_use":
                    name = block.get("name") or ""
                    tool_input = block.get("input") or {}
                    if name == SKILL_TOOL:
                        skill = next((tool_input[k] for k in SKILL_NAME_KEYS if isinstance(tool_input.get(k), str)), None)
                        if skill and skill not in activated:
                            activated.append(skill)
                    else:
                        tool_calls.append({"name": name, "arguments": tool_input if isinstance(tool_input, dict) else {}})
        elif kind == "result":
            result = event

    if result is None:
        raise RuntimeError("claude produced no result event (did it crash or get killed?)")
    if result.get("is_error"):
        raise RuntimeError("claude reported an error: " + str(result.get("result") or result.get("subtype") or "unknown"))

    text = result.get("result")
    if not isinstance(text, str):
        text = "\n".join(text_parts)

    usage = result.get("usage") or {}
    return {
        "text": text,
        "tool_calls": tool_calls,
        "activated": activated,
        "usage": {
            "input_tokens": int(usage.get("input_tokens") or 0),
            "output_tokens": int(usage.get("output_tokens") or 0),
        },
    }


def build_command(prompt, env):
    cmd = [env.get("EVAL_CLAUDE_BIN", "claude"), "-p", prompt, "--output-format", "stream-json", "--verbose"]
    if env.get("EVAL_CLAUDE_PLUGIN_DIR"):
        cmd += ["--plugin-dir", env["EVAL_CLAUDE_PLUGIN_DIR"]]
    if env.get("EVAL_CLAUDE_MODEL"):
        cmd += ["--model", env["EVAL_CLAUDE_MODEL"]]
    if env.get("EVAL_CLAUDE_EXTRA_ARGS"):
        cmd += shlex.split(env["EVAL_CLAUDE_EXTRA_ARGS"])
    return cmd


def main():
    prompt = sys.stdin.read()
    if not prompt.strip():
        print("claude_code.py: no prompt on stdin", file=sys.stderr)
        return 2

    proc = subprocess.run(build_command(prompt, os.environ), capture_output=True, text=True)
    if proc.stderr:
        sys.stderr.write(proc.stderr)
    try:
        payload = parse_stream(proc.stdout.splitlines())
    except RuntimeError as err:
        print("claude_code.py (%s): %s" % (os.environ.get("AGENTWORKS_EVAL_CASE", "?"), err), file=sys.stderr)
        return 1
    if proc.returncode != 0:
        print("claude_code.py: claude exited %d" % proc.returncode, file=sys.stderr)
        return 1

    json.dump(payload, sys.stdout)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
