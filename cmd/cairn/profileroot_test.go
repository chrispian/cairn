package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAProfileBundleRelocatesWithoutEdits is the mechanism --profile exists
// for, asserted the only way it can be: the same profile, rendered twice
// against two bundles, produces each bundle's own content.
//
// One boot proves nothing. A profile naming "$CAIRN_PROFILE_ROOT/templates/..."
// that renders the right file once could be reading a path that happens to be
// right for other reasons — the workdir, the home directory, an ambient
// variable. Two bundles at two paths, with nothing between the runs but the
// flag, is what says the bundle root decided it.
//
// The profile now travels inside the bundle rather than in a database beside
// it, so "the same profile" is two identical files. That is the catalog being
// the store, and it makes the relocation whole: what moved is everything.
//
// All three of the places a manifest names somewhere to read from are in play,
// because they resolve through three different calls at the composition root
// and a thread that reached two of them would look exactly like this from one:
// a template's source, a slot's source, and a files entry's source.
func TestAProfileBundleRelocatesWithoutEdits(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)

	for _, label := range []string{"A", "B"} {
		t.Run("bundle "+label, func(t *testing.T) {
			bundle := filepath.Join(home, "bundle-"+label)
			writeBundle(t, bundle, label)

			dir := bootBundled(t, ctx, scopeDir,
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
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)

	writeBundle(t, filepath.Join(home, "bundle"), "the cwd")
	t.Chdir(home)

	dir := bootBundled(t, ctx, scopeDir, filepath.Join(home, "boot"), "--profile", "bundle")
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
	if got := showBundled(t, ctx, "--profile", "bundle"); !strings.Contains(got, "profile root  "+absolute+"\n") {
		t.Errorf("the reported bundle root is not the absolute %s:\n%s", absolute, got)
	}
}

// TestTheProfileFlagWinsOverTheVariable pins the precedence, in both
// directions.
//
// It is the precedence every other path cairn resolves already has —
// CAIRN_BOOT_ROOT loses to its flag, and CAIRN_DB did before the store went —
// and the second half matters as much as the first: a command run without the
// flag must read the variable, because a bundle exported into a shell is the
// form that costs an operator nothing to use.
//
// Since the catalog became the store the variable decides more than it did. It
// no longer only says what a path expands to; it says which bundle the profile
// itself is read out of. Both halves are asserted by the same line, because
// under this fixture only the bundle that was read can render its own label.
func TestTheProfileFlagWinsOverTheVariable(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)

	flagged := filepath.Join(home, "bundle-flag")
	exported := filepath.Join(home, "bundle-env")
	writeBundle(t, flagged, "the flag")
	writeBundle(t, exported, "the variable")
	t.Setenv(envProfileRoot, exported)

	dir := bootBundled(t, ctx, scopeDir, filepath.Join(home, "boot-flag"), "--profile", flagged)
	if got := read(t, dir, "AGENTS.md"); !strings.Contains(got, "# agents from the flag") {
		t.Errorf("--profile did not win over %s:\n%s", envProfileRoot, got)
	}

	dir = bootBundled(t, ctx, scopeDir, filepath.Join(home, "boot-env"))
	if got := read(t, dir, "AGENTS.md"); !strings.Contains(got, "# agents from the variable") {
		t.Errorf("without the flag, %s was not read from the environment:\n%s", envProfileRoot, got)
	}
}

// TestTheBundleFallsBackToTheConfigDirectory covers the last link of the chain,
// which is the one an operator who has exported nothing lands on.
//
// $XDG_CONFIG_HOME/agents, then ~/.config/agents — the same two the database
// path resolved through, minus the file name. The directory is named "agents"
// rather than "cairn" because several tools read the same profiles.
func TestTheBundleFallsBackToTheConfigDirectory(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)

	config := filepath.Join(home, "config")
	writeBundle(t, filepath.Join(config, "agents"), "the config directory")
	t.Setenv(envXDGConfigHome, config)

	dir := bootBundled(t, ctx, scopeDir, filepath.Join(home, "boot"))
	if got := read(t, dir, "AGENTS.md"); !strings.Contains(got, "# agents from the config directory") {
		t.Errorf("with no flag and no variable the config directory was not read:\n%s", got)
	}
}

// TestTheProfileFlagIsRefusedWhenItNamesNoBundle covers the one thing --profile
// validates, on all four commands.
//
// It is not a rule about where a bundle may live: any directory holding
// profiles is accepted. What it buys is where the failure lands. A root that is
// not there makes every value expanding it wrong at once — and, since the
// catalog became the store, makes the profile unfindable as well. Without this
// the operator reads that as "no binding and no profile named x", which sends
// them after a target that was never the problem.
func TestTheProfileFlagIsRefusedWhenItNamesNoBundle(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()

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
				{"list"},
			} {
				var stdout, stderr bytes.Buffer
				full := append(append([]string{}, args...), "--profile", named.path)
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

// TestABundleWithNoProfilesIsNamedRatherThanEmpty covers the failure the store
// used to hide, and it is the diagnostic T07c asked for.
//
// A directory that exists and holds no profiles is not an empty catalog. It is
// cairn pointed at something that is not a bundle — the parent of one, a
// checkout that has moved, a variable exported for something else — and the
// error has to name the bundle and its path. Saying "no binding and no profile
// named x" instead sends the operator after the target, which was never the
// problem.
func TestABundleWithNoProfilesIsNamedRatherThanEmpty(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	empty := filepath.Join(home, "not-a-bundle")
	mustMkdir(t, empty)

	var stdout, stderr bytes.Buffer
	err := run(ctx, []string{"show", "bundled", "--profile", empty}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("show against a directory with no profiles reported success")
	}
	for _, want := range []string{empty, "profiles"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "no binding and no profile") {
		t.Errorf("the refusal blames the target instead of the bundle: %v", err)
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

	bundle := filepath.Join(home, "bundle")
	writeBundle(t, bundle, "the installed layer")

	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run(ctx, []string{
		"install", "bundled", "--root", root, "--profile", bundle,
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
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)

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
			"boot", "bundled-paths", "--scope", scopeDir,
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
			"install", "bundled-paths", "--root", root, "--profile", bundle,
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

// TestShowReportsTheBundleItRead covers the field that says which catalog the
// document above it came out of.
//
// The field was only ever reported and never used, back when the profiles were
// in a database and the bundle was somewhere paths expanded against. It is both
// now, which makes it the one line that says whether the operator is reading
// the profile they think they are.
//
// The variable's value is reported exactly as it is held. Only the flag is
// absolutized, because only the flag is cairn's to resolve, and the redundant
// "/./" below is what tells a pass-through from a tidy-up — the two are
// indistinguishable on a path that was already canonical.
func TestShowReportsTheBundleItRead(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()

	bundle := filepath.Join(home, "bundle")
	writeBundle(t, bundle, "shown")

	if got := showBundled(t, ctx, "--profile", bundle); !strings.Contains(got, "profile root  "+bundle+"\n") {
		t.Errorf("--profile is not reported:\n%s", got)
	}

	// Built by hand rather than with filepath.Join, which would clean the
	// "/./" back out before cairn ever saw it.
	asHeld := home + string(filepath.Separator) + "." + string(filepath.Separator) + "bundle"
	t.Setenv(envProfileRoot, asHeld)
	if got := showBundled(t, ctx); !strings.Contains(got, "profile root  "+asHeld+"\n") {
		t.Errorf("the variable is not reported as it is held (%s):\n%s", asHeld, got)
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

// TestBootReportsTheBundleItComposedFrom covers the key a launcher needs and
// could not read, and it asserts the two things that make it worth having.
//
// The first is that the value is there at all. A boot directory records nothing
// about the bundle it came from, and [bundleRoot] resolves without one — with
// no flag and no variable it falls to $XDG_CONFIG_HOME/agents and then to
// ~/.config/agents. So a harness launched with --profile that re-runs cairn
// from inside its own boot directory reads a DIFFERENT bundle, silently, and
// correctly by every rule as written; the variable is the launcher's to export,
// and it had no value to export it from.
//
// The second is that `boot` and `show` answer with one string. They are two
// documents describing one directory, a launcher may well read both in one
// session, and a difference between them would be a difference nothing else
// would ever report. Both halves of [resolveProfileRoot]'s asymmetry are put
// through it — the flag, which cairn absolutizes, and the variable, which it
// passes through as it was exported — because a report that tidied either one
// would agree with the other command on a path that was already canonical and
// disagree on the one that was not. The redundant "/./" is what tells a
// pass-through from a tidy-up.
func TestBootReportsTheBundleItComposedFrom(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	bundle := filepath.Join(home, "bundle")
	writeBundle(t, bundle, "one")

	if got := bootBundledJSON(t, ctx, scopeDir, filepath.Join(home, "boot-flag"),
		"--profile", bundle).ProfileRoot; got != bundle {
		t.Errorf("profile_root = %q, want the absolutized --profile %q", got, bundle)
	}

	// Built by hand rather than with filepath.Join, which would clean the
	// "/./" back out before cairn ever saw it.
	asHeld := home + string(filepath.Separator) + "." + string(filepath.Separator) + "bundle"
	t.Setenv(envProfileRoot, asHeld)
	booted := bootBundledJSON(t, ctx, scopeDir, filepath.Join(home, "boot-env"))
	if booted.ProfileRoot != asHeld {
		t.Errorf("profile_root = %q, want the variable as it is held (%s)", booted.ProfileRoot, asHeld)
	}
	if shown := showBundledJSON(t, ctx).ProfileRoot; shown != booted.ProfileRoot {
		t.Errorf("show reports profile_root %q where boot reports %q — one directory, two answers",
			shown, booted.ProfileRoot)
	}
}

// bootBundledJSON boots the fixture profile with --json and returns the report.
func bootBundledJSON(t *testing.T, ctx context.Context, scopeDir, bootRoot string, args ...string) bootReport {
	t.Helper()
	var report bootReport
	out := bootBundled(t, ctx, scopeDir, bootRoot, append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("boot --json is not one object: %v\n%s", err, out)
	}
	return report
}

// showBundledJSON shows the fixture profile with --json and returns the report.
func showBundledJSON(t *testing.T, ctx context.Context, args ...string) showReport {
	t.Helper()
	var report showReport
	out := showBundled(t, ctx, append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("show --json is not one object: %v\n%s", err, out)
	}
	return report
}

// writeBundle lays down a whole profile bundle at root and labels every file in
// it, so that two bundles are told apart by what they render rather than by
// where they are.
//
// The profiles are in it, which is the change the catalog made: a bundle is not
// a directory of content that a database points into any more, it is the store.
// Every value that names somewhere to read from is written against
// $CAIRN_PROFILE_ROOT, and none of them is ever edited again.
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

	// The profile these tests render: a template source, a slot source and a
	// files source, which are the three calls at the composition root that
	// resolve a manifest value.
	writeProfile(t, root, bundleProfile{
		ID:       "bundled",
		Name:     "Bundled",
		Provider: "claude",
		Model:    "opus",
		Spec: map[string]string{
			"templates": `{
				"AGENTS.md": {"kind":"static_file","static_file":{"path":"$CAIRN_PROFILE_ROOT/templates/agents.md"}},
				"CLAUDE.md": "@AGENTS.md\n"
			}`,
			"slots": `[
				{"name":"note","source":{"kind":"static_file","static_file":{"path":"$CAIRN_PROFILE_ROOT/templates/note.md"}}}
			]`,
			"files": `{
				"docs/handbook.md": {"kind":"static_file","static_file":{"path":"$CAIRN_PROFILE_ROOT/skills/handbook.md"}}
			}`,
		},
	})

	// The three manifest keys that take a path rather than a source, kept in a
	// second profile so the relocation test renders only what it asserts on.
	// These reach the lookup through the rendered instance instead of through a
	// resolver, which is a different thread and the one with the most at stake.
	writeProfile(t, root, bundleProfile{
		ID:       "bundled-paths",
		Name:     "Bundled paths",
		Provider: "claude",
		Model:    "opus",
		Spec: map[string]string{
			"templates":  `{"AGENTS.md": "# paths\n", "CLAUDE.md": "@AGENTS.md\n"}`,
			"skills":     `["relocatable"]`,
			"install":    `{"skills":["relocatable"]}`,
			"skills_dir": `"$CAIRN_PROFILE_ROOT/skills"`,
			"trees":      `{"bundled": "$CAIRN_PROFILE_ROOT/templates"}`,
			"access":     `{"directories": ["$CAIRN_PROFILE_ROOT"]}`,
		},
	})
}

// bootBundled boots the fixture profile and returns the directory it planted,
// failing the test on anything reported along the way. A slot that did not
// resolve is a failure here rather than a warning: every one of them names a
// path inside the bundle, which is the whole subject.
func bootBundled(t *testing.T, ctx context.Context, scopeDir, bootRoot string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	full := append([]string{
		"boot", "bundled", "--scope", scopeDir,
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
func showBundled(t *testing.T, ctx context.Context, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	full := append([]string{"show", "bundled"}, args...)
	if err := run(ctx, full, &stdout, &stderr); err != nil {
		t.Fatalf("show: %v\nstderr: %s", err, stderr.String())
	}
	return stdout.String()
}
