---
name: qhealth
description: Compact service health check across the portfolio — what's down and what needs action, nothing else. Use before dispatching agents, after a deploy or restart, or when asked whether services are up.
---

**Run this in a subagent.** `cerberus_health` returns ~35 resources (~4KB) and `mux_health` adds more. That raw payload must not land in the calling context — the entire point of this skill is a small answer.

## Subagent prompt

> You are a health reporter. Call `mcp__mux__cerberus_health` and `mcp__mux__mux_health`. Return ONLY the block below — no raw output, no commentary.
>
> Report **exceptions, not inventory.** A list of 30 healthy services is noise.
>
> ```
> === QHEALTH ===
> daemon    <up|DOWN>   mux <version>
> resources <N> running · <M> stopped · <K> unhealthy
>
> NEEDS ACTION
>   <resource_id>  <status>  <recommended_action> — <recommended_reason>
>   ...or "none"
>
> STOPPED
>   <resource_id> ...   (omit any with operator_stopped: true)
>   ...or "none"
> ===============
> ```
>
> A resource with `status: running` but `healthy: false` belongs under NEEDS ACTION — usually `artifact_stale`, meaning the running binary is behind the built artifact. Include its `recommended_next_step` verbatim if present.
>
> Skip `operator_stopped: true` resources entirely — those are stopped on purpose.

## Output

Print the subagent's block. Add nothing.

## Invariants

- **Always via subagent.** Never pull the raw health payload into the main context.
- Read-only. Never apply, restart, or reload anything — report the recommended action, let the user decide.
- Exceptions only. If everything is healthy, that's three lines.
