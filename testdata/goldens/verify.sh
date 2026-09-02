#!/usr/bin/env bash
#
# verify.sh — re-capture the golden tree into a scratch root and diff.
#
# Exit 0 means rendering is unchanged. Exit 1 prints the diff: either the
# change under review altered what a profile renders, or the fixture the
# harness stages has moved out from under it. Both are worth reading.
#
# environment:
#   AGENT_SETUP   passed through to capture.sh when set
#   CAIRN         passed through to capture.sh when set

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
GOLDEN=$SCRIPT_DIR/trees

if [ ! -d "$GOLDEN" ]; then
	echo "verify.sh: no golden tree at $GOLDEN — run capture.sh first" >&2
	exit 1
fi

TMP=$(cd "$(mktemp -d)" && pwd -P)
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

"$SCRIPT_DIR/capture.sh" --out "$TMP/trees" >"$TMP/capture.log" 2>&1 || {
	echo "verify.sh: capture failed" >&2
	cat "$TMP/capture.log" >&2
	exit 1
}

if diff -r "$GOLDEN" "$TMP/trees" >"$TMP/diff.txt" 2>&1; then
	echo "goldens match"
	exit 0
fi

echo "verify.sh: the render no longer matches $GOLDEN" >&2
cat "$TMP/diff.txt" >&2
exit 1
