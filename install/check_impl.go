package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
)

// SweepPlan is exactly what cairn claims in the install root, and therefore
// exactly what a check will report on.
//
// It is derived from the provider's renderer registration rather than from one
// render's output — see [Renderer.Tree] — which is what lets a check look into
// a directory the current profile renders nothing into. A sweep scoped to what
// was rendered would stop looking in precisely the case where something was
// left behind.
//
// # Cairn does not own the provider directory
//
// The plan is two lists rather than one because ~/.claude is a live harness's
// home: session state, credentials, caches, one directory per project. Cairn
// writes three files into it and fills one subtree, and claims nothing else.
//
// A sweep that read the provider directory one level deep and called every
// unrendered file an orphan would report settings.local.json and
// .credentials.json on every run of every real installation, so `--check`
// would exit non-zero forever and stop meaning anything. That is the same
// disease as a lint gate configured not to fail.
//
// It is exported so that a caller can print how far a check reached. A check
// that reports nothing says nothing unless its scope can be read.
type SweepPlan struct {
	// Claims are the exact file paths this layer's renderers can produce,
	// relative to the install root and slash-separated. A claim the render
	// did not produce, sitting on disk anyway, is a [StatusOrphan] — that is
	// how a settings.json left behind by a profile that stopped declaring one
	// gets found. A path that is not a claim is not cairn's and is not
	// reported, whatever is in it.
	Claims []string

	// Trees are the directories cairn fills whole, from the renderers that
	// set [Renderer.Tree]. Each is walked to the bottom, and every file in one
	// that the render did not produce is a [StatusOrphan]. Here the
	// whole-directory rule is right: cairn writes every file under
	// .claude/skills, so anything else in it is a leftover.
	Trees []string
}

// NewSweepPlan returns the [SweepPlan] for lay: the provider directory its
// profile is installed into, plus every directory artifact that provider's
// renderers register.
//
// The registration list is the source, not the render. A profile that declares
// no skills renders nothing into the skills directory, and a plan derived from
// the render would stop looking at that directory in exactly the case where
// something was left behind.
//
// Errors wrap [ErrNoProfile], or
// [github.com/chrispian/cairn/bootdir.ErrUnsupportedProvider] for a provider
// the installed layer has no harness for.
func NewSweepPlan(lay *Layer) (SweepPlan, error) {
	if lay == nil || lay.Profile == nil {
		return SweepPlan{}, ErrNoProfile
	}
	h, err := harnessFor(lay.Profile.Provider)
	if err != nil {
		return SweepPlan{}, err
	}
	var plan SweepPlan
	for _, r := range h.renderers {
		if r.Artifact == "" {
			continue
		}
		// Renderer.Artifact is a label relative to the provider directory,
		// which is why the two arrive together from one lookup: a directory
		// and an artifact list answered by different switch statements could
		// disagree, and the sweep would claim the wrong paths.
		p := path.Join(h.dir, r.Artifact)
		if r.Tree {
			plan.Trees = append(plan.Trees, p)
			continue
		}
		plan.Claims = append(plan.Claims, p)
	}
	slices.Sort(plan.Claims)
	plan.Claims = slices.Compact(plan.Claims)
	slices.Sort(plan.Trees)
	plan.Trees = slices.Compact(plan.Trees)
	return plan, nil
}

// Check renders lay and compares it against what is on disk beneath lay's
// root, returning what it found.
//
// It reports and never repairs. It does not create a missing file, does not
// rewrite a modified one, and does not delete an orphan — not on a flag, not
// on a follow-up call. Anything that writes belongs in [Install], where the
// operator asked for a write. Nothing here should acquire one later.
//
// Two halves, and the second is the one a check built from a manifest leaves
// out:
//
//  1. Every file the render produces is looked up on disk: identical, absent,
//     different, something that is not a file, or unreadable.
//  2. Every directory in the [SweepPlan] is swept for files the render did not
//     produce.
//
// The first half can only confirm what it already knew to look for. The second
// is why the plan is derived from the renderer registration.
//
// The returned error is for a check that could not run: an unusable root, a
// filesystem that cannot report symbolic links, a failed render. What the
// check found is never an error — it comes back in the [Report], and
// [Report.ExitCode] is what turns it into a process exit.
func Check(lay *Layer) (*Report, error) {
	if lay == nil || lay.Profile == nil {
		return nil, ErrNoProfile
	}
	fsys, err := lay.Root.FS()
	if err != nil {
		return nil, err
	}
	report, err := CheckFS(fsys, lay)
	if err != nil {
		return nil, err
	}
	// CheckFS is handed a filesystem and no location, so it cannot name the
	// root it read. Here the root is known.
	report.Root = lay.Root.Dir()
	return report, nil
}

// CheckFS is [Check] against a supplied filesystem, so a test does not need a
// real directory and a caller can check a root it is not standing in.
//
// The parameter type is the mechanism rather than a convenience.
// [fs.ReadLinkFS] carries Open, ReadLink and Lstat and no method that writes,
// so "--check repairs nothing" is a property of what this function was handed
// rather than a claim about its body. A function given a directory path can
// always call os.WriteFile on it; one given a filesystem view cannot.
//
// Lstat is also why a plain [fs.FS] will not do. A Stat that follows links
// reports a symbolic link as whatever it points at, so a link standing in for
// a rendered file could compare equal to the render — and a link is exactly
// what an operator reaches for when they want an installed file to be editable
// in place.
//
// It leaves [Report.Root] empty: the filesystem it reads names no location,
// and lay's root may not be the root it was handed. [Check] fills it.
func CheckFS(fsys fs.ReadLinkFS, lay *Layer) (*Report, error) {
	if fsys == nil {
		return nil, errors.New("install: check was given no filesystem to read")
	}
	if lay == nil || lay.Profile == nil {
		return nil, ErrNoProfile
	}
	rendered, err := Render(lay)
	if err != nil {
		return nil, err
	}
	plan, err := NewSweepPlan(lay)
	if err != nil {
		return nil, err
	}
	entries := checkManifest(fsys, rendered)
	entries = append(entries, sweep(fsys, plan, manifestPaths(rendered))...)
	return &Report{Entries: entries}, nil
}

// manifestPaths returns the set of paths the render produced, so the sweep can
// skip what the first half has already classified against the bytes.
func manifestPaths(rendered []File) map[string]struct{} {
	paths := make(map[string]struct{}, len(rendered))
	for _, f := range rendered {
		paths[f.Path] = struct{}{}
	}
	return paths
}

// checkManifest is the first half: every rendered file, looked up on disk, in
// render order.
func checkManifest(fsys fs.ReadLinkFS, rendered []File) []Entry {
	entries := make([]Entry, 0, len(rendered))
	for _, f := range rendered {
		entries = append(entries, checkRendered(fsys, f))
	}
	return entries
}

// checkRendered classifies one rendered file against what is at its path.
//
// Only the bytes are compared. A mode is not: the mode a file lands with
// depends on the umask of whoever ran the install, so reporting one would
// report the operator's shell rather than the layer's content.
//
// A path that cannot be read is a finding rather than a failure of the check.
// One unreadable file in a skills tree should not hide what the rest of the
// layer looks like.
func checkRendered(fsys fs.ReadLinkFS, f File) Entry {
	entry := Entry{Path: f.Path}
	info, err := fs.Lstat(fsys, f.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		entry.Status = StatusMissing
		return entry
	case err != nil:
		entry.Status = StatusUnreadable
		entry.Detail = err.Error()
		return entry
	}
	if !info.Mode().IsRegular() {
		entry.Status = StatusNotAFile
		entry.Detail = describeKind(fsys, f.Path, info.Mode())
		return entry
	}
	content, err := fs.ReadFile(fsys, f.Path)
	if err != nil {
		entry.Status = StatusUnreadable
		entry.Detail = err.Error()
		return entry
	}
	if !bytes.Equal(content, f.Content) {
		entry.Status = StatusModified
		entry.Detail = fmt.Sprintf("the bytes on disk are not the render's: %d on disk, %d rendered",
			len(content), len(f.Content))
		return entry
	}
	entry.Status = StatusMatch
	return entry
}

// sweep is the second half: every file inside a directory of the plan that the
// render did not produce.
//
// A read that fails is reported as a [StatusUnreadable] entry for the
// directory and the sweep carries on, for the reason [checkRendered] does not
// abort either.
func sweep(fsys fs.ReadLinkFS, plan SweepPlan, rendered map[string]struct{}) []Entry {
	seen := make(map[string]struct{})
	var entries []Entry
	for _, claim := range plan.Claims {
		if entry, found := sweepClaim(fsys, claim, rendered); found {
			seen[claim] = struct{}{}
			entries = append(entries, entry)
		}
	}
	for _, tree := range plan.Trees {
		entries = append(entries, sweepDir(fsys, tree, rendered, seen)...)
	}
	return entries
}

// sweepClaim reports the one path cairn claims and this render did not
// produce, when something is there anyway.
//
// A claim the render does produce was already classified against its bytes by
// the manifest half, and a claim with nothing at it is simply a file this
// profile does not ask for.
func sweepClaim(fsys fs.ReadLinkFS, claim string, rendered map[string]struct{}) (Entry, bool) {
	if _, produced := rendered[claim]; produced {
		return Entry{}, false
	}
	info, err := fs.Lstat(fsys, claim)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Entry{}, false
	case err != nil:
		return Entry{Path: claim, Status: StatusUnreadable, Detail: err.Error()}, true
	}
	entry := Entry{Path: claim, Status: StatusOrphan}
	if info.Mode()&fs.ModeSymlink != 0 {
		entry.Detail = describeLink(fsys, claim)
	} else if info.IsDir() {
		entry.Detail = "a directory"
	}
	return entry, true
}

// sweepDir reports every file directly inside dir that the render did not
// produce, descending only when dir is a directory cairn fills whole.
//
// A directory that is absent is not a finding: nothing has been installed
// there, and the first half already reports every file that should have been.
//
// The symbolic-link test comes before the directory test and stays there. A
// link to a directory is not a directory to descend into — cairn renders bytes
// and has no way to emit a link, so whatever is on the far end is not cairn's
// tree, and following it is how a sweep ends up walking someone's whole source
// layer.
func sweepDir(fsys fs.ReadLinkFS, dir string, rendered map[string]struct{}, seen map[string]struct{}) []Entry {
	listing, err := fs.ReadDir(fsys, dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return []Entry{{Path: dir, Status: StatusUnreadable, Detail: err.Error()}}
	}

	var entries []Entry
	for _, item := range listing {
		p := path.Join(dir, item.Name())
		if _, produced := rendered[p]; produced {
			// The first half classified this one against the bytes.
			continue
		}
		if _, already := seen[p]; already {
			continue
		}
		seen[p] = struct{}{}

		if item.Type()&fs.ModeSymlink != 0 {
			entries = append(entries, Entry{
				Path:   p,
				Status: StatusOrphan,
				Detail: describeLink(fsys, p),
			})
			continue
		}
		if item.IsDir() {
			entries = append(entries, sweepDir(fsys, p, rendered, seen)...)
			continue
		}
		entries = append(entries, Entry{Path: p, Status: StatusOrphan})
	}
	return entries
}

// describeKind names what is at a path that is not a regular file, so that the
// entry says what an operator has to look at rather than only that the path is
// occupied.
func describeKind(fsys fs.ReadLinkFS, p string, mode fs.FileMode) string {
	switch {
	case mode&fs.ModeSymlink != 0:
		return describeLink(fsys, p)
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	case mode&fs.ModeDevice != 0:
		return "a device"
	default:
		return "not a regular file: mode " + mode.String()
	}
}

// describeLink names a symbolic link's target as it is written in the link,
// unresolved. The operator has to see the string that is actually in the link
// to know what it points at.
func describeLink(fsys fs.ReadLinkFS, p string) string {
	target, err := fs.ReadLink(fsys, p)
	if err != nil {
		return fmt.Sprintf("a symbolic link whose target could not be read: %v", err)
	}
	return "a symbolic link to " + target
}
