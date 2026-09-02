---
name: capture-followup
description: Record deliberately deferred work to Tesseract so it resurfaces when a future session touches the same area. Use when you choose not to fix something now, or a "## Follow-ups" section lands in a tracking file.
---

A soft signal for future recall — no ticket, no lifecycle. For work that should actually get scheduled, use `/blg` (Torque).

## Procedure

**0. Dedup.** `tesseract_recall namespaces=["user/chrispian/memory/followups"] query="<slug>"`. Followups go stale faster than anything else here — the work gets done, or its context moves.

**On a hit, you must choose one** — a hit you neither supersede nor deprecate stays live and reads as independent corroboration:

| The hit is… | Do |
|---|---|
| the same record, understanding changed | `memory_write` same key + **`supersedes: <revision_id>`** from the lookup result |
| wrong, or no longer applies | `mcp__mux__memory_deprecate revision_id=<id>` |
| genuinely a different record | write the new key — and say in one line why it is different |

The lookup you just ran returned `revision_id`. That is the value `supersedes` takes.

**1. Dedup.** `tesseract_recall namespaces=["user/chrispian/memory/followups"] query="<slug>"`. Already there → say so and offer to update it rather than duplicating.

**2. Write.**

```
memory_write
  namespace       = user/chrispian/memory/followups
  memory_key      = <slug>                  # 3–5 words, a-z 0-9 _ ONLY
  payload_summary = one sentence, ≤140 chars
  payload_body    = what's deferred, WHY it was deferred, and where to pick it up
  tags            = ["followup", "captured_during_session", "project:<project>"]
  author_agent_id = claude-code | codex | …
  author_version  = model slug
  trigger         = manual
  session_id      = the SESSION: value from the SessionStart hook
  origin          = observation
  confidence      = 0.85
  status          = canonical
```

**3. Confirm** in one line: `Captured follow-up: <summary>`

## Rules

- **Hyphens are rejected** in keys. Normalize to underscores.
- **Always record why it was deferred.** Without it a future session has to reconstruct the reasoning and usually just re-decides. The "why" is most of the value.
- Point at the files or prior decisions involved.
- Project and ticket are tags, never key segments.

## Follow-up vs backlog

- **Follow-up** (here) — surfaces on recall when someone works nearby. Agents only.
- **`/blg`** (Torque) — a real item with priority that gets scheduled.

Both when it's genuinely both. Neither for "would be nice someday."
