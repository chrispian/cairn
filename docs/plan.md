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
indirect graph is `creack/pty`, `go-llm-contracts`, `go-llm-types`.

`yaml.v3` is the one direct dependency outside those four. It was already in
the graph, and it arrives directly because a subagent definition's frontmatter
is YAML while the manifest it comes from is JSON — see §5. Hand-rolling a YAML
encoder for an opaque map is how quoting bugs plant an unparseable frontmatter,
which a harness reads as no frontmatter at all.

### Kept from the prior tree

Ported from `~/dev/projects/cairn-prior-20260825/`, not rewritten:

| Source | Why it survives |
|---|---|
| `install/` (~4,650 lines) | Rendering the installed layer from the same profile source, with drift detection. Nothing in the portfolio does this. Port with the MCP-audit refusal removed (see §6). |
| `bootdir.writeTree` + `cleanArtifactPath` | Renders every file first, stages into a sibling temp dir, then one `rename`. The whole boot directory is all-or-nothing. Stronger than the per-file atomic writers in the libs, and worth keeping. |
| `bootdir/skills.go` (directory-tree copier) | See §5 — deliberately does not use `NativeFileSkill`. |
| `scope`'s containment check | The boot dir must never land inside the scope directory. ~40 lines guarding a write into a repo under work. |
| `.github/workflows/check.yml`, `Makefile`, `.golangci.yml` | Already in place in this tree. |

### Built here

- The sqlite store and the `extends` cascade. Nothing in the portfolio does
  profile inheritance; `agentlaunch/catalog` is file-backed and flat.
- The composition root and the CLI.

### Dropped outright

The dispatch **authority model** (`bootdir/subagents.go`, 1,626 lines) — a
subagent's tools computed as its own allow-list intersected with the
dispatcher's ceiling, a depth budget enforced by withholding the `Agent` tool
under every spelling, and a conflict error for one profile reached through two
dispatchers. How agents relate to each other is not Cairn's concern, and Cairn
has no `tools` concept to intersect with. What survives is the artifact and
nothing else: a profile names other profiles and each is rendered from its own
declaration — see §5. Instance identity and liveness
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
  "slots":     [ /* agentcontext.SlotSpec */ ],
  "mcp":       [ /* agentlaunch.MCPServerSpec */ ],
  "skills":    [ "code-review", "capture-decision" ],
  "settings":  { /* verbatim into .claude/settings.json */ },
  "subagents": [ "reviewer", "worker" ],   // profile ids; each renders a definition
  "subagent":  { /* this profile's own definition, when another names it */ },
  "files":    {
    "rel/path.md":             "literal content",
    "tasks/current/task.md":   { "kind": "cmd", "cmd": { "run": "torque task get $TASK --format md" } },
    "process/implement.md":    { "kind": "static_file", "static_file": { "path": "~/.config/agents/process/implement.md" } }
  }
}
```

A `files` value is **either a literal string or an `agentcontext.SlotSource`**,
resolved by the same resolvers `slots` uses. This is what gives parity with
Torque, which plants a task bundle — `tasks/<id>/task.md`, `task.json`, and a
per-task `process.md` — all rendered from live state, not from static profile
content. Slots render into `boot.md`; `files` renders the same sources to
arbitrary paths. A value that is neither of those two shapes is refused, by
path — a silent coercion would plant bytes nobody wrote at a path a profile
promised.

**A file source that fails fails the boot**, which is deliberately the opposite
of a slot. A slot that does not resolve leaves a section out of `boot.md` and
the agent asks its tools instead; a file that does not resolve leaves a hole at
a path the profile promised, and whatever reads that path cannot tell "never
declared" from "the command that fills it fell over". Sources resolve before
any file is rendered, so a refusal writes nothing at all rather than half a
directory.

A source that **resolves empty** is not a failure, and plants an empty file.
The resolver was reached and it answered; that the answer was empty is content,
and content is a black box (§1). Concretely, this is why the request declares
its slots non-required: `agentcontext`'s `Required` flag fails an assembly on
an empty result as well as on a failed one, and a task list that is
legitimately empty is not a boot that should refuse to start.

### Cascade

`extends` composes ancestor-first. **Uniform closest-wins for every field,
including every top-level key of `spec`.** No per-field special cases — no
union for deny-lists, no merge for slots. A profile author who wants an
ancestor's value keeps it by restating it. `body` is the one exception: it
concatenates ancestor-first, because the persona is additive by nature.

An unknown key in `spec` is carried through the cascade and rendered by the
provider renderer if it knows it, and ignored otherwise. It is never an error.

`"key": null` is how a descendant **clears** an ancestor's key. Presence wins,
and an explicit null is presence — it wins with an empty value rather than
falling through to the ancestor.

`spec` is JSON, so slot entries use `agentcontext.SlotSpec`'s **JSON** tags.
`SlotSource.Kind` is `json:"kind"` and `yaml:"type"` — a slot copied out of a
Tether boot profile will say `type:` and fail with "unknown slot kind". Write
`"kind"`.

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
  .claude/agents/<id>.md ← spec.subagents, one per named profile
  <spec.files>           ← arbitrary paths
```

Location: `~/dev/agent-os/runtime/boot/<binding-or-profile>/<session>/`.
Gitignored. Retention is the caller's. `cairn boot` prints the path and exits;
a human launches.

Rendering is deterministic: same inputs, byte-identical output. Slots resolve
at materialization and may therefore vary between two runs — that is a property
of the resolver, and `agentcontext` hashes the request rather than the result
for exactly this reason.

**A slot that resolves empty, and a slot that fails, both render nothing at
all** — no heading, no marker. `agentcontext`'s `DefaultRenderer` emits a bare
heading for both; Cairn's renderer omits the section entirely, matching Tether's
template behaviour.

An earlier revision had a failed slot render `**Unavailable.**` plus the error,
to distinguish it from an empty one. That was wrong twice over: it is Cairn
authoring prose into the agent's context, which §1 forbids, and it is
unnecessary now that agents pull current truth from tools rather than from
`boot.md`. An absent section is correct — an agent that needs the data queries
the tool. **The failure still reports on stderr, to the operator.**

### Subagent definitions are rendered, not computed

`spec.subagents` is a list of profile ids. Each is looked up, resolved through
its own cascade, and written to `.claude/agents/<id>.md`. The content is that
profile's own `spec.subagent`: an opaque map, transcribed into the
definition's frontmatter the way `spec.settings` is transcribed into the
settings document.

**A parent may not narrow or expand a child.** There is no intersection of tool
lists, because Cairn has no `tools` concept and is not getting one. If a child
lacks a tool it needs, that is a change to the child. Depth is 1 structurally:
Cairn renders the ids a profile named and stops — it does not read a named
profile's own `spec.subagents`, and a subagent gets no boot directory of its
own. Nothing is withheld to enforce that, because there is nothing to withhold.

Three things Cairn does decide, each because the alternative fails silently:

- **`name` is forced to the profile id.** The harness resolves a definition by
  its `name` field rather than by its filename, and a definition carrying no
  name is dropped with no diagnostic at all — verified against Claude Code
  2.1.246, whose loader returns before it logs. A declaration whose `name`
  disagrees with its own id is **refused** rather than overwritten: overwriting
  discards a line the operator wrote. Every other key, known or not, is carried.
- **An id with no profile, an abstract profile, or a profile declaring no
  `spec.subagent`** each refuse the boot, naming the id and the profile that
  named it. Abstract matches `boot`: an abstract profile exists to be extended,
  and a definition is something a harness runs.
- **A duplicate path is still a duplicate path.** A `spec.files` entry at
  `.claude/agents/<id>.md` collides with the definition and refuses, because
  the definitions arrive as their own renderer rather than merged into the
  files map, where one would have silently won.

**The body comes from the declaration's own `body` key**, lifted out of the
frontmatter, and deliberately not from the named profile's cascaded body. The
reason is measured rather than assumed: a subagent's query carries the project
instruction block unless its definition sets `omitClaudeMd`, which only Claude
Code's built-in definitions do — so a subagent dispatched inside a boot
directory already receives that directory's `CLAUDE.md`, and through it
`AGENTS.md`. Rendering the named profile's cascade into the definition would
repeat every ancestor body the parent already supplies, and would put an
ancestor's persona — "you implement one task end to end" — into a definition
for a profile that reviews. Taking the named profile's own row body instead
would mean reading a field that had skipped the cascade, which §3 says no field
does. The declaration is where the definition is declared, so the body is
declared there too, and a descendant restating `spec.subagent` restates it
whole.

Transcription rather than a byte copy is forced by the destination: the
manifest is JSON and frontmatter is YAML. Values are carried by value — a
string that reads as a number stays a string, a number keeps the text the
manifest spelled it with, and the operator's key order is kept.

### The installed layer

`cairn install <binding|profile>` renders a different, shorter set into
`~/.claude`:

```
~/.claude/
  AGENTS.md              composed body + rendered sections, + a provenance comment
  CLAUDE.md              @AGENTS.md, byte-exactly, no marker
  settings.json          ← spec.settings, verbatim, no marker (JSON has no comments)
  skills/                ← spec.skills, directory trees
```

Four artifacts are deliberately absent:

- **No `boot.md`.** Slots resolve at materialization; this layer is not
  materialized per session.
- **No `.mcp.json`.** §6 drops the audit, and user-level MCP configuration is
  not a file in that directory.
- **No `spec.subagents`.** Same reason as `spec.files`, plus one of its own:
  `~/.claude/agents/` is a directory the operator fills by hand, and claiming
  it would put every definition Cairn did not render into the orphan report.
- **No `spec.files`.** `boot` writes into a directory Cairn creates fresh and
  refuses if it already exists; `install` writes into a directory that already
  exists and is full of the operator's live state. Arbitrary path→content
  planting is safe in the first and not the second — and rendering it here
  would make Cairn claim ownership of paths in the operator's home for the
  orphan report below.

Both layers run the **same renderers**. A second renderer per artifact is two
renderings that drift; the prior tree carried them and they did.

### Cairn owns four things in `~/.claude`, not the directory

`install --check` reports against an explicit claim: the exact file paths its
renderers can produce, plus the directories it fills whole (`skills/`).
**Anything else in that directory is invisible to it, whatever it is.**

This is not a convenience. `settings.local.json`, `.credentials.json`,
`projects/`, `todos/` and `history.jsonl` all live there and Cairn renders none
of them; a sweep that called them orphans would make `--check` exit non-zero
forever, which is a gate configured not to gate. Narrowing the claim to the
subtree instead would lose the orphan worth finding: a `settings.json` left by
a profile that stopped declaring one is a path Cairn claims, so Cairn reports
it.

`--check` reports and repairs nothing. It exits non-zero when out of sync.

An occupied destination — a directory or a symlink where a file renders — is
refused, and refused for the whole layer before anything moves. Half an
installed layer is not recoverable; a refusal is.

### Directory modes follow the umask

Files are chmod'd past the umask, because a skill's exec bit is load-bearing.
Directories are not, so `~/.claude` lands 0700 or 0755 depending on the shell
`install` ran from. Left alone deliberately per §1 — single user, own home. If
it is ever made explicit, 0700 is the right value, not 0755: that directory
sits beside `.credentials.json`.

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
cairn boot <binding|profile> [--scope <path>]       materialize a boot dir, print its path
cairn install <binding|profile>                     render the installed layer
cairn install <binding|profile> --check             re-render, diff against disk, report drift
```

`install` takes the same argument as `boot`, and there is no default. A
well-known id like `base` would mean Cairn knowing the name of a profile it
does not ship; a reserved binding is the same magic with indirection. Unlike
`boot`, `install` may be given an `abstract` profile — the installed layer is
normally rendered from the abstract root of the cascade.

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
   `Resolve` carries the `abstract` flag rather than acting on it — `install`
   legitimately resolves an abstract profile. `cairn boot` is what refuses one.
4. **Render** — `[]bootdir.File` from a resolved profile: `AGENTS.md`,
   `CLAUDE.md`, `.mcp.json`, `.claude/settings.json`, skills, subagent
   definitions, `spec.files`. Path validation and duplicate-path detection. No
   filesystem writes.
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

### Named at the end of the MVP build, deliberately not chased

None is a defect. Each is recorded because the reason for leaving it is the
useful part, and an unnamed loose end gets rediscovered as a bug.

- **`bootdir.Renderers()` labels are Claude-shaped strings** — `.mcp.json`,
  `.claude/settings.json`, `.claude/skills`. `Renderer.Artifact` is a label and
  never a path, so nothing breaks, but a render error would name a path codex
  does not use. Rewriting them provider-neutral with no second provider to test
  against is speculative; the right moment is when the codex layout arrives.
- **`bootdir.Planter`, the `plant.Planter` adapter, has no caller.** It exists
  because §2 names that contract as the write boundary, so Cairn speaks what
  Nanite speaks. Cairn's own path uses `PlantFiles`, because `plant.Spec`
  carries no file modes and a skill's executable bit is load-bearing. If
  nothing external ever calls it, it is ceremony — decide when something tries.
- **A golden fixture would have to avoid the literal `.claude` name.** Cairn's
  output *is* a `.claude/` tree, so recording one byte-for-byte checks live
  configuration into the repo. `testdata` is inert to the go command and means
  nothing to a harness: reading any file under the project root makes Claude
  Code probe `<dir>/.claude/skills` in every directory between that file and
  the root and register what it finds as project skills, and load a `CLAUDE.md`
  from those same directories as instructions — which then follows its
  `@AGENTS.md` pointer, the exact pair cairn renders. The prior tree solved this
  by storing the segment as `_claude` and mapping it back where the fixture is
  read; it had three such directories and no literal one. This tree keeps no
  copy of its output at all — it asserts structure in code, and
  `bootdir/skills_test.go` builds skill trees under the test's own temporary
  directory for this reason. The module-wide walk that used to enforce the
  naming rule is gone with the goldens: it guarded a shape the design no longer
  produces, and a build that fails on a `.claude/skills` directory is failing on
  a supported feature it has no way to know was a mistake. If goldens return, so
  does `_claude`.
- **`cairn install` prints every path it wrote, unchanged ones included.**
  `--check` before an install already answers "what would change", and
  inventing a diff format nobody has asked for is how a small tool grows.

Bounded by construction, and worth knowing: §6 says `spec.settings` is written
verbatim with no validation, but a value that is not valid JSON cannot be
stored at all — `encodeSpec` runs `json.Valid` per value. Verbatim is bounded
by "it is at least JSON", which is shape rather than meaning. That is the
intended line.
