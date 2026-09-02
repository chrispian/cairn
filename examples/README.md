# Using Cairn

Cairn assembles files and writes them into a directory. It prints the path and
exits. **You launch.** Nothing here starts, watches, or supervises an agent.

Two commands and one workflow:

```
cairn boot <binding|profile> [--scope <path|alias>]   materialize a boot dir, print its path
cairn install <binding|profile> [--check]             render ~/.claude from the same source
```

---

## 1. Build

```bash
cd ~/dev/projects/cairn
make build            # → bin/cairn
```

## 2. Seed a store

Cairn ships no profiles and has no authoring command yet, so profiles are rows
in a sqlite database at `$XDG_CONFIG_HOME/agents/cairn.db` (override with
`--db` or `CAIRN_DB`). `seed.sh` is the worked example:

```bash
./examples/seed.sh
```

It writes an abstract `base`, a concrete `engineer` that extends it, a
`reviewer` the engineer dispatches and that also boots on its own, two scope
aliases, two bindings, and the templates those profiles name. Read it — it is
the profile-authoring documentation.

Three things it demonstrates that are easy to get wrong:

- **`extends` merges keyed collections by key and replaces everything else.**
  A child declaring one slot, one template or one skill keeps the ancestor's
  rest — it does not restate them. Every other key is taken whole from the
  nearest profile that declares it, including every key Cairn has never heard
  of. `body` is the exception to both and concatenates ancestor-first.
- **`spec` is JSON, so a slot's kind key is `"kind"`.** Every boot profile in
  the wider portfolio is YAML and writes `type:`. Cairn will tell you.
- **`"key": null` clears an ancestor's key.** Presence wins, and an explicit
  null is presence.

## 3. Boot

```bash
cairn boot eng
# /Users/chrispian/dev/agent-os/runtime/boot/eng/20260826T014133Z-9f2a1c
```

The path goes to **stdout**; diagnostics go to **stderr**. A slot that fails to
resolve reports on stderr and the boot still succeeds — a missing memory
service should not stop you working.

`boot.sh` is the wrapper that turns the path into a session:

```bash
./examples/boot.sh eng                     # boot, then launch claude in it
./examples/boot.sh eng --scope ~/dev/nanite  # override the binding's scope
PRINT_ONLY=1 ./examples/boot.sh eng        # just the path
```

What lands:

```
<boot-dir>/
  <spec.templates>       your text, markers substituted; any path, any number
  .mcp.json              spec.mcp
  .claude/settings.json  spec.settings, verbatim
  .claude/skills/        spec.skills, whole directory trees
  .claude/agents/<id>.md spec.subagents, one per named profile
  <spec.trees>           source directories, copied whole
  <spec.files>           arbitrary paths, literal or resolved
```

**Cairn names none of those files.** `AGENTS.md`, `CLAUDE.md` and `boot.md` are
destinations the seeded profiles happen to declare. Declare none and you get no
prose; declare fifty and you get fifty. Cairn composes no heading, no section
and no ordering of its own.

### Templates and markers

A template is your text with markers in it. Two verbs, nothing else:

```markdown
<!-- cairn:slot memory -->     one of spec.slots, by name
<!-- cairn:value scope -->     one of: binding, model, profile, provider, scope, session
```

An HTML comment, so it is invisible in rendered markdown and cannot be mistaken
for the harness's own `@AGENTS.md` import — which a template has to be able to
pass through untouched, since that is how `CLAUDE.md` points at the file beside
it.

A marker Cairn cannot parse is a **refusal**, and nothing is written: a verb
that is not `slot` or `value`, a body that is not exactly a verb and one name —
no name written in it at all, or more than one — and a `cairn:value` naming
something outside the six. Leaving one in place would plant the marker's own
text where an agent reads it. Everything outside `cairn:` is left alone.

A slot *name* is not checked against the manifest, so a marker naming a slot
that was never declared is a different case and is not a refusal: it renders
nothing and says nothing. An undeclared slot may be one you are about to add; an
unknown value can only be a typo. A `cairn:slot` whose slot *was* declared and
then resolved empty or failed renders nothing too — but that one `cairn boot`
reports on stderr, so the operator can tell a missing block from one they
removed. The warning exists to debug a slot that was *supposed* to fill, and one
shared template can carry every marker any profile might fill: a line for every
marker a profile does not use would be noise deep enough to bury the one line
that matters.

**Slots are where the leverage is.** They resolve at materialization, so they
carry live state rather than a paragraph that went stale a month ago. The seeded
`engineer` pulls `git status` and `git log`; the same mechanism reaches an HTTP
endpoint or any command, which is how memory and task state get in:

```jsonc
"slots": [
  { "name": "memory", "section": "## Memory",
    "source": { "kind": "http_json",
                "http_json": { "url": "${TESSERACT_URL}/v1/recall?limit=20" } } },
  { "name": "tasks", "section": "## Open tasks",
    "source": { "kind": "cmd",
                "cmd": { "run": "torque tasks list --status in_progress --format brief" } } }
]
```

Kinds: `static_file`, `static_dir`, `inline`, `cmd`, `http_text`, `http_json`,
`role_summary`. Write `"kind"`, not `"type"` — Cairn will say so if you forget.

**A slot that fails renders nothing at all** — no heading, no marker — and so
does one that resolves empty. Cairn writes no sentence of its own into an
agent's context, and a section that says "unavailable" is one nobody declared
and nobody can correct. An agent that needs the data asks the tool, which is
current where a planted file is a snapshot. The failure is not lost: it goes to
**stderr**, where the operator reads, and so does a marker whose slot was
declared and filled nothing.

**Put the heading on the slot, not in the template.** `"section": "## Memory"`
comes back with the content or not at all. Write `## Memory` in the template
above the marker and you keep that heading on the day the slot fails, which is
the one thing this rule exists to prevent.

**Leave `section` out and the slot substitutes bare content** — no heading at
all. That is what you want for a block of prose that carries its own headings,
which is most of what composes an instruction file.

`$VAR` and `${VAR}` are expanded in every manifest value that names somewhere to
read from: a source's **path** and **URL**, a `trees` source, and `skills_dir`.
A leading `~/` expands too, after the variables — so a variable holding
`~/agents` works. A `cmd`'s command line is not expanded; it already runs
through a shell that does its own.

An unset name expands to nothing, so `$ROOT/docs` becomes `/docs`. Cairn's
diagnostics name both forms for exactly that reason — a resolver is handed the
expansion and can only report that one, so what you wrote comes from Cairn or
from nowhere:

```
spec.trees copies "docs" from "$ROOT/docs", which expanded to "/docs",
which does not exist

slot "memory" did not resolve: the static_file path is "$AGENT_DOCS/process.md",
which expanded to "/process.md": static_file: read /process.md: no such file
```

A path with no variable in it reads exactly as it always did; the pair appears
only when expansion changed something.

### `files`: the same sources, at arbitrary paths

`spec.files` maps a boot-relative path to **either a literal string or a slot
source**. Literals cover content the profile already knows; sources cover
content that is only true at boot — a task bundle, say, planted where a process
expects to find it:

```jsonc
"files": {
  "README-for-this-session.md": "read task.md first\n",
  "tasks/T-42/task.md":  { "kind": "cmd", "cmd": { "run": "torque task get T-42 --format md" } },
  "tasks/T-42/task.json": { "kind": "cmd", "cmd": { "run": "torque task get T-42 --format json" } },
  "tasks/T-42/process.md": { "kind": "static_file",
                             "static_file": { "path": "~/.config/agents/process/implement.md" } }
}
```

**A file source that fails fails the boot** — the opposite of a slot, on
purpose. A missing section is degraded context and the agent routes around it;
a missing file is a hole at a path the profile promised, and whatever opens
that path cannot tell "never declared" from "the command that fills it fell
over". Sources resolve before anything is written, so a refusal leaves no
directory behind.

A source that resolves *empty* is not a failure and plants an empty file — the
resolver answered, and what it answered is content, which Cairn does not read.

### `trees`: copying a directory whole

```jsonc
"trees": { "docs/engineering": "~/.config/agents/docs/engineering" }
```

Destination on the left, source directory on the right, copied recursively with
executable bits preserved. The source takes `$VAR` and `~/` — it is nothing but
"point at a path", so it is the value most likely to want one. A single file rides `files` with a `static_file`
source instead. **Do not reach for a `static_dir` slot** — that concatenates
every file it finds into one string, which is right for a slot and destroys a
directory.

A symlink to a file is followed and copied by value. A symlink to a directory,
or one that dangles, is refused by name — Cairn does not walk into linked
directories, and saying so beats planting something that is not a file.

### `subagents`: naming other profiles

`spec.subagents` is a list of profile ids. Each is looked up, resolved through
its own cascade, and written to `.claude/agents/<id>.md`. What lands in the
file is **that profile's own `spec.subagent`** — an opaque map, carried into
the frontmatter the way `spec.settings` is carried into the settings document:

```jsonc
// in the profile that boots
"subagents": ["reviewer"]

// in reviewer's own spec
"subagent": {
  "description": "Reviews a diff with no shared context.",
  "tools": ["Read", "Grep", "Glob"],
  "model": "sonnet",
  "body": "Read the diff. Report what you found and nothing else.\n"
}
```

which renders:

```markdown
---
name: reviewer
description: Reviews a diff with no shared context.
tools:
  - Read
  - Grep
  - Glob
model: sonnet
---

Read the diff. Report what you found and nothing else.
```

**Being dispatchable is a capability, not a mode.** `spec.subagent` is what
lets another profile name this one. It takes nothing away: the same `reviewer`
boots into a session of its own with `cairn boot reviewer`, off the same
cascade and the same prose, and the planted definition is that role narrowed to
a dispatch rather than a different role. A profile with no `subagent` block
refuses to be named; `abstract: true` is the only key that rules out both.

**A parent may not narrow or expand a child.** There is no tool intersection,
no ceiling, no depth cap — Cairn has no `tools` concept and reads none of these
keys. If the reviewer needs a tool it does not have, edit the reviewer. Depth is
1 because Cairn renders the ids you named and stops: it never reads a named
profile's own `subagents`, and a subagent gets no boot directory of its own.

Two things are Cairn's, and both exist because the alternative fails quietly:

- **`name` is forced to the profile id**, because the harness resolves a
  definition by that field rather than by the filename, and a definition with
  no `name` at all is dropped with no message. Write a *different* `name` and
  Cairn refuses rather than overwriting what you wrote.
- **A named id with no profile, an `abstract` profile, or a profile with no
  `subagent` key refuses the boot**, naming the id and the profile that named
  it. Nothing is written.

`body` is the definition's prompt, and it is the declaration's own key rather
than the profile's `body` column. A dispatched subagent already receives the
boot directory's `CLAUDE.md`, and through it `AGENTS.md` — so the cascade is in
its context already, and rendering it again would only add an ancestor's
persona to a profile that does something else. Leave `body` out and the
definition is frontmatter and nothing else.

A `files` entry at `.claude/agents/<id>.md` is a duplicate path, and refuses.
Neither wins.

### The settings tier, and why `boot.sh` passes `--settings`

A `settings.json` that merely sits in the boot directory is read as
**`projectSettings`** — the untrusted tier, because a repo can control it.
Verbatim from Claude Code 2.1.246:

```
settings defaultMode "auto" ignored — only policy/user/flag settings may grant
auto mode (projectSettings and localSettings are repo-controllable)
```

So a profile declaring `defaultMode: auto` validates, renders, plants — and is
then **silently downgraded** at launch with a warning. The same rule governs
`autoMode` classifier rules.

Passing `--settings <boot-dir>/.claude/settings.json` promotes the file to
`flagSettings`, which is trusted. `boot.sh` does this.

This is deliberately the launcher's job. Cairn writes the file and prints a
path; whoever owns the invocation decides what tier it lands in. Cairn taking
it over would mean Cairn owning the launch, which is a different product.

## 4. The installed layer

Renders `~/.claude` from the same profiles, so the global floor and a boot
directory cannot disagree about what a profile says.

```bash
cairn install base --check     # what would change; writes nothing; exit 1 if out of sync
cairn install base             # write it
```

**Run `install` yourself, never from inside an agent session** — every agent on
this machine runs under `~/.claude`, so an agent that runs it rewrites its own
live configuration mid-session.

Slots resolve here too, but only the kinds whose answer changes when *you*
change something — `static_file`, `static_dir`, `inline`, `role_summary`. A
`cmd` or `http_*` slot renders nothing in this layer and says so on stderr,
because `--check` re-renders and diffs against disk: a `git status` slot would
report drift on every run and the check would stop meaning anything.

Cairn claims four things in that directory — `AGENTS.md`, `CLAUDE.md`,
`settings.json`, and the `skills/` subtree. Not `agents/`: subagent definitions
are a boot-directory artifact, and claiming that directory would report every
definition you wrote by hand as an orphan. Everything else (`settings.local.json`,
`.credentials.json`, `projects/`, `todos/`, `history.jsonl`) is invisible to
`--check`, which is why it does not cry wolf on a real installation.

---

## 5. Calling Cairn from a launcher (Tachyon)

Tachyon's engine seam is already the right shape for this: the Swift app shells
out to `tachyon-engine`, parses JSON on stdout, and spawns the agent itself.
`tachyon-engine launch` **emits** a runnable command rather than exec'ing it —
exactly Cairn's contract.

So Cairn slots in as a step before the spawn:

```
Tachyon.app  ──shell──▶  cairn boot <binding>  ──▶  boot dir on disk
     │                                                    │
     └────────────── spawns `claude --add-dir <scope>` with cwd = boot dir
```

Minimal Go, engine-side:

```go
// Materialize a boot directory. Cairn writes files and prints a path; it
// starts nothing.
cmd := exec.CommandContext(ctx, cairnBin, "boot", binding, "--scope", scope)
cmd.Stderr = os.Stderr // slot failures and diagnostics surface to the operator
out, err := cmd.Output()
if err != nil {
    return fmt.Errorf("cairn boot %s: %w", binding, err)
}
bootDir := strings.TrimSpace(string(out))

// Tachyon spawns it. Cairn is done.
launch := exec.Command("claude", "--add-dir", scope)
launch.Dir = bootDir
```

### What Cairn is missing for this, honestly

`cairn boot` prints a bare path. That is enough to `cd` into and launch, but a
launcher building a UI wants what `tachyon-engine launch` already returns —
provider, model, the scope to pass to `--add-dir`, and which slots failed so it
can show a warning rather than making the operator read stderr.

Today a launcher has to scrape `AGENTS.md` for the scope, as `boot.sh` does.
That works and is stable, but it is scraping.

**A `--json` flag is the fix** and would mirror Tachyon's own contract:

```jsonc
{
  "status": "ready",
  "boot_dir": "/Users/.../boot/eng/20260826T014133Z-9f2a1c",
  "scope":    "/Users/chrispian/dev/projects/cairn",
  "provider": "claude",
  "model":    "",
  "files":    [".claude/settings.json", ".mcp.json", "AGENTS.md", "boot.md", "CLAUDE.md"],
  "slots":    [ { "name": "memory", "status": "failed", "error": "connection refused" } ]
}
```

It is not built. It is the first thing to add when a launcher actually consumes
Cairn, and it is small — the data all exists at the point the path is printed.

### On `plant.Planter`

Cairn implements `go-agent-wrapper/plant.Planter` so it speaks the same
planting contract Nanite does. **Nothing calls it**, including Cairn itself,
which uses its own `PlantFiles` because `plant.Spec` carries no file modes and
a skill's executable bit is load-bearing.

For a launcher that shells out — Tachyon, `boot.sh`, you at a prompt — it is
irrelevant: it is a Go interface, and the process boundary is the seam. It only
matters if something imports Cairn as a library and wants to plant *through*
the contract it already speaks. It costs ~40 lines and nothing at runtime, so
it stays until something either uses it or clearly never will.
