#!/usr/bin/env bash
# Boot a Cairn binding and open a session in it.
#
#   ./examples/boot.sh eng                    # boot and launch
#   ./examples/boot.sh eng --scope ~/dev/x    # override the binding's scope
#   PRINT_ONLY=1 ./examples/boot.sh eng       # print the dir, launch nothing
#
# Cairn writes the directory and prints a description of it. Launching is this
# script's job, not Cairn's — which is the whole point of the seam.
#
# It reads `cairn boot --json`, a data contract Cairn owns, rather than the
# files inside the boot directory. An earlier revision pulled the scope out of
# the rendered AGENTS.md with sed; that worked, and it worked by luck — the
# scrape depended on a marker's position in a document whose whole purpose is to
# be re-authored, and one template edit would have returned an empty scope with
# no error, which is a launcher granting nothing and saying so nowhere.
#
# The parser is jq, which is a dependency this script did not previously have.
# That is the trade: a JSON parser for a document with named keys, in place of a
# regular expression over prose.
set -euo pipefail

[ $# -ge 1 ] || { echo "usage: $0 <binding|profile> [cairn boot flags...]" >&2; exit 64; }
command -v jq >/dev/null 2>&1 || {
	echo "$0: jq is required to read \`cairn boot --json\`" >&2; exit 69; }

TARGET="$1"; shift
CAIRN="${CAIRN:-cairn}"

# stdout is the JSON object, stderr is diagnostics — so capture one and let the
# other through to the terminal. A slot that failed to resolve reports here and
# the boot still succeeds.
BOOT="$("$CAIRN" boot "$TARGET" "$@" --json)"

# `// empty` on every read: an absent value is null, and `jq -r` would otherwise
# hand back the four characters "null" as if they were a path.
BOOT_DIR="$(jq -r '.boot_dir // empty' <<<"$BOOT")"
[ -n "$BOOT_DIR" ] || { echo "$0: cairn reported no boot directory" >&2; exit 70; }

echo "boot dir: $BOOT_DIR" >&2
[ -n "${PRINT_ONLY:-}" ] && { echo "$BOOT_DIR"; exit 0; }

# --settings promotes the planted file from `projectSettings` to `flagSettings`.
# That matters twice over.
#
# It is what makes `permissions.defaultMode: auto` stick: a settings file simply
# sitting in the boot dir is read as `projectSettings`, the untrusted tier, and
# Claude Code 2.1.246 refuses to honour it from there —
#
#   settings defaultMode "auto" ignored — only policy/user/flag settings may
#   grant auto mode (projectSettings and localSettings are repo-controllable)
#
# — warns, and falls back. The same rule applies to `autoMode` classifier rules.
#
# It is also the whole access grant. Cairn writes the scope, and everything
# `spec.access.directories` names, into `permissions.additionalDirectories` in
# this file; a session launched with --settings and no --add-dir reads its scope
# with no prompt and no refusal. So this script passes no project-dir flag at
# all — `project_dir_arg` is in the JSON for a launcher whose provider grants
# directories on the command line rather than in a config file, which Claude
# Code is not.
#
# That the grant is never lost is an invariant and not a hope: the scope is
# itself one of the granted directories, and one directory to grant is enough
# on its own to render the file. So a non-null `scope` guarantees a non-null
# `settings_path`. The reverse does not hold — a profile can grant a directory
# it is not scoped to — which is why this reads the key rather than inferring
# it from the scope.
#
# Passing the path here is the launcher's job, not cairn's: cairn writes the
# file and describes it, and whoever owns the invocation decides what tier it
# lands in.
SETTINGS="$(jq -r '.settings_path // empty' <<<"$BOOT")"

set --
[ -n "$SETTINGS" ] && set -- "$@" --settings "$SETTINGS"

cd "$BOOT_DIR"
exec claude "$@"
