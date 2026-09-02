---
name: capture-limitation
description: Record a known limitation, preserved tech debt, or a scope fence with its rationale to Tesseract. Use when a change deliberately does not fix something, or a "## Known limitations" / "## Out of scope" section lands in a tracking file.
---

For things the codebase will keep having — by choice or by cost. Distinct from a follow-up, which is expected to get fixed.

## Procedure

**1. Dedup.** `tesseract_recall namespaces=["user/chrispian/memory/limitations"] query="<slug>"`.

**On a hit, you must choose one** — a hit you neither supersede nor deprecate stays live and reads as independent corroboration:

| The hit is… | Do |
|---|---|
| the same record, understanding changed | `memory_write` same key + **`supersedes: <revision_id>`** from the lookup result |
| wrong, or no longer applies | `mcp__mux__memory_deprecate revision_id=<id>` |
| genuinely a different record | write the new key — and say in one line why it is different |

The lookup you just ran returned `revision_id`. That is the value `supersedes` takes.


**2. Write.**

```
memory_write
  namespace       = user/chrispian/memory/limitations
  memory_key      = <slug>                  # 3–5 words, a-z 0-9 _ ONLY
  payload_summary = one sentence, ≤140 chars
  payload_body    = what the limitation is, why it exists, the workaround,
                    and what a real fix would take
  tags            = ["limitation", "tech_debt", "captured_during_session", "project:<project>"]
  author_agent_id = claude-code | codex | …
  author_version  = model slug
  trigger         = manual
  session_id      = the SESSION: value from the SessionStart hook
  origin          = observation
  confidence      = 0.9
  status          = canonical
```

**3. Confirm** in one line: `Captured limitation: <summary>`

## Rules

- **Hyphens are rejected** in keys. Normalize to underscores.
- **Always give the workaround** if one exists. "What's broken" without "what to do instead" is half the value.
- Tag the ticket that *shipped with* the limitation, not the one that might fix it.
- A limitation stays recorded even after it's fixed — it documents the period it was live.

## Limitation vs follow-up

- **Limitation** — semi-permanent. Non-trivial fix cost, or deliberately accepted.
- **`/capture-followup`** — expected to be fixed, with a clear path.

Bugs are neither. Those go to Torque via `/blg`.

## Don't capture

Every scope-out. Only the ones that are non-obvious, or that change how the shipped thing should be used.
