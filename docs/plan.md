# Cairn — MVP plan

**Status:** authoritative. Written 2026-08-25 after an audit of the prior
implementation and of the prior art in `~/dev/hollis-labs`.

This is the only prose in this repository. Earlier documents — the spec under
`docs/spec/`, the ADRs, the READMEs, and the doc comments of the prior
implementation — are superseded and are not in the git history of this repo.
The prior tree is archived at `~/dev/projects/cairn-prior-20260825/`.

---

## 1. What Cairn is

**Cairn assembles files and writes them into a directory.**

It reads a profile from its own store, resolves it through an `extends`
cascade, and materializes a boot directory a CLI coding agent can be launched
from. It renders the installed layer (`~/.claude`, `~/.codex`) from the same
source.

That is the whole job.

### What Cairn is not

Cairn does not launch, monitor, track, control, create, harness, or steer
agents. It has no opinion about how agents work, relate to each other, or
behave.

**File contents are a black box.** Cairn composes and writes `AGENTS.md`; it
does not parse it, reason about it, validate its prose, or know what any of it
means. The same holds for every file it plants. Whoever owns the profiles owns
the semantics.

Cairn provides the shape and validates the shape. It ships no profiles, no
skills, and no defaults.

### Single user, by design

Cairn is built for one operator who also builds the system it configures. It
is not guarding a stranger from a footgun; it is not guarding its author from
a system its author has full access to anyway. Validation that exists only to
prevent the operator from doing something the operator meant to do is out.

---

## 2. The core decision: adopt, don't rebuild

The prior implementation was 25,677 lines. Roughly 8,000 of them reimplemented
problems already solved in `~/dev/hollis-labs/libs`, and about 4,700 more
implemented concerns that are not Cairn's. Tether, Torque, and Nanite each grew
a near-identical context-assembly pipeline before `agentcontext` was extracted
to end exactly that; Cairn was becoming the fourth.

**Cairn adopts the portfolio libraries.** What is left is genuinely Cairn's.

### Adopted

| Library | What Cairn takes |
|---|---|
| `agentkit/agentcontext` + `.../resolvers` | The entire slot mechanism. 8 slot kinds (`static_file`, `static_dir`, `inline`, `cmd`, `http_text`, `http_json`, `role_summary`, `skill_index`), 7 shipped resolvers, per-slot timeouts and headers, byte/token budgets, per-slot provenance, a determinism contract, and non-required-slot failure that records the error instead of blocking. Entry point: `DefaultProvider.Assemble(ctx, ContextRequest) (*ContextResult, error)`. `ContextResult.Rendered` becomes `boot.md`. |
| `agentkit/agentlaunch` | Vocabulary, not pipeline: `MCPServerSpec{Name, Command, Args, Env}` for MCP server definitions, `NativeFile{Kind: raw, RelPath, Content, Mode}` for arbitrary planted files, and `ValidateBootDirRelPath` as a second path-safety opinion. |
| `go-agent-wrapper/plant` | `plant.Planter` / `plant.Spec` as the write boundary, so Cairn speaks the same planting contract as Nanite. |
| `go-providers/provider` | Per-provider layout convention via `BootDirSpec`: which files, at which relative paths, cwd preference, the `--add-dir` argument pattern, and per-file modes (codex `auth.json` is 0600). This is also how codex and opencode arrive without inventing their layouts. |
| `go-sqlite` (`sqlitekit`, `txutil`) | Store. Pure Go via `modernc.org/sqlite`, no cgo. |

Verified: all four modules resolve and build together (`agentkit v0.5.1`,
`go-agent-wrapper v0.8.1`, `go-providers v0.24.0`, `go-sqlite v0.1.0`),
indirect graph is `creack/pty`, `go-llm-contracts`, `go-llm-types`, `yaml.v3`.

### Kept from the prior tree

Ported from `~/dev/projects/cairn-prior-20260825/`, not rewritten:

| Source | Why it survives |
|---|---|
| `install/` (~4,650 lines) | Rendering the installed layer from the same profile source, with drift detection. Nothing in the portfolio does this. Port with the MCP-audit refusal removed (see §6). |
| `bootdir.writeTree` + `cleanArtifactPath` | Renders every file first, stages into a sibling temp dir, then one `rename`. The whole boot directory is all-or-nothing. Stronger than the per-file atomic writers in the libs, and worth keeping. |
| `bootdir/skills.go` (directory-tree copier) | See §5 — deliberately does not use `NativeFileSkill`. |
| `scope`'s containment check | The boot dir must never land inside the scope directory. ~40 lines guarding a write into a repo under work. |
| `testdata_guard_test.go` | Rejects dot-prefixed directories and instruction files under `testdata/`. Earned: fixture `.claude/skills` once registered as live skills in a running session. |
| `.github/workflows/check.yml`, `Makefile`, `.golangci.yml` | Already in place in this tree. |

### Built here

- The sqlite store and the `extends` cascade. Nothing in the portfolio does
  profile inheritance; `agentlaunch/catalog` is file-backed and flat.
- The composition root and the CLI.

### Dropped outright

Dispatch and team semantics (`bootdir/subagents.go`, 1,626 lines) — how agents
relate to each other is not Cairn's concern. Instance identity and liveness
(`internal/identity`, 885 lines) — that is a registry function. Override
discipline (`overrides/`, 2,235 lines) — judging whether a correction is
well-formed is content review; if scoped content needs planting it rides the
`files` key like any other file. Cross-session messaging intent. Authority
resolution as policy (see §6).

---

## 3. Store

`go-sqlite`'s `sqlitekit` for opening, `txutil` for transactions. Default path
`~/.config/agents/cairn.db`, `XDG_CONFIG_HOME`-aware, overridable by flag and
by `CAIRN_DB`. **Cairn creates the database if it is absent** — unlike the
prior config-root rule, because an empty database is a usable starting state
and a missing one is not a configuration error.

Deliberately small. The cautionary example is Nanite's `agents` table: 40-plus
columns across 134 migrations, several deprecated but inert, entangled with
plugins, consumers, roles, and tenancy. That is what happens when the database
schema becomes the model.

```sql
CREATE TABLE profiles (
  id          TEXT PRIMARY KEY,
  extends     TEXT NOT NULL DEFAULT '',
  abstract    INTEGER NOT NULL DEFAULT 0,
  name        TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  provider    TEXT NOT NULL DEFAULT '',
  model       TEXT NOT NULL DEFAULT '',
  body        TEXT NOT NULL DEFAULT '',
  spec        TEXT NOT NULL DEFAULT '{}',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE bindings (
  name       TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  scope      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE scopes (
  alias TEXT PRIMARY KEY,
  path  TEXT NOT NULL
);
```

`spec` is the rendering manifest: opaque JSON, of which Cairn interprets only
the keys it renders. Anything else is carried and ignored.

```jsonc
{
  "slots":    [ /* agentcontext.SlotSpec */ ],
  "mcp":      [ /* agentlaunch.MCPServerSpec */ ],
  "skills":   [ "code-review", "capture-decision" ],
  "settings": { /* verbatim into .claude/settings.json */ },
  "files":    { "rel/path.md": "content" }
}
```

### Cascade

`extends` composes ancestor-first. **Uniform closest-wins for every field,
including every top-level key of `spec`.** No per-field special cases — no
union for deny-lists, no merge for slots. A profile author who wants an
ancestor's value keeps it by restating it. `body` is the one exception: it
concatenates ancestor-first, because the persona is additive by nature.

An unknown key in `spec` is carried through the cascade and rendered by the
provider renderer if it knows it, and ignored otherwise. It is never an error.

---

## 4. Scope

Scope is one directory path, supplied at boot, optional. It is the materialized
instance's working directory.

**One validation, and it guards a write:** the boot directory must never land
at or inside the scope directory. Checked, with a test.

The rest of the prior scope validation — rejecting `/etc`, `/usr`, `/var`,
`$HOME`, the filesystem root — is dropped as premature. See §1, single user.

---

## 5. Output contract

```
<boot-dir>/
  AGENTS.md              composed body + rendered sections
  CLAUDE.md              @AGENTS.md
  boot.md                ← agentcontext assembles spec.slots
  .mcp.json              ← spec.mcp
  .claude/settings.json  ← spec.settings, verbatim
  .claude/skills/        ← spec.skills, directory trees
  <spec.files>           ← arbitrary paths
```

Location: `~/dev/agent-os/runtime/boot/<binding-or-profile>/<session>/`.
Gitignored. Retention is the caller's. `cairn boot` prints the path and exits;
a human launches.

Rendering is deterministic: same inputs, byte-identical output. Slots resolve
at materialization and may therefore vary between two runs — that is a property
of the resolver, and `agentcontext` hashes the request rather than the result
for exactly this reason.

### Skills are directories, deliberately

`agentlaunch.NativeFileSkill` resolves to a flat `.claude/skills/<id>.md`.
Claude Code's current convention is a **directory**: `.claude/skills/<slug>/SKILL.md`
alongside `references/` and scripts. Cairn therefore ports the prior tree's
directory-tree copier and uses `NativeFileKind` only for `raw`.

Do not "fix" this back to `NativeFileSkill`. It would silently flatten every
multi-file skill.

---

## 6. Authority is rendered, not decided

`spec.settings` is written into the provider settings file verbatim. Cairn does
not model permission modes, does not validate tool names, does not translate
access grants into permission rules, and makes no claim about what the harness
does with what it writes.

If a rendered rule turns out not to enforce, that is a finding about the
harness, not a Cairn defect.

This drops the prior tree's `bootdir/settings.go` and its 636 lines of tests,
its closed `PermissionMode` enum, and its `access[] → Read(path/**)` translator.
It also drops `install`'s MCP audit, which refused to install when the
installed layer already configured an MCP server.

---

## 7. Commands

```
cairn boot <binding|profile> [--scope <path>]    materialize a boot dir, print its path
cairn install                                    render the installed layer
cairn install --check                            re-render, diff against disk, report drift
```

`cairn install` is human-executed, permanently. Every agent working on Cairn
runs under `~/.claude`; an agent running `install` rewrites its own live
configuration mid-session.

Profile authoring is out of MVP scope — the operator writes rows directly, or
via a later `cairn profile import/export`. That command is a convenience, not a
prerequisite.

---

## 8. Build order

**Nothing else lands until `cairn boot` writes a directory that can be opened
and read.** Until that exists, every question about content is answered by
reasoning instead of by looking.

1. **Module skeleton** — `go.mod` with the four adopted modules. `cmd/cairn`
   with flag parsing that errors honestly on every unimplemented path.
2. **Store** — schema, migration, open/create, profile CRUD, binding and scope
   lookup. `sqlitekit.OpenWriter` / `OpenReader`, `txutil.WithImmediate`.
3. **Profile + cascade** — load by id, walk `extends` ancestor-first, detect
   cycles, closest-wins merge over columns and `spec` keys, concatenate `body`.
   Reject a direct boot of an `abstract` profile.
4. **Render** — `[]bootdir.File` from a resolved profile: `AGENTS.md`,
   `CLAUDE.md`, `.mcp.json`, `.claude/settings.json`, skills, `spec.files`.
   Path validation and duplicate-path detection. No filesystem writes.
5. **Slots** — `spec.slots` → `agentcontext.ContextRequest` →
   `DefaultProvider.Assemble` → `boot.md`. Wire `resolvers.Default()`; add
   `resolvers.WithSkillIndex` only if a profile needs it.
6. **Plant** — `writeTree` ported. Scope containment check. `cairn boot` end
   to end, printing a path. **This is the MVP gate.**
7. **Install** — port `install/`, minus the MCP audit refusal. `cairn install`
   and `cairn install --check`.

Steps 2–5 are independent enough to run in parallel behind the interfaces step
1 fixes. Step 6 is the barrier.

---

## 9. Open, tracked, not blocking

- **Operator review of rendered prose.** The prior tree's `AGENTS.md` section
  renderers and `overrides/render.go` preamble wrote Cairn's own editorial voice
  into agent contracts — "Escalate to your `reports_to`, not sideways", "never
  as instructions addressed to you". Under §1 that content belongs in a profile
  body, not in the binary. **Chrispian reviews and rewrites these before go-live.**
  Until then, render structured sections from declared fields only, and no
  authored paragraphs.
- Codex and opencode planters, via `go-providers` `BootDirSpec`. Claude first.
- `cairn profile import/export`.
