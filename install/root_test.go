package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewRootRejectsAnEmptyPath checks that the zero root is never reached by
// accident. There is no directory cairn installs into unless one was named.
func TestNewRootRejectsAnEmptyPath(t *testing.T) {
	for _, dir := range []string{"", " ", "\t", "\n  \n"} {
		root, err := NewRoot(dir)
		if !errors.Is(err, ErrNoRoot) {
			t.Errorf("NewRoot(%q) = %v, want ErrNoRoot", dir, err)
		}
		if !root.IsZero() {
			t.Errorf("NewRoot(%q) returned %s alongside its error", dir, root)
		}
	}
}

// TestNewRootRejectsARelativePath checks that a root is absolute or nothing. A
// relative one would resolve against whatever directory cairn happened to be
// invoked from, which is not a property of the installation.
func TestNewRootRejectsARelativePath(t *testing.T) {
	for _, dir := range []string{"home/agent", "./claude", "..", "~/", "~/.claude"} {
		root, err := NewRoot(dir)
		if !errors.Is(err, ErrRootNotAbsolute) {
			t.Errorf("NewRoot(%q) = %v, want ErrRootNotAbsolute", dir, err)
		}
		if !root.IsZero() {
			t.Errorf("NewRoot(%q) returned %s alongside its error", dir, root)
		}
		if err != nil && !strings.Contains(err.Error(), dir) {
			t.Errorf("NewRoot(%q) reported %v, which does not name the path", dir, err)
		}
	}
}

// TestNewRootCleansThePath checks the surrounding whitespace and the redundant
// separators an operator's shell leaves behind are removed, so that two spellings
// of one directory are one root.
func TestNewRootCleansThePath(t *testing.T) {
	for dir, want := range map[string]string{
		"/home/agent":      "/home/agent",
		"/home/agent/":     "/home/agent",
		"/home/./agent":    "/home/agent",
		"/home//agent":     "/home/agent",
		"  /home/agent  ":  "/home/agent",
		"/home/x/../agent": "/home/agent",
	} {
		root, err := NewRoot(dir)
		if err != nil {
			t.Fatalf("NewRoot(%q): %v", dir, err)
		}
		if root.Dir() != want {
			t.Errorf("NewRoot(%q).Dir() = %q, want %q", dir, root.Dir(), want)
		}
		if root.String() != want {
			t.Errorf("NewRoot(%q) formats as %q, want %q", dir, root.String(), want)
		}
		// Formatted through a verb rather than compared to String() directly:
		// the point is that a Root reaches a diagnostic as its directory and
		// not as a struct.
		if got := fmt.Sprintf("the install root is %s", root); got != "the install root is "+want {
			t.Errorf("NewRoot(%q) formats with %%s as %q, want %q", dir, got, want)
		}
		if root.IsZero() {
			t.Errorf("NewRoot(%q) reports itself zero", dir)
		}
	}
}

// TestZeroRootIsUnusable checks that every method of the zero root reports
// [ErrNoRoot] rather than acting on an empty path — which, joined with a
// relative one, would name something in the process's working directory.
func TestZeroRootIsUnusable(t *testing.T) {
	var root Root
	if !root.IsZero() {
		t.Fatal("the zero Root does not report itself zero")
	}
	if root.Dir() != "" || root.String() != "" {
		t.Errorf("the zero Root names %q", root.Dir())
	}
	if err := root.Check(); !errors.Is(err, ErrNoRoot) {
		t.Errorf("the zero Root's Check = %v, want ErrNoRoot", err)
	}
	if _, err := root.Path(".claude/AGENTS.md"); !errors.Is(err, ErrNoRoot) {
		t.Errorf("the zero Root's Path = %v, want ErrNoRoot", err)
	}
	if _, err := root.FS(); !errors.Is(err, ErrNoRoot) {
		t.Errorf("the zero Root's FS = %v, want ErrNoRoot", err)
	}
}

// TestRootCheckReportsTheThreeSentinels covers what Check can find: a root
// that was never named, one that is not there, and one that is not a
// directory. Cairn creates none of them — a missing install root means cairn
// resolved the wrong path, which is a thing to report rather than to make.
func TestRootCheckReportsTheThreeSentinels(t *testing.T) {
	var zero Root
	if err := zero.Check(); !errors.Is(err, ErrNoRoot) {
		t.Errorf("the zero Root's Check = %v, want ErrNoRoot", err)
	}

	present, err := NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot on a temporary directory: %v", err)
	}
	if err := present.Check(); err != nil {
		t.Errorf("Check on an existing directory: %v", err)
	}

	absent, err := NewRoot(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if err := absent.Check(); !errors.Is(err, ErrRootNotFound) {
		t.Errorf("Check on a missing directory = %v, want ErrRootNotFound", err)
	}
	if _, err := os.Stat(absent.Dir()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Check created %s; cairn never creates the install root", absent)
	}

	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
	notDir, err := NewRoot(file)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if err := notDir.Check(); !errors.Is(err, ErrRootNotDirectory) {
		t.Errorf("Check on a file = %v, want ErrRootNotDirectory", err)
	}
}

// TestRootPathResolvesInsideTheRoot checks a valid relative path lands where
// the platform's separator says it should.
func TestRootPathResolvesInsideTheRoot(t *testing.T) {
	dir := t.TempDir()
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	for rel, want := range map[string]string{
		".claude/AGENTS.md":                   filepath.Join(dir, ".claude", "AGENTS.md"),
		".claude/skills/code-review/SKILL.md": filepath.Join(dir, ".claude", "skills", "code-review", "SKILL.md"),
		".claude/./settings.json":             filepath.Join(dir, ".claude", "settings.json"),
		".claude/x/../settings.json":          filepath.Join(dir, ".claude", "settings.json"),
	} {
		got, err := root.Path(rel)
		if err != nil {
			t.Fatalf("Path(%q): %v", rel, err)
		}
		if got != want {
			t.Errorf("Path(%q) = %q, want %q", rel, got, want)
		}
	}
}

// TestRootPathRejectsAnythingThatCouldEscape checks the rule that decides
// where an install may write. The root is a home directory: a path that leaves
// it does not land somewhere harmless.
func TestRootPathRejectsAnythingThatCouldEscape(t *testing.T) {
	root, err := NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	for _, rel := range []string{
		"",
		"   ",
		"/etc/passwd",
		"/",
		".",
		"..",
		"../escaped.md",
		"../../escaped.md",
		".claude/../../escaped.md",
		`.claude\AGENTS.md`,
		`..\escaped.md`,
	} {
		got, err := root.Path(rel)
		if !errors.Is(err, ErrRootRelativePath) {
			t.Errorf("Path(%q) = %q, %v; want ErrRootRelativePath", rel, got, err)
		}
		if got != "" {
			t.Errorf("Path(%q) returned %q alongside its error", rel, got)
		}
	}
}

// TestRootFSReadsInsideTheRoot checks the filesystem a check reads the
// installed layer through: it reaches what is inside the root, and it reports
// a symlink as a symlink rather than as whatever it points at.
//
// The second half is the one that matters. A link is exactly what an operator
// reaches for when they want an installed file to be editable in place, and a
// check that could not see one would call it a match and let the next install
// replace it.
func TestRootFSReadsInsideTheRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("create the provider directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "AGENTS.md"), []byte("# Base\n"), 0o644); err != nil {
		t.Fatalf("write the instruction file: %v", err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(dir, ".claude", "CLAUDE.md")); err != nil {
		t.Fatalf("create the pointer symlink: %v", err)
	}

	root, err := NewRoot(dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	fsys, err := root.FS()
	if err != nil {
		t.Fatalf("FS on a real directory: %v", err)
	}

	content, err := fs.ReadFile(fsys, ".claude/AGENTS.md")
	if err != nil {
		t.Fatalf("read .claude/AGENTS.md through the root's filesystem: %v", err)
	}
	if string(content) != "# Base\n" {
		t.Errorf(".claude/AGENTS.md holds %q", content)
	}

	target, err := fsys.ReadLink(".claude/CLAUDE.md")
	if err != nil {
		t.Fatalf("read the symlink through the root's filesystem: %v", err)
	}
	if target != "AGENTS.md" {
		t.Errorf("the symlink points at %q, want %q", target, "AGENTS.md")
	}
	info, err := fsys.Lstat(".claude/CLAUDE.md")
	if err != nil {
		t.Fatalf("lstat the symlink: %v", err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf(".claude/CLAUDE.md reads as mode %v, want a symlink", info.Mode())
	}

	if _, err := fs.ReadFile(fsys, "../escaped.md"); err == nil {
		t.Error("a path escaping the root was readable through the root's filesystem")
	}
}

// TestRootFSChecksTheRootFirst checks the filesystem is not handed back for a
// root that is not there, so a check reports a missing install root rather
// than an empty layer.
func TestRootFSChecksTheRootFirst(t *testing.T) {
	absent, err := NewRoot(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if _, err := absent.FS(); !errors.Is(err, ErrRootNotFound) {
		t.Errorf("FS on a missing root = %v, want ErrRootNotFound", err)
	}
}
