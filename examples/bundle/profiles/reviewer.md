---
# reviewer — a profile that exists to be dispatched, and boots on its own too.
#
# `spec.subagent` is the WHOLE of what its definition holds. The profile that
# names it neither narrows nor widens it: there is no tool intersection and no
# depth cap, so a tool the reviewer needs is a change made HERE. `name` is the
# one key cairn writes — it is forced to the profile id, because the harness
# resolves a definition by that field and a mismatch is silent.
#
# The `body` key is the definition's prompt, and it is not this profile's own
# prose below the frontmatter: a dispatched subagent already receives the boot
# directory's CLAUDE.md, and through it AGENTS.md, so the cascade is already in
# its context and repeating it here would only add an ancestor's persona to a
# profile that does something else.
id: reviewer
extends: base
name: Reviewer
description: Reviews a diff with no shared context.
provider: claude
spec:
  subagent:
    description: Reviews a diff with no shared context. Use after a change is written and before it lands.
    tools: [Read, Grep, Glob]
    model: sonnet
    body: |
      Read the diff. Report what you found and nothing else.
---
