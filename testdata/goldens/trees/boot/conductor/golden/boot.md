## What Chrispian can boot
Bindings. The name is all the launcher needs.
  chrispian     planner       ~/dev/chrispian
  conductor     conductor     ~/dev/chrispian
  dir-nanite    director      ~/dev/hollis-labs/apps/nanite
  eng-cairn     engineer      ~/dev/projects/cairn
  eng-nanite    engineer      ~/dev/hollis-labs/apps/nanite
  eng-setup     engineer      ~/dev/projects/agent-setup
  nanite        orchestrator  ~/dev/hollis-labs/apps/nanite
  orch-nanite   orchestrator  ~/dev/hollis-labs/apps/nanite

Profiles. Each one needs a scope: a path, or an alias below.
  architect     Decides structure and boundaries, including what they rule out.
  conductor     Runs Chrispian's other sessions from one seat, and holds no scope of its own.
  director      Holds a program across orchestrators and is the escalation point.
  engineer      Implements one task end to end and reports without landing it.
  orchestrator  Runs a batch of workers against one scope and reports up.
  planner       Turns intent into an ordered, fenced plan an execution session can follow.
  reviewer      Reviews a change with no shared context on how it was built.
  writer        Writes prose for a human audience. Drafts and stops.

Scope aliases.
  agent-setup   ~/dev/projects/agent-setup
  cairn         ~/dev/projects/cairn
  chrispian     ~/dev/chrispian
  nanite        ~/dev/hollis-labs/apps/nanite
  tesseract     ~/dev/hollis-labs/apps/tesseract
  torque        ~/dev/hollis-labs/apps/torque
