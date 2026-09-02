---
name: qstatus
description: Compact snapshot of in-flight work — active sprints, tasks in progress, anything stuck. Use for a mid-session check-in or when asked how things are going.
---

**Run this in a subagent.** Raw task and sprint lists are large; only the digest should reach the calling context.

## Resolve the project first

`torque_project_list`, matched against the current working directory's `repo_path` (longest prefix). If that doesn't resolve, ask — don't guess, and never hardcode a project id.

## Subagent prompt

> You are a status reporter. Return ONLY the block below — no raw API output, no commentary.
>
> 1. `mcp__mux__torque_sprint_list` — project_id=`<id>`, limit=5, active only
> 2. For each active sprint: `mcp__mux__torque_task_list` parent_id=`<sprint_id>` — count by status
> 3. `mcp__mux__torque_task_list` — project_id=`<id>`, status=doing, limit=8
>
> ```
> === QSTATUS ===
> <project>
>
> Sprints
>   <code>  <todo>t / <doing>d / <done>✓  of <total>
>   ...or "none active"
>
> In progress
>   <task-id>  <title>            (<sprint>)
>   ...or "none"
>
> Stalled
>   <task-id>  <title>  — doing for <N>d
>   ...or "none"
> ===============
> ```
>
> "Stalled" = `status: doing` with no update in over 7 days. That's the line worth reading; the rest is context for it.

## Output

Print the subagent's block. Add nothing.

## Invariants

- **Always via subagent.** Never pull raw task lists into the main context.
- Read-only. Never transition a task from here — if something looks stale, surface it via `/surface-discovery`.
- Compact. Minimal footprint is the whole purpose.
