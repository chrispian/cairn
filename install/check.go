package install

// Status is what a check found at one path of the installed layer.
//
// # What `--check` certifies, and what it does not
//
// A check answers one question: would an install change this root? It
// re-renders the layer, compares every path cairn claims against what is on
// disk, and sweeps the directories cairn fills for files the render did not
// produce. [Report.Clean] is the verdict on that comparison and on nothing
// else, and [Report.ExitCode] carries it out of the process unchanged.
//
// It deliberately does not certify that the layer it found is internally
// sound. A document cairn rendered may point at a document that was never
// written, and a check says "In sync" over it. That is a position and not an
// oversight, and the argument is written here so the next person to hit a
// dangling pointer finds it made rather than making it again.
//
// # The dangling pointer
//
// It is the failure docs/plan.md §5 made the pointer file a template to
// prevent. A template whose markers all stand for nothing renders no bytes, a
// render of no bytes writes no file, and .claude/CLAUDE.md therefore lands
// holding `@AGENTS.md` with no .claude/AGENTS.md beside it. The harness
// resolves the include to nothing and says nothing about it. The layer boots,
// and every session under it is missing its instructions.
//
// A check catches that on a root that already held the file. The claim set
// comes from the renderer registration rather than from one render — see
// [SweepPlan] — so an instruction file that stopped rendering is a
// [StatusOrphan] and the check exits non-zero naming it. Into a root that
// never held one there is nothing to orphan and nothing rendered to diff, and
// the report is clean because the root does hold exactly what this render
// produces: a pointer, and no target. Drift is the wrong instrument for that
// case rather than a broken one.
//
// # The soundness reading, and why it is not taken
//
// The other reading is that a check certifies soundness: an operator runs one
// to ask whether their installed layer is right, and a pointer into nothing is
// the clearest possible no. It would arrive here, as a status [Status.Finding]
// counts, beside the informational [StatusUnclaimed]. Three things stand
// against it.
//
// The finding has no path to sit at. An [Entry] says what was found at one
// path, and in this case every path holds what it should: the pointer matches
// the render byte for byte, and the instruction file is absent from a render
// that produced no instruction file. Nothing on disk tells that root apart
// from one whose profile declares no instruction file at all, which §5 makes a
// supported configuration — a destination with no template declared is not
// rendered. The difference exists only while rendering, and a report assembled
// from disk cannot see it.
//
// The exit code would answer two questions with one number. A caller branching
// on it could no longer tell "somebody edited a generated file" from "a marker
// in the manifest is misspelled", and those want opposite responses: the first
// wants an install, the second wants an edit that an install would faithfully
// re-render.
//
// And the render a check compares against is not the render a boot gets. The
// installed layer resolves only [slots.DeterministicKinds], because a check
// that ran the profile's commands would report drift on every invocation. A
// soundness verdict over that render would fail a layer for a slot that fills
// at boot and is empty here by construction — a gate that fires on a healthy
// installation, which is the disease [SweepPlan] and [StatusUnclaimed] were
// each written to avoid, arriving a third time in a new key.
//
// # Where the diagnostic lives instead
//
// On `install`, before the render, on a check as well as a write, naming the
// path the file lands at rather than the manifest key it was declared under.
// The operator hears about the emptied template from the command that emptied
// it, and is told the file they will go looking for and fail to find. It is a
// report and not a refusal, which is the slot rule it follows from: a section
// that is not there is degraded context, and the operator is the only one who
// can fix it.
//
// So a check is not silent about a dangling pointer. It prints the diagnostic
// and then returns a drift verdict — two answers to two questions, rather than
// one answer that got it wrong.
//
// Do not settle this by having a check re-run the marker report and fail on
// what it finds. That puts a render-time diagnostic behind a disk-comparison
// exit code and buys back every objection above.
type Status string

const (
	// StatusMatch means the file on disk is byte-identical to the render.
	StatusMatch Status = "match"

	// StatusMissing means cairn renders a file at this path and there is
	// nothing there.
	StatusMissing Status = "missing"

	// StatusModified means a file is there and its bytes are not the render's.
	// It is the ordinary way an operator learns they edited generated output.
	StatusModified Status = "modified"

	// StatusNotAFile means something is at the path that is not a regular
	// file: a directory, or a symlink. A symlink is called out on its own
	// because it is what an operator reaches for when they want an installed
	// file to be editable in place, and an install would replace it.
	StatusNotAFile Status = "not a file"

	// StatusUnreadable means the path could not be read, so nothing can be
	// said about it. It is a finding rather than an error on the whole check:
	// one unreadable path should not hide what the rest of the layer looks
	// like.
	StatusUnreadable Status = "unreadable"

	// StatusOrphan means cairn claims a path, this render does not produce it,
	// and something is there anyway. It is what a profile leaves behind when
	// it stops declaring something.
	//
	// Cairn claims the exact file paths its renderers can write, plus the
	// directories it fills whole — and nothing else. The provider directory
	// itself is not claimed: ~/.claude is a live harness's home, and calling
	// every unrendered file in it an orphan would report the harness's own
	// state on every run. See [SweepPlan].
	StatusOrphan Status = "orphan"

	// StatusUnclaimed means something sits in a directory cairn writes into
	// and does not own, and cairn did not put it there. A skill the operator
	// wrote by hand, beside the ones their profile declares, is the case it
	// exists for.
	//
	// It is informational and never a finding: a report carrying nothing else
	// is clean and exits zero. Cairn says what it found in a directory it
	// shares, and only what cairn claims can fail a check. The rule this
	// replaced claimed ~/.claude/skills whole and reported every hand-written
	// skill in it as drift, on every run, forever.
	StatusUnclaimed Status = "unclaimed"
)

// Finding reports whether a status is something wrong at a path cairn claims,
// and so whether it makes a report unclean — see [Report.Clean].
//
// Two statuses are not findings. [StatusMatch] is the layer exactly as cairn
// rendered it. [StatusUnclaimed] is the operator's own, in a directory cairn
// shares with them, reported so that a check says what it saw. Every other
// status is a finding, including one this package has not defined yet: a new
// status counts against a check until somebody decides otherwise, which is the
// safe direction for a gate to default in.
//
// One status has been proposed and declined rather than merely not written: a
// soundness finding, for a rendered document pointing at one that was never
// rendered. See [Status] for the case and the argument, before adding it.
func (s Status) Finding() bool {
	switch s {
	case StatusMatch, StatusUnclaimed:
		return false
	default:
		return true
	}
}

// Entry is one finding: a path, what was found there, and a sentence naming
// what to do about it when the status alone does not say.
type Entry struct {
	// Path is relative to the install root and slash-separated.
	Path string

	// Status is what was found.
	Status Status

	// Detail carries what the status cannot: the read error behind an
	// unreadable path, the target of a symlink, what kind of thing is at a
	// path that is not a file. Empty when the status says everything.
	Detail string
}

// Report is what one [Check] found, in path order within each status.
type Report struct {
	// Root is the install root that was checked.
	Root string

	// Entries are every finding, in render order and then sweep order.
	Entries []Entry
}
