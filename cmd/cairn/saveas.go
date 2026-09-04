package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chrispian/cairn/catalog"
)

// saveAsFlagUsage describes --save-as, including the two things it does not
// save. Both are in the help because both are surprises otherwise: a flag that
// claims to save what you just did, and quietly saves less than that, is worse
// than one that never offered.
const saveAsFlagUsage = "write this composition to the bundle as a new binding of that name, so " +
	"the same boot is reachable by name. The parts, the skills and the scope are saved as " +
	"they were written; --set values are not, because a binding names what to compose and " +
	"an inline value is content — each one dropped is named on stderr, and this boot still " +
	"has it. A composition holding a --with <path> is refused rather than saved short"

// bindingSave is a --save-as: a binding checked before the boot runs and
// written after it succeeds.
//
// The split is the whole shape of the type. Every reason to refuse a save is
// known before any work happens — the name, the file, and whether the
// composition holds a path — and refusing then means the operator who mistyped
// a name has not also had a boot directory planted for them. Every reason the
// save could still be pointless is a reason the boot failed, so the write
// waits for it.
type bindingSave struct {
	// path is the file to create, checked to be absent at [newBindingSave].
	path string

	// binding is what goes in it.
	binding catalog.Binding

	// dropped names the --set slots this binding does not carry.
	dropped []string
}

// newBindingSave checks that the composition just resolved can be saved as
// name, and returns what to write. A nil result means no --save-as was given.
//
// The three refusals here are the ones that do not depend on the boot, and
// they are refusals rather than reports because each one leaves the operator
// with a binding that is not what they asked for. The fourth behaviour — a
// --set — is not a refusal at all, and is reported at [bindingSave.write].
func newBindingSave(name, root string, t bootTarget, c *composition, scope string) (*bindingSave, error) {
	if name == "" {
		return nil, nil
	}
	path, err := catalog.BindingPath(root, name)
	if err != nil {
		return nil, fmt.Errorf("--save-as: %w", err)
	}

	// A path member, refused and named. The contrast with --set below is the
	// part of this design most likely to be got wrong, so the reasoning is
	// here rather than left to be re-derived: a --set can be dropped soundly
	// because this run still receives it and nothing is lost but reuse. A
	// path member cannot. Dropping it silently changes what the binding
	// composes, and inlining its content instead was rejected — that turns a
	// handle into content, which is the seam the bundle's whole shape rests
	// on. What is left is to refuse.
	for i, raw := range c.with {
		if partIsPath(raw) {
			return nil, fmt.Errorf("--save-as %s: %s names a file, and a binding must be reproducible "+
				"by name — a path is a handle to something that may not be there later. Put the part in "+
				"%s and name it, or boot without --save-as",
				name, c.partAt(i), filepath.Join(root, catalog.ProfilesDir))
		}
	}

	// An existing file, refused rather than overwritten. A binding file is
	// two to four lines and often carries a comment saying why the scope is
	// what it is, which nothing else in the bundle records; a save that
	// silently replaced one would destroy the only copy of it.
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("--save-as %s: %s already exists, and a binding is not overwritten — "+
			"its file may hold a comment nothing else carries. Remove it, or save under another name",
			name, path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("--save-as %s: %s: %w", name, path, err)
	}

	dropped := make([]string, 0, len(c.sets))
	for _, s := range c.sets {
		dropped = append(dropped, s.name)
	}
	return &bindingSave{
		path: path,
		binding: catalog.Binding{
			Name:      name,
			ProfileID: t.profileID,
			Parts:     c.savedParts(),
			Skills:    c.savedSkills(),
			Scope:     scope,
		},
		dropped: dropped,
	}, nil
}

// write creates the binding file and reports what did not go into it.
//
// The write is exclusive, which is not redundant with the check at
// [newBindingSave]. That check is what produces the diagnostic worth reading,
// and this is what makes the create the decision: between the two there is a
// boot, and a boot is long enough for another terminal to have saved the same
// name.
//
// Every dropped --set is named. A silent drop is worse than a refusal — the
// operator finds out the next time they boot the binding and something they
// wrote is not there — so the line says what happened, what it applied to, and
// that this run still has it.
func (s *bindingSave) write(stderr io.Writer) error {
	text, err := catalog.MarshalBinding(s.binding)
	if err != nil {
		return err
	}
	// The bindings directory is created when it is not there. A bundle with
	// profiles and no bindings is a legitimate bundle — the catalog reads one
	// without complaint — so the first --save-as in such a bundle is the thing
	// that makes the directory, and refusing because of that would be
	// refusing over the bundle's own valid shape.
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("--save-as %s: write %s: %w", s.binding.Name, s.path, err)
	}
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("--save-as %s: write %s: %w", s.binding.Name, s.path, err)
	}
	if _, err := f.Write(text); err != nil {
		_ = f.Close()
		return fmt.Errorf("--save-as %s: write %s: %w", s.binding.Name, s.path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("--save-as %s: write %s: %w", s.binding.Name, s.path, err)
	}

	_, _ = fmt.Fprintf(stderr, "cairn: --save-as %s: wrote %s\n", s.binding.Name, s.path)
	for _, name := range s.dropped {
		_, _ = fmt.Fprintf(stderr,
			"cairn: --save-as %s: --set %s was not saved — a binding names what to compose and an "+
				"inline value is content. This boot still has it.\n", s.binding.Name, name)
	}
	return nil
}
