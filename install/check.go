package install

// Status is what a check found at one path of the installed layer.
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
)

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
