#!/usr/bin/env bash
#
# capture.sh — render every Cairn profile into a byte-reproducible tree.
#
# The tree under testdata/goldens/trees is a golden fixture: it is what the
# nine profiles render to, byte for byte, at the commit that captured it. A
# change that alters rendering shows up as a diff in that tree, which is the
# point — verify.sh re-captures into a scratch root and diffs.
#
# Everything the render can reach is staged into one throwaway fixture root and
# every non-deterministic input is pinned:
#
#   $FIX/home    a fixture HOME holding templates/ and skills/, staged the same
#                way `make install-system` stages the real ~/.config/agents
#   $FIX/scope   a git repo built from literals, so the git slots print the
#                same two blocks on every machine
#   $FIX/cairn.db  a store seeded from agent-setup/profiles/*.md
#
# and the session segment is pinned to "golden", which removes the only
# non-determinism inside cairn itself (bootdir.NewSession's timestamp + random
# suffix, used only when --session is empty).
#
# usage:
#   capture.sh [--out <dir>] [--force]
#
#   --out <dir>   where the tree goes; defaults to <repo>/testdata/goldens/trees
#   --force       clear a non-empty --out first
#
# environment:
#   AGENT_SETUP   the agent-setup checkout; defaults to ~/dev/projects/agent-setup
#   CAIRN         the cairn binary; built from this repo when unset

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
REPO=$(cd "$SCRIPT_DIR/../.." && pwd -P)

# The real HOME, read before anything shadows it. AGENT_SETUP defaults off it.
REAL_HOME=$HOME
AGENT_SETUP=${AGENT_SETUP:-$REAL_HOME/dev/projects/agent-setup}

OUT=$SCRIPT_DIR/trees
FORCE=0
while [ $# -gt 0 ]; do
	case $1 in
	--out)
		[ $# -ge 2 ] || { echo "capture.sh: --out needs a directory" >&2; exit 2; }
		OUT=$2
		shift 2
		;;
	--out=*)
		OUT=${1#--out=}
		shift
		;;
	--force)
		FORCE=1
		shift
		;;
	-h | --help)
		sed -n '2,32p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "capture.sh: unknown argument $1" >&2
		exit 2
		;;
	esac
done

for tool in rsync git sqlite3 python3; do
	command -v "$tool" >/dev/null 2>&1 || { echo "capture.sh: $tool is not on PATH" >&2; exit 1; }
done
[ -d "$AGENT_SETUP/profiles" ] || {
	echo "capture.sh: no profiles under $AGENT_SETUP — set AGENT_SETUP" >&2
	exit 1
}

# --- the output root -------------------------------------------------------
#
# `cairn boot` refuses to plant over an existing directory (bootdir.ErrExists),
# so the out dir has to be fresh. Refuse to clear one the operator did not
# name for clearing.
if [ -e "$OUT" ]; then
	if [ -n "$(ls -A "$OUT" 2>/dev/null)" ]; then
		if [ "$FORCE" -eq 1 ]; then
			rm -rf "$OUT"
		else
			echo "capture.sh: $OUT exists and is not empty; pass --force to clear it" >&2
			exit 1
		fi
	fi
fi
mkdir -p "$OUT"
# Canonicalized so that the token substitution below sees the same spelling the
# render wrote. On macOS /tmp and $TMPDIR are symlinks, and a path that reaches
# a golden through one form while the token is the other form is a false diff.
OUT=$(cd "$OUT" && pwd -P)

# --- the fixture root ------------------------------------------------------
FIX=$(cd "$(mktemp -d)" && pwd -P)
cleanup() { rm -rf "$FIX"; }
trap cleanup EXIT

# 1. the fixture home. Same flags `make install-system` uses in agent-setup, so
#    what the render reads is what the real installed location holds.
mkdir -p "$FIX/home/.config/agents"
rsync -a --delete --delete-excluded --exclude='.DS_Store' \
	"$AGENT_SETUP/templates/" "$FIX/home/.config/agents/templates/"
rsync -a --delete --delete-excluded --exclude='.DS_Store' \
	"$AGENT_SETUP/skills/" "$FIX/home/.config/agents/skills/"

# 2. the fixture scope: a git repo whose every input is a literal.
#
#    GIT_CONFIG_GLOBAL/SYSTEM are nulled on every invocation so the operator's
#    own gitconfig — hooks path, commit.gpgsign, commit template, core.abbrev,
#    init.defaultBranch — cannot reach the fixture. The author and committer
#    identity and dates are literals so the abbreviated hash `git log --oneline`
#    prints is stable, and core.abbrev is pinned rather than left to git's
#    object-count heuristic. There is no remote, so `git status --short
#    --branch` prints the branch line and nothing else.
mkdir -p "$FIX/scope"
fixgit() {
	env GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 \
		GIT_AUTHOR_NAME='Golden Fixture' GIT_AUTHOR_EMAIL='golden@fixture.invalid' \
		GIT_AUTHOR_DATE='2020-01-01T00:00:00+0000' \
		GIT_COMMITTER_NAME='Golden Fixture' GIT_COMMITTER_EMAIL='golden@fixture.invalid' \
		GIT_COMMITTER_DATE='2020-01-01T00:00:00+0000' \
		TZ=UTC LC_ALL=C \
		git -C "$FIX/scope" "$@"
}
fixgit init -q -b main
fixgit config user.name 'Golden Fixture'
fixgit config user.email 'golden@fixture.invalid'
fixgit config core.abbrev 7
fixgit config commit.gpgsign false
printf 'The scope a golden capture renders against.\n' >"$FIX/scope/README.md"
fixgit add README.md
fixgit commit -q -m 'Seed the golden fixture scope'

# 3. the store, seeded from the profile files. seed.py needs an absolute --db:
#    it makedirs os.path.dirname(--db), which a bare filename makes empty.
"$AGENT_SETUP/bin/seed.py" --db "$FIX/cairn.db" >/dev/null

# --- the environment every render runs under -------------------------------
#
# Built from nothing rather than inherited, so the operator's environment is
# not an input. PATH survives because the git and sqlite3 slots need it.
#
# CAIRN_DB is exported and not merely passed as --db: the conductor's `fleet`
# slot is a shell command that reads ${CAIRN_DB:-$HOME/.config/agents/cairn.db}
# out of the environment, and cairn's --db flag never reaches it.
render_env=(env -i
	PATH="$PATH"
	HOME="$FIX/home"
	CAIRN_DB="$FIX/cairn.db"
	TZ=UTC
	LC_ALL=C)

# --- the binary ------------------------------------------------------------
#
# Built from this working tree unless CAIRN names one. Every task from here on
# runs verify.sh to prove its own change did not alter rendering, so the
# harness has to exercise the source in front of it — not a stale cairn that
# happens to be on $PATH.
if [ -n "${CAIRN:-}" ]; then
	CAIRN_BIN=$CAIRN
else
	CAIRN_BIN=$FIX/bin/cairn
	mkdir -p "$FIX/bin"
	(cd "$REPO" && go build -o "$CAIRN_BIN" ./cmd/cairn)
fi

# --- the nine renders ------------------------------------------------------
mkdir -p "$OUT/stdout" "$OUT/stderr"

# Eight concrete profiles, in sorted order.
BOOTS=(architect conductor director engineer orchestrator planner reviewer writer)

fail() {
	local id=$1
	echo "capture.sh: rendering $id failed" >&2
	echo "--- stderr ---" >&2
	cat "$OUT/stderr/$id.txt" >&2 || true
	exit 1
}

for id in "${BOOTS[@]}"; do
	"${render_env[@]}" "$CAIRN_BIN" boot "$id" \
		--db "$FIX/cairn.db" \
		--scope "$FIX/scope" \
		--session golden \
		--boot-root "$OUT/boot" \
		>"$OUT/stdout/$id.txt" 2>"$OUT/stderr/$id.txt" || fail "$id"
done

# base is abstract: true, and `cairn boot` refuses an abstract profile by
# design. The installed layer is the only way to render it, and rendering it is
# the point — it is where the templates, the settings and the skill set live.
#
# Unlike a boot, an install wants its root to exist already: install.NewRoot
# validates the shape and Root.Check requires the directory.
mkdir -p "$OUT/install/base"
"${render_env[@]}" "$CAIRN_BIN" install base \
	--db "$FIX/cairn.db" \
	--root "$OUT/install/base" \
	>"$OUT/stdout/base.txt" 2>"$OUT/stderr/base.txt" || fail base

# --- tokenize --------------------------------------------------------------
#
# Both roots are per-run paths: $FIX is a mktemp directory and $OUT differs
# between the capture that wrote the golden and every verification run after
# it. Left raw, either one guarantees a false diff.
#
# Rewritten in place through the same inode so the mode survives — a planted
# skill's executable bit is load-bearing. The longer path is replaced first, so
# that one being a prefix of the other cannot leave a half-substituted path.
FIX="$FIX" OUT="$OUT" python3 - "$OUT" <<'PY'
import os, sys

root = sys.argv[1]
subs = [(os.environ["FIX"].encode(), b"@FIXTURE@"),
        (os.environ["OUT"].encode(), b"@OUT@")]
subs.sort(key=lambda p: len(p[0]), reverse=True)

for dirpath, dirnames, filenames in os.walk(root):
    dirnames.sort()
    for name in sorted(filenames):
        path = os.path.join(dirpath, name)
        if os.path.islink(path) or not os.path.isfile(path):
            continue
        with open(path, "rb") as fh:
            data = fh.read()
        out = data
        for needle, token in subs:
            out = out.replace(needle, token)
        if out == data:
            continue
        # r+b, not a rename: rewriting through the existing inode keeps the
        # file's mode.
        with open(path, "r+b") as fh:
            fh.write(out)
            fh.truncate()
PY

# --- sort stderr -----------------------------------------------------------
#
# Slot failures are reported in manifest declaration order, which the keyed
# merge deliberately makes meaningless — it is not a contract, so pinning it
# would freeze a detail the design says is free. Sorted after tokenization, so
# the order does not depend on the fixture's per-run path. stdout is left
# alone: it is the planted path, and its order is the argument order above.
for f in "$OUT"/stderr/*.txt; do
	# Through the same inode, for the reason the tokenizer does it: `sort -o`
	# renames a fresh 0600 temp file over the target and loses the mode.
	LC_ALL=C sort "$f" >"$f.sorted"
	cat "$f.sorted" >"$f"
	rm -f "$f.sorted"
done

# --- refuse a degraded capture ---------------------------------------------
#
# A golden that records a slot which failed to resolve, or a declared slot that
# filled nothing, is a golden of a broken render — and the damage is that it
# reads exactly like a golden of a working one. A section that is missing
# because its slot failed is indistinguishable, in a captured tree, from a
# section the profile never declared.
#
# The conductor's `fleet` slot is the case that motivated this. It is a cmd slot
# reading ${CAIRN_DB:-$HOME/.config/agents/cairn.db} out of the environment,
# `cairn boot --db <path>` does not export CAIRN_DB, and sqlite3's immutable=1
# on an absent path opens an EMPTY database rather than erroring. So the query
# fails with "no such table", the section renders nothing, and the boot still
# exits 0. The export above is what prevents it; this is what catches it if the
# export is ever lost.
#
# Both patterns are cmd/cairn/main.go's own: reportSlotFailures and
# reportUnfilledMarkers. Anything else on stderr is printed and does not fail —
# the installed layer's "renders no section for" line is a fact about which
# kinds install resolves, not a fault.
degraded=$(grep -lE 'slot "[^"]*" did not resolve|slot "[^"]*" filled nothing' \
	"$OUT"/stderr/*.txt 2>/dev/null || true)
if [ -n "$degraded" ]; then
	echo "capture.sh: refusing to capture a degraded render" >&2
	for f in $degraded; do
		echo "  $(basename "$f" .txt):" >&2
		grep -E 'did not resolve|filled nothing' "$f" | sed 's/^/    /' >&2
	done
	echo "" >&2
	echo "A golden recording a failed or unfilled slot looks like a golden of a" >&2
	echo "profile that never declared it. Fix the render; do not capture this." >&2
	exit 1
fi

# Whatever else reached stderr is surfaced rather than silently baselined. It is
# still captured — it is part of the golden — but an operator re-capturing sees
# it rather than discovering it in a diff later.
for f in "$OUT"/stderr/*.txt; do
	[ -s "$f" ] || continue
	echo "capture.sh: $(basename "$f" .txt) wrote to stderr:" >&2
	sed 's/^/  /' "$f" >&2
done

files=$(find "$OUT" -type f | wc -l | tr -d ' ')
bytes=$(find "$OUT" -type f -exec cat {} + | wc -c | tr -d ' ')
echo "captured ${#BOOTS[@]} boots + 1 installed layer into $OUT ($files files, $bytes bytes)"
