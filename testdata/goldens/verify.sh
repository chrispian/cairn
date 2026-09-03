#!/usr/bin/env bash
#
# verify.sh — re-capture the golden tree into a scratch root and diff.
#
# Exit 0 means rendering is unchanged. Exit 1 prints the diff: either the
# change under review altered what a profile renders, or the fixture the
# harness stages has moved out from under it. Both are worth reading — and
# this says which one it is, rather than leaving the reader to work it out.
#
# environment:
#   AGENT_SETUP   passed through to capture.sh when set, and read here to
#                 attribute a diff
#   CAIRN         passed through to capture.sh when set

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
GOLDEN=$SCRIPT_DIR/trees
RECORD=$SCRIPT_DIR/agent-setup.commit
AGENT_SETUP=${AGENT_SETUP:-$HOME/dev/projects/agent-setup}

# head_of prints the HEAD of the git checkout rooted at $1, and fails for
# anything that is not one.
#
# The toplevel check is not redundant. `git -C <dir> rev-parse HEAD` walks UP to
# an enclosing repository, so an rsynced copy of agent-setup sitting anywhere
# inside another checkout would be attributed to THAT repository's HEAD — a sha
# with nothing to do with the profiles, reported as confidently as a real one.
# Requiring the toplevel to be the directory itself is what keeps "cannot be
# attributed" from quietly becoming a wrong answer.
head_of() {
	local top
	top=$(git -C "$1" rev-parse --show-toplevel 2>/dev/null) || return 1
	[ "$(cd "$top" 2>/dev/null && pwd -P)" = "$(cd "$1" 2>/dev/null && pwd -P)" ] || return 1
	git -C "$1" rev-parse HEAD 2>/dev/null
}

# short abbreviates a sha for reading, and leaves anything else alone.
short() {
	case $1 in
	[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]*) printf '%.7s' "$1" ;;
	*) printf '%s' "$1" ;;
	esac
}

# attribute says which of the two cases above this run is in.
#
# It reports and never decides: nothing here touches the exit status, because a
# diff is a diff and the operator still reads it. What it removes is the step
# where they first have to establish whether the diff is even theirs.
#
# That step is not free. On 2026-09-03 it cost a full diagnosis cycle twice in
# one day, both times on a pristine tree, because agent-setup had moved and a
# legitimate upstream move reads exactly like "you broke rendering".
#
# Every branch says how confident it is. "Cannot be attributed" is a real
# answer here and is always preferred to a plausible one: a wrong attribution
# is worse than the silence this replaced, because silence at least prompts
# somebody to look.
attribute() {
	local recorded sha current

	if [ ! -f "$RECORD" ]; then
		echo "  this diff cannot be attributed: no capture record at" >&2
		echo "    $RECORD" >&2
		echo "  Re-capture to write one." >&2
		return
	fi

	# Redirected and `|| true` because this runs under `set -euo pipefail`:
	# grep exits 1 when it selects nothing and sed exits 2 on a file it cannot
	# read, and either would kill verify.sh right here — after it announced a
	# diff and before it printed one, which is worse than saying nothing.
	recorded=$(sed -n '/^[^#]/p' "$RECORD" 2>/dev/null | tr -d '[:space:]' || true)
	if [ -z "$recorded" ]; then
		echo "  this diff cannot be attributed: the record names no commit." >&2
		echo "  Re-capture to write one." >&2
		return
	fi

	# A record holds one token: 40 hex characters, optionally -dirty, or the
	# literal "unknown". Checking that is what stops two lines in the file from
	# being concatenated by tr into an 80-character string that short() then
	# truncates back into something plausible — which reads as
	# "moved since capture (d710775 -> d710775)", a message that contradicts
	# itself and is worse than no message.
	sha=${recorded%-dirty}
	if [ "$recorded" != unknown ] &&
		{ [ ${#sha} -ne 40 ] || [ -n "${sha//[0-9a-f]/}" ]; }; then
		echo "  this diff cannot be attributed: the record is malformed." >&2
		echo "  It should hold one commit id; re-capture to rewrite it." >&2
		return
	fi

	if [ "$recorded" = unknown ]; then
		echo "  this diff cannot be attributed: the trees were captured from a" >&2
		echo "  directory that was not a git checkout." >&2
		return
	fi

	if ! current=$(head_of "$AGENT_SETUP"); then
		echo "  this diff cannot be attributed: not a git checkout —" >&2
		echo "    $AGENT_SETUP" >&2
		echo "  The trees were captured from $(short "$sha")." >&2
		return
	fi

	if [ -n "$(git -C "$AGENT_SETUP" status --porcelain 2>/dev/null)" ]; then
		echo "  uncommitted changes in —" >&2
		echo "    $AGENT_SETUP" >&2
		echo "  so its HEAD ($(short "$current")) does not describe what was just" >&2
		echo "  captured, and this diff may be neither yours nor any commit's." >&2
		git -C "$AGENT_SETUP" status --porcelain 2>/dev/null | sed 's/^/    /' >&2
		return
	fi

	case $recorded in
	*-dirty)
		# Deliberately compares nothing. A capture taken from an uncommitted
		# tree describes no commit, so whether its sha matches HEAD says
		# nothing about whether these trees can be reproduced. Re-capture is
		# the answer either way.
		echo "  the trees were captured from a tree with uncommitted edits" >&2
		echo "  ($(short "$sha")-dirty), so they describe no commit. Re-capture" >&2
		echo "  from a clean checkout before reading this diff." >&2
		return
		;;
	esac

	if [ "$sha" != "$current" ]; then
		echo "  agent-setup moved since capture ($(short "$sha") -> $(short "$current"))." >&2
		echo "  This is a stale fixture, not your change: re-baseline with capture.sh." >&2
		echo "  Read the diff first — additions are upstream gaining something, and a" >&2
		echo "  removal should be paired with an addition at the same key. An unpaired" >&2
		echo "  removal means cairn stopped rendering something, which is yours." >&2
		return
	fi
	echo "  agent-setup is unchanged since capture ($(short "$sha")), so this" >&2
	echo "  diff is your change." >&2
}

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
	# A pass taken against an uncommitted upstream is still a pass, but it is
	# not one anybody can reproduce — so it is worth one line, and no more.
	if [ -n "$(git -C "$AGENT_SETUP" status --porcelain 2>/dev/null)" ]; then
		echo "verify.sh: note — $AGENT_SETUP has uncommitted changes, so this" >&2
		echo "  match is against a working tree rather than a commit." >&2
	fi
	exit 0
fi

echo "verify.sh: the render no longer matches $GOLDEN" >&2
attribute
echo "" >&2
cat "$TMP/diff.txt" >&2
exit 1
