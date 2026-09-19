<!-- Generated from internal/artifact/fields.go. Do not edit by hand:
     run `UPDATE_GOLDEN=1 go test ./internal/artifact` to regenerate. -->

# Frontmatter reference

Every artifact is a `<kind>.md` file: YAML frontmatter, then a Markdown body. This lists
every supported frontmatter field. `agentworks validate` rejects a supported field with
the wrong type and warns about any other key, because an unrecognized key is ignored on
export -- usually a typo. Prefix a key with `x-` (for example `x-owner`) to keep your own
metadata without the warning.

Fields marked *deprecated* are still accepted for now but warned about; they will be
removed in a later minor version (see COMPATIBILITY.md).

## agent

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `kind` | string | yes | The artifact kind: `agent`, `skill`, `mcp`, or `hook`. Must match the file name (`<kind>.md`). |
| `name` | string | yes | Lowercase letters, digits, and hyphens. Must match the directory name. |
| `description` | string | yes | What the artifact does and when to use it. This is how an agent decides whether to use it, so it is linted for quality. Keep it under 1024 characters. |
| `version` | string |  | The artifact's own version (default `0.1.0`). |
| `namespace` | string |  | Scopes the artifact under a team or origin so two artifacts can share a name. Files it at `<kind-dir>/<namespace>/<name>/`. |
| `test` | string |  | Shell command `agentworks test` runs from the artifact's directory. |
| `build` | string |  | Shell command `agentworks build` runs from the artifact's directory. `agentworks export` runs it first. |
| `entrypoint` | string |  | Path (relative to the artifact) to its main file. `agentworks doctor` checks it exists. |
| `eval_runner` | string |  | Shell command `agentworks eval` pipes each case's prompt to; its stdout is what the assertions check. Falls back to the project's `eval.default_runner`. |
| `eval_protocol` | string |  | How the eval runner reports a response: `text` (stdout is the response) or `json` (one JSON object that also reports tool calls and activations, needed by the tool and trigger assertions). Falls back to the project's `eval.protocol`. |
| `judge_runner` | string |  | Shell command that grades `rubric` assertions: reads a JSON request on stdin, prints a JSON verdict. Falls back to the project's `eval.judge_runner`. |
| `source` | object |  | Provenance written by `agentworks add` (where an imported artifact came from). Not meant to be edited by hand. |
| `targets` | list of strings |  | *Deprecated:* targets are set once in agentworks.yaml, not per artifact. Ignored. Targets live in `agentworks.yaml`. |
| `tools` | list of strings |  | What the agent may do, from a closed vendor-agnostic set (`agentworks validate` lists the values). Mapped to Claude Code's and Gemini CLI's real tool fields. |
| `model` | string |  | How capable a model the agent needs: `fast`, `balanced`, or `powerful`. |
| `resources` | list of strings |  | Files under the agent's `resources/` directory it should consult. Informational: the whole directory ships either way. |

## skill

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `kind` | string | yes | The artifact kind: `agent`, `skill`, `mcp`, or `hook`. Must match the file name (`<kind>.md`). |
| `name` | string | yes | Lowercase letters, digits, and hyphens. Must match the directory name. |
| `description` | string | yes | What the artifact does and when to use it. This is how an agent decides whether to use it, so it is linted for quality. Keep it under 1024 characters. |
| `version` | string |  | The artifact's own version (default `0.1.0`). |
| `namespace` | string |  | Scopes the artifact under a team or origin so two artifacts can share a name. Files it at `<kind-dir>/<namespace>/<name>/`. |
| `test` | string |  | Shell command `agentworks test` runs from the artifact's directory. |
| `build` | string |  | Shell command `agentworks build` runs from the artifact's directory. `agentworks export` runs it first. |
| `entrypoint` | string |  | Path (relative to the artifact) to its main file. `agentworks doctor` checks it exists. |
| `eval_runner` | string |  | Shell command `agentworks eval` pipes each case's prompt to; its stdout is what the assertions check. Falls back to the project's `eval.default_runner`. |
| `eval_protocol` | string |  | How the eval runner reports a response: `text` (stdout is the response) or `json` (one JSON object that also reports tool calls and activations, needed by the tool and trigger assertions). Falls back to the project's `eval.protocol`. |
| `judge_runner` | string |  | Shell command that grades `rubric` assertions: reads a JSON request on stdin, prints a JSON verdict. Falls back to the project's `eval.judge_runner`. |
| `source` | object |  | Provenance written by `agentworks add` (where an imported artifact came from). Not meant to be edited by hand. |
| `targets` | list of strings |  | *Deprecated:* targets are set once in agentworks.yaml, not per artifact. Ignored. Targets live in `agentworks.yaml`. |
| `license` | string |  | SPDX license identifier, exported into the skill's `SKILL.md`. |
| `compatibility` | string |  | Environment requirements, exported into the skill's `SKILL.md`. |

## mcp

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `kind` | string | yes | The artifact kind: `agent`, `skill`, `mcp`, or `hook`. Must match the file name (`<kind>.md`). |
| `name` | string | yes | Lowercase letters, digits, and hyphens. Must match the directory name. |
| `description` | string | yes | What the artifact does and when to use it. This is how an agent decides whether to use it, so it is linted for quality. Keep it under 1024 characters. |
| `version` | string |  | The artifact's own version (default `0.1.0`). |
| `namespace` | string |  | Scopes the artifact under a team or origin so two artifacts can share a name. Files it at `<kind-dir>/<namespace>/<name>/`. |
| `test` | string |  | Shell command `agentworks test` runs from the artifact's directory. |
| `build` | string |  | Shell command `agentworks build` runs from the artifact's directory. `agentworks export` runs it first. |
| `entrypoint` | string |  | Path (relative to the artifact) to its main file. `agentworks doctor` checks it exists. |
| `eval_runner` | string |  | Shell command `agentworks eval` pipes each case's prompt to; its stdout is what the assertions check. Falls back to the project's `eval.default_runner`. |
| `eval_protocol` | string |  | How the eval runner reports a response: `text` (stdout is the response) or `json` (one JSON object that also reports tool calls and activations, needed by the tool and trigger assertions). Falls back to the project's `eval.protocol`. |
| `judge_runner` | string |  | Shell command that grades `rubric` assertions: reads a JSON request on stdin, prints a JSON verdict. Falls back to the project's `eval.judge_runner`. |
| `source` | object |  | Provenance written by `agentworks add` (where an imported artifact came from). Not meant to be edited by hand. |
| `targets` | list of strings |  | *Deprecated:* targets are set once in agentworks.yaml, not per artifact. Ignored. Targets live in `agentworks.yaml`. |
| `transport` | string |  | `stdio` (default, a local process), `http` (streamable HTTP), or `sse`. |
| `command` | string |  | Shell command. For an mcp server, what starts it (stdio only; run via `sh -c`, or exec'd directly when `args` is set). For a hook, shorthand for a single handler command together with `events`. |
| `args` | list of strings |  | Arguments to `command`. When set, `command` is run directly rather than through a shell. |
| `env` | map of strings |  | Non-secret environment variables for the server. Never put a secret here. |
| `auth` | list of strings |  | Names of secret environment variables the server needs. Exported as `${VAR}` references, never as values. |
| `url` | string |  | The endpoint of a remote (`http` or `sse`) server. |
| `headers` | map of strings |  | Headers for a remote server. A sensitive header (Authorization, or anything with token/key/secret/password) must reference `${VAR}` rather than carry a literal. |

## hook

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `kind` | string | yes | The artifact kind: `agent`, `skill`, `mcp`, or `hook`. Must match the file name (`<kind>.md`). |
| `name` | string | yes | Lowercase letters, digits, and hyphens. Must match the directory name. |
| `description` | string | yes | What the artifact does and when to use it. This is how an agent decides whether to use it, so it is linted for quality. Keep it under 1024 characters. |
| `version` | string |  | The artifact's own version (default `0.1.0`). |
| `namespace` | string |  | Scopes the artifact under a team or origin so two artifacts can share a name. Files it at `<kind-dir>/<namespace>/<name>/`. |
| `test` | string |  | Shell command `agentworks test` runs from the artifact's directory. |
| `build` | string |  | Shell command `agentworks build` runs from the artifact's directory. `agentworks export` runs it first. |
| `entrypoint` | string |  | Path (relative to the artifact) to its main file. `agentworks doctor` checks it exists. |
| `eval_runner` | string |  | Shell command `agentworks eval` pipes each case's prompt to; its stdout is what the assertions check. Falls back to the project's `eval.default_runner`. |
| `eval_protocol` | string |  | How the eval runner reports a response: `text` (stdout is the response) or `json` (one JSON object that also reports tool calls and activations, needed by the tool and trigger assertions). Falls back to the project's `eval.protocol`. |
| `judge_runner` | string |  | Shell command that grades `rubric` assertions: reads a JSON request on stdin, prints a JSON verdict. Falls back to the project's `eval.judge_runner`. |
| `source` | object |  | Provenance written by `agentworks add` (where an imported artifact came from). Not meant to be edited by hand. |
| `targets` | list of strings |  | *Deprecated:* targets are set once in agentworks.yaml, not per artifact. Ignored. Targets live in `agentworks.yaml`. |
| `command` | string |  | Shell command. For an mcp server, what starts it (stdio only; run via `sh -c`, or exec'd directly when `args` is set). For a hook, shorthand for a single handler command together with `events`. |
| `events` | list of strings |  | Shorthand: the lifecycle events that trigger `command`. Use the target vendor's own event names. |
| `handlers` | list of objects |  | Explicit handlers: a list of `{event, matcher, command, timeout}`. `matcher` filters the event (a regex for tool events on most vendors); `timeout` is seconds. Use either this or the `events` + `command` shorthand, not both. A command may use `${ARTIFACT_DIR}` for the directory holding the hook's own files (a bundled script); claude-code ships them and resolves the path, and other targets skip such a hook with a warning. |
