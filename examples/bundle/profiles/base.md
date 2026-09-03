---
# base — abstract, the root of the cascade and the profile `install` renders.
#
# The prose is a TEMPLATE, not a body. Cairn substitutes markers in it and
# writes it where the profile said; it composes no heading, no section and no
# order of its own, and it renders nothing for a profile that declares none.
#
# CLAUDE.md is a template too. `@AGENTS.md` is the harness's own import syntax
# and cairn leaves it alone — markers are `<!-- cairn:... -->` precisely so the
# two cannot collide.
#
# Every value that names somewhere to read from takes $VAR and ~/, and
# $CAIRN_PROFILE_ROOT is this bundle. That is what lets the whole directory be
# copied somewhere else and still boot.
id: base
abstract: true
name: Base
description: The floor every profile extends.
provider: claude
spec:
  skills_dir: $CAIRN_PROFILE_ROOT/skills

  settings:
    permissions: { defaultMode: acceptEdits }

  templates:
    "AGENTS.md": { kind: static_file, static_file: { path: $CAIRN_PROFILE_ROOT/templates/base.md } }
    "CLAUDE.md": "@AGENTS.md\n"
---
