---
name: push
description: Push work off this machine. Use before any git push.
---

**Invoker precedence.** If Chrispian's instruction contradicts a default here, they win.

This is the last cheap moment. After it, the work is somewhere else.

## Steps

**1. The tree is what you think it is.**
`git status --short` and `git log --oneline @{u}..` — read what is about to leave.

**2. The gate is green.**
`make check` where a repo has one. If it does not pass, say so and stop here.

**3. Torque reflects reality.**
Every task this work belongs to carries a comment saying what landed, and a
status that matches. `torque_task_get` to confirm, `torque_comment_add` and
`torque_task_transition` to correct.

Torque refuses `todo → done` directly, so a task that skipped the middle walks
`todo → doing → review → done`.

**4. Push.**
