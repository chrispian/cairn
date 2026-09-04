---
# engineer — a working profile. Slots are the interesting part: they resolve at
# boot, so the rendered document carries a live snapshot rather than a stale
# paragraph.
#
# A duration is written the way Go writes one. Cairn translates it as it reads
# the catalog, so `5s` here is five seconds and not five nanoseconds.
#
# "templates" merges by destination path, so this block names only the entry it
# changes: base's "CLAUDE.md" stands untouched and is not restated. That is the
# cascade in one line. Ordering is yours — nothing puts an ancestor's prose
# first unless the template does.
#
# "files" plants arbitrary paths. A value is either a literal string or a slot
# source, resolved by the same resolvers, which is how a task bundle gets
# planted from live state instead of frozen into the profile. Note the
# `|| true`: a slot that fails is survivable and cairn carries on, but a FILE
# source that fails REFUSES THE BOOT, because a missing file is a hole at a
# path the profile promised and nothing downstream notices.
#
# "trees" copies a directory whole. A single file rides "files" with a
# static_file source; do NOT reach for a static_dir slot, which concatenates
# what it finds into one string.
#
# "subagents" names other profiles. Each renders .claude/agents/<id>.md from
# THAT profile's own spec.subagent — see reviewer.
#
# "prompts" names files in ../prompts/. Each is SUBSTITUTED like a template and
# planted at .claude/commands/boot/<name>.md, so the operator types
# /boot:handoff. Cairn plants it and stops; nothing fires a prompt. Try
# `--prompt reset-scope` to add the other one for a single launch.
id: engineer
extends: base
name: Engineer
description: Implements one task end to end.
provider: claude
spec:
  skills: [capture-decision]
  prompts: [handoff]
  subagents: [reviewer]

  templates:
    "AGENTS.md": { kind: static_file, static_file: { path: $CAIRN_PROFILE_ROOT/templates/engineer.md } }

  trees:
    "docs/templates": $CAIRN_PROFILE_ROOT/templates

  slots:
    - name: git
      section: "## Repository"
      source: { kind: cmd, cmd: { run: git status --short --branch, timeout: 5s } }
    - name: recent
      section: "## Recent commits"
      source: { kind: cmd, cmd: { run: git log --oneline -10, timeout: 5s } }

  mcp:
    - name: mux
      command: mux
      args: [mcp, --proxy, --servers, "tesseract,torque"]

  files:
    "notes/scratch.md": "Scratch space for this session. Nothing reads it.\n"
    "context/branch.md": { kind: cmd, cmd: { run: git branch --show-current || true } }
---
