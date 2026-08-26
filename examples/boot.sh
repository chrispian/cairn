#!/usr/bin/env bash
# Boot a Cairn binding and open a session in it.
#
#   ./examples/boot.sh eng                    # boot and launch
#   ./examples/boot.sh eng --scope ~/dev/x    # override the binding's scope
#   PRINT_ONLY=1 ./examples/boot.sh eng       # print the dir, launch nothing
#
# Cairn writes the directory and prints its path. Launching is this script's
# job, not Cairn's — which is the whole point of the seam.
set -euo pipefail

[ $# -ge 1 ] || { echo "usage: $0 <binding|profile> [cairn boot flags...]" >&2; exit 64; }

TARGET="$1"; shift
CAIRN="${CAIRN:-cairn}"

# stdout is the path, stderr is diagnostics — so capture one and let the other
# through to the terminal. A slot that failed to resolve reports here and the
# boot still succeeds.
BOOT_DIR="$("$CAIRN" boot "$TARGET" "$@")"

echo "boot dir: $BOOT_DIR" >&2
[ -n "${PRINT_ONLY:-}" ] && { echo "$BOOT_DIR"; exit 0; }

# The scope is what the agent should be able to reach beyond its boot dir.
# Cairn renders it into AGENTS.md; the launcher grants it.
SCOPE="$(sed -n 's/^- scope: //p' "$BOOT_DIR/AGENTS.md" | head -1)"

cd "$BOOT_DIR"
if [ -n "$SCOPE" ]; then
  exec claude --add-dir "$SCOPE"
else
  exec claude
fi
