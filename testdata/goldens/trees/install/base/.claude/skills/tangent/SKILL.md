---
name: tangent
description: Open a browser workflow when chat is the wrong shape for structured input — triaging many items with one decision, multi-question feedback, or design review. Requires the Tangent MCP server.
---

Chat is linear and lossy for structured input. Tangent opens a browser room over MCP and blocks until the user submits or cancels.

> **Availability:** the Tangent MCP server is configured in Codex only. In Claude Code these tools won't resolve — check with `mux_discover` before assuming, and fall back to plain CLI questions if absent.

## Use when

- The same decision applies across many items (triage).
- Structured feedback across several questions or mixed input types.
- Design review that needs to be seen rather than described.
- A multi-step workflow that should persist in one room over time.

**Not** when a CLI question would do, or the task is output-only.

## Procedure

1. **Check availability.** Confirm the `tangent.*` tools resolve. If Tangent isn't running: `cd ~/dev/hollis-labs/apps/tangent && ./tangent`

2. **Pick the tool.**
   - `tangent.triage` — one decision shape, many items
   - `tangent.feedback` — structured questions, mixed answer types
   - `tangent.design-iteration` — iterative HTML/CSS review
   - `tangent.session_*` — multi-envelope room over time

3. **Build one coherent payload.** Unique ids, batch related items, use `context`/`help` where it saves the user effort. Never send an empty workflow.

4. **Send.** Tangent returns a room URL (`http://127.0.0.1:7842/r/<roomID>`). The call completes on submit or cancel.

5. **Handle the result.**
   - Submit → process the returned payload.
   - Cancel → acknowledge, ask how to proceed. Don't retry blindly.
   - `SESSION_BUSY` → don't queue. Wait, switch rooms, or ask which room should continue.

## Notes

- Rooms persist across restarts and support multi-envelope history.
- `tangent.session_get` is the checkpoint surface for long-running rooms.
- Replaces the retired `fast-triage` skill.
