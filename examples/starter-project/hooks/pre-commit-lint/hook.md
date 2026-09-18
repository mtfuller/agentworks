---
kind: hook
name: pre-commit-lint
description: Lint staged files before commit.
version: 0.1.0
command: gofmt -l .
events:
  - pre-commit
---

# Pre Commit Lint

Runs before a commit and fails it if any staged Go file isn't
`gofmt`-formatted. Swap `command:` for whatever your project's own
formatter/linter is -- this is just an example.
