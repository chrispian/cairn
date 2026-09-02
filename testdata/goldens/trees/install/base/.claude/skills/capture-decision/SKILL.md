---
name: capture-decision
description: Record a design decision and its rationale to Tesseract. Use when the session chose between real alternatives, or a "## Decisions" section lands in a tracking file.
---

Short decision records go **direct** to Tesseract — no draft step. They're extracts of something already settled in conversation, not documents in flight. For a long-form structured record, use `/adr`.

## Procedure

**1. Dedup.** `tesseract_recall namespaces=["user/chrispian/memory/decisions"] query="<slug>"`.

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
  namespace       = user/chrispian/memory/decisions
  memory_key      = <slug>                  # 3–5 words, a-z 0-9 _ ONLY
  payload_summary = one sentence, ≤140 chars
  payload_body    = the decision + why + what was rejected
  tags            = ["decision", "captured_during_session", "project:<project>"]
  author_agent_id = claude-code | codex | …
  author_version  = model slug
  trigger         = manual
  session_id      = the SESSION: value from the SessionStart hook
  origin          = observation
  confidence      = 0.9
  status          = canonical
```

Use `status = draft` when confidence is below 0.7, the decision may still change, or the user hedged ("maybe", "tentatively"). Drafts get reviewed at end-of-session.

**3. Confirm** in one line: `Captured decision: <summary>`

## Rules

- **Hyphens are rejected.** `my-decision` fails; `my_decision` works. Normalize before writing — this is the most common papercut.
- **Project and ticket are tags, not key segments.** The key is just `<slug>`. `decisions.<project>.<ticket>.<slug>` is the legacy shape the namespace migration replaced.
- **Record the rejected alternatives.** A decision without them is an announcement. The reason a future session needs this is to know what was already ruled out and why.
- Link related records with `[[key]]`.

## Don't capture

Restated decisions, trivial calls, or anything a future session wouldn't search for. Signal over volume — the corpus is only useful while everything in it earns its place.
