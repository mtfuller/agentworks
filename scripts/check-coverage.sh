#!/usr/bin/env bash
# Fails if any package listed in scripts/coverage-floors.txt is below its floor.
# Usage: scripts/check-coverage.sh [floors-file]
set -uo pipefail

cd "$(dirname "$0")/.."
floors_file="${1:-scripts/coverage-floors.txt}"
failed=0

printf '%-40s %8s %8s\n' PACKAGE COVERAGE FLOOR
while read -r pkg floor _; do
  case "$pkg" in ''|'#'*) continue ;; esac

  if ! out=$(go test -cover "$pkg" 2>&1); then
    echo "FAIL  $pkg: tests failed"
    echo "$out"
    failed=1
    continue
  fi
  pct=$(printf '%s\n' "$out" | sed -n 's/.*coverage: \([0-9.]*\)%.*/\1/p' | tail -1)
  if [ -z "$pct" ]; then
    echo "FAIL  $pkg: no coverage figure in the output"
    failed=1
    continue
  fi

  status=ok
  if awk -v p="$pct" -v f="$floor" 'BEGIN { exit !(p + 0 < f + 0) }'; then
    status="BELOW FLOOR"
    failed=1
  fi
  printf '%-40s %7s%% %7s%%  %s\n' "$pkg" "$pct" "$floor" "$status"
done < "$floors_file"

exit "$failed"
