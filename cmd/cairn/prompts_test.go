package main

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The two prompts the example bundle ships. engineer declares the first; the
// second exists only to be added by --prompt, which is what makes the flag's
// contribution visible as a file rather than as a manifest key.
const (
	handoffCommand = ".claude/commands/boot/handoff.md"
	resetCommand   = ".claude/commands/boot/reset-scope.md"
)

// TestAPromptIsPlantedAsACommandTheOperatorCanType is the feature end to end,
// against the bundle cairn actually ships rather than a fixture built for it.
//
// The three claims are the three halves of the design, and each is asserted
// against something whose absence would show:
//
//   - The destination. Both prompts land under .claude/commands/boot/, which
//     is what makes them /boot:handoff and /boot:reset-scope. A file one
//     directory up is a different command, so the flattened path is named.
//   - The substitution. The scope is a temporary directory this test made, so
//     its appearance in the planted file cannot come from the bundle, and the
//     marker that stood for it must be gone.
//   - The selection. engineer declares one prompt and the bundle holds two, so
//     a boot that planted the directory rather than the declaration would
//     plant both without the flag — which is the first subtest.
func TestAPromptIsPlantedAsACommandTheOperatorCanType(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)

	declared := bootTree(t, ctx, filepath.Join(home, "declared"),
		"engineer", "--profile", bundle, "--scope", scopeDir)

	t.Run("a profile carries the prompts it declares and no others", func(t *testing.T) {
		if _, ok := declared[handoffCommand]; !ok {
			t.Errorf("the declared prompt was not planted; the tree holds %v", sortedPaths(declared))
		}
		if _, ok := declared[resetCommand]; ok {
			t.Error("a prompt the profile did not declare was planted, so spec.prompts selects nothing")
		}
		// The bundle's own directory is not the boot directory's. A render
		// that copied prompts/ through would put the README beside them.
		if _, ok := declared[".claude/commands/boot/README.md"]; ok {
			t.Error("the prompts directory's README was planted as a command")
		}
	})

	t.Run("it is planted inside the boot namespace", func(t *testing.T) {
		for _, wrong := range []string{".claude/commands/handoff.md", "handoff.md", ".claude/skills/handoff.md"} {
			if _, ok := declared[wrong]; ok {
				t.Errorf("a prompt was planted at %s, where /boot:handoff cannot reach it", wrong)
			}
		}
	})

	t.Run("it is substituted with what only this instance knows", func(t *testing.T) {
		got := declared[handoffCommand]
		if !strings.Contains(got, scopeDir) {
			t.Errorf("the planted prompt does not carry this boot's scope %s:\n%s", scopeDir, got)
		}
		if strings.Contains(got, "cairn:value") || strings.Contains(got, "cairn:slot") {
			t.Errorf("the planted prompt still holds a marker, which an agent reads as prose:\n%s", got)
		}
		// The source is unchanged, which is what "substituted" means as
		// against "rewritten in place".
		source := read(t, bundle, "prompts/handoff.md")
		if !strings.Contains(source, "<!-- cairn:value scope -->") {
			t.Errorf("the bundle's own prompt was modified by the boot:\n%s", source)
		}
		if strings.Contains(source, scopeDir) {
			t.Errorf("the boot wrote its scope back into the bundle:\n%s", source)
		}
	})

	t.Run("--prompt adds one for a single launch", func(t *testing.T) {
		added := bootTree(t, ctx, filepath.Join(home, "added"),
			"engineer", "--profile", bundle, "--scope", scopeDir, "--prompt", "reset-scope")
		if got, want := changedFiles(t, declared, added), []string{resetCommand}; !slices.Equal(got, want) {
			t.Fatalf("--prompt changed %v, want only %v", got, want)
		}
		if !strings.Contains(added[resetCommand], scopeDir) {
			t.Errorf("the added prompt was not substituted:\n%s", added[resetCommand])
		}
	})
}

// TestPromptsSurviveSaveAsAndReplay is the round trip: a --prompt typed once
// is a binding that plants the same command by name.
//
// The assertion is on the two trees rather than on the binding file alone.
// A binding that recorded the key and a replay that ignored it would leave the
// file looking right and the boot directory missing a command, which is the
// half an operator would find by typing /boot:reset-scope and being told it
// does not exist.
func TestPromptsSurviveSaveAsAndReplay(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)

	typed := bootTree(t, ctx, filepath.Join(home, "typed"),
		"engineer",
		"--profile", bundle,
		"--prompt", "reset-scope",
		"--scope", scopeDir,
		"--save-as", "handy",
	)
	saved := read(t, bundle, "bindings/handy.yaml")
	if !strings.Contains(saved, "prompts:\n  - reset-scope\n") {
		t.Fatalf("the binding does not carry the prompt that was typed:\n%s", saved)
	}

	replayed := bootTree(t, ctx, filepath.Join(home, "replayed"), "handy", "--profile", bundle)
	// Every file, not only the commands: a replay that planted the prompt and
	// dropped something else is not a round trip either.
	diffTrees(t, typed, replayed)
	if _, ok := replayed[resetCommand]; !ok {
		t.Errorf("the replayed boot did not plant the binding's prompt; it holds %v", sortedPaths(replayed))
	}
}

// TestComposePrompt covers the flag itself, in the shape --skill is covered:
// the two spellings compose, the contribution is additive and by id, and a
// value naming nothing is refused rather than ignored.
func TestComposePrompt(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)

	t.Run("the comma-separated and repeated forms compose", func(t *testing.T) {
		lists := mustShow(t, ctx, bundle, "engineer", "--prompt", "reset-scope,handoff")
		repeats := mustShow(t, ctx, bundle, "engineer", "--prompt", "reset-scope", "--prompt", "handoff")
		if a, b := promptsOf(t, lists), promptsOf(t, repeats); !slicesEqual(a, b) {
			t.Errorf("the two spellings resolve differently: %v and %v", a, b)
		}
	})

	t.Run("it is additive, by id, and the profile's own prompts stand", func(t *testing.T) {
		// handoff is engineer's own and is also passed: one member, because
		// the collection is keyed by the id.
		out := mustShow(t, ctx, bundle, "engineer", "--prompt", "handoff,reset-scope")
		if got := promptsOf(t, out); !slicesEqual(got, []string{"handoff", "reset-scope"}) {
			t.Errorf("the composed prompts are %v, want the union keyed by id", got)
		}
	})

	t.Run("the flag is named as a contributor", func(t *testing.T) {
		out := mustShow(t, ctx, bundle, "engineer", "--prompt", "reset-scope")
		if got, want := declarersOfOrFail(t, out, "prompts"), "engineer, --prompt"; got != want {
			t.Errorf("spec.prompts is declared by %q, want %q", got, want)
		}
	})

	t.Run("a value naming nothing is refused", func(t *testing.T) {
		var l idList
		if err := l.Set(" , "); err == nil {
			t.Error("a --prompt naming nothing was accepted")
		}
	})

	t.Run("the additive-only rule is in the help", func(t *testing.T) {
		for _, text := range []string{promptFlagUsage, usage} {
			if !strings.Contains(text, "Additive only") {
				t.Errorf("the help does not say the flag is additive only:\n%s", text)
			}
		}
	})
}

// TestInstallSaysItRendersNoPrompts pins the deliberate omission out loud.
//
// install renders the machine-wide layer, and a prompt is a per-launch choice,
// so the installed layer carries none — the flag is not registered and the
// binding's own prompts are not replayed. Saying so is what keeps an operator
// comparing `cairn boot <binding>` with `cairn install <binding>` from reading
// a decision as a bug.
func TestInstallSaysItRendersNoPrompts(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	writeFile(t, filepath.Join(bundle, "bindings", "handy.yaml"),
		"profile: engineer\nprompts:\n  - reset-scope\n", 0o644)

	root := filepath.Join(home, "root")
	mustMkdir(t, root)

	var stdout, stderr bytes.Buffer
	err := runInstall(ctx, []string{"handy", "--profile", bundle, "--root", root}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("install: %v\nstderr: %s", err, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "prompt(s)") {
		t.Errorf("install did not say it renders none of the binding's prompts:\n%s", got)
	}

	// That --prompt is not registered on install is asserted by
	// TestInstallDoesNotCompose, which is the table for that claim and passes
	// --root. It is deliberately NOT re-asserted here: runInstall with no
	// --root falls through to the operator's home directory, so a second copy
	// of the claim would overwrite the developer's live ~/.claude on the day
	// the claim stopped being true — which is the day the test exists for.
}

// promptsOf reads the composed prompt ids out of a show document.
func promptsOf(t *testing.T, out string) []string {
	t.Helper()
	raw, ok := valueOf(t, out, "prompts").([]any)
	if !ok {
		t.Fatalf("spec.prompts is not a list:\n%s", out)
	}
	ids := make([]string, 0, len(raw))
	for _, v := range raw {
		ids = append(ids, v.(string))
	}
	return ids
}

// sortedPaths names a planted tree's files, for a failure that has to say what
// was there instead.
func sortedPaths(tree map[string]string) []string {
	out := make([]string, 0, len(tree))
	for rel := range tree {
		out = append(out, rel)
	}
	slices.Sort(out)
	return out
}
