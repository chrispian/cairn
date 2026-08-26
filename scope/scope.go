// Package scope holds the one validation Cairn performs on a scope directory:
// that the boot directory it is about to write does not land at or inside it.
//
// Scope is one directory path, supplied at boot, optional. It is the
// materialized instance's working directory, and Cairn has no other opinion
// about it — the prior rules rejecting /etc, /usr, /var, the home directory
// and the filesystem root are gone. Cairn is a single-operator tool and those
// rules only ever stopped the operator from doing something the operator
// meant.
//
// # A path string is not a path identity
//
// Every check here asks the filesystem rather than comparing two strings.
// [filepath.EvalSymlinks] resolves symlinks and nothing else: it normalizes
// neither letter case nor Unicode form, so on a case-folding or form-folding
// volume — APFS, for one — two different canonical strings routinely name one
// directory. [os.SameFile] compares device and inode, which settles case,
// Unicode form, hard links and every other spelling axis at once, without this
// package having to detect anything about the volume. A prefix comparison
// would fail in the permissive direction, which for a check that guards a
// write is the wrong direction to be wrong in.
package scope

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotDirectory reports that a scope does not name an existing directory.
//
// This is not a policy rule about where a scope may point. It is what makes
// the containment check answerable: [Contains] walks a candidate's ancestry
// asking the filesystem for identity at each step, and a scope the filesystem
// cannot identify contains nothing, so a mistyped scope would silently turn
// the one guard Cairn has into a no-op.
var ErrNotDirectory = errors.New("scope does not name an existing directory")

// ErrInsideScope reports that a boot directory would be written at or inside
// the scope directory. The scope is a repository under work; a boot directory
// planted inside it is a tree the agent's own tools would then find.
var ErrInsideScope = errors.New("boot directory is inside the scope directory")

// ErrNoHome reports that a scope beginning with "~" could not be expanded
// because no home directory is known.
var ErrNoHome = errors.New("home directory unknown")

// Parse resolves raw into the canonical absolute directory it names: a leading
// "~" is expanded against home, the result is made absolute, and symlinks are
// resolved so that two spellings of one directory arrive here as one path.
//
// An empty or whitespace-only raw is the empty scope — no declared target —
// and returns the empty string with no error. Everything else must already
// name an existing directory, reporting [ErrNotDirectory] if it does not.
//
// home is passed rather than read so that nothing here consults the process
// environment on its own.
func Parse(raw, home string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	expanded, err := expandHome(trimmed, home)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("absolute path of %q: %w", raw, err)
	}
	info, err := os.Stat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("%w: %s", ErrNotDirectory, abs)
	case err != nil:
		return "", fmt.Errorf("stat scope %q: %w", abs, err)
	case !info.IsDir():
		return "", fmt.Errorf("%w: %s is not a directory", ErrNotDirectory, abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve scope %q: %w", abs, err)
	}
	return resolved, nil
}

// CheckBootDir reports [ErrInsideScope] when dir would be written at or inside
// scopeDir. The empty scope declares no target and contains nothing, so it
// passes.
//
// dir need not exist — this is asked before the directory is created. The walk
// climbs to the deepest ancestor of dir that does exist and asks there, so a
// path whose whole tail is absent is still reported as inside when the tree it
// would be created in is the scope.
//
// An error other than [ErrInsideScope] means the question could not be
// answered, and the caller must treat that as a refusal: it never means "not
// inside".
func CheckBootDir(scopeDir, dir string) error {
	inside, err := Contains(scopeDir, dir)
	if err != nil {
		return err
	}
	if inside {
		return fmt.Errorf("%w: %s is at or inside %s", ErrInsideScope, dir, scopeDir)
	}
	return nil
}

// Contains reports whether candidate is ancestor itself or lies inside it, by
// walking candidate's own ancestry and asking the filesystem for identity at
// each step rather than comparing path prefixes. That costs one stat per level
// of a path that is about to be created anyway, and in exchange no spelling of
// either argument can make the check miss.
//
// An empty ancestor contains nothing. An ancestor that does not exist contains
// nothing, for the same reason: only a directory the filesystem can identify
// has an identity to compare. An error means the question could not be
// answered; it never means "no".
func Contains(ancestor, candidate string) (bool, error) {
	if strings.TrimSpace(ancestor) == "" {
		return false, nil
	}
	if strings.TrimSpace(candidate) == "" {
		return false, fmt.Errorf("scope %q: the candidate directory is empty", ancestor)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return false, fmt.Errorf("absolute path of %q: %w", candidate, err)
	}
	ancestorInfo, err := statForIdentity(ancestor)
	if err != nil {
		return false, err
	}
	if ancestorInfo == nil {
		return false, nil
	}
	for dir := abs; ; {
		info, err := statForIdentity(dir)
		if err != nil {
			return false, fmt.Errorf("walk up from %q: %w", abs, err)
		}
		if info != nil && os.SameFile(ancestorInfo, info) {
			return true, nil
		}
		parent := filepath.Dir(dir)
		// filepath.Dir is its own fixpoint at the volume root, so this detects
		// the end of the walk. It is not a path-identity test — both sides are
		// the same string produced by the same call.
		if parent == dir {
			return false, nil
		}
		dir = parent
	}
}

// statForIdentity stats path for an identity comparison, keeping the three
// outcomes its callers need apart.
//
// A path that exists reports its [os.FileInfo] and no error. A path that does
// not exist reports a nil FileInfo and no error: it is definitively not the
// same directory as one that does. Every other failure — a permission denied
// on an ancestor, a component that is not a directory — is returned wrapped,
// so a directory that cannot be stat'd fails its caller closed rather than
// quietly reading as "not a match".
func statForIdentity(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return nil, fmt.Errorf("stat %q: %w", path, err)
}

// expandHome replaces a leading "~" or "~/" in p with home. A "~user" form is
// left alone: Cairn is a single-operator tool and expanding another user's
// home is not something it has any business guessing at.
func expandHome(p, home string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("%w: cannot expand %q", ErrNoHome, p)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}
