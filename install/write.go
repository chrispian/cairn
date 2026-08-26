package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/chrispian/cairn/bootdir"
)

// ErrDestinationOccupied reports that something which is not a regular file
// already sits where the installed layer renders one.
var ErrDestinationOccupied = errors.New("a rendered file's destination is occupied")

// Install renders lay and writes it beneath the root, reporting what it wrote.
//
// Each step gates the next:
//
//  1. The root must exist and be a directory. Cairn creates neither — see
//     [ErrRootNotFound].
//  2. Every file is rendered, in memory. A render that fails writes nothing at
//     all, which is the failure worth preventing: a manifest error that left
//     half a layer on disk would leave every session on the machine reading a
//     truncated contract.
//  3. Every destination is checked before anything is written: a path where
//     something other than a regular file already sits fails the whole install
//     rather than half of it. A rename onto a directory fails, and without
//     this the failure would come part way through the moves with the earlier
//     files already in place.
//  4. The rendered files are written into a staging directory inside the root,
//     then moved into place one rename at a time, with parent directories
//     created at [DirMode].
//
// # The move is per file, not per layer
//
// A boot directory is staged whole and moved with a single rename, so it is
// either complete or absent. The installed layer cannot be: the root already
// exists, and most of what is in it is not cairn's. Renaming a staged
// directory over it would take the operator's own files with it. So each file
// appears whole or not at all, and a failure part way through leaves the files
// already moved in their new state.
//
// That is the limit of what a filesystem offers here, and it is stated rather
// than implied. The staging directory is inside the root so that every move is
// a rename within one filesystem rather than a copy that can fail halfway
// through one file.
//
// # Nothing is ever removed
//
// A file a previous install wrote and this render does not produce stays where
// it is. Sweeping it is not this function's job and deliberately not any
// function's: `cairn install --check` reports an orphan and leaves the removal
// to the operator, because cairn does not delete out of a home directory on
// the strength of its own bookkeeping. Do not add it here.
//
// Errors wrap [ErrNoProfile] or come from [Root.Check], [Render], or the
// write.
func Install(lay *Layer) (*Result, error) {
	if lay == nil || lay.Profile == nil {
		return nil, ErrNoProfile
	}
	if err := lay.Root.Check(); err != nil {
		return nil, err
	}
	files, err := Render(lay)
	if err != nil {
		return nil, err
	}
	if err := writeFiles(lay.Root, files); err != nil {
		return nil, err
	}
	written := make([]string, 0, len(files))
	for _, f := range files {
		written = append(written, f.Path)
	}
	return &Result{Root: lay.Root.Dir(), Files: written}, nil
}

// writeFiles writes files beneath root: every file into a staging directory
// inside the root first, then one rename each into its final path.
//
// Every destination is resolved through [Root.Path] and inspected before the
// first byte is staged, so a path that cannot name something inside the root,
// or that already holds something a file cannot replace, fails the write
// before it has begun rather than part way through it.
func writeFiles(root Root, files []File) error {
	if len(files) == 0 {
		return nil
	}
	destinations := make([]string, len(files))
	for i, f := range files {
		dest, err := root.Path(f.Path)
		if err != nil {
			return err
		}
		if err := checkDestination(f.Path, dest); err != nil {
			return err
		}
		destinations[i] = dest
	}

	staging, err := os.MkdirTemp(root.Dir(), StagingPattern)
	if err != nil {
		return fmt.Errorf("install: create the staging directory in %s: %w", root.Dir(), err)
	}
	// Removing the staging directory is a no-op once the renames have emptied
	// it, so one deferred cleanup covers every failure path and costs nothing
	// on the success path.
	defer func() { _ = os.RemoveAll(staging) }()

	staged, err := stage(staging, files)
	if err != nil {
		return err
	}
	for i, f := range files {
		parent := filepath.Dir(destinations[i])
		if err := os.MkdirAll(parent, DirMode); err != nil {
			return fmt.Errorf("install: create %s: %w", parent, err)
		}
		if err := os.Rename(staged[i], destinations[i]); err != nil {
			return fmt.Errorf("install: write %s: %w", f.Path, err)
		}
	}
	return nil
}

// checkDestination reports a path that a rendered file cannot be moved onto.
//
// Replacing a regular file is the ordinary case and is how an install updates
// what a previous one wrote. Replacing anything else is not: a rename onto a
// directory fails, and a rename onto a symlink would silently replace the link
// rather than write through it — which is the opposite of what an operator who
// linked an installed file into a repository meant. Both are refused here,
// before any file has moved, because a refusal is recoverable and half an
// installed layer is not.
func checkDestination(rel, dest string) error {
	info, err := os.Lstat(dest)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		// A path cannot be inspected when something above it is a file rather
		// than a directory, and the raw error then names the leaf — a path
		// that does not exist — and says it is not a directory, which reads
		// like nonsense. Name the component that is actually in the way.
		if blocker, ok := nonDirectoryAncestor(dest); ok {
			return fmt.Errorf("%w: %s is a file, and cairn renders %s below it",
				ErrDestinationOccupied, blocker, rel)
		}
		return fmt.Errorf("install: inspect %s: %w", rel, err)
	case info.Mode().IsRegular():
		return nil
	}
	what := "something that is not a regular file"
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		what = "a symbolic link"
	case info.IsDir():
		what = "a directory"
	}
	return fmt.Errorf("%w: %s is %s, and cairn renders a file there", ErrDestinationOccupied, rel, what)
}

// nonDirectoryAncestor returns the first existing ancestor of dest that is not
// a directory, which is what stops the path from being walked at all.
//
// It climbs rather than descends because the caller already has the leaf and
// wants the component above it that is in the way; the walk ends at the volume
// root, where filepath.Dir is its own fixpoint.
func nonDirectoryAncestor(dest string) (string, bool) {
	for dir := filepath.Dir(dest); ; {
		info, err := os.Lstat(dir)
		if err == nil {
			if !info.IsDir() {
				return dir, true
			}
			return "", false
		}
		if !errors.Is(err, fs.ErrNotExist) {
			// Something above this one is in the way; keep climbing to it.
			parent := filepath.Dir(dir)
			if parent == dir {
				return "", false
			}
			dir = parent
			continue
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// stage writes every file into dir and returns the staged paths, in the order
// files were given.
//
// The staged name is flat and derived from the index, so a rendered path can
// never steer where a file is staged: the only place a rendered path decides
// anything is the destination, which [Root.Path] has already validated.
func stage(dir string, files []File) ([]string, error) {
	staged := make([]string, len(files))
	for i, f := range files {
		staged[i] = filepath.Join(dir, fmt.Sprintf("%04d", i))
		if err := os.WriteFile(staged[i], f.Content, writeMode(f)); err != nil {
			return nil, fmt.Errorf("install: stage %s: %w", f.Path, err)
		}
		// os.WriteFile's mode is masked by the process umask, so the installed
		// mode would otherwise depend on the shell cairn was launched from —
		// and a skill's executable bit is load-bearing.
		if err := os.Chmod(staged[i], writeMode(f)); err != nil {
			return nil, fmt.Errorf("install: set the mode on staged %s: %w", f.Path, err)
		}
	}
	return staged, nil
}

// writeMode returns the mode f is written with, substituting
// [bootdir.DefaultFileMode] for the zero value exactly as a boot directory
// does when it plants one.
func writeMode(f File) fs.FileMode {
	if f.Mode == 0 {
		return bootdir.DefaultFileMode
	}
	return f.Mode
}
