---
name: agentworks-evals
description: >
  How to write and run behavior-eval cases for a skill or agent in this AgentWorks project:
  the evals/*.yaml format, text, tool-call, and activation assertions, trigger cases,
  rubrics and a judge, runner protocols (text and json), repeated runs, and agentworks eval.
  Use when asked to test, evaluate, or regression-check how a skill or agent responds, or
  whether its description makes a model choose it.
---

# Behavior evals

`agentworks test` runs unit tests. `agentworks eval` checks how an artifact **responds**:
each case sends a prompt to a runner command and asserts on what comes back. AgentWorks never
calls a model itself; the runner and the judge are commands you supply.

## Case files

Put one or more `*.yaml` / `*.yml` files directly in `<artifact>/evals/` (no subfolders).
Scaffolded skills and agents start with `evals/example.yaml`; replace its placeholder.

```yaml
cases:
  - name: summarizes a csv           # unique across the artifact's files
    prompt: "Profile the attached sales.csv and report row count."
    assert:
      contains: ["rows", "columns"]        # case-insensitive substrings, all required
      not_contains: ["as an AI"]           # none may appear
      matches: "\\d+ rows"                 # Go regex that must match
      not_matches: "(?i)error"             # Go regex that must not match
      min_length: 40
      max_length: 800
```

Every assertion set on a case must pass. Files are parsed **strictly**: an unknown key (a typo such
as `contians:`), a case that asserts nothing, or a duplicate name is an error, not a case that
passes for any response. Assert on facts, keywords, and format, not exact prose. Cover the happy
path, a should-refuse or should-not-trigger case, and one edge input.

## Trace assertions (need `eval_protocol: json`)

```yaml
    assert:
      tool_called: [search]
      tool_not_called: [delete]
      tool_args:                     # some call to the tool must satisfy every condition
        - tool: search
          equals: {limit: 10}        # exact value
          matches: {query: "^unit"}  # Go regex on the argument's string form
      activated: [csv-analyzer]      # skills that must / must not have been activated
      not_activated: [other-skill]
```

## Trigger cases: does the description work?

```yaml
  - name: chosen for a csv request
    prompt: "Can you analyze this sales csv?"
    should_trigger: true
  - name: not chosen for arithmetic
    prompt: "What is 2 + 2?"
    should_trigger: false
```

A model decides whether to use a skill from its description alone, so this is how you test it. A
failure says whether the description is too narrow (missed a trigger) or too broad (a false one).
Write a pair for each skill: phrasings it should catch and near-misses it should not.

## Rubrics

`rubric: "Cites at least one source."` is graded by a judge: a command (`judge_runner` in
frontmatter, or `eval.judge_runner`) that reads `{"subject","prompt","response","rubric"}` as JSON
on stdin and prints `{"pass": true|false, "reason": "..."}`. No judge, or a judge whose output can't
be read, fails the case rather than passing it unchecked.

## Runner

The runner is a shell command that reads the prompt on **stdin**. Set it per artifact in
frontmatter, or once in `agentworks.yaml` (an artifact's own value wins):

```yaml
eval_runner: python3 examples/eval-runners/claude_code.py
eval_protocol: json          # or "text" (the default)
judge_runner: python3 examples/eval-runners/claude_judge.py
```

```yaml
# agentworks.yaml
eval:
  default_runner: claude -p
  protocol: text
  judge_runner: ...
  runs: 1                    # default repetitions per case
  timeout: 120               # seconds per runner/judge call
```

**text protocol:** stdout is the response. **json protocol:** stdout is exactly one JSON object
`{"text": "...", "tool_calls": [{"name": "...", "arguments": {}}], "activated": ["skill"],
"usage": {"input_tokens": 0, "output_tokens": 0}}` (logs go to stderr). Only `text` is required, but
a case that asserts on `tool_calls` or `activated` fails if the runner did not report them; an empty
list means "none", a missing key means "not reported".

The runner and judge run in the artifact's directory with `AGENTWORKS_ARTIFACT_NAME`, `_KIND`,
`_DIR`, `AGENTWORKS_EVAL_CASE`, `_PROTOCOL`, and `_ROLE` set. With no runner configured, artifacts
with cases are skipped with a warning, not failed. A runner that errors (including an
authentication failure) must exit non-zero, so a broken setup is never scored as an answer.

## Nondeterminism and time

```yaml
    runs: 5               # repeat the case
    pass_threshold: 0.8   # the fraction of runs that must pass (default: all)
    timeout: 60           # seconds per run; a runner that hangs is killed with its children
```

## Run

```bash
agentworks eval                  # every artifact with an evals/ directory
agentworks eval skills/<name>    # one artifact
agentworks eval --case "<name>"  # one case by exact name
agentworks eval --junit eval.xml # also write a JUnit report for CI
agentworks eval --json           # machine-readable report on stdout
```

`agentworks validate` also parses eval files, so malformed YAML and typos are caught early.
`agentworks doctor` verifies the runner's and judge's binaries are on PATH.
