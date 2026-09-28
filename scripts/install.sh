#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
SOURCE="$SCRIPT_DIR/agentworks"
DESTINATION="${AGENTWORKS_INSTALL_DIR:-$HOME/.local/bin}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --source) SOURCE=$2; shift 2 ;;
    --dir) DESTINATION=$2; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ ! -f "$SOURCE" ]; then
  echo "agentworks binary not found: $SOURCE" >&2
  exit 1
fi

mkdir -p "$DESTINATION"
TEMPORARY="$DESTINATION/.agentworks-install-$$"
trap 'rm -f "$TEMPORARY"' EXIT HUP INT TERM
cp "$SOURCE" "$TEMPORARY"
chmod 755 "$TEMPORARY"
mv -f "$TEMPORARY" "$DESTINATION/agentworks"
trap - EXIT HUP INT TERM

echo "Installed AgentWorks to $DESTINATION/agentworks"
case ":${PATH:-}:" in
  *:"$DESTINATION":*) ;;
  *) echo "Add $DESTINATION to PATH to run agentworks." ;;
esac
