package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/chrispian/cairn/catalog"
)

// TestList covers the command that answers "what can I boot" against a
// directory of files. It replaces a SQL query the conductor profile ran against
// the store, so the question is the same one and the answer has to carry the
// same three facts: what a binding is called, what it boots, and where.
//
// And now a fourth, which the query never could: what the binding composes on
// top. The listing rendered three of catalog.Binding's fields and went on
// rendering three as parts, skills and prompts arrived, so two bindings that
// boot materially different sessions read as one row but for the name. See
// TestListRendersEveryBindingField, which is that decay written down as a
// failure rather than as a comment.
func TestList(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := filepath.Join(home, "bundle")
	skillsDir := filepath.Join(home, "skills")
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	writeSkill(t, skillsDir)
	seed(t, bundle, skillsDir, scopeDir)
	writeScopes(t, bundle, map[string]string{"repo": scopeDir})
	writeBinding(t, bundle, "aliased", "engineer", "repo")
	// A binding that composes, standing beside two that do not. A listing
	// resolves none of these ids — the catalog checks that a binding names a
	// profile it holds and checks nothing else — so what is printed is what the
	// file says, which is the promise this column makes.
	writeFile(t, filepath.Join(bundle, catalog.BindingsDir, "composed.yaml"),
		"profile: engineer\n"+
			"parts:\n  - docs-only\n  - git-flow\n"+
			"skills:\n  - code-review\n"+
			"prompts:\n  - handoff\n"+
			fmt.Sprintf("scope: %q\n", scopeDir), 0o644)

	list := func(t *testing.T, args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if err := run(ctx, append([]string{"list"}, append(args, "--profile", bundle)...), &stdout, &stderr); err != nil {
			t.Fatalf("list: %v\nstderr: %s", err, stderr.String())
		}
		if stderr.Len() > 0 {
			t.Errorf("list wrote to stderr with nothing to report:\n%s", stderr.String())
		}
		return stdout.String()
	}

	t.Run("a binding carries its profile and its directory", func(t *testing.T) {
		// One line, not three substrings: three assertions that each pass
		// somewhere in the document would pass on a listing that never put the
		// three facts together.
		out := list(t)
		if !hasLine(out, "aliased", "engineer", scopeDir) {
			t.Errorf("no line carries the binding, its profile and its directory:\n%s", out)
		}
	})

	t.Run("a binding says what it composes, and all three kinds of it", func(t *testing.T) {
		// One line again, and for a sharper reason than above: three
		// assertions that each pass somewhere in the document would pass on a
		// listing that showed one binding's parts and another's prompts.
		//
		// Ids and not counts. A count would leave two bindings that each add
		// one part rendering identically, which is the very complaint this
		// column answers, and for prompts it would stand for a command a
		// person can type without saying which command.
		out := list(t)
		if !hasLine(out, "composed", "engineer", scopeDir,
			"parts: docs-only, git-flow", "skills: code-review", "prompts: handoff") {
			t.Errorf("the row does not carry what the binding composes:\n%s", out)
		}
	})

	t.Run("a binding that composes nothing ends where it always ended", func(t *testing.T) {
		// The column is paid for only by the bindings that use it. writeRows
		// right-trims, so a row with an empty composition is byte-for-byte the
		// row it was before the column existed — which is what keeps a bundle
		// of plain bindings from re-rendering every file this listing is
		// planted into.
		out := list(t)
		row := lineStarting(t, out, "aliased")
		if !strings.HasSuffix(row, scopeDir) {
			t.Errorf("a binding composing nothing grew a column anyway: %q", row)
		}
	})

	t.Run("no line ends in whitespace", func(t *testing.T) {
		// writeRows' own promise, asserted against the document rather than
		// read off the function. This render is planted into a boot file, where
		// a run of invisible spaces is a diff, and adding a column is exactly
		// the change that breaks it.
		for _, line := range strings.Split(list(t), "\n") {
			if line != strings.TrimRight(line, " ") {
				t.Errorf("a line ends in spaces, which is a diff in a planted file: %q", line)
			}
		}
	})

	t.Run("a scope alias is resolved, and listed as well", func(t *testing.T) {
		// The alias is what the binding file says and the directory is what a
		// boot would work in. A listing showing the alias would make an
		// operator open a second file to find out where that is, which is the
		// whole reason the command exists.
		out := list(t)
		if strings.Contains(out, "aliased      engineer      repo\n") {
			t.Errorf("the binding shows its alias rather than the directory it resolves to:\n%s", out)
		}
		if !hasLine(out, "repo", scopeDir) {
			t.Errorf("the alias itself is not listed:\n%s", out)
		}
	})

	t.Run("an abstract profile is listed apart from the bootable ones", func(t *testing.T) {
		out := list(t)
		bootable := rowIDs(blockOf(t, out, "Profiles"))
		abstract := rowIDs(blockOf(t, out, "Abstract profiles"))
		if slices.Contains(bootable, "base") {
			t.Errorf("an abstract profile is listed as bootable: %v", bootable)
		}
		if !slices.Contains(abstract, "base") {
			t.Errorf("the abstract profile is not listed at all:\n%s", out)
		}
	})

	t.Run("the bundle's own path is not printed", func(t *testing.T) {
		// Deliberate, and load-bearing twice: the listing is planted into a
		// boot file, where an absolute path would be the one line of a render
		// that differs between two checkouts, and the operator running this at
		// a terminal just typed the flag that chose the bundle.
		if out := list(t); strings.Contains(out, bundle) {
			t.Errorf("the listing names the bundle it read:\n%s", out)
		}
	})

	t.Run("a block with nothing in it renders nothing", func(t *testing.T) {
		// install.Report's rule: a bundle with no aliases should not have to
		// scroll past a heading saying so.
		bare := filepath.Join(home, "bare")
		writeProfile(t, bare, bundleProfile{ID: "only", Name: "Only", Provider: "claude"})

		var stdout, stderr bytes.Buffer
		if err := run(ctx, []string{"list", "--profile", bare}, &stdout, &stderr); err != nil {
			t.Fatalf("list: %v\nstderr: %s", err, stderr.String())
		}
		for _, absent := range []string{"Bindings", "Abstract profiles", "Scope aliases"} {
			if strings.Contains(stdout.String(), absent) {
				t.Errorf("a bundle with no %s printed the heading anyway:\n%s", absent, stdout.String())
			}
		}
		if !strings.Contains(stdout.String(), "Profiles (1)") {
			t.Errorf("the one block with something in it is missing:\n%s", stdout.String())
		}
	})

	t.Run("it takes no target", func(t *testing.T) {
		// A listing is of the catalog. One profile is what `cairn show` is for,
		// and a target silently ignored would read as a filter that does not
		// filter.
		var stdout, stderr bytes.Buffer
		err := run(ctx, []string{"list", "engineer", "--profile", bundle}, &stdout, &stderr)
		if err == nil {
			t.Fatal("list with a target reported success")
		}
		if !strings.Contains(err.Error(), "engineer") {
			t.Errorf("the refusal does not name what it was given: %v", err)
		}
	})
}

// hasLine reports whether some line of out carries every field, in order. It is
// how a row is asserted as one fact rather than as several that happen to be in
// the same document.
func hasLine(out string, fields ...string) bool {
	for _, line := range strings.Split(out, "\n") {
		rest, ok := line, true
		for _, field := range fields {
			_, after, found := strings.Cut(rest, field)
			if !found {
				ok = false
				break
			}
			rest = after
		}
		if ok {
			return true
		}
	}
	return false
}

// rowIDs returns the first column of every row of a block, dropping the note
// under the heading. It is a whole-field read rather than a substring one:
// "base2" carries "base", and an assertion that could not tell them apart
// would pass on a listing that put an abstract profile among the bootable
// ones.
func rowIDs(block string) []string {
	var out []string
	for i, line := range strings.Split(block, "\n") {
		fields := strings.Fields(line)
		if i == 0 || len(fields) == 0 {
			continue
		}
		out = append(out, fields[0])
	}
	return out
}

// blockOf returns the lines under a heading, up to the blank line that ends it.
func blockOf(t *testing.T, out, heading string) string {
	t.Helper()
	var block []string
	in := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, heading+" ("):
			in = true
		case in && strings.TrimSpace(line) == "":
			return strings.Join(block, "\n")
		case in:
			block = append(block, line)
		}
	}
	if !in {
		t.Fatalf("the listing has no %q block:\n%s", heading, out)
	}
	return strings.Join(block, "\n")
}

// lineStarting returns the one line of out whose first field is name, failing
// when there is not exactly one. It is how a single row is pulled out whole, so
// an assertion can be about where the row ends and not only about what it
// contains.
func lineStarting(t *testing.T, out, name string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == name {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one row for %q, got %d:\n%s", name, len(found), out)
	}
	return found[0]
}

// TestListRendersEveryBindingField fails when catalog.Binding gains a field the
// listing was not taught to show.
//
// It is this task's own defect, mechanized. A binding grew parts, then skills,
// then prompts, and listDocument went on rendering the three fields it was
// written against — so a binding that composed a different session rendered as
// the row beside it. catalog.Binding's doc comment predicts exactly this decay
// for its own prose, and prose is what failed: nothing anywhere said the
// listing had fallen behind.
//
// Reflection rather than a golden of the render, because the question is about
// the struct and not about the bytes. A field added and left unshown changes no
// output, so no output test can notice it; this one turns the next field into a
// failure that names it and asks for a decision. Answering the failure is
// editing the map — after deciding that the field belongs in a row, or that it
// does not and why.
func TestListRendersEveryBindingField(t *testing.T) {
	shown := map[string]string{
		"Name":      "the first column",
		"ProfileID": "the second column",
		"Scope":     "the third column, resolved through the alias registry",
		"Parts":     "the composition column",
		"Skills":    "the composition column",
		"Prompts":   "the composition column",
	}
	fields := reflect.VisibleFields(reflect.TypeFor[catalog.Binding]())
	for _, f := range fields {
		if _, ok := shown[f.Name]; !ok {
			t.Errorf("catalog.Binding.%s is new and `cairn list` does not show it. Decide whether a "+
				"row should carry it, then say so here — a field silently left out is how this "+
				"listing fell three fields behind before.", f.Name)
		}
	}
	for name := range shown {
		if !slices.ContainsFunc(fields, func(f reflect.StructField) bool { return f.Name == name }) {
			t.Errorf("this test claims `cairn list` shows catalog.Binding.%s, which no longer exists", name)
		}
	}
}
