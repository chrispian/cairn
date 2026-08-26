package install_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/install"
	"github.com/chrispian/cairn/profile"
)

// Every fixture below is built under t.TempDir(). Nothing here names a real
// home directory, and nothing here runs an install: `cairn install` rewrites
// the configuration the session running these tests reads, and a check against
// a fixture root is what answers "does it work".

// checkFixture is one install root holding the render of one layer, ready for
// a test to break in one specific way.
type checkFixture struct {
	// dir is the install root, a temporary directory.
	dir string

	// lay is the layer the root was rendered from.
	lay *install.Layer

	// files is that render, in render order.
	files []install.File
}

// newCheckFixture renders a layer declaring the named skills and writes the
// render into a fresh temporary install root, so the root starts out matching.
func newCheckFixture(t *testing.T, skills ...string) checkFixture {
	t.Helper()
	rootDir := t.TempDir()
	root, err := install.NewRoot(rootDir)
	if err != nil {
		t.Fatalf("NewRoot(t.TempDir()): %v", err)
	}
	manifest := map[string]any{
		"settings": map[string]any{"model": "opus"},
	}
	if len(skills) > 0 {
		manifest["skills"] = skills
		manifest["skills_dir"] = writeCheckSkills(t, skills)
	}
	lay := &install.Layer{
		Root: root,
		Profile: &profile.Resolved{
			ID:       "base",
			Name:     "Base",
			Provider: profile.ProviderClaude,
			Model:    "opus",
			Body:     "Read the profile.",
			Spec:     checkSpec(t, manifest),
		},
		Home: t.TempDir(),
	}
	files, err := install.Render(lay)
	if err != nil {
		t.Fatalf("render the fixture layer: %v", err)
	}
	fixture := checkFixture{dir: rootDir, lay: lay, files: files}
	fixture.plant(t)
	return fixture
}

// plant writes the render into the install root, which is the state a check
// finds after an install nobody has touched since.
func (f checkFixture) plant(t *testing.T) {
	t.Helper()
	for _, file := range f.files {
		dest := f.path(file.Path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatalf("create the directory for %s: %v", dest, err)
		}
		mode := file.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(dest, file.Content, mode); err != nil {
			t.Fatalf("write %s: %v", dest, err)
		}
	}
}

// path returns the absolute location of a slash-separated path inside the
// install root.
func (f checkFixture) path(rel string) string {
	return filepath.Join(f.dir, filepath.FromSlash(rel))
}

// check runs the check against the fixture's root and fails on an error, since
// every finding a test asserts on is in the report rather than in an error.
func (f checkFixture) check(t *testing.T) *install.Report {
	t.Helper()
	report, err := install.Check(f.lay)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return report
}

// writeCheckSkills writes one directory per skill under a fresh temporary
// directory and returns it, for the manifest's skills_dir.
func writeCheckSkills(t *testing.T, names []string) string {
	t.Helper()
	source := t.TempDir()
	for _, name := range names {
		dir := filepath.Join(source, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create the skill directory %s: %v", dir, err)
		}
		body := []byte("# " + name + "\n")
		if err := os.WriteFile(filepath.Join(dir, bootdir.SkillFileName), body, 0o644); err != nil {
			t.Fatalf("write the skill file for %q: %v", name, err)
		}
	}
	return source
}

// checkSpec encodes a manifest written inline in a test into a [profile.Spec],
// so a test declares the JSON an operator would have stored.
func checkSpec(t *testing.T, manifest map[string]any) profile.Spec {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode the manifest: %v", err)
	}
	var spec profile.Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode the manifest: %v", err)
	}
	return spec
}

// writeInRoot writes content at a slash-separated path inside the install
// root, creating the directories above it.
func writeInRoot(t *testing.T, rootDir, rel, content string) string {
	t.Helper()
	dest := filepath.Join(rootDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("create the directory for %s: %v", dest, err)
	}
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", dest, err)
	}
	return dest
}

// entryAt returns the report's entry for a path, and fails when the report
// carries none or more than one.
func entryAt(t *testing.T, report *install.Report, rel string) install.Entry {
	t.Helper()
	var found []install.Entry
	for _, entry := range report.Entries {
		if entry.Path == rel {
			found = append(found, entry)
		}
	}
	switch len(found) {
	case 1:
		return found[0]
	case 0:
		t.Fatalf("no entry for %q; the report holds %v", rel, entryPaths(report.Entries))
	default:
		t.Fatalf("%d entries for %q; a path is classified once", len(found), rel)
	}
	return install.Entry{}
}

// pathsWithStatus returns the paths carrying status, in path order.
func pathsWithStatus(report *install.Report, status install.Status) []string {
	entries := report.ByStatus(status)
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Path)
	}
	return out
}

// entryPaths returns every path in a report, for a failure message.
func entryPaths(entries []install.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Path+" ("+string(entry.Status)+")")
	}
	return out
}

// symlinkOrSkip links target at name, skipping the test when the platform will
// not make a symbolic link. The link cases below are the reason the check
// reads through [fs.ReadLinkFS], so a platform that cannot make one has
// nothing to say about them.
func symlinkOrSkip(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("this platform cannot create a symbolic link: %v", err)
	}
}

func TestNewSweepPlanClaimsTheFilesAndTheTreesAndNothingElse(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t)
	plan, err := install.NewSweepPlan(fixture.lay)
	if err != nil {
		t.Fatalf("NewSweepPlan: %v", err)
	}
	wantClaims := []string{".claude/AGENTS.md", ".claude/CLAUDE.md", ".claude/settings.json"}
	if !slices.Equal(plan.Claims, wantClaims) {
		t.Errorf("Claims = %v, want %v", plan.Claims, wantClaims)
	}
	wantTrees := []string{".claude/skills"}
	if !slices.Equal(plan.Trees, wantTrees) {
		t.Errorf("Trees = %v, want %v", plan.Trees, wantTrees)
	}
	// The provider directory itself is claimed by neither list. ~/.claude is a
	// live harness's home, and a sweep that read it one level deep would call
	// settings.local.json and .credentials.json orphans on every run of every
	// real installation.
	for _, claimed := range append(slices.Clone(plan.Claims), plan.Trees...) {
		if claimed == ".claude" {
			t.Error("the plan claims the provider directory itself")
		}
	}
	// The fixture declares no skills at all. The plan still claims the skills
	// tree, because it is derived from the renderer registration and not from
	// what this profile rendered.
	if skills, err := fixture.lay.Profile.Spec.Skills(); err != nil || len(skills) != 0 {
		t.Fatalf("the fixture should declare no skills; got %v, %v", skills, err)
	}
}

// TestEveryClaimIsAPathTheRenderProduces pins the sweep's claims against the
// render, so the two cannot drift.
//
// A claim is built from a renderer's Artifact label joined to the provider
// directory, while the file itself is written at the path the Layout names.
// Those are two different derivations of one path, and if they disagree the
// sweep would report a file cairn writes as an orphan on every check while
// missing the leftover it exists to find.
func TestEveryClaimIsAPathTheRenderProduces(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	// A profile declaring everything, so every non-tree renderer produces its
	// file and the two derivations can actually be compared.
	plan, err := install.NewSweepPlan(fixture.lay)
	if err != nil {
		t.Fatalf("NewSweepPlan: %v", err)
	}
	files, err := install.Render(fixture.lay)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	produced := make(map[string]struct{}, len(files))
	for _, f := range files {
		produced[f.Path] = struct{}{}
	}
	for _, claim := range plan.Claims {
		if _, ok := produced[claim]; !ok {
			t.Errorf("the sweep claims %q and the render does not produce it; the Artifact label "+
				"and the Layout path have drifted apart. Rendered: %v", claim, produced)
		}
	}
}

func TestNewSweepPlanRefusesALayerItCannotPlan(t *testing.T) {
	t.Parallel()
	if _, err := install.NewSweepPlan(nil); !errors.Is(err, install.ErrNoProfile) {
		t.Errorf("NewSweepPlan(nil) = %v, want ErrNoProfile", err)
	}
	if _, err := install.NewSweepPlan(&install.Layer{}); !errors.Is(err, install.ErrNoProfile) {
		t.Errorf("NewSweepPlan(empty layer) = %v, want ErrNoProfile", err)
	}
	lay := &install.Layer{Profile: &profile.Resolved{ID: "base", Provider: profile.ProviderCodex}}
	if _, err := install.NewSweepPlan(lay); !errors.Is(err, bootdir.ErrUnsupportedProvider) {
		t.Errorf("NewSweepPlan(codex) = %v, want ErrUnsupportedProvider", err)
	}
}

func TestCheckRootMatchingTheRender(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	report := fixture.check(t)

	if !report.Clean() {
		t.Errorf("Clean() = false, want true; report:\n%s", report)
	}
	if got := report.ExitCode(); got != 0 {
		t.Errorf("ExitCode() = %d, want 0", got)
	}
	if report.Root != fixture.dir {
		t.Errorf("Root = %q, want %q", report.Root, fixture.dir)
	}
	if len(report.Entries) != len(fixture.files) {
		t.Errorf("the report holds %d entries and the render holds %d files: %v",
			len(report.Entries), len(fixture.files), entryPaths(report.Entries))
	}
	for _, entry := range report.Entries {
		if entry.Status != install.StatusMatch {
			t.Errorf("%s is %q, want %q", entry.Path, entry.Status, install.StatusMatch)
		}
	}
	// The render is the whole installed layer, so the fixture is only a
	// fixture if it produced the artifacts an operator would recognise.
	for _, want := range []string{".claude/AGENTS.md", ".claude/CLAUDE.md", ".claude/settings.json", ".claude/skills/alpha/SKILL.md"} {
		if entry := entryAt(t, report, want); entry.Status != install.StatusMatch {
			t.Errorf("%s is %q", want, entry.Status)
		}
	}
}

func TestCheckReportsADeletedFileAsMissing(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	const gone = ".claude/settings.json"
	if err := os.Remove(fixture.path(gone)); err != nil {
		t.Fatalf("remove %s: %v", gone, err)
	}

	report := fixture.check(t)
	if got := pathsWithStatus(report, install.StatusMissing); !slices.Equal(got, []string{gone}) {
		t.Errorf("missing = %v, want [%s]", got, gone)
	}
	if report.Clean() {
		t.Error("Clean() = true with a file missing")
	}
	if got := report.ExitCode(); got == 0 {
		t.Error("ExitCode() = 0 with a file missing")
	}
	if detail := entryAt(t, report, gone).Detail; detail != "" {
		t.Errorf("Detail = %q, want empty: the status says everything", detail)
	}
	// The check repairs nothing.
	if _, err := os.Lstat(fixture.path(gone)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the check recreated %s", gone)
	}
}

func TestCheckReportsAnEditedFileAsModified(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	const edited = ".claude/AGENTS.md"
	const content = "# Base\n\nedited in place by the operator\n"
	if err := os.WriteFile(fixture.path(edited), []byte(content), 0o644); err != nil {
		t.Fatalf("edit %s: %v", edited, err)
	}

	report := fixture.check(t)
	if got := pathsWithStatus(report, install.StatusModified); !slices.Equal(got, []string{edited}) {
		t.Errorf("modified = %v, want [%s]", got, edited)
	}
	if detail := entryAt(t, report, edited).Detail; !strings.Contains(detail, "bytes on disk") {
		t.Errorf("Detail = %q, want it to name the bytes", detail)
	}
	if report.Count(install.StatusMatch) != len(fixture.files)-1 {
		t.Errorf("%d matched, want %d: one edit is one finding",
			report.Count(install.StatusMatch), len(fixture.files)-1)
	}
	// The check repairs nothing.
	got, err := os.ReadFile(fixture.path(edited))
	if err != nil {
		t.Fatalf("read %s back: %v", edited, err)
	}
	if string(got) != content {
		t.Error("the check rewrote the edited file")
	}
}

func TestCheckReportsASymlinkWhereItRendersAFile(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	const linked = ".claude/CLAUDE.md"
	target := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(target, []byte("@AGENTS.md\n"), 0o644); err != nil {
		t.Fatalf("write the link target: %v", err)
	}
	if err := os.Remove(fixture.path(linked)); err != nil {
		t.Fatalf("remove %s: %v", linked, err)
	}
	symlinkOrSkip(t, target, fixture.path(linked))

	report := fixture.check(t)
	if got := pathsWithStatus(report, install.StatusNotAFile); !slices.Equal(got, []string{linked}) {
		t.Errorf("not a file = %v, want [%s]", got, linked)
	}
	detail := entryAt(t, report, linked).Detail
	if !strings.Contains(detail, "symbolic link") {
		t.Errorf("Detail = %q, want it to say the path is a symbolic link", detail)
	}
	if !strings.Contains(detail, target) {
		t.Errorf("Detail = %q, want it to name the target %q", detail, target)
	}
}

func TestCheckReportsADirectoryWhereItRendersAFile(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t)
	const occupied = ".claude/settings.json"
	if err := os.Remove(fixture.path(occupied)); err != nil {
		t.Fatalf("remove %s: %v", occupied, err)
	}
	if err := os.Mkdir(fixture.path(occupied), 0o755); err != nil {
		t.Fatalf("put a directory at %s: %v", occupied, err)
	}

	report := fixture.check(t)
	entry := entryAt(t, report, occupied)
	if entry.Status != install.StatusNotAFile {
		t.Errorf("%s is %q, want %q", occupied, entry.Status, install.StatusNotAFile)
	}
	if entry.Detail != "a directory" {
		t.Errorf("Detail = %q, want it to name the directory", entry.Detail)
	}
}

func TestCheckReportsAStrayFileInASkillsTree(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	const stray = ".claude/skills/stale/SKILL.md"
	writeInRoot(t, fixture.dir, stray, "# a skill the profile no longer declares\n")

	report := fixture.check(t)
	if got := pathsWithStatus(report, install.StatusOrphan); !slices.Equal(got, []string{stray}) {
		t.Errorf("orphan = %v, want [%s]", got, stray)
	}
	if report.Clean() {
		t.Error("Clean() = true with an orphan in the skills tree")
	}
	// The check deletes nothing.
	if _, err := os.Lstat(fixture.path(stray)); err != nil {
		t.Errorf("the check removed %s: %v", stray, err)
	}
}

func TestCheckReportsAStrayFileInASkillsTreeTheProfileDoesNotFill(t *testing.T) {
	t.Parallel()
	// This is the case a sweep derived from the render misses. The profile
	// declares no skills, so nothing is rendered into the skills directory and
	// a render-derived sweep would stop looking at it — which is exactly when
	// something is left behind there. It is why Renderer.Tree exists.
	fixture := newCheckFixture(t)
	for _, file := range fixture.files {
		if strings.HasPrefix(file.Path, ".claude/skills/") {
			t.Fatalf("the fixture rendered %s; it must declare no skills", file.Path)
		}
	}
	stray := []string{
		".claude/skills/stale/SKILL.md",
		".claude/skills/stale/references/notes.md",
	}
	for _, rel := range stray {
		writeInRoot(t, fixture.dir, rel, "left behind\n")
	}

	report := fixture.check(t)
	if got := pathsWithStatus(report, install.StatusOrphan); !slices.Equal(got, stray) {
		t.Errorf("orphan = %v, want %v", got, stray)
	}
}

// TestCheckReportsAClaimedFileTheProfileStoppedDeclaring covers the orphan the
// claim list exists to find: a profile that used to declare settings and no
// longer does leaves a settings.json behind, and cairn wrote that file, so
// cairn reports it.
func TestCheckReportsAClaimedFileTheProfileStoppedDeclaring(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t)
	const left = ".claude/settings.json"
	if _, declared := fixture.lay.Profile.Spec.Settings(); !declared {
		t.Fatal("the fixture should declare settings, so that dropping the key leaves one behind")
	}

	// The layer was installed while the profile declared settings. Now it does
	// not, and the file it wrote is still there.
	delete(fixture.lay.Profile.Spec, "settings")
	if _, declared := fixture.lay.Profile.Spec.Settings(); declared {
		t.Fatal("the settings key was not dropped")
	}

	report := fixture.check(t)
	if got := pathsWithStatus(report, install.StatusOrphan); !slices.Equal(got, []string{left}) {
		t.Errorf("orphan = %v, want [%s] — a file cairn wrote and no longer renders", got, left)
	}
}

// TestCheckIgnoresTheHarnessOwnStateFiles is the defect that would have made
// `--check` useless on a real installation.
//
// ~/.claude is a live harness's home. If the sweep read it one level deep and
// called every unrendered file an orphan, settings.local.json and
// .credentials.json would be reported on every run of every real installation,
// the exit code would be non-zero forever, and a gate that always fails is not
// a gate.
func TestCheckIgnoresTheHarnessOwnStateFiles(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	theirs := []string{
		".claude/settings.local.json",
		".claude/.credentials.json",
		".claude/history.jsonl",
	}
	for _, rel := range theirs {
		writeInRoot(t, fixture.dir, rel, "not cairn's\n")
	}

	report := fixture.check(t)
	if !report.Clean() {
		t.Errorf("Clean() = false; the harness's own files in its own directory are not cairn's. Report:\n%s", report)
	}
	for _, entry := range report.Entries {
		if slices.Contains(theirs, entry.Path) {
			t.Errorf("%s was reported as %q and belongs to the harness", entry.Path, entry.Status)
		}
	}
}

func TestCheckIgnoresWhatIsOutsideTheDirectoriesCairnOwns(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	outside := []string{
		// Cairn does not own the home directory.
		"notes.md",
		".config/other/config.json",
		// Nor anything in the provider directory that cairn does not itself
		// write: it is the harness's own home — session state, caches, one
		// directory per project.
		".claude/projects/some-project/session.jsonl",
		".claude/todos/list.json",
	}
	for _, rel := range outside {
		writeInRoot(t, fixture.dir, rel, "not cairn's\n")
	}

	report := fixture.check(t)
	if !report.Clean() {
		t.Errorf("Clean() = false; nothing outside cairn's directories is a finding. Report:\n%s", report)
	}
	for _, entry := range report.Entries {
		if slices.Contains(outside, entry.Path) {
			t.Errorf("%s was reported as %q and is not cairn's", entry.Path, entry.Status)
		}
	}
}

func TestCheckReportsAnUnreadablePathAndKeepsGoing(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode, so there is no unreadable path to make")
	}
	fixture := newCheckFixture(t, "alpha")
	const locked = ".claude/settings.json"
	dest := fixture.path(locked)
	if err := os.Chmod(dest, 0o000); err != nil {
		t.Fatalf("make %s unreadable: %v", locked, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dest, 0o644); err != nil {
			t.Logf("restore the mode on %s: %v", locked, err)
		}
	})

	report := fixture.check(t)
	entry := entryAt(t, report, locked)
	if entry.Status != install.StatusUnreadable {
		t.Fatalf("%s is %q, want %q", locked, entry.Status, install.StatusUnreadable)
	}
	if entry.Detail == "" {
		t.Error("Detail is empty, want the read error")
	}
	// One unreadable path does not hide the rest of the layer.
	if got, want := len(report.Entries), len(fixture.files); got != want {
		t.Errorf("the report holds %d entries, want %d: %v", got, want, entryPaths(report.Entries))
	}
	if got, want := report.Count(install.StatusMatch), len(fixture.files)-1; got != want {
		t.Errorf("%d matched, want %d", got, want)
	}
	if report.Clean() {
		t.Error("Clean() = true with an unreadable path")
	}
}

func TestCheckFSReadsAFilesystemItIsNotStandingIn(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t)
	// A synthetic filesystem, holding the render plus one leftover. Nothing
	// here is on disk anywhere.
	fsys := fstest.MapFS{}
	for _, file := range fixture.files {
		fsys[file.Path] = &fstest.MapFile{Data: file.Content, Mode: 0o644}
	}
	const stray = ".claude/skills/stale/SKILL.md"
	fsys[stray] = &fstest.MapFile{Data: []byte("left behind\n"), Mode: 0o644}
	delete(fsys, ".claude/CLAUDE.md")

	report, err := install.CheckFS(fsys, fixture.lay)
	if err != nil {
		t.Fatalf("CheckFS: %v", err)
	}
	if report.Root != "" {
		t.Errorf("Root = %q, want empty: a filesystem view names no location", report.Root)
	}
	if got := pathsWithStatus(report, install.StatusMissing); !slices.Equal(got, []string{".claude/CLAUDE.md"}) {
		t.Errorf("missing = %v, want [.claude/CLAUDE.md]", got)
	}
	if got := pathsWithStatus(report, install.StatusOrphan); !slices.Equal(got, []string{stray}) {
		t.Errorf("orphan = %v, want [%s]", got, stray)
	}
}

func TestCheckFSRefusesWhatItCannotCheck(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t)
	if _, err := install.CheckFS(nil, fixture.lay); err == nil {
		t.Error("CheckFS(nil, layer) returned no error")
	}
	fsys, err := fixture.lay.Root.FS()
	if err != nil {
		t.Fatalf("Root.FS(): %v", err)
	}
	if _, err := install.CheckFS(fsys, nil); !errors.Is(err, install.ErrNoProfile) {
		t.Errorf("CheckFS(fs, nil) = %v, want ErrNoProfile", err)
	}
	if _, err := install.CheckFS(fsys, &install.Layer{}); !errors.Is(err, install.ErrNoProfile) {
		t.Errorf("CheckFS(fs, empty layer) = %v, want ErrNoProfile", err)
	}
}

func TestCheckRefusesARootItCannotRead(t *testing.T) {
	t.Parallel()
	if _, err := install.Check(nil); !errors.Is(err, install.ErrNoProfile) {
		t.Errorf("Check(nil) = %v, want ErrNoProfile", err)
	}
	fixture := newCheckFixture(t)
	root, err := install.NewRoot(filepath.Join(fixture.dir, "not-a-directory-that-exists"))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	lay := &install.Layer{Root: root, Profile: fixture.lay.Profile, Home: fixture.lay.Home}
	if _, err := install.Check(lay); !errors.Is(err, install.ErrRootNotFound) {
		t.Errorf("Check(missing root) = %v, want ErrRootNotFound", err)
	}
}

func TestCheckReportsALinkTheSweepFindsWithoutFollowingIt(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t)
	// A directory of someone else's, linked into the skills tree. Cairn
	// renders bytes and has no way to emit a link, so this is never cairn's
	// output — and it is not descended into, whatever is on the far end.
	target := t.TempDir()
	writeInRoot(t, target, "SKILL.md", "# not cairn's\n")
	writeInRoot(t, target, "references/deep.md", "# also not cairn's\n")
	if err := os.MkdirAll(fixture.path(".claude/skills"), 0o755); err != nil {
		t.Fatalf("create the skills directory: %v", err)
	}
	const linked = ".claude/skills/linked"
	symlinkOrSkip(t, target, fixture.path(linked))

	report := fixture.check(t)
	if got := pathsWithStatus(report, install.StatusOrphan); !slices.Equal(got, []string{linked}) {
		t.Errorf("orphan = %v, want [%s]: the link is reported and not walked", got, linked)
	}
	if detail := entryAt(t, report, linked).Detail; !strings.Contains(detail, target) {
		t.Errorf("Detail = %q, want it to name the target %q", detail, target)
	}
}

func TestCheckClassifiesEachPathOnce(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	writeInRoot(t, fixture.dir, ".claude/skills/stale/SKILL.md", "left behind\n")

	report := fixture.check(t)
	seen := make(map[string]int, len(report.Entries))
	for _, entry := range report.Entries {
		seen[entry.Path]++
	}
	for p, n := range seen {
		if n != 1 {
			t.Errorf("%s appears %d times; the manifest half and the sweep must not both claim it", p, n)
		}
	}
	// The rendered skill file lives inside the swept tree and is classified by
	// the first half, against the bytes.
	if entry := entryAt(t, report, ".claude/skills/alpha/"+bootdir.SkillFileName); entry.Status != install.StatusMatch {
		t.Errorf("the rendered skill file is %q, want %q", entry.Status, install.StatusMatch)
	}
}

func TestCheckLeavesTheRootExactlyAsItFoundIt(t *testing.T) {
	t.Parallel()
	fixture := newCheckFixture(t, "alpha")
	if err := os.Remove(fixture.path(".claude/settings.json")); err != nil {
		t.Fatalf("remove the settings document: %v", err)
	}
	if err := os.WriteFile(fixture.path(".claude/AGENTS.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatalf("edit the instruction file: %v", err)
	}
	writeInRoot(t, fixture.dir, ".claude/skills/stale/SKILL.md", "left behind\n")
	before := treeOf(t, fixture.dir)

	if report := fixture.check(t); report.Clean() {
		t.Fatal("the fixture is broken: the report is clean")
	}
	if after := treeOf(t, fixture.dir); !slices.Equal(before, after) {
		t.Errorf("the check changed the root.\nbefore: %v\nafter:  %v", before, after)
	}
}

// treeOf returns every path beneath dir with its size, so a test can assert
// that a check wrote, deleted and rewrote nothing.
func treeOf(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, path.Join(filepath.ToSlash(rel), info.Mode().String())+":"+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	slices.Sort(out)
	return out
}
