package cairn_test

import (
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/profile"
)

// TestNoDotDirectoriesUnderTestdata guards a defect that already reached a
// live session rather than one that might: a golden tree carried a literal
// .claude directory, and a running agent registered the fixture skills beneath
// it as real, directory-scoped skills that shadowed the genuine ones.
//
// testdata is inert to the Go tool. It is not inert to an agent harness, which
// resolves configuration by literal directory name anywhere in the working
// tree. A shadowed skill is bad; a spurious subagent definition is
// dispatchable.
//
// The rule is therefore broader than the one harness that caught us out: no
// directory anywhere under a testdata tree may be dot-prefixed, whichever tool
// claims the name. Store the fixture under an underscore-prefixed segment and
// map it back where the fixture is read.
func TestNoDotDirectoriesUnderTestdata(t *testing.T) {
	var found []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." || !d.IsDir() || !strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		// A dot-prefixed directory is never part of the module's source tree:
		// .git and .github sit at the repo root, and the harness materializes
		// its own worktrees under .claude. Report it when a testdata tree
		// contains it, then stop descending, since nothing below is source.
		if underTestdata(p) {
			found = append(found, filepath.ToSlash(p))
		}
		return fs.SkipDir
	})
	if err != nil {
		t.Fatalf("walk the module root: %v", err)
	}
	for _, dir := range found {
		t.Errorf("%s is a dot-prefixed fixture directory. An agent harness scans the "+
			"working tree for configuration by literal directory name, so a fixture "+
			"named .claude, .codex, or the like registers as live configuration in "+
			"whatever session has this repo open. Rename the segment to _ and map it "+
			"back where the fixture is read.", dir)
	}
}

// TestNoInstructionFilesUnderTestdata guards the same class of defect one file
// down from the directory the sibling test covers, and a worse case of it.
//
// A dot-prefixed fixture directory registers a skill, and a skill still has to
// be invoked before it does anything. An instruction file needs no invocation:
// Claude Code loads <dir>/CLAUDE.md and <dir>/AGENTS.md as instructions when
// any file in <dir> is read, and resolves the "@AGENTS.md" pointer
// transitively, so a one-line pointer pulls in the whole document beside it.
//
// This was observed rather than reasoned. A fixture pair planted beside the
// goldens and read with a file-reading tool injected both halves; the same
// pair read after its contents had been printed with cat did not — which is
// why an investigation that only ever cats these files concludes they are
// inert.
//
// Cairn writes agent instruction files, so its testdata will always hold the
// most directive-shaped text in the repo. That is why the rule is derived from
// the layouts rather than written out: an instruction file a later provider
// adds is covered the day its layout is registered.
func TestNoInstructionFilesUnderTestdata(t *testing.T) {
	names := instructionFileNames(t)
	var found []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Never descend into a dot-prefixed directory: .git and .github sit at
		// the repo root, and the harness materializes other agents' worktrees
		// under .claude, whose trees are not this module's to report on.
		if d.IsDir() {
			if p != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if _, ok := names[d.Name()]; ok && underTestdata(p) {
			found = append(found, filepath.ToSlash(p))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module root: %v", err)
	}
	for _, file := range found {
		t.Errorf("%s is an unmapped instruction file in a fixture tree. An agent harness "+
			"loads a file of this name as instructions when any file beside it is read, and "+
			"follows its @AGENTS.md pointer, so a fixture document is injected into whatever "+
			"session has this repo open. Store it with an underscore prefix and map it back "+
			"where the fixture is read.", file)
	}
}

// instructionFileNames returns the boot-directory artifacts an agent harness
// loads as instructions by literal filename, derived from the provider layouts
// so that a file a later provider reads is covered the day its layout is
// registered.
//
// The required check is what keeps it fail-closed. The derivation is a filter,
// and a filter that stops matching yields an empty set, which would make the
// walk above report nothing on any tree at all.
func instructionFileNames(t *testing.T) map[string]struct{} {
	t.Helper()
	names := make(map[string]struct{})
	for _, p := range []profile.Provider{
		profile.ProviderClaude,
		profile.ProviderCodex,
		profile.ProviderOpenCode,
	} {
		lay, err := bootdir.LayoutFor(p)
		if err != nil {
			// A provider with no layout yet renders nothing, so it contributes
			// no instruction file. Claude is required below, so an error there
			// is still caught.
			continue
		}
		for _, a := range []bootdir.Artifact{lay.Agents, lay.Pointer} {
			if !a.Declared() {
				continue
			}
			names[path.Base(a.RelPath)] = struct{}{}
		}
	}
	for _, required := range []string{"AGENTS.md", "CLAUDE.md"} {
		if _, ok := names[required]; !ok {
			t.Fatalf("%q is rendered into a boot directory but was not derived as an instruction "+
				"file, so this guard would pass on a tree containing one. Check that it is still "+
				"part of a registered provider layout.", required)
		}
	}
	return names
}

// underTestdata reports whether any ancestor directory of p is named testdata.
func underTestdata(p string) bool {
	for dir := filepath.Dir(p); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "testdata" {
			return true
		}
	}
	return false
}
