package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/profile"
	"github.com/chrispian/cairn/store"
)

// TestShow covers the command that answers "what does this profile actually
// resolve to". It is the mitigation the keyed merge owes the operator — a
// value composed from three profiles reads exactly like a value one profile
// wrote — so what is asserted here is that the document says which profiles
// contributed, and not only what came out.
func TestShow(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	skillsDir := filepath.Join(home, "skills")
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	writeSkill(t, skillsDir)
	seed(t, ctx, dbPath, skillsDir, scopeDir)

	show := func(t *testing.T, args ...string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if err := run(ctx, append([]string{"show"}, append(args, "--db", dbPath)...), &stdout, &stderr); err != nil {
			t.Fatalf("show %v: %v\nstderr: %s", args, err, stderr.String())
		}
		return stdout.String(), stderr.String()
	}

	t.Run("an abstract profile shows", func(t *testing.T) {
		// `cairn boot` refuses one and this does not. The abstract root is
		// where the settings, the templates and the standing skill set live,
		// which makes it the profile most worth reading.
		out, _ := show(t, "base")
		for _, want := range []string{
			"profile      base",
			"chain        base",
			"abstract     true",
			"spec.settings",
			"spec.templates",
			"somethingCairnHasNeverHeardOf", // an unknown key shows like any other
		} {
			if !strings.Contains(out, want) {
				t.Errorf("show base does not carry %q:\n%s", want, out)
			}
		}
	})

	t.Run("the chain is walked ancestor-first", func(t *testing.T) {
		out, errOut := show(t, "engineer")
		if !strings.Contains(out, "chain        base -> engineer") {
			t.Errorf("the chain is not reported ancestor-first:\n%s", out)
		}
		// The document is the whole output. Everything a boot reports on
		// stderr — a slot that failed, a marker nothing filled — belongs to
		// rendering, and nothing here renders.
		if errOut != "" {
			t.Errorf("show wrote to stderr with nothing to report:\n%s", errOut)
		}
	})

	t.Run("a profile declaring nothing prints an empty spec", func(t *testing.T) {
		st, err := store.Open(ctx, dbPath)
		if err != nil {
			t.Fatalf("open the store: %v", err)
		}
		if err := st.PutProfile(ctx, profile.Profile{
			ID: "bare", Extends: "", Name: "Bare", Provider: profile.ProviderClaude,
		}); err != nil {
			t.Fatalf("put the profile: %v", err)
		}
		_ = st.Close()

		out, _ := show(t, "bare")
		if !strings.HasSuffix(out, "\nspec (0 keys)\n") {
			t.Errorf("an empty manifest does not end the document with its count:\n%s", out)
		}
		// No note under a heading with nothing under it, matching
		// install.Report, where a section with no entries renders nothing.
		if strings.Contains(out, "The names beside a key") {
			t.Errorf("an empty manifest carried the section note anyway:\n%s", out)
		}
	})

	t.Run("a key names every profile that declares it", func(t *testing.T) {
		// The single most important assertion in this file, so it is made on
		// the whole line: the key and the names have to be the same fact.
		// Asserting that both strings appear somewhere in the document would
		// pass on a document that never associated them, and a suffix match
		// would pass on "base, engineer" while claiming to reject it.
		out, _ := show(t, "engineer")
		for key, want := range map[string]string{
			"templates": "base, engineer", // both declare it: a composition
			"skills":    "engineer",       // the leaf alone
			"settings":  "base",           // the root alone
		} {
			got, ok := declarersOf(out, key)
			if !ok {
				t.Errorf("spec.%s was not printed:\n%s", key, out)
				continue
			}
			if got != want {
				t.Errorf("spec.%s is declared by %q, want %q", key, got, want)
			}
		}
	})

	t.Run("a merged value carries what both profiles declared", func(t *testing.T) {
		// The labels being right is not the same as the value being composed.
		// base declares AGENTS.md and CLAUDE.md; engineer restates all three
		// of its own. What proves a merge happened is that the merged object
		// carries a destination the leaf never mentioned.
		out, _ := show(t, "engineer")
		templates := valueOf(t, out, "templates").(map[string]any)
		for _, dest := range []string{"AGENTS.md", "CLAUDE.md", "boot.md"} {
			if _, ok := templates[dest]; !ok {
				t.Errorf("the merged templates lost %s: %v", dest, templates)
			}
		}
	})

	t.Run("a value one profile declares is that profile's own bytes", func(t *testing.T) {
		// The promise the section note makes. Indentation is whitespace, so
		// compacting what was printed has to give back exactly what the store
		// holds — the property bootdir.RenderSettings depends on.
		out, _ := show(t, "base")
		st, err := store.Open(ctx, dbPath)
		if err != nil {
			t.Fatalf("open the store: %v", err)
		}
		defer func() { _ = st.Close() }()
		stored, err := st.Profile(ctx, "base")
		if err != nil {
			t.Fatalf("load base: %v", err)
		}
		for key, raw := range stored.Spec {
			var printed, want bytes.Buffer
			if err := json.Compact(&printed, []byte(rawValueOf(t, out, key))); err != nil {
				t.Fatalf("spec.%s: the printed value is not JSON: %v", key, err)
			}
			if err := json.Compact(&want, raw); err != nil {
				t.Fatalf("spec.%s: the stored value is not JSON: %v", key, err)
			}
			if printed.String() != want.String() {
				t.Errorf("spec.%s was re-spelled:\n printed %s\n stored  %s", key, printed.String(), want.String())
			}
		}
	})

	t.Run("the merged spec is laid out with its keys sorted", func(t *testing.T) {
		out, _ := show(t, "engineer")
		var keys []string
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "spec.") {
				keys = append(keys, strings.Fields(line)[0])
			}
		}
		if len(keys) == 0 {
			t.Fatalf("no manifest keys were printed:\n%s", out)
		}
		if !slices.IsSorted(keys) {
			t.Errorf("the manifest keys are not sorted: %v", keys)
		}
		// Laid out rather than printed raw: a nested value the store holds on
		// one line arrives here across several, indented under its key.
		if !strings.Contains(out, "\n  {\n    \"") {
			t.Errorf("a nested value was not indented for reading:\n%s", out)
		}
	})

	t.Run("the scope reported is the one boot would work in", func(t *testing.T) {
		// Symlinks resolved, because that is what a boot resolves — scope.Parse
		// canonicalizes so that two spellings of one directory arrive as one
		// path, and on macOS the test's own temp root is behind one.
		declared := canonical(t, scopeDir)

		// The binding declares it, so showing the binding shows it.
		out, _ := show(t, "engineer")
		if !strings.Contains(out, "scope        "+declared+"\n") {
			t.Errorf("the binding's scope is not reported as %s:\n%s", declared, out)
		}
		// A profile with no binding declares none, and the field says so by
		// carrying nothing.
		out, _ = show(t, "base")
		if !strings.Contains(out, "\nscope\n") {
			t.Errorf("a profile with no declared scope reports one anyway:\n%s", out)
		}
		// --scope overrides the binding's, exactly as it does for a boot.
		other := filepath.Join(home, "elsewhere")
		mustMkdir(t, other)
		out, _ = show(t, "engineer", "--scope", other)
		if !strings.Contains(out, "scope        "+canonical(t, other)+"\n") {
			t.Errorf("--scope was not reported:\n%s", out)
		}
		if strings.Contains(out, "scope        "+declared+"\n") {
			t.Errorf("--scope did not override the binding's:\n%s", out)
		}
	})

	t.Run("a scope that does not resolve is reported, not refused", func(t *testing.T) {
		// scope.ErrNotDirectory exists to keep the containment guard
		// answerable, and this command runs that guard on nothing. Refusing
		// the whole document over a directory nothing is about to be written
		// into would make show least usable exactly when something is wrong.
		var stdout, stderr bytes.Buffer
		err := run(ctx, []string{
			"show", "engineer", "--db", dbPath,
			"--scope", filepath.Join(home, "no-such-directory"),
		}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("show with an unresolvable scope = %v, want the document anyway", err)
		}
		if !strings.Contains(stdout.String(), "spec.templates") {
			t.Errorf("the document was not printed:\n%s", stdout.String())
		}
		if !strings.Contains(stdout.String(), "\nscope\n") {
			t.Errorf("a scope that did not resolve was reported as one:\n%s", stdout.String())
		}
		// The operator hears about it where an operator reads, with the value
		// they wrote ahead of the error that says what it expanded to.
		if !strings.Contains(stderr.String(), "no-such-directory") ||
			!strings.Contains(stderr.String(), "did not resolve") {
			t.Errorf("stderr does not report the scope that did not resolve:\n%s", stderr.String())
		}
	})

	t.Run("show takes exactly one target", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if err := run(ctx, []string{"show", "--db", dbPath}, &out, &errOut); err == nil {
			t.Error("show with no target reported success")
		}
		if err := run(ctx, []string{"show", "base", "engineer", "--db", dbPath}, &out, &errOut); err == nil {
			t.Error("show with two targets reported success")
		}
		if err := run(ctx, []string{"show", "nobody", "--db", dbPath}, &out, &errOut); err == nil ||
			!strings.Contains(err.Error(), "nobody") {
			t.Errorf("show of an unknown target = %v, want a refusal naming it", err)
		}
	})
}

// TestShowRendersNothing is the command's other half of its contract, and the
// half a reader cannot check by looking at the output.
//
// Rendering, not writing. Opening the database writes — store.Open creates the
// file and its parent directory when they are absent and applies the schema in
// an immediate transaction — so "writes nothing" is not the claim, and the
// subtest below pins what it does write rather than pretending it does not.
// What is asserted here is that nothing else appears: no boot directory, no
// installed layer, no file beside the database.
//
// HOME is pointed at the fixture so that every default a write would take —
// the boot root under $HOME, and the installed layer's root, which is $HOME —
// lands inside the tree this walks. Without that the defaults resolve to the
// operator's real home and the assertion covers a directory nothing was ever
// going to touch.
func TestShowRendersNothing(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	skillsDir := filepath.Join(home, "skills")
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	writeSkill(t, skillsDir)
	seed(t, ctx, dbPath, skillsDir, scopeDir)

	t.Setenv("HOME", home)
	t.Setenv("CAIRN_BOOT_ROOT", "")

	before := pathsUnder(t, home)
	for _, target := range []string{"base", "engineer"} {
		var stdout, stderr bytes.Buffer
		if err := run(ctx, []string{"show", target, "--db", dbPath}, &stdout, &stderr); err != nil {
			t.Fatalf("show %s: %v\nstderr: %s", target, err, stderr.String())
		}
		if stdout.Len() == 0 {
			t.Fatalf("show %s printed nothing", target)
		}
	}
	if after := pathsUnder(t, home); !slices.Equal(before, after) {
		t.Errorf("show left something behind:\nbefore %v\nafter  %v", before, after)
	}
	// Named individually as well, because the paths above are only as strong
	// as the tree they cover. The boot root comes from the constant rather
	// than a literal: a literal left behind by a change to the default would
	// go on passing while asserting nothing.
	for _, rel := range []string{".claude", filepath.FromSlash(bootdir.DefaultRootRel)} {
		if _, err := os.Stat(filepath.Join(home, rel)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("show created %s: %v", rel, err)
		}
	}

	// The one thing it does write, pinned rather than denied. store.Open
	// creates an absent database, and it does so before the target is looked
	// up, so even a show that goes on to fail leaves the file behind. It is
	// cairn's rule everywhere — docs/plan.md §3 — and the reason the contract
	// above is "renders nothing" and not "writes nothing".
	fresh := filepath.Join(t.TempDir(), "made", "up", "cairn.db")
	var stdout, stderr bytes.Buffer
	if err := run(ctx, []string{"show", "nobody", "--db", fresh}, &stdout, &stderr); err == nil {
		t.Fatal("show of an unknown target in an empty database reported success")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("opening an absent database did not create it, which the doc comment claims it does: %v", err)
	}
}

// declarersOf returns the profiles named beside spec.<key> in a printed
// document, and whether the key was printed at all.
func declarersOf(out, key string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "spec."+key+" "); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// rawValueOf returns the JSON text printed under spec.<key>, with the margin
// every line of it sits in removed.
func rawValueOf(t *testing.T, out, key string) string {
	t.Helper()
	var body []string
	found := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "spec."+key+" "):
			found = true
		case found && strings.HasPrefix(line, "spec."):
			return strings.Join(body, "\n")
		case found && strings.HasPrefix(line, "  "):
			body = append(body, strings.TrimPrefix(line, "  "))
		}
	}
	if !found {
		t.Fatalf("spec.%s was not printed:\n%s", key, out)
	}
	return strings.Join(body, "\n")
}

// valueOf returns the value printed under spec.<key>, decoded.
func valueOf(t *testing.T, out, key string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(rawValueOf(t, out, key)), &v); err != nil {
		t.Fatalf("spec.%s did not print valid JSON: %v", key, err)
	}
	return v
}

// canonical is a directory as scope.Parse would report it, which is the form
// every scope reaches the output in.
func canonical(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %s: %v", dir, err)
	}
	return resolved
}

// pathsUnder returns every path beneath dir, sorted, with sqlite's own
// sidecars left out: opening the database is a read this command has to make,
// and a journal appearing beside it is not cairn writing anything.
func pathsUnder(t *testing.T, dir string) []string {
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
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if strings.HasSuffix(rel, suffix) {
				return nil
			}
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	slices.Sort(out)
	return out
}
