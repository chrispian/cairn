---
name: surface-discovery
description: Raise an unexpected off-path finding to the user with a neutral A/B/C choice, instead of dropping it, deferring it silently, or burying it in a summary. Use the moment you notice something outside your current task.
---

A small primitive. You noticed something that isn't on your task's critical path — surface it now, with no opinion attached, and let the user route it.

## Trigger

**The trigger is the noticing, not the severity.** A bug in adjacent code, a stale doc, a failing unrelated test, an outdated dependency, a footgun, a TODO in code you're touching, anything that makes you think *"huh, that's odd."*

If you noticed it, surface it.

## The three failures this prevents

1. **Silent defer** — mentioned once in passing, never raised again.
2. **Self-deferred** — "that's a separate concern," decided unilaterally.
3. **Surfaced with an opinion baked in** — framed as a yes/no or a recommendation, so the user is choosing your answer instead of their own.

## The format

Output exactly this. Don't paraphrase, don't add framing, don't argue for an option:

```
Hey, noticed [one line: what it is] ([file:line, or 1–2 lines of why it's notable]).

Want me to:
- A) Go into detail
- B) Capture for later review (`/capture-followup`)
- C) Add a follow-up task (Torque)
```

Then **stop and wait.**

- **A** — pause the task, show the evidence (read the code, quote it), explain the technical impact. No priority opinion. Don't act further without instruction.
- **B** — `/capture-followup`, report `Captured follow-up: <slug>`, resume immediately.
- **C** — `torque_task_create`, report the id, resume immediately.

## Rules

- **Surface immediately.** Batching becomes deferring.
- **One per message.** Each gets routed independently.
- **No opinion.** No "I think", "we should", "critical", "minor", "low priority".
- **Return to the task** after routing. A discovery is not a licence to scope-creep.
- **Don't surface what IS the task**, or a direct prerequisite of it. Handle those inline.
- **Don't surface twice.** Track what you've raised this session.
- **Cluster by root cause.** Several symptoms of one thing → surface the cause, note it likely affects N others.

## Not this skill's job

Deciding whether it matters, triaging it, fixing it, or assigning severity. You notice and present. The user routes.
