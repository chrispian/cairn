package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrRootRelativePath reports a path that cannot name something inside the
// install root: it is empty, absolute, not slash-separated, or escapes upward.
var ErrRootRelativePath = errors.New("invalid path inside the install root")

// ErrRootNoReadLink reports that the install root's filesystem cannot report
// symbolic links.
//
// A check that cannot see a symlink cannot tell a rendered file from a link
// pointing at one, and a link is exactly what an operator reaches for when
// they want an installed file to be editable in place. Failing closed is the
// only honest answer.
var ErrRootNoReadLink = errors.New("the install root's filesystem cannot read symbolic links")

// Root is a validated install root: the directory the provider directories are
// written beneath.
//
// The zero Root is not usable. [NewRoot] is the only way to obtain one, so a
// Root in hand has already been checked for shape — though not for existence,
// which is [Root.Check].
type Root struct {
	dir string
}

// NewRoot returns the [Root] at dir, reporting [ErrNoRoot] for an empty path
// and [ErrRootNotAbsolute] for a relative one. It does not touch the
// filesystem — see [Root.Check].
func NewRoot(dir string) (Root, error) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return Root{}, ErrNoRoot
	}
	if !filepath.IsAbs(trimmed) {
		return Root{}, fmt.Errorf("%w: %q", ErrRootNotAbsolute, dir)
	}
	return Root{dir: filepath.Clean(trimmed)}, nil
}

// Dir returns the root's absolute directory, or the empty string for the zero
// Root.
func (r Root) Dir() string { return r.dir }

// IsZero reports whether r is the zero Root.
func (r Root) IsZero() bool { return r.dir == "" }

// String returns the root's directory, so a Root formats usefully with %s.
func (r Root) String() string { return r.dir }

// Check reports whether the root exists and is a directory, wrapping
// [ErrNoRoot], [ErrRootNotFound] or [ErrRootNotDirectory].
//
// Cairn never creates the install root. The database it will create, because
// an empty one is a usable starting state; a missing home directory is not a
// starting state, it is a sign cairn resolved the wrong path.
func (r Root) Check() error {
	if r.IsZero() {
		return ErrNoRoot
	}
	info, err := os.Stat(r.dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s", ErrRootNotFound, r.dir)
	case err != nil:
		return fmt.Errorf("stat the install root %s: %w", r.dir, err)
	case !info.IsDir():
		return fmt.Errorf("%w: %s", ErrRootNotDirectory, r.dir)
	}
	return nil
}

// Path returns the absolute path of rel inside the root, reporting
// [ErrRootRelativePath] for anything that could name something outside it.
func (r Root) Path(rel string) (string, error) {
	if r.IsZero() {
		return "", ErrNoRoot
	}
	clean, err := cleanRelativePath(rel)
	if err != nil {
		return "", err
	}
	return filepath.Join(r.dir, filepath.FromSlash(clean)), nil
}

// FS returns a filesystem rooted at r that can report symbolic links,
// reporting [ErrRootNoReadLink] if the platform's cannot.
//
// The check reads through this rather than through os.Stat so that a path
// which escapes the root cannot be reached at all, and so that a symlink is
// visible as a symlink instead of as whatever it points at.
func (r Root) FS() (fs.ReadLinkFS, error) {
	if err := r.Check(); err != nil {
		return nil, err
	}
	fsys, ok := os.DirFS(r.dir).(fs.ReadLinkFS)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrRootNoReadLink, r.dir)
	}
	return fsys, nil
}

// cleanRelativePath validates a path inside the install root and returns it
// cleaned. It is [bootdir.CleanArtifactPath]'s rule, applied to the layer that
// is not a boot directory.
func cleanRelativePath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("%w: the path is empty", ErrRootRelativePath)
	}
	if strings.ContainsRune(raw, '\\') {
		return "", fmt.Errorf("%w: %q is not slash-separated", ErrRootRelativePath, raw)
	}
	if path.IsAbs(raw) {
		return "", fmt.Errorf("%w: %q is absolute", ErrRootRelativePath, raw)
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: %q does not name something inside the install root", ErrRootRelativePath, raw)
	}
	return clean, nil
}
