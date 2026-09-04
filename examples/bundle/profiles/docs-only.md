---
# docs-only — a part.
#
# There is no "fragment" kind. A part is an ordinary profile that happens to be
# small, which is why this file looks like every other one and why
# `cairn show docs-only` and `cairn boot docs-only` both work on it alone.
#
# It extends base for the same reason engineer does — a part needs a provider
# and a template to be bootable on its own — and that shared ancestor is
# exactly the case composition has to get right: folding base a second time
# behind engineer would put base's AGENTS.md template back in front of
# engineer's. A profile the resolution has already reached is folded once,
# where it first landed.
#
# What it contributes when composed is what it declares below and nothing else.
id: docs-only
extends: base
name: Docs only
description: Narrows a session to user-facing documentation.
spec:
  # An empty slot: it renders nothing until `--set direction=<text>` supplies
  # the content at materialization. A direction worth reusing is written here
  # instead, and then it needs no flag at all.
  #
  # The section below is what a --set does NOT keep. A --set replaces the slot
  # of that name WHOLE, exactly as a part declaring that slot would, so the
  # heading goes with it — spec.slots composes by name and a member is never
  # merged field by field. The heading is here for the case where this file
  # carries the content.
  slots:
    - name: direction
      section: "## Direction"
      source:
        kind: inline
        inline:
          content: ""
  skills:
    - capture-decision
---

Write user-facing documentation only. Prefer the reader's vocabulary over the
codebase's, and leave the API reference to the generator that owns it.
