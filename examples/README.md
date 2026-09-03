# Using Cairn

Cairn assembles files and writes them into a directory. It prints the path —
or, with `--json`, one object describing the boot — and exits. **You launch.**
Nothing here starts, watches, or supervises an agent.

Two commands and one workflow:

```
cairn boot <binding|profile> [--scope <path|alias>]   materialize a boot dir, print its path
cairn boot <binding|profile> --json                   ... and describe it for a launcher
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
# /Users/chrispian/.local/state/cairn/boot/eng/20260826T014133Z-9f2a1c
```

The path goes to **stdout**; diagnostics go to **stderr**. A slot that fails to
resolve reports on stderr and the boot still succeeds — a missing memory
service should not stop you working.

Pass `--json` and stdout is one JSON object describing the boot instead of the
bare path — the scope, the settings file to promote, and how the harness wants
to be opened. That is what a launcher reads; see §5.

What lands:

```
<boot-dir>/
  <spec.templates>       your text, markers substituted; any path, any number
  .mcp.json              spec.mcp
  .claude/settings.json  spec.settings, laid out, plus the access grant
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
that is not `slot` or `value`, and a body that is not exactly a verb and one
name — no name written in it at all, or more than one. Leaving one in place
would plant the marker's own text where an agent reads it. Everything outside
`cairn:` is left alone.

Neither *name* is checked before rendering, so a marker naming a slot that was
never declared, or a `cairn:value` outside the six, is not a refusal: it renders
nothing. What Cairn *says* about the two differs, because the two sets belong to
different people. The slot set is yours, so a marker for a block this profile
does not declare is ordinary and Cairn says nothing about it. The six values are
Cairn's, so a name outside them is one nothing will ever fill: **`boot` and
`install` both name it on stderr**, with the six beside it. `install --check`
names it too, and needs to: a template emptied by a marker Cairn cannot fill
renders no file at all, and into a root that never held that file there is
nothing on disk for the check to call an orphan, so it reports "In sync" over a
pointer that now includes nothing. (Empty a template that *was* installed and
the check does catch it — the file becomes an orphan and the check fails.) A
`cairn:slot` whose slot *was* declared and then resolved
empty or failed renders nothing too, and is reported the same way, so you can
tell a missing block from one you removed. The warning exists to debug a marker
that was *supposed* to fill, and one shared template can carry every marker any
profile might fill: a line for every marker a profile does not use would be
noise deep enough to bury the one line that matters.

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

### The access grant

`spec.access.directories` is a list of paths — `$VAR` and `~/` expand — naming
what the agent may reach beyond the directory it works in. **The scope is
granted without being declared**; these add to it.

```json
"access": {"directories": ["~/dev/hollis-labs/apps/nanite"]}
```

It is neutral: it names directories, not one harness's permission key. Cairn
hands the paths to the provider adapter that owns the harness's spelling for
them, so for Claude Code they land at `permissions.additionalDirectories` in
the settings document beside whatever the profile declared there. Two profiles
in a chain that each name directories grant the union.

**Every path is checked before it is granted**, and each of these is a refusal
that names what you wrote and what it expanded to:

```
spec.access.directories declares "..", which is not an absolute path
spec.access.directories declares "$ROOT/docs", which expanded to "/docs", which does not exist
```

Relative is the one that matters. The harness reads this file from inside the
boot directory, so a `..` granted verbatim is the boot root — every other
session's boot directory for that profile. Absolute, existing, and then
symlink-resolved, which is what the scope already gets.

**Do not declare `permissions.additionalDirectories` under `spec.settings`.**
That was the only way to grant a directory before this key existed; it is now
the key cairn writes the grant into, and a document declaring it by hand is
refused by name rather than quietly overwritten. Move it here — it is the only
spelling that composes with the scope, unions across a chain, and gets the
checks above.

It is the one key cairn contributes to that document. Everything else under
`spec.settings` is still the operator's own, unread — see the section below for
the tier that decides whether the harness honours any of it.

### The settings tier, and why a launcher passes `--settings`

A `settings.json` that merely sits in the boot directory is read as
**`projectSettings`** — the untrusted tier, because a repo can control it.
Verbatim from Claude Code 2.1.246:

```
settings defaultMode "auto" ignored — only policy/user/flag settings may
grant auto mode (projectSettings and localSettings are repo-controllable)
```

So a profile declaring `defaultMode: auto` validates, renders, plants — and is
then **silently downgraded** at launch with a warning. The same rule governs
`autoMode` classifier rules.

Passing `--settings <boot-dir>/.claude/settings.json` promotes the file to
`flagSettings`, which is trusted. That is what `settings_path` in the `--json`
report is for.

This is deliberately the launcher's job. Cairn writes the file and describes
it; whoever owns the invocation decides what tier it lands in. Cairn taking it
over would mean Cairn owning the launch, which is a different product.

**`--settings` is also the whole access grant, and that has been watched.** A
session opened as `claude --settings <boot-dir>/.claude/settings.json`, with no
`--add-dir`, in a boot directory scoped to a real project, read that project
with no permission prompt and no refusal. So `permissions.additionalDirectories`
in a flag-tier settings file is the grant, and `--add-dir` is redundant for any
launcher that passes `--settings` — which is every launcher, because
`--settings` is what keeps `defaultMode` from being downgraded in the first
place.

A launcher therefore passes `--settings` and nothing else. `cairn boot --json`
still reports `project_dir_arg`, because a provider that grants directories on
the command line rather than in a config file will need it — Codex does — and
that is a fact about the provider for a launcher to act on, not a flag Cairn
has an opinion about.

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
Tachyon.app  ──shell──▶  cairn boot <binding> --json  ──▶  boot dir on disk
     │                                                          │
     └────── spawns `claude --settings <settings_path>` with cwd = boot dir
```

### `cairn boot --json`

Without `--json`, stdout is the bare path and nothing else — that seam has not
moved. With it, stdout is one JSON object and nothing else, so
`$(cairn boot eng --json)` is parseable and diagnostics still go to stderr:

```json
{
  "boot_dir": "/Users/.../boot/eng/20260826T014133Z-9f2a1c",
  "provider": "claude",
  "scope": "/Users/chrispian/dev/projects/cairn",
  "settings_path": "/Users/.../boot/eng/20260826T014133Z-9f2a1c/.claude/settings.json",
  "cwd_preference": "boot_dir",
  "project_dir_arg": ["--add-dir", "{{.ProjectDir}}"]
}
```

It exists because the alternative was scraping. An earlier launcher pulled the
scope out of the rendered `AGENTS.md` with `sed`, which made a launcher's
access grant depend on a marker's position in a document written to be
re-authored. That worked, and it worked by luck: one template edit moving the
line would have returned an empty scope with no error, which is a launcher
granting nothing and saying so nowhere.

Six keys, flat, `snake_case`, and **every one of them is emitted on every
boot** — the key set is the contract, and a key that came and went would make a
consumer handle two shapes for one meaning.

**A value Cairn does not have is `null`, never `""` and never `[]`.** An empty
string is the shape most likely to be interpolated straight into argv: `claude
$FLAG "$VALUE"` passes an empty argument and the launch is wrong with nothing
reporting it, where `null` forces the consumer to decide. Three keys can be
null and each says something different:

- `scope` — the binding declared none and no `--scope` was given.
- `settings_path` — the render produced no file at the harness's settings path.
  That is a real case: a profile declaring no `spec.settings` with no directory
  to grant produces none. The path is read off what the render actually
  produced, so it never names a file that is not there.
- `project_dir_arg` — this harness needs no flag to grant a directory. It is
  **not** how "there is no scope" is spelled; `scope` says that.

**`scope` and `settings_path` are not equivalent, and only one implication
holds.** The scope is itself one of the granted directories, and one directory
to grant is enough on its own to render the settings document — so a non-null
`scope` guarantees a non-null `settings_path`, which is the invariant a
launcher rests on when it drops `--add-dir`. The reverse is false: a profile
that grants a directory it is not scoped to renders the document with no scope
at all. Read the key; do not infer it.

`cwd_preference` is `"boot_dir"` or `"project_dir"` — the preference, not a
resolved directory, because Cairn launches nothing and choosing between the two
on a launcher's behalf would be Cairn deciding the invocation.

`project_dir_arg` is the provider's own flag, **already split into argv tokens**
and with the placeholder **left standing**. Both halves are deliberate:

- *Split here.* The spec spells it as one string, `"--add-dir {{.ProjectDir}}"`,
  and a consumer handed that must substitute and split itself. Substitute first
  and a scope named `.../scope with space` becomes two arguments instead of one
  — silently, on the one input nobody tests. Nothing in `go-providers` states
  the safe order, and both of that library's own boot-dir examples replace on
  the whole pattern, so a consumer reading this document alongside them lands
  on the unsafe recipe. Split into tokens, no element can contain whitespace
  and the shape is safe by construction.
- *Substituted there.* `go-providers` documents substitution as the app's job
  at spawn time, and doing it here would tie the key to `scope`: a boot with no
  scope would have to report `null`, which already means "this harness needs no
  such flag". Replace `{{.ProjectDir}}` in each token with `scope`.

There is no `version` field, and the rule that replaces it is: **new keys are
free; renaming or removing one is breaking and must update every consumer in
the same change.** That does not rest on how many consumers there are to
break, and it holds at none, because the cost of a rename lands on whoever
adopts the contract next and the two directions do not cost the same. Keeping
a key that has already been published costs a line in a struct. Renaming one
hands a consumer still reading the old name a `null` where a path used to be —
not a parse error, but a session opened without `--settings`: no access grant,
no trusted tier, and nothing reporting either. A version number would not have
stopped a rename; the sentence might.

Minimal shell, at a prompt. It needs `jq` — a JSON parser for a document with
named keys, in place of a regular expression over prose:

```bash
BOOT="$(cairn boot eng --json)"   # stdout is the object, stderr still yours
BOOT_DIR="$(jq -r '.boot_dir // empty' <<<"$BOOT")"
SETTINGS="$(jq -r '.settings_path // empty' <<<"$BOOT")"
[ -n "$BOOT_DIR" ] || { echo "cairn reported no boot directory" >&2; exit 70; }

set --
[ -n "$SETTINGS" ] && set -- "$@" --settings "$SETTINGS"
cd "$BOOT_DIR" && exec claude "$@"
```

**`// empty` on every read, and it is not decoration.** `jq -r` prints the four
characters `null` for a null value, so a read without it hands back `null` as
though it were a path — `--settings null`, which is exactly the argv the null
rule above exists to prevent, arriving through the one reader that turns the
null back into a string. Building the arguments with `set --` rather than
interpolating a string is the other half: a settings path with a space in it
stays one argument.

Minimal Go, engine-side:

```go
// Materialize a boot directory. Cairn writes files and describes them; it
// starts nothing.
cmd := exec.CommandContext(ctx, cairnBin, "boot", binding, "--scope", scope, "--json")
cmd.Stderr = os.Stderr // slot failures and diagnostics surface to the operator
out, err := cmd.Output()
if err != nil {
    return fmt.Errorf("cairn boot %s: %w", binding, err)
}
var boot struct {
    BootDir       string   `json:"boot_dir"`
    Scope         *string  `json:"scope"`
    SettingsPath  *string  `json:"settings_path"`
    ProjectDirArg []string `json:"project_dir_arg"`
}
if err := json.Unmarshal(out, &boot); err != nil {
    return fmt.Errorf("cairn boot %s: %w", binding, err)
}

// Tachyon spawns it. Cairn is done.
args := []string{}
if boot.SettingsPath != nil {
    args = append(args, "--settings", *boot.SettingsPath)
}
// Only for a provider that grants directories on the command line. Claude Code
// does not — the grant is in the settings file above — so this is nil here.
// Substituting per token is what keeps a scope with a space in it one argument.
if boot.Scope != nil {
    for _, tok := range boot.ProjectDirArg {
        args = append(args, strings.ReplaceAll(tok, "{{.ProjectDir}}", *boot.Scope))
    }
}
launch := exec.Command("claude", args...)
launch.Dir = boot.BootDir
```

### What Cairn is still missing for this

Which slots failed. A launcher building a UI wants to show a warning rather
than making the operator read stderr, and the report carries no `slots` array
yet. Adding one is additive and therefore free; it is the next key to add when
a launcher wants it.

### On `plant.Planter`

Cairn implements `go-agent-wrapper/plant.Planter` so it speaks the same
planting contract Nanite does. **Nothing calls it**, including Cairn itself,
which uses its own `PlantFiles` because `plant.Spec` carries no file modes and
a skill's executable bit is load-bearing.

For a launcher that shells out — Tachyon, a shell script, you at a prompt — it
is irrelevant: it is a Go interface, and the process boundary is the seam. It
only matters if something imports Cairn as a library and wants to plant
*through* the contract it already speaks. It costs ~40 lines and nothing at
runtime, so it stays until something either uses it or clearly never will.
