---
name: capture-investigation
description: Write a dense dossier to Tesseract knowledge after a complex investigation, so a future session boots with the findings instead of re-deriving them. Use after root-causing something non-trivial, while the evidence is still fresh.
---

The point is that nobody re-runs this investigation. Not a status report — a reusable artifact.

## Preconditions

All three, or stop and ask:

1. A non-trivial investigation just **completed** — root cause identified, evidence gathered.
2. Follow-on work exists that depends on the findings (tasks filed, or about to be).
3. The evidence is **still in this session** — queries, `file:line` refs, commit SHAs, session ids. Don't reconstruct it after the fact; a dossier assembled from memory is worse than none.

**Not for:** session status (`/end-of-session`), a single decision (`/capture-decision`), or a cross-project pattern (that's knowledge, but not investigation-shaped).

## Procedure

### 1. Draft it

This is a document, so it drafts first — `~/dev/agent-os/workspaces/drafts/<project>/<session>/investigation-<slug>.md`:

```markdown
# Investigation: {slug}

**Boot context** — project, branch, commit SHA, related task ids.
**Identifiers** — session ids, run ids, anything needed to re-pull the evidence.

## Evidence
{Compact tables, one per source (DB / log / tool trace). Rows, not prose.}

## Root cause
{Numbered chains. Each names the mechanical "why" and the affected task.
 Where chains compose (A → B → symptom), say so explicitly.}

## Replay
{Literal shell/SQL/tool invocations, copy-pasteable. No narration inside the block.}

## Code refs
{path:line — one per finding}

## Dead ends
{Hypotheses considered and falsified, each with what disproved it.}

## Where to start
{Which follow-on work is entangled and why. A hint, not a plan.}
```

**The Dead ends section is the one that pays.** Anyone can re-find the root cause given time; what's expensive is re-eliminating the six things it wasn't. If you cut a section, don't cut that one.

### 2. Stop and report

> Drafted investigation: {slug} → {path}. Review and I'll promote.

### 3. On approval, promote

```
knowledge_write
  namespace       = user/chrispian/knowledge/<project>/investigations
  key             = <slug>_<YYYY_MM_DD>       # a-z 0-9 _ only
  kind            = investigation
  source          = manual
  pointer_scheme  = nil                        # content-first; the body IS the artifact
  pointer_locator = <project>
  summary         = 3–5 sentences — what broke, why, what it blocks.
                    This is what surfaces in lookups. Make it load-bearing.
  body            = the full dossier
  tags            = ["<project>", "investigation", "<theme>"]
  confidence      = 0.9
```

Then archive the draft to `~/dev/agent-os/archive/drafts/<project>/<YYYY-MM-DD>/`, and comment the key onto the related Torque tasks so the pointer is findable from the work.

## Note on pointers

`pointer_scheme: nil` is deliberate. File pointers rot — 44% of the existing ones are dead, because files move and Tesseract never hears about it. The body survives regardless, so the body is the artifact.
