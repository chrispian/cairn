---
name: end-of-session
description: Wrap up a work session — reconcile the git tree, commit what this session authored, review drafts, and capture durable signal. Use when the user signals the session is ending.
---

A fixed checklist so wrap-up doesn't depend on remembering. Run the steps in order; skip a step only when it plainly doesn't apply, and say so.

**Invoker precedence.** If the user's instruction contradicts a default here ("just commit, skip the rest"), they win.

---

## 1. Snapshot the tree

```bash
git status --porcelain -u    # -u is load-bearing: untracked files are where stray work hides
git log --oneline -1
git branch --show-current
```

Not a git repo → skip steps 1–3, note it, continue.

## 2. Classify every changed file

- **SESSION** — this session touched it via Write/Edit/Bash. Your tool history is the source of truth.
- **USER** — changed, but you never touched it.
- **AMBIGUOUS** — a broad command (`go fmt ./...`, a codegen script) may have touched it.

When unsure, mark AMBIGUOUS. Erring toward the user is cheap; auto-committing something they were mid-edit on is not.

## 3. Commit SESSION files only

- Stage **by explicit name**. Never `git add -A`, `.`, or `-u`.
- Match the repo's commit style — check `git log -5 --oneline`.
- Multiple logical changes → multiple commits.
- **Never push.** Never amend or squash.

USER and AMBIGUOUS files are **never** auto-committed. Surface them (§6) instead.

## 4. Review this session's drafts

```bash
ls ~/dev/agent-os/workspaces/drafts/<project>/<session>/
```

For each draft, present one line and ask where it goes:

> `boot-arch.md` — promote to Tesseract / promote to repo docs / keep drafting / discard?

On promote: write it to its real home (`knowledge_write` for inward, repo + PR for outward), then move the draft to `~/dev/agent-os/archive/drafts/<project>/<YYYY-MM-DD>/`.

**Never self-promote.** No drafts → skip silently.

## 5. Capture durable signal

Scan this session's tracking files and your own history for content matching these headings, and route each:

| Found | Skill |
|---|---|
| `## Decisions` / `## Decisions locked` | `/capture-decision` |
| `## Follow-ups` / `## Follow-up candidates` | `/capture-followup` |
| `## Known limitations` / `## Preserved tech debt` / `## Out of scope` (with rationale) | `/capture-limitation` |

The capture skills dedup themselves. Skip entirely on a trivial session — no commits, no decisions, nothing deferred.

**Capture signal, not narration.** If a future session wouldn't search for it, don't write it.

## 6. Surface anything needing a decision

For each USER/AMBIGUOUS file, and anything else unresolved, use exactly this:

> Hey, noticed **{thing}** — {1–3 lines, facts only}. Want me to:
> - **A) Go into detail**
> - **B) Capture for later** (`/capture-followup`)
> - **C) Add a follow-up task** (Torque)

State the discovery as fact. No opinion, no recommended pick, no yes/no substitute. Pose one at a time and **wait**.

Batch tightly-related files into one surface. If a directory holds many pre-existing files this session didn't touch, that's baseline — give a one-line count, don't enumerate.

## 7. Reconcile Torque

If the session advanced tracked work:

```
torque_task_list  project_id=<id>  status=todo,doing,review
```

The shape to look for is a task carrying a comment newer than its own last
status change — work was logged and the status did not follow. The Stop hook
surfaces these too; this step is the deliberate pass over the same set.

Match against what actually happened. Stale status → surface via §6 (option A
transitions it). Never auto-transition.

Torque refuses `todo → done` directly, so a task that skipped the middle walks
`todo → doing → review → done`.

Torque is the tracker — `mcp__mux__torque_*`.

## 8. Write the session-close record

**This is what the next session reads.** `/boot-prompt` pulls the most recent record for this project when orienting — if nothing writes them, there's nothing current to pull.

Write only what no tool can regenerate. Commits come from git, task state from Torque, decisions and limitations from the captures in §5. Three things don't:

```
knowledge_write
  namespace       = user/chrispian/knowledge/session-close/<project>
  key             = <session_id>
  kind            = session_close
  source          = agent
  pointer_scheme  = nil
  pointer_locator = <project>
  summary         = one line: what shipped, what's still open
  tags            = ["session-close", "<project>", "<role-if-any>"]
  session_id      = <session_id>
  body            = the three sections below
```

**Tag it with the project**, so a scoped lookup finds it. If the session ran under a named role, tag that too.

```markdown
## Next
- <what you'd pick up first, and why it's first>

## Narrative
<3–5 sentences, written while context is hot. What a cold agent needs to feel
 oriented: the shape of the session, where the risk sits, what surprised you.
 This goes into the next boot close to verbatim.>

## Ruled out
- <hypothesis, and what disproved it>
```

**`## Next` is a suggestion, not a dispatch.** It says what you'd do next and why — it is not a prompt for another agent. If this session feeds a *specific* next session in a workflow chain, compose that agent's prompt instead: `agent-setup/bin/prompt.sh <role> --vars`. The two are different artifacts and both can be right.

**`## Ruled out` is the section that pays.** Anyone can re-find the root cause given time; what's expensive is re-eliminating the six things it wasn't.

**Skip on a trivial session** — no commits, no decisions, nothing deferred. A record saying "read some files" is noise in the next boot.

## 9. Report

```
End of session — {branch} @ {sha}

Committed:  {sha} {subject}
Drafts:     {n promoted, n still in flight | none}
Captured:   {n decisions, n follow-ups | none}
Open:       {n items awaiting your decision | nothing}
```

Omit empty lines. Fully clean session → `All clean. Nothing to commit, nothing to surface.`

---

## Do not

- **Do not write a boot-prompt file.** Boot context is materialized — see `/boot-prompt`. The session-close *record* (§8) is different: it's append-only and timestamped, so boot pulls the newest and a stale one is visibly stale. A boot-prompt *file* has exactly one copy that is silently either current or wrong.
- **Do not invoke `/boot-prompt`** unless the user asks.
- **Do not run mid-task** to check status. This is wrap-up, not a progress report.
- **Do not push, open PRs, run tests, or clean worktrees.** All separate, explicit actions.

## Worktree note

If asked to clean worktrees: remove only those **merged AND clean**. Anything with unmerged commits or uncommitted changes gets surfaced, never silently deleted. Measured on one repo, only half of "finished" worktrees were actually safe.
