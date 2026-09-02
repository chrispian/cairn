---
name: adr
description: Write a formal Architecture Decision Record — context, options, decision, consequences. Use for a structural decision that needs a reviewable long-form record. For a short decision note, use /capture-decision instead.
---

The long-form sibling of `/capture-decision`. Same destination, different depth.

| | `/capture-decision` | `/adr` |
|---|---|---|
| Shape | a few sentences + rationale | structured: Context / Options / Decision / Consequences |
| Path | direct to Tesseract | draft → review → promote |
| Use for | a call made in conversation | a structural decision others will need to reconstruct |

If you're unsure, use `/capture-decision`. Most decisions don't need an ADR.

## Procedure

### 1. Draft it

Write to `~/dev/agent-os/workspaces/drafts/<project>/<session>/adr-<slug>.md`:

```markdown
# ADR: {title}

**Status:** proposed
**Date:** {YYYY-MM-DD}

## Context
{What forced a decision. The constraint or problem — not background narration.}

## Options considered
{Each real alternative and why it lost. An ADR with one option is not an ADR.}

## Decision
{What was chosen. Specific and actionable.}

## Consequences
{What follows — including the bad parts. Migration cost, new constraints,
what becomes harder. An ADR listing only upsides is marketing.}
```

Keep each section to a paragraph or two. Length is not rigor.

### 2. Stop and report

> Drafted ADR: {title} → {path}. Review it and I'll promote.

**Do not promote yourself.**

### 3. On approval, promote

Write to Tesseract:

```
knowledge_write
  namespace  = user/chrispian/knowledge/<project>/adr
  key        = adr_<slug>              # a-z 0-9 _ only
  kind       = doc
  source     = manual
  pointer_scheme  = nil                # content-first; the body IS the artifact
  pointer_locator = <project>
  summary    = one-line statement of the decision
  body       = the full ADR
  tags       = ["adr", "decision", "<project>"]
```

Then archive the draft to `~/dev/agent-os/archive/drafts/<project>/<YYYY-MM-DD>/`.

**Only write a file into the repo** if that project keeps an outward-facing `adr/` directory its users read. Inward ADRs live in Tesseract — repo `adr/` folders are how three separate architecture homes happened.

## Notes

- Never renumber or overwrite an existing ADR. A decision that changed gets a new record with `supersedes` set to the prior revision.
- Status is `proposed` until Chrispian says otherwise.
- Cross-link related records with `[[key]]`.
