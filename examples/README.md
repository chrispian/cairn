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

It writes an abstract `base`, a concrete `engineer` that extends it, two scope
aliases, and two bindings. Read it — it is the profile-authoring documentation.

Three things it demonstrates that are easy to get wrong:

- **`extends` is uniform closest-wins.** A child that restates a key replaces
  the ancestor's value entirely; there is no union, no merge, no per-field
  special case. `body` is the sole exception and concatenates ancestor-first.
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
  AGENTS.md              body from the cascade + a scalar Profile block
  CLAUDE.md              @AGENTS.md, and nothing else
  boot.md                slots, resolved just now
  .mcp.json              spec.mcp
  .claude/settings.json  spec.settings, verbatim
  .claude/skills/        spec.skills, whole directory trees
  <spec.files>           arbitrary paths, literal or resolved
```

**`boot.md` is where the leverage is.** It is resolved at materialization, so
it carries live state rather than a paragraph that went stale a month ago. The
seeded `engineer` pulls `git status` and `git log`; the same mechanism reaches
an HTTP endpoint or any command, which is how memory and task state get in:

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
current where `boot.md` is a snapshot. The failure is not lost: it goes to
**stderr**, where the operator reads.

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

Cairn claims four things in that directory — `AGENTS.md`, `CLAUDE.md`,
`settings.json`, and the `skills/` subtree. Everything else (`settings.local.json`,
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
