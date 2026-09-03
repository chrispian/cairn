package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrispian/cairn/profile"
	"github.com/chrispian/cairn/store"
)

// TestAProfileBundleRelocatesWithoutEdits is the mechanism --profile exists
// for, asserted the only way it can be: the same profile row, rendered twice
// against two bundles, produces each bundle's own content.
//
// One boot proves nothing. A profile naming "$CAIRN_PROFILE_ROOT/templates/..."
// that renders the right file once could be reading a path that happens to be
// right for other reasons — the workdir, the home directory, an ambient
// variable. Two bundles at two paths, with nothing between the runs but the
// flag, is what says the bundle root decided it.
//
// All three of the places a manifest names somewhere to read from are in play,
// because they resolve through three different calls at the composition root
// and a thread that reached two of them would look exactly like this from one:
// a template's source, a slot's source, and a files entry's source.
func TestAProfileBundleRelocatesWithoutEdits(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	seedBundled(t, ctx, dbPath)

	for _, label := range []string{"A", "B"} {
		t.Run("bundle "+label, func(t *testing.T) {
			bundle := filepath.Join(home, "bundle-"+label)
			writeBundle(t, bundle, label)

			dir := bootBundled(t, ctx, dbPath, scopeDir,
				filepath.Join(home, "boot-"+label), "--profile", bundle)

			// The template itself came out of the bundle, and so did the
			// section the slot filled inside it.
			agents := read(t, dir, "AGENTS.md")
			for _, want := range []string{"# agents from " + label, "the note in " + label} {
				if !strings.Contains(agents, want) {
					t.Errorf("AGENTS.md does not carry %q:\n%s", want, agents)
				}
			}
			// And the file the profile promised at a path of its own.
			if got, want := read(t, dir, "docs/handbook.md"), "the handbook in "+label+"\n"; got != want {
				t.Errorf("docs/handbook.md = %q, want %q", got, want)
			}
		})
	}
}

// TestAProfileBundleIsAbsolutizedFromWhereTheOperatorTyped covers the failure a
// relative --profile would be, which is silent rather than loud.
//
// A bundle root is a prefix on paths cairn resolves elsewhere: a slot's static
// path resolves against the instance's scope, not against the shell. So
// "--profile bundle-A" run from the directory that holds bundle-A would reach
// "<scope>/bundle-A/templates/agents.md" — a path the operator never wrote,
// under a directory they were not talking about. The scope here is deliberately
// not the working directory, so a run that skipped the absolutizing would fail
// rather than pass by coincidence.
func TestAProfileBundleIsAbsolutizedFromWhereTheOperatorTyped(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	seedBundled(t, ctx, dbPath)

	writeBundle(t, filepath.Join(home, "bundle"), "the cwd")
	t.Chdir(home)

	dir := bootBundled(t, ctx, dbPath, scopeDir, filepath.Join(home, "boot"), "--profile", "bundle")
	if got := read(t, dir, "AGENTS.md"); !strings.Contains(got, "# agents from the cwd") {
		t.Errorf("a relative --profile did not resolve against the working directory:\n%s", got)
	}
	// The absolute form is what the variable carries, not the spelling the
	// operator typed: a value expanding to "bundle/templates/agents.md" would
	// be resolved again, somewhere else, by whatever reads it.
	//
	// Built from the working directory cairn itself would have read rather
	// than from the temp root, because a temp root reached through a symlink
	// — every one on macOS — has two spellings and only one of them is the one
	// filepath.Abs produces.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	absolute := filepath.Join(wd, "bundle")
	if got := showBundled(t, ctx, dbPath, "--profile", "bundle"); !strings.Contains(got, "profile root  "+absolute+"\n") {
		t.Errorf("the reported bundle root is not the absolute %s:\n%s", absolute, got)
	}
}

// TestTheProfileFlagWinsOverTheVariable pins the precedence, in both
// directions.
//
// It is the precedence every other path cairn resolves already has — CAIRN_DB
// and CAIRN_BOOT_ROOT each lose to their flag — and the second half matters as
// much as the first: a command run without the flag must read the variable
// exactly as it did before this flag existed, because a bundle exported into a
// shell is the form that costs an operator nothing to use.
func TestTheProfileFlagWinsOverTheVariable(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	seedBundled(t, ctx, dbPath)

	flagged := filepath.Join(home, "bundle-flag")
	exported := filepath.Join(home, "bundle-env")
	writeBundle(t, flagged, "the flag")
	writeBundle(t, exported, "the variable")
	t.Setenv(envProfileRoot, exported)

	dir := bootBundled(t, ctx, dbPath, scopeDir, filepath.Join(home, "boot-flag"), "--profile", flagged)
	if got := read(t, dir, "AGENTS.md"); !strings.Contains(got, "# agents from the flag") {
		t.Errorf("--profile did not win over %s:\n%s", envProfileRoot, got)
	}

	dir = bootBundled(t, ctx, dbPath, scopeDir, filepath.Join(home, "boot-env"))
	if got := read(t, dir, "AGENTS.md"); !strings.Contains(got, "# agents from the variable") {
		t.Errorf("without the flag, %s was not read from the environment:\n%s", envProfileRoot, got)
	}
}

// TestTheProfileFlagIsRefusedWhenItNamesNoBundle covers the one thing --profile
// validates, on all three commands.
//
// It is not a rule about where a bundle may live: any directory is accepted and
// nothing is required inside it. What it buys is where the failure lands. A
// root that is not there makes every value expanding it wrong at once, and the
// operator would otherwise read that as one diagnostic per derived path, none
// of which names the flag behind them.
func TestTheProfileFlagIsRefusedWhenItNamesNoBundle(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	seedBundled(t, ctx, dbPath)

	notADirectory := filepath.Join(home, "bundle.txt")
	writeFile(t, notADirectory, "a file where a bundle was named\n", 0o644)
	absent := filepath.Join(home, "no-such-bundle")

	for _, named := range []struct{ name, path string }{
		{"a directory that is not there", absent},
		{"a file", notADirectory},
	} {
		t.Run(named.name, func(t *testing.T) {
			for _, args := range [][]string{
				{"boot", "bundled", "--boot-root", filepath.Join(home, "boot"), "--session", "s1"},
				{"install", "bundled", "--root", t.TempDir()},
				{"show", "bundled"},
			} {
				var stdout, stderr bytes.Buffer
				full := append(append([]string{}, args...), "--db", dbPath, "--profile", named.path)
				err := run(ctx, full, &stdout, &stderr)
				if err == nil {
					t.Fatalf("%s with --profile %s reported success", args[0], named.path)
				}
				if !strings.Contains(err.Error(), named.path) {
					t.Errorf("the %s refusal does not name the directory: %v", args[0], err)
				}
				if got := stdout.String(); got != "" {
					t.Errorf("a refused %s printed %q, want nothing on stdout", args[0], got)
				}
			}
		})
	}
}

// TestTheInstalledLayerReadsTheBundleToo is the second command's half of the
// thread. The installed layer resolves its own templates and its own static
// slots, through calls the boot path does not share, so a bundle root wired
// into one says nothing about the other.
//
// Against a fixture --root, never a real home: `cairn install` rewrites the
// live ~/.claude every agent working on cairn runs under.
func TestTheInstalledLayerReadsTheBundleToo(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	seedBundled(t, ctx, dbPath)

	bundle := filepath.Join(home, "bundle")
	writeBundle(t, bundle, "the installed layer")

	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run(ctx, []string{
		"install", "bundled", "--db", dbPath, "--root", root, "--profile", bundle,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("install: %v\nstderr: %s", err, stderr.String())
	}
	agents := read(t, root, ".claude/AGENTS.md")
	for _, want := range []string{"# agents from the installed layer", "the note in the installed layer"} {
		if !strings.Contains(agents, want) {
			t.Errorf(".claude/AGENTS.md does not carry %q:\n%s", want, agents)
		}
	}
}

// TestTheBundleReachesTheKeysThatTakeAPathRatherThanASource covers the half of
// the thread that does not run through a resolver.
//
// spec.skills_dir, spec.trees and spec.access.directories are expanded by the
// renderers, out of the lookup carried on the rendered instance —
// bootdir.Instance.Env and install.Layer.Env — rather than by slots.Options.Env
// on the way in. They are two more sites the one value has to reach, and the
// third key is the one with something at stake: an access directory is written
// into the settings document the harness matches paths against, so a thread
// that came loose there changes what a launched agent may read, in a file
// nobody reads by hand.
//
// The decoy bundle exported into the environment is what makes this a test of
// which root was used rather than of whether one was. An unwired site would
// otherwise expand to nothing, fail to find "/skills", and fail the render —
// passing for a reason that says nothing about a site reaching the wrong
// bundle. With a decoy in the variable, every unwired site resolves to a real
// directory holding the wrong label.
func TestTheBundleReachesTheKeysThatTakeAPathRatherThanASource(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	seedBundled(t, ctx, dbPath)

	bundle := filepath.Join(home, "bundle-named")
	decoy := filepath.Join(home, "bundle-decoy")
	writeBundle(t, bundle, "the named bundle")
	writeBundle(t, decoy, "the decoy")
	t.Setenv(envProfileRoot, decoy)

	// A granted directory leaves cairn for a harness to compare paths against,
	// so it is written out with its symlinks resolved — and every temp root on
	// macOS is behind one.
	granted, refused := canonical(t, bundle), canonical(t, decoy)

	t.Run("a boot directory", func(t *testing.T) {
		dir := filepath.Join(home, "boot")
		var stdout, stderr bytes.Buffer
		if err := run(ctx, []string{
			"boot", "bundled-paths", "--db", dbPath, "--scope", scopeDir,
			"--boot-root", dir, "--session", "s1", "--profile", bundle,
		}, &stdout, &stderr); err != nil {
			t.Fatalf("boot: %v\nstderr: %s", err, stderr.String())
		}
		planted := strings.TrimSpace(stdout.String())

		// spec.skills_dir, through the skills renderer.
		if got, want := read(t, planted, ".claude/skills/relocatable/SKILL.md"),
			"# the skill in the named bundle\n"; got != want {
			t.Errorf("the planted skill is %q, want %q", got, want)
		}
		// spec.trees, through the tree copier.
		if got := read(t, planted, "bundled/agents.md"); !strings.Contains(got, "# agents from the named bundle") {
			t.Errorf("the copied tree did not come from the named bundle:\n%s", got)
		}
		assertGrants(t, read(t, planted, ".claude/settings.json"), granted, refused)
	})

	// The installed layer renders its skills and its settings from a lookup of
	// its own, so the boot above says nothing about it. Against a fixture
	// --root, never a real home: `cairn install` rewrites the live ~/.claude
	// every agent working on cairn runs under.
	t.Run("the installed layer", func(t *testing.T) {
		root := t.TempDir()
		var stdout, stderr bytes.Buffer
		if err := run(ctx, []string{
			"install", "bundled-paths", "--db", dbPath, "--root", root, "--profile", bundle,
		}, &stdout, &stderr); err != nil {
			t.Fatalf("install: %v\nstderr: %s", err, stderr.String())
		}
		if got, want := read(t, root, ".claude/skills/relocatable/SKILL.md"),
			"# the skill in the named bundle\n"; got != want {
			t.Errorf("the installed skill is %q, want %q", got, want)
		}
		assertGrants(t, read(t, root, ".claude/settings.json"), granted, refused)
	})
}

// assertGrants reads the granted directories out of a settings document and
// asserts the bundle is among them and the decoy is not.
//
// The document is walked rather than searched for a substring. The key the
// directories land under is the provider adapter's to choose and cairn never
// names it — see bootdir.accessFragment — so this asks the same question
// without hardcoding the answer: the bundle's path appears somewhere in the
// document as a value, and the decoy's appears nowhere at all.
func assertGrants(t *testing.T, document, granted, refused string) {
	t.Helper()
	if !strings.Contains(document, granted) {
		t.Errorf("the settings document does not grant %s:\n%s", granted, document)
	}
	if strings.Contains(document, refused) {
		t.Errorf("the settings document grants %s, which the flag overrode:\n%s", refused, document)
	}
}

// TestShowReportsTheProfileRoot covers the only thing --profile can change
// about a command that reads no value and renders nothing.
//
// The field is reported whether it was set by the flag or by the environment,
// because both are what a boot would expand the variable to — and the
// environment's is reported exactly as it is held, since cairn passes that one
// through rather than resolving it.
func TestShowReportsTheProfileRoot(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	seedBundled(t, ctx, dbPath)

	bundle := filepath.Join(home, "bundle")
	writeBundle(t, bundle, "shown")

	if got := showBundled(t, ctx, dbPath); !strings.Contains(got, "\nprofile root\n") {
		t.Errorf("with no bundle in play the field does not print bare:\n%s", got)
	}
	if got := showBundled(t, ctx, dbPath, "--profile", bundle); !strings.Contains(got, "profile root  "+bundle+"\n") {
		t.Errorf("--profile is not reported:\n%s", got)
	}

	t.Setenv(envProfileRoot, "~/wherever/the/operator/put/it")
	if got := showBundled(t, ctx, dbPath); !strings.Contains(got, "profile root  ~/wherever/the/operator/put/it\n") {
		t.Errorf("the variable is not reported as it is held:\n%s", got)
	}
}

// TestUsageNamesTheProfileRootVariable ties the usage text to the constant, for
// the reason TestUsageNamesTheBootRootDefault does: the flag is only usable if
// an operator knows which variable it seeds, and nothing else would notice the
// two drifting apart.
func TestUsageNamesTheProfileRootVariable(t *testing.T) {
	if !strings.Contains(usage, envProfileRoot) {
		t.Errorf("usage does not name %s, which --profile seeds:\n%s", envProfileRoot, usage)
	}
	if !strings.Contains(profileFlagUsage, envProfileRoot) {
		t.Errorf("the flag's own description does not name %s: %q", envProfileRoot, profileFlagUsage)
	}
}

// writeBundle lays down a profile bundle at root and labels every file in it,
// so that two bundles are told apart by what they render rather than by where
// they are.
//
// All four directories the bundle layout names are created, and cairn opens
// none of them: the bundle holds handles, and what is read is whatever path a
// profile names inside it. They are here because the fixture is meant to be
// the shape an operator will build, not the subset this test reaches into.
func writeBundle(t *testing.T, root, label string) {
	t.Helper()
	for _, dir := range []string{"profiles", "bindings", "templates", "skills"} {
		mustMkdir(t, filepath.Join(root, dir))
	}
	writeFile(t, filepath.Join(root, "templates", "agents.md"),
		"# agents from "+label+"\n\n<!-- cairn:slot note -->\n", 0o644)
	writeFile(t, filepath.Join(root, "templates", "note.md"), "the note in "+label+"\n", 0o644)
	writeFile(t, filepath.Join(root, "skills", "handbook.md"), "the handbook in "+label+"\n", 0o644)
	// A skill is a directory, and skills_dir names the directory those sit in
	// rather than any one of them. The loose handbook.md beside it is read as a
	// file source and never as a skill: a render copies the names the profile
	// declared and looks at nothing else in here.
	mustMkdir(t, filepath.Join(root, "skills", "relocatable"))
	writeFile(t, filepath.Join(root, "skills", "relocatable", "SKILL.md"),
		"# the skill in "+label+"\n", 0o644)
}

// seedBundled writes the one profile these tests render: every value that names
// somewhere to read from is written against $CAIRN_PROFILE_ROOT, and none of
// them is ever edited again.
func seedBundled(t *testing.T, ctx context.Context, dbPath string) {
	t.Helper()
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer func() { _ = st.Close() }()

	if err := st.PutProfile(ctx, profile.Profile{
		ID:       "bundled",
		Name:     "Bundled",
		Provider: profile.ProviderClaude,
		Model:    "opus",
		Spec: profile.Spec{
			"templates": json.RawMessage(`{
				"AGENTS.md": {"kind":"static_file","static_file":{"path":"$CAIRN_PROFILE_ROOT/templates/agents.md"}},
				"CLAUDE.md": "@AGENTS.md\n"
			}`),
			"slots": json.RawMessage(`[
				{"name":"note","source":{"kind":"static_file","static_file":{"path":"$CAIRN_PROFILE_ROOT/templates/note.md"}}}
			]`),
			"files": json.RawMessage(`{
				"docs/handbook.md": {"kind":"static_file","static_file":{"path":"$CAIRN_PROFILE_ROOT/skills/handbook.md"}}
			}`),
		},
	}); err != nil {
		t.Fatalf("put the profile: %v", err)
	}

	// The three manifest keys that take a path rather than a source, kept in a
	// second profile so the relocation test above renders only what it asserts
	// on. These reach the lookup through the rendered instance instead of
	// through a resolver, which is a different thread and the one with the
	// most at stake.
	if err := st.PutProfile(ctx, profile.Profile{
		ID:       "bundled-paths",
		Name:     "Bundled paths",
		Provider: profile.ProviderClaude,
		Model:    "opus",
		Spec: profile.Spec{
			"templates":  json.RawMessage(`{"AGENTS.md": "# paths\n", "CLAUDE.md": "@AGENTS.md\n"}`),
			"skills":     json.RawMessage(`["relocatable"]`),
			"install":    json.RawMessage(`{"skills":["relocatable"]}`),
			"skills_dir": json.RawMessage(`"$CAIRN_PROFILE_ROOT/skills"`),
			"trees":      json.RawMessage(`{"bundled": "$CAIRN_PROFILE_ROOT/templates"}`),
			"access":     json.RawMessage(`{"directories": ["$CAIRN_PROFILE_ROOT"]}`),
		},
	}); err != nil {
		t.Fatalf("put the profile: %v", err)
	}
}

// bootBundled boots the fixture profile and returns the directory it planted,
// failing the test on anything reported along the way. A slot that did not
// resolve is a failure here rather than a warning: every one of them names a
// path inside the bundle, which is the whole subject.
func bootBundled(t *testing.T, ctx context.Context, dbPath, scopeDir, bootRoot string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	full := append([]string{
		"boot", "bundled", "--db", dbPath, "--scope", scopeDir,
		"--boot-root", bootRoot, "--session", "s1",
	}, args...)
	if err := run(ctx, full, &stdout, &stderr); err != nil {
		t.Fatalf("boot: %v\nstderr: %s", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Fatalf("the boot reported something the bundle should have answered:\n%s", stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

// showBundled shows the fixture profile and returns the document.
func showBundled(t *testing.T, ctx context.Context, dbPath string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	full := append([]string{"show", "bundled", "--db", dbPath}, args...)
	if err := run(ctx, full, &stdout, &stderr); err != nil {
		t.Fatalf("show: %v\nstderr: %s", err, stderr.String())
	}
	return stdout.String()
}
