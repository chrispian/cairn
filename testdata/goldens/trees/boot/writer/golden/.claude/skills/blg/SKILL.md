---
name: blg
description: File a backlog item in Torque. Use when something surfaces mid-session that deserves prioritized work but is out of scope right now.
---

Fast capture into Torque. Should take one exchange — this is a capture tool, not a planning exercise.

## Procedure

### 1. Resolve the project

In order, stopping at the first that works:

1. The user named a project → `torque_project_list`, match on name.
2. Otherwise match the current working directory against each project's `repo_path`, longest prefix wins.
3. Otherwise **ask**.

**Never fall back to a hardcoded project id.** A backlog item filed against the wrong project is worse than one extra question.

> Some `repo_path` values in Torque point at directories that no longer exist. If a project's name clearly matches but its path doesn't resolve, use it anyway and say so in your confirmation.

### 2. Shape the item

- **Title** — imperative, specific. *"Investigate PostgreSQL migration path"*, not *"postgres"*.
- **Body** — why it matters and enough context to act on it cold. Two or three sentences.
- **Tags** — infer from content; always include `backlog`.
- **Priority** — `B` unless the user says otherwise.

### 3. Create and confirm

```
torque_task_create  project_id=<id>  title=  body=  tags=  status=backlog
```

One line back:

```
✓ {CW-id}: {title}   [{project} · {priority} · {tags}]
```

## Backlog vs follow-up

- **`/blg`** → Torque. A real work item with priority and a lifecycle. Use when it should get scheduled.
- **`/capture-followup`** → Tesseract. A soft signal for a future session, no lifecycle. Use when it should surface if someone touches that area again.

Both when it's genuinely both. Neither for passing thoughts.

## Do not

- Don't file vague items. *"Improve error handling"* with no location is noise a future session can't act on.
- Don't file what you could just do in under a minute.
- Don't turn this into planning. Capture and move on.
