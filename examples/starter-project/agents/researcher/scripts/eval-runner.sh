#!/bin/sh
# Fake stand-in for a real model call, so this example runs (in CI or
# locally) without network access or an API key -- the same "simulate
# rather than call the real thing" approach tools/jira-fetch's
# tests/test_main.py uses for its API. Point a real project's eval_runner
# at something that actually calls a model to get a real behavior test.
cat >/dev/null
cat <<'EOF'
Sources agree that primary documentation is more reliable than blog
summaries. One source (vendor-blog.example) claims a 2x speedup; the
original benchmark report doesn't confirm that figure, so it's flagged as
unconfirmed rather than repeated as fact.
EOF
