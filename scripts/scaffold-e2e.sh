#!/usr/bin/env bash
# End-to-end check that every MCP scaffold and the Node skill scaffolds actually
# work: scaffold each into a fresh project, install and build, run its tests
# (or the built-in MCP smoke test), validate --strict, and export it. Needs
# network (npm install) plus python3, node (22+), and npm.
#
# Usage: scripts/scaffold-e2e.sh [path-to-agentworks-binary]
set -euo pipefail

BIN="$(cd "$(dirname "${1:-agentworks}")" && pwd)/$(basename "${1:-agentworks}")"
[ -x "$BIN" ] || BIN="$(command -v "${1:-agentworks}")"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$work"

"$BIN" init proj --target claude-code --target github-copilot >/dev/null
cd proj

# Every artifact needs a description distinct enough from the others to pass the
# overlap lint under `validate --strict`.
mcp() { "$BIN" new mcp "$1" ${2:+--from-template "$2"} --description "$3" >/dev/null; }
skill() { "$BIN" new skill "$1" --from-template "$2" --description "$3" >/dev/null; }

# MCP servers: the default scaffold and each template that ships code.
mcp e2e-default "" "Greets callers by name through a dependency free Python protocol loop."
mcp e2e-api api-wrapper "Fetches paths from a configured REST endpoint using a bearer token."
mcp e2e-cli cli-wrapper "Runs one allow listed command line program and returns its captured output."
mcp e2e-node node-mcp "Answers protocol requests from a plain JavaScript module without packages."
mcp e2e-ts node-ts-mcp "Compiles typed source into a single bundled file that speaks the protocol."
# Templates that are pure configuration still have to validate and export.
mcp e2e-npx npx-wrapper "Registers a published package launched through the node package runner."
mcp e2e-remote remote-http "Points at a hosted endpoint reached over the network with headers."

# Node skills.
skill e2e-node-skill node-skill "Summarizes spreadsheets by grouping rows and counting distinct values."
skill e2e-ts-skill node-ts-skill "Rewrites meeting notes into short action lists with named owners."

echo "== build"
"$BIN" build

echo "== test"
"$BIN" test

echo "== validate --strict"
"$BIN" validate --strict

echo "== export"
"$BIN" export --out "$work/dist" >/dev/null

# The built TypeScript server must actually answer the MCP handshake.
echo "== run the bundled TypeScript server"
reply="$(printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  | node mcp/e2e-ts/dist/index.js | head -1)"
case "$reply" in
  *'"serverInfo"'*) ;;
  *) echo "the bundled server did not answer initialize: $reply" >&2; exit 1 ;;
esac

echo "scaffold end-to-end check passed"
