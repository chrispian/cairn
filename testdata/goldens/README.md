# Golden fixtures

`trees/` is what Cairn's nine profiles render to, byte for byte. A change that
alters rendering shows up as a diff there — which is the point.

    testdata/goldens/verify.sh        # re-render, diff against trees/, exit 1 on drift
    testdata/goldens/capture.sh       # re-render into trees/ (accept the new bytes)

`testdata` is the directory name on purpose: the Go toolchain ignores it, so
none of this reaches a build or a test binary.

## These goldens are not hermetic

**They prove byte-identity on this machine, against one working tree, and they
prove nothing anywhere else.** `capture.sh` reads `~/dev/projects/agent-setup`
live — the checkout as it stands, not a pinned revision — `rsync`s its
`templates/` and `skills/` into the fixture home and seeds the fixture store by
running its `bin/seed.py` over `profiles/*.md`. `verify.sh` re-runs
`capture.sh`, so it inherits all of it. `AGENT_SETUP` moves the path; nothing
moves the dependency.

The consequences are worth naming, because this is the proof the whole
migration rests on:

- On a machine without `agent-setup`, `verify.sh` cannot run at all — it exits
  with `no profiles under …`, not with a pass.
- In CI, likewise. **These goldens are not a CI check** and adding them to one
  would need the profiles vendored or pinned first.
- When `agent-setup` moves on, `verify.sh` reports a diff that is about the
  profiles rather than about cairn. Read the diff before believing it is a
  regression here.

Everything cairn controls *is* pinned — see the table below. The profiles,
templates and skills are the one input that lives somewhere else, and the
goldens are as strong as that checkout is stable.

## What is captured

Eight boots — `architect`, `conductor`, `director`, `engineer`, `orchestrator`,
`planner`, `reviewer`, `writer` — each planted at `boot/<id>/golden`, plus one
installed layer for `base` at `install/base`. `base` is `abstract: true` and
`cairn boot` refuses an abstract profile by design, so the installed layer is
the only way to render it.

`stdout/<id>.txt` and `stderr/<id>.txt` are captured alongside. Capturing
stdout is a deliberate extension beyond "render the tree": stdout is the
planted path, so pinning it pins the plant location too — a change to where a
boot lands, or to the file list an install prints, is then a diff like any
other. Today every `stderr` file is empty: against a fully staged fixture home
no slot goes unfilled and no marker stands for nothing.

## What is pinned, and why

Everything the render can reach is staged into one throwaway fixture root:

| Pinned | Why |
| --- | --- |
| `--session golden` | `bootdir.NewSession` is the only nondeterminism inside cairn, and it is used only when `--session` is empty: a UTC timestamp plus a random suffix. |
| a fixture `HOME` | The profiles read `~/.config/agents/templates/…` and `~/.config/agents/skills`. Staged with the same `rsync` flags `make install-system` uses in agent-setup, so what the render reads is what the installed location holds. |
| a fixture scope | `director`, `engineer` and `orchestrator` run `git status --short --branch` and `git log --oneline -12` against the scope. The fixture is a git repo built from literals: pinned author and committer identity and dates so the abbreviated hash is stable, `core.abbrev` set rather than left to git's object-count heuristic, and `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` nulled on every invocation so the operator's own gitconfig cannot leak in. No remote, so `git status` prints just the branch line. |
| a fixture store | Seeded from `agent-setup/profiles/*.md` by `bin/seed.py`, so the profiles under review are the ones rendered. |
| `CAIRN_DB` in the environment | The `conductor` profile's `fleet` slot is a shell command reading `${CAIRN_DB:-$HOME/.config/agents/cairn.db}`. Cairn's `--db` flag never reaches it. Without the export the slot queries a store that is not there, and the boot still exits 0 with an empty `boot.md`. |
| `TZ=UTC`, `LC_ALL=C`, `XDG_CONFIG_HOME` and `CAIRN_BOOT_ROOT` unset | The environment is built from nothing rather than inherited (`env -i` plus `PATH`), so the operator's shell is not an input. |

Two per-run paths are rewritten to tokens after the render, because leaving
either raw would guarantee a false diff:

- the fixture root → `@FIXTURE@`
- the output root → `@OUT@` (it differs between the capture that wrote the
  golden and every verification run after it)

Both roots are canonicalized with `pwd -P` first. On macOS `/tmp` and `$TMPDIR`
are symlinks, and a path that reaches a golden through one spelling while the
token is the other is a diff nobody enjoys chasing.

The `stderr` files are sorted (`LC_ALL=C sort`) after tokenization. Slot
failures are reported in manifest declaration order, which the keyed merge
deliberately makes meaningless — it is not a contract, so pinning it would
freeze a detail the design says is free. `stdout` is left in the order it was
written.

## Re-capturing

    testdata/goldens/capture.sh --force

`capture.sh` builds `cairn` from the working tree unless `CAIRN` names a
binary. That is deliberate: every change from here on runs `verify.sh` to prove
it did not alter rendering, so the harness must exercise the source in front of
it and not whatever stale `cairn` is on `$PATH`.

`AGENT_SETUP` overrides where the profiles, templates and skills come from; it
defaults to `~/dev/projects/agent-setup`.

Re-capturing rewrites `trees/`. Read the diff before committing it — a golden
that changed without an intended reason is the finding, not the noise.
