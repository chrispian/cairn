---
name: pull-request
description: Open a pull request. Use before gh pr create.
---

**Invoker precedence.** If Chrispian's instruction contradicts a default here, they win.

A PR is read by someone who was not here. Everything it claims has to stand on
its own.

## Steps

**1. Run the push skill first.** A PR inherits whatever the branch carries.

**2. Derive the description from the task**, not from recollection.
`torque_task_get <id>` — the title, the acceptance criteria, and what the work
log says actually landed.

**3. State the scope, including what is outside it.** A reviewer who knows what
a change does not cover reviews the change in front of them.

**4. Carry the evidence.** Test output, the gate's result, the command behind any
number in the body.

**5. Link the task.** The PR names the task id; the task gets a comment naming
the PR.
