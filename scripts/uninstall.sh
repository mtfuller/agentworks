#!/bin/sh
set -eu

DESTINATION="${AGENTWORKS_INSTALL_DIR:-$HOME/.local/bin}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --dir) DESTINATION=$2; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

TARGET="$DESTINATION/agentworks"
if [ ! -e "$TARGET" ]; then
  echo "AgentWorks is not installed at $TARGET"
  exit 0
fi
rm -f "$TARGET"
echo "Removed $TARGET"
echo "Project definitions and local runtime data were left in place."
