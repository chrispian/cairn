package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The tests below run against examples/bundle rather than a fixture written
// for them.
//
// A minimal repro proves a mechanism and says nothing about reach. The example
// bundle is the one composition in the repository somebody actually reads —
// engineer with its slots, subagents, trees and files, and docs-only as the
// part — so a round trip through it is a round trip through everything a boot
// directory has in it, and a claim about it is a claim about the shipped
// thing.

// TestSaveAsRoundTripsAComposition is the acceptance criterion, and it is
// deliberately not an assertion about the file's contents.
//
// The claim --save-as makes is that the same boot is reachable by name
// afterwards. A test that read the binding back and checked its keys would
// prove that cairn can write down what it was told, which was never in doubt;
// what is in doubt is whether booting that name replays it. So this saves a
// composition, boots the binding, and diffs the two trees.
//
// Two boot roots and one session segment, so nothing outside cairn's rendering
// can differ between them.
//
// **The binding is saved under the profile's own name, and that is load
// bearing.** A boot directory is planted under the name it was booted by, and
// that name is also the `binding` instance value a template may substitute —
// so saving as "eng-docs" and booting that would legitimately differ from
// booting "engineer", in every artifact carrying the marker. The example
// bundle's templates happen not to carry it today, which would make an
// equality assertion pass by fixture accident and fail later for a reason
// having nothing to do with --save-as. Holding the name fixed removes the
// class instead of tolerating it, and costs nothing: a binding outranks a
// profile of the same name, so `cairn boot engineer` afterwards is the
// binding.
func TestSaveAsRoundTripsAComposition(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)

	// docs-only is the bundle's own part and contributes nothing a boot
	// directory can show — an empty slot that renders nothing without a --set,
	// and prose no template engineer names. So a tree comparison built on it
	// alone would pass whether or not parts were composed, on either side.
	// This part exists to make the parts half observable in a tree, which is
	// the only thing the comparison below can see.
	writeProbePart(t, bundle, "round-trip-part", "round-trip-skill")

	typed := bootTree(t, ctx, filepath.Join(home, "typed"),
		"engineer",
		"--profile", bundle,
		"--with", "docs-only",
		"--with", "round-trip-part",
		"--skill", "qhealth,adr",
		"--scope", scopeDir,
		"--save-as", "engineer",
	)
	replayed := bootTree(t, ctx, filepath.Join(home, "replayed"),
		"engineer",
		"--profile", bundle,
	)

	if len(typed) == 0 {
		t.Fatal("the composed boot wrote nothing, so the comparison below would prove nothing")
	}
	diffTrees(t, typed, replayed)

	// Equality is only worth something if both halves of the composition were
	// observable to begin with, and each of these ids is planted by exactly
	// one of them: two by --skill, and the third only because the part the
	// binding names declares it.
	for _, id := range []string{"qhealth", "adr", "round-trip-skill"} {
		if _, ok := replayed[".claude/skills/"+id+"/SKILL.md"]; !ok {
			t.Errorf("the replayed boot did not plant the skill %q; it planted %v",
				id, slices.Sorted(maps.Keys(replayed)))
		}
	}
}

// TestABindingIsNotWrittenWhenTheBootFails pins the ordering, which is
// documented and was otherwise untested.
//
// A --save-as is checked before the boot and written after it. The write waits
// because a binding worth reusing is one that booted: saving first leaves a
// file behind for a composition that could not render, under a name the
// operator will later type expecting it to work.
func TestABindingIsNotWrittenWhenTheBootFails(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	bootRoot := filepath.Join(home, "runtime")

	// A skill id the bundle has no directory for. It passes every --save-as
	// check — it is an id, not a path, and it is exactly the kind of thing a
	// binding may hold — and then fails the render, which is the only shape
	// that tells these two orderings apart.
	err := run(ctx, []string{
		"boot", "engineer",
		"--profile", bundle,
		"--skill", "not-in-this-bundle",
		"--scope", scopeDir,
		"--boot-root", bootRoot,
		"--session", "s",
		"--save-as", "leftover",
	}, discard(), discard())
	if err == nil {
		t.Fatal("a boot naming a skill the bundle does not hold succeeded")
	}
	nothingUnder(t, filepath.Join(bundle, "bindings", "leftover.yaml"))
	if got := bindingFiles(t, bundle); slices.Contains(got, "leftover.yaml") {
		t.Errorf("a binding was left behind for a boot that failed: %v", got)
	}
}

// TestSaveAsUnderANewNameDiffersOnlyByThatName is the other half of the test
// above: it says what a save under a different name legitimately changes, so
// that the equality asserted there is understood rather than assumed.
//
// The boot directory is planted under the name it was booted by and that name
// is the `binding` value, so the two boots differ wherever a template
// substitutes it — and nowhere else. The example bundle's templates carry no
// such marker, so one is added here rather than waited for.
func TestSaveAsUnderANewNameDiffersOnlyByThatName(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	tmpl := filepath.Join(bundle, "templates", "engineer.md")
	writeFile(t, tmpl, read(t, bundle, "templates/engineer.md")+
		"\nBooted as <!-- cairn:value binding -->.\n", 0o644)

	typed := bootTree(t, ctx, filepath.Join(home, "typed"),
		"engineer", "--profile", bundle, "--with", "docs-only",
		"--scope", scopeDir, "--save-as", "eng-docs")
	replayed := bootTree(t, ctx, filepath.Join(home, "replayed"), "eng-docs", "--profile", bundle)

	if got := changedFiles(t, typed, replayed); !slices.Equal(got, []string{"AGENTS.md"}) {
		t.Fatalf("the two boots differ in %v, want only the file carrying the binding marker", got)
	}
	if !strings.Contains(typed["AGENTS.md"], "Booted as engineer.") {
		t.Errorf("the composed boot does not name itself:\n%s", typed["AGENTS.md"])
	}
	if !strings.Contains(replayed["AGENTS.md"], "Booted as eng-docs.") {
		t.Errorf("the replayed boot does not name itself:\n%s", replayed["AGENTS.md"])
	}
	// And that one substitution is the whole of the difference.
	if strings.ReplaceAll(typed["AGENTS.md"], "as engineer.", "as eng-docs.") != replayed["AGENTS.md"] {
		t.Errorf("the two renderings differ by more than the binding's name:\n%s\n---\n%s",
			typed["AGENTS.md"], replayed["AGENTS.md"])
	}
}

// TestARelativeScopeIsSavedAsTheDirectoryItResolvedTo is the one value a
// binding cannot record as written.
//
// Everything else in a composition still means the same thing tomorrow. A
// relative scope is anchored to the working directory of the process that
// typed it, a binding records no working directory, and booting the same
// binding from somewhere else would silently resolve somewhere else — which is
// the failure this whole design refuses elsewhere by refusing a path member.
// Here there is a sound answer, because the boot already resolved it.
func TestARelativeScopeIsSavedAsTheDirectoryItResolvedTo(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)

	// A relative --scope has to be relative to the test process's own working
	// directory, which is the package directory; a directory of its own under
	// it, removed afterwards, is the only way to write one honestly.
	rel := filepath.Join("testdata", "relative-scope-probe")
	mustMkdir(t, rel)
	t.Cleanup(func() { _ = os.RemoveAll(rel) })
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("absolute path of %s: %v", rel, err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatalf("resolve %s: %v", rel, err)
	}

	var stderr bytes.Buffer
	bootTreeErr(t, ctx, &stderr, filepath.Join(home, "runtime"),
		"engineer", "--profile", bundle, "--scope", rel, "--save-as", "here")

	if got, want := read(t, bundle, "bindings/here.yaml"),
		fmt.Sprintf("profile: engineer\nscope: %s\n", abs); got != want {
		t.Errorf("the saved binding is\n%q\nwant\n%q", got, want)
	}
	// And it is not silent, because the value the operator typed is not the
	// value that was written.
	if !strings.Contains(stderr.String(), "is relative, so "+abs+" was saved instead") {
		t.Errorf("the substitution was not reported:\n%s", stderr.String())
	}

	// An absolute scope, by contrast, names the same place from anywhere, so
	// it is saved exactly as typed and nothing is said. Spelled with a "/."
	// the boot resolves away, so a pass proves the value passed through rather
	// than that the two happened to agree.
	var quiet bytes.Buffer
	bootTreeErr(t, ctx, &quiet, filepath.Join(home, "runtime2"),
		"engineer", "--profile", bundle, "--scope", scopeDir+"/.", "--save-as", "absolute")
	if got, want := read(t, bundle, "bindings/absolute.yaml"),
		fmt.Sprintf("profile: engineer\nscope: %s\n", scopeDir+"/."); got != want {
		t.Errorf("the saved binding is\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(quiet.String(), "was saved instead") {
		t.Errorf("an absolute scope was reported as substituted:\n%s", quiet.String())
	}
}

// TestSaveAsSaysWhenItShadowsAProfile.
//
// A binding outranks a profile of the same name at every lookup, which is
// deliberate. Creating that shadow is not wrong; doing it without a word is,
// because from then on `cairn boot <name>` means something else and nothing in
// the output would have hinted at it — including for an abstract profile,
// which refuses to boot until a binding of its name quietly makes it succeed.
func TestSaveAsSaysWhenItShadowsAProfile(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)

	var stderr bytes.Buffer
	bootTreeErr(t, ctx, &stderr, filepath.Join(home, "runtime"),
		"engineer", "--profile", bundle, "--scope", scopeDir, "--save-as", "reviewer")
	if !strings.Contains(stderr.String(), `a profile is also named reviewer`) {
		t.Errorf("the shadow was not reported:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "`cairn boot reviewer` now boots this binding") {
		t.Errorf("the report does not say what changed:\n%s", stderr.String())
	}

	// A name the bundle has no profile for says nothing, because nothing
	// changed meaning. A diagnostic that fires on the ordinary case is noise.
	var quiet bytes.Buffer
	bootTreeErr(t, ctx, &quiet, filepath.Join(home, "runtime2"),
		"engineer", "--profile", bundle, "--scope", scopeDir, "--save-as", "not-a-profile")
	if strings.Contains(quiet.String(), "a profile is also named") {
		t.Errorf("a save that shadows nothing reported a shadow:\n%s", quiet.String())
	}
}

// TestSaveAsIntoABundleWithNoBindingsDirectory. A bundle with profiles and no
// bindings is a legitimate bundle — the catalog reads one without complaint —
// so the first --save-as in such a bundle is the thing that makes the
// directory.
func TestSaveAsIntoABundleWithNoBindingsDirectory(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	if err := os.RemoveAll(filepath.Join(bundle, "bindings")); err != nil {
		t.Fatalf("remove the bindings directory: %v", err)
	}

	bootTree(t, ctx, filepath.Join(home, "runtime"),
		"engineer", "--profile", bundle, "--scope", scopeDir, "--save-as", "first")
	if got, want := read(t, bundle, "bindings/first.yaml"),
		fmt.Sprintf("profile: engineer\nscope: %s\n", scopeDir); got != want {
		t.Errorf("the saved binding is\n%q\nwant\n%q", got, want)
	}
	// And it boots, which is what says the directory is the one the catalog
	// reads rather than one beside it.
	bootTree(t, ctx, filepath.Join(home, "runtime2"), "first", "--profile", bundle)
}

// TestInstallRendersNoneOfABindingsComposition pins the decision, in both
// directions.
//
// install renders the machine-wide layer every session loads, so a per-launch
// composition has no meaning there — the same reason it takes none of --with,
// --skill or --set. Nothing else asserts this, and an install that quietly
// started replaying a binding's parts would render a different ~/.claude with
// no test to notice.
func TestInstallRendersNoneOfABindingsComposition(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)

	install := func(root, target string, stderr io.Writer) map[string]string {
		t.Helper()
		mustMkdir(t, root)
		if err := run(ctx, []string{"install", target, "--profile", bundle, "--root", root},
			discard(), stderr); err != nil {
			t.Fatalf("install %s: %v", target, err)
		}
		return treeOf(t, root)
	}

	var stderr bytes.Buffer
	// "docs" is the example bundle's binding that composes a part.
	composed := install(filepath.Join(home, "composed"), "docs", &stderr)
	plain := install(filepath.Join(home, "plain"), "engineer", discard())
	if len(plain) == 0 {
		t.Fatal("install wrote nothing, so the comparison below would prove nothing")
	}
	if !maps.Equal(composed, plain) {
		t.Errorf("installing a binding that composes a part differs from installing its profile:\n%v",
			changedFiles(t, composed, plain))
	}
	// Said out loud, because `show` and `boot` both report a composition this
	// command renders nothing of.
	if !strings.Contains(stderr.String(), "install renders none of them") {
		t.Errorf("install did not say the binding's composition was not replayed:\n%s", stderr.String())
	}
}

// exampleBundle copies examples/bundle

// TestSaveAsRoundTripsSkillsAndDropsSets covers the two behaviours that look
// alike at the flag level and are not alike.
//
// Both flags name something the profile did not declare, and only one of them
// survives into the binding. --skill carries ids, which is what a binding
// already holds for its parts; --set carries content, which is the thing the
// catalog is kept clean of. So the skills round-trip and the --set does not,
// and the difference shows up as exactly one section of one file.
func TestSaveAsRoundTripsSkillsAndDropsSets(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)

	// docs-only declares a "direction" slot with no content, which is what
	// --set fills. The value is distinctive so that finding it in a rendered
	// file is not a coincidence.
	const direction = "Only the release notes, and nothing about the API."

	var stderr bytes.Buffer
	typed := bootTreeErr(t, ctx, &stderr, filepath.Join(home, "typed"),
		"engineer",
		"--profile", bundle,
		"--with", "docs-only",
		"--skill", "qhealth,adr",
		"--set", "direction="+direction,
		"--scope", scopeDir,
		"--save-as", "eng-docs",
	)
	replayed := bootTree(t, ctx, filepath.Join(home, "replayed"), "eng-docs", "--profile", bundle)

	// The dropped value is named, and named as a --set rather than as a
	// mystery. A silent drop is the failure this line exists to prevent.
	if got := stderr.String(); !strings.Contains(got, "--set direction was not saved") {
		t.Errorf("the dropped --set was not named on stderr:\n%s", got)
	}
	if !strings.Contains(stderr.String(), "This boot still has it.") {
		t.Errorf("stderr does not say the run still gets the value:\n%s", stderr.String())
	}

	// The skills reached the binding. Both of them, and by id.
	saved := read(t, bundle, "bindings/eng-docs.yaml")
	for _, id := range []string{"qhealth", "adr"} {
		if !strings.Contains(saved, "\n  - "+id+"\n") {
			t.Errorf("the binding does not carry the skill %q:\n%s", id, saved)
		}
	}
	if strings.Contains(saved, direction) {
		t.Errorf("the --set value was written into the catalog:\n%s", saved)
	}

	// And the difference between the two trees is that value and nothing
	// else — which is what "the run still gets it" and "the binding does not"
	// mean together, stated as one fact rather than two.
	changed := changedFiles(t, typed, replayed)
	if !slices.Equal(changed, []string{"AGENTS.md"}) {
		t.Fatalf("the two boots differ in %v, want only the file the --set filled", changed)
	}
	if !strings.Contains(typed["AGENTS.md"], direction) {
		t.Errorf("the --set did not reach the boot it was typed at:\n%s", typed["AGENTS.md"])
	}
	if strings.Contains(replayed["AGENTS.md"], direction) {
		t.Errorf("the --set survived into the saved binding's boot:\n%s", replayed["AGENTS.md"])
	}
	// Removing the one line accounts for the whole difference. Without this
	// the test would pass on a boot that differed in that line AND somewhere
	// else in the same file.
	if without(typed["AGENTS.md"], direction) != replayed["AGENTS.md"] {
		t.Errorf("the two renderings of AGENTS.md differ by more than the --set value:\n%s\n---\n%s",
			typed["AGENTS.md"], replayed["AGENTS.md"])
	}
}

// TestSaveAsRefusesAPathMember is the contrast that carries the design.
//
// The surface reading is that --set and a path member are both non-persistable
// and should both be dropped. They are not alike. A --set can be dropped
// soundly because the run still receives it and nothing is lost but reuse; a
// path member cannot, because dropping it changes what the binding composes
// and nobody is told. Inlining its content instead was rejected: that turns a
// handle into content. What is left is to refuse, and to name it.
func TestSaveAsRefusesAPathMember(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	mustMkdir(t, filepath.Join(home, "tmp"))
	part := filepath.Join(home, "tmp", "for-this-launch.md")
	writeFile(t, part, "---\nspec:\n  skills: [\"inlining-this-would-be-wrong\"]\n---\n", 0o644)

	bootRoot := filepath.Join(home, "runtime")
	var stderr bytes.Buffer
	err := run(ctx, []string{
		"boot", "engineer",
		"--profile", bundle,
		"--with", part,
		"--scope", scopeDir,
		"--boot-root", bootRoot,
		"--session", "s",
		"--save-as", "one-off",
	}, discard(), &stderr)
	if err == nil {
		t.Fatal("a composition holding a path member was saved")
	}
	if !strings.Contains(err.Error(), part) {
		t.Errorf("the refusal does not name the path member: %v", err)
	}
	// Refused before anything happened, not after. An operator whose save
	// cannot be honoured should not also have to clean up a boot directory
	// they are about to re-plant.
	nothingUnder(t, bootRoot)
	nothingUnder(t, filepath.Join(bundle, "bindings", "one-off.yaml"))
	// And specifically not by inlining what the part held. The rejected
	// alternative to refusing was to fold the part's content into the binding,
	// which turns a handle into content; nothing the part declared should be
	// anywhere near this.
	if strings.Contains(err.Error(), "inlining-this-would-be-wrong") {
		t.Errorf("the refusal carries the part's content: %v", err)
	}

	// The same refusal for a path a binding carries rather than a flag. A
	// hand-authored binding may name one — nothing stops it, because a part is
	// a part however it arrives — and saving that composition under a second
	// name would copy the unreproducible half of it into a new file without
	// anybody being told. What the composition holds is the question, not
	// which of the two ways it got there.
	writeFile(t, filepath.Join(bundle, "bindings", "carries-a-path.yaml"),
		fmt.Sprintf("profile: engineer\nparts:\n  - %s\nscope: %s\n", part, scopeDir), 0o644)
	err = run(ctx, []string{
		"boot", "carries-a-path",
		"--profile", bundle,
		"--boot-root", filepath.Join(home, "runtime2"),
		"--session", "s",
		"--save-as", "copied",
	}, discard(), discard())
	if err == nil {
		t.Fatal("a binding holding a path member was copied into a new binding")
	}
	if !strings.Contains(err.Error(), part) {
		t.Errorf("the refusal does not name the path member: %v", err)
	}
	if !strings.Contains(err.Error(), `binding "carries-a-path"`) {
		t.Errorf("the refusal does not say which file holds it: %v", err)
	}
	nothingUnder(t, filepath.Join(home, "runtime2"))
	nothingUnder(t, filepath.Join(bundle, "bindings", "copied.yaml"))
}

// TestBootJSONReportsWhatTheSaveDidAndDidNotSave covers the two keys a
// --save-as contributes to the launcher's document.
//
// A save is the one thing `cairn boot` does that leaves nothing in the boot
// directory to read. It was announced on stderr and nowhere else, so a launcher
// that composed and saved in one call had to parse a diagnostic to learn what
// it had just created — the scrape `--json` exists to end, arriving through the
// other half of the same command.
//
// The dropped --set is the half worth testing hardest, because the key reports
// a NAME and the drop is about a VALUE: the launcher already holds what it
// typed, and what it cannot otherwise know is which of those stopped at this
// run. So the value must be nowhere in the document, exactly as it is nowhere
// in the binding.
func TestBootJSONReportsWhatTheSaveDidAndDidNotSave(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)

	// Distinctive, so that finding it in the document could not be a
	// coincidence, and content-shaped, because that is what a --set carries.
	const direction = "Only the release notes, and nothing about the API."

	var stdout, stderr bytes.Buffer
	if err := run(ctx, []string{
		"boot", "engineer",
		"--profile", bundle,
		"--with", "docs-only",
		"--set", "direction=" + direction,
		"--scope", scopeDir,
		"--boot-root", filepath.Join(home, "runtime"),
		"--session", "s",
		"--save-as", "eng-docs",
		"--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("boot --save-as --json: %v\nstderr: %s", err, stderr.String())
	}

	var report bootReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal the report: %v", err)
	}
	// The path names a binding that is there. The write is the last thing
	// before the document is built and a failed write fails the command, so
	// there is no state where this key names a file that was not created.
	want := filepath.Join(bundle, "bindings", "eng-docs.yaml")
	if report.SavedBindingPath == nil || *report.SavedBindingPath != want {
		t.Fatalf("saved_binding_path = %v, want %q", deref(report.SavedBindingPath), want)
	}
	if _, err := os.Stat(*report.SavedBindingPath); err != nil {
		t.Errorf("saved_binding_path names a file that is not there: %v", err)
	}
	if got := []string{"direction"}; !slices.Equal(report.SavedDroppedSets, got) {
		t.Errorf("saved_dropped_sets = %v, want %v", report.SavedDroppedSets, got)
	}
	// Names, never values — and the check is over the bytes rather than the
	// field, because a value could reach the document through any key.
	if strings.Contains(stdout.String(), direction) {
		t.Errorf("the --set value reached the launcher's document:\n%s", stdout.String())
	}
	// The document says what stderr says. Two readings of one save is how a
	// report and a diagnostic start disagreeing about what was saved.
	if got := stderr.String(); !strings.Contains(got, "--set direction was not saved") {
		t.Errorf("stderr no longer names the dropped --set:\n%s", got)
	}

	// A save that dropped nothing is null and not [], which only the bytes
	// say: [] would read as "these zero were dropped" where the key's absent
	// value is the same "nothing to tell you about" as no --save-as at all.
	stdout.Reset()
	stderr.Reset()
	if err := run(ctx, []string{
		"boot", "engineer",
		"--profile", bundle,
		"--scope", scopeDir,
		"--boot-root", filepath.Join(home, "runtime2"),
		"--session", "s",
		"--save-as", "plain",
		"--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("boot --save-as --json: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"saved_dropped_sets": null`) {
		t.Errorf("saved_dropped_sets is not null on a save that dropped nothing:\n%s", stdout.String())
	}
	var plain bootReport
	if err := json.Unmarshal(stdout.Bytes(), &plain); err != nil {
		t.Fatalf("unmarshal the report: %v", err)
	}
	// And the path is still there, which is what separates the two states the
	// one null covers.
	if plain.SavedBindingPath == nil {
		t.Error("saved_binding_path is null on a boot that saved a binding")
	}
}

// TestARefusedSaveIsNotADocument settles the key --json does not have.
//
// Every refusal --save-as can raise is knowable before the boot runs and is
// raised there, so a refusal plants no directory — which means there is no
// document to carry a "the save was refused" key, and stdout is not a channel
// the refusal could arrive on even in principle. A launcher learns it the way
// it learns any other failed command: a non-zero exit and a diagnostic.
func TestARefusedSaveIsNotADocument(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	mustMkdir(t, filepath.Join(home, "tmp"))
	part := filepath.Join(home, "tmp", "for-this-launch.md")
	writeFile(t, part, "---\nspec:\n  skills: [\"one-off\"]\n---\n", 0o644)

	bootRoot := filepath.Join(home, "runtime")
	var stdout, stderr bytes.Buffer
	err := run(ctx, []string{
		"boot", "engineer",
		"--profile", bundle,
		"--with", part,
		"--scope", scopeDir,
		"--boot-root", bootRoot,
		"--session", "s",
		"--save-as", "one-off",
		"--json",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("a composition holding a path member was saved")
	}
	// Nothing on stdout at all — not an object reporting the refusal, and not
	// a half-written one. `$(cairn boot x --json)` either parses or the
	// command failed, and a consumer has one thing to check.
	if stdout.Len() != 0 {
		t.Errorf("a refused save printed a document:\n%s", stdout.String())
	}
	nothingUnder(t, bootRoot)
}

// TestSavedBindingFormat pins the file --save-as writes, in full.
//
// It is a contract and not an implementation detail: bindings are hand-edited,
// and by more than one writer. The shape asserted here is what a person would
// type — model-named keys in resolution order, two-space block sequences, no
// generated header — so that a saved binding and a hand-authored one are the
// same kind of file.
func TestSavedBindingFormat(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	// Spelled with a "/." the boot resolves away, so what is saved can be told
	// apart from what it resolved to. A binding recording this machine's
	// resolution of a path the operator typed would be a binding that carried
	// a spelling the operator cannot find in their own shell.
	declared := scopeDir + "/."

	bootTree(t, ctx, filepath.Join(home, "runtime"),
		"engineer",
		"--profile", bundle,
		"--with", "docs-only",
		"--skill", "qhealth", "--skill", "adr",
		"--scope", declared,
		"--save-as", "eng-docs",
	)

	want := "profile: engineer\n" +
		"parts:\n" +
		"  - docs-only\n" +
		"skills:\n" +
		"  - qhealth\n" +
		"  - adr\n" +
		fmt.Sprintf("scope: %s\n", declared)
	if got := read(t, bundle, "bindings/eng-docs.yaml"); got != want {
		t.Errorf("the saved binding is\n%q\nwant\n%q", got, want)
	}
}

// TestSavedBindingOmitsWhatWasNotComposed keeps a plain save down to the two
// lines a binding has been since there were bindings.
//
// An empty list written out as "parts: []" would be a saved binding that reads
// differently from every hand-authored one beside it, for no reason a reader
// could see.
func TestSavedBindingOmitsWhatWasNotComposed(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)

	bootTree(t, ctx, filepath.Join(home, "runtime"),
		"engineer", "--profile", bundle, "--scope", scopeDir, "--save-as", "plain")

	want := fmt.Sprintf("profile: engineer\nscope: %s\n", scopeDir)
	if got := read(t, bundle, "bindings/plain.yaml"); got != want {
		t.Errorf("the saved binding is\n%q\nwant\n%q", got, want)
	}
}

// TestSaveAsDoesNotOverwrite protects the one thing a binding file holds that
// nothing else in the bundle does: the comment saying why.
//
// eng.yaml in the example bundle carries three lines of prose above two lines
// of YAML. Saving over it would destroy the only copy of them, and it would do
// so as a side effect of a flag whose subject is a different binding.
func TestSaveAsDoesNotOverwrite(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	before := read(t, bundle, "bindings/eng.yaml")

	err := run(ctx, []string{
		"boot", "engineer",
		"--profile", bundle,
		"--scope", scopeDir,
		"--boot-root", filepath.Join(home, "runtime"),
		"--session", "s",
		"--save-as", "eng",
	}, discard(), discard())
	if err == nil {
		t.Fatal("an existing binding was overwritten")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the refusal does not say the file is already there: %v", err)
	}
	if got := read(t, bundle, "bindings/eng.yaml"); got != before {
		t.Errorf("the existing binding was rewritten:\n%s\nwas\n%s", got, before)
	}
	if !strings.Contains(before, "#") {
		t.Fatal("the fixture no longer carries a comment, so this test asserts nothing")
	}
}

// TestSaveAsRefusesANameThatIsNotAFileName. A binding's name is its file's
// base name, so a name that is not one is a binding nothing could boot — and,
// for the "../" spelling, a write outside the directory the operator pointed
// at.
func TestSaveAsRefusesANameThatIsNotAFileName(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	before := bindingFiles(t, bundle)

	for _, name := range []string{"../evil", "nested/one", ".hidden", "  "} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			bootRoot := filepath.Join(home, "runtime", strings.Map(safeSegment, name))
			err := run(ctx, []string{
				"boot", "engineer",
				"--profile", bundle,
				"--scope", scopeDir,
				"--boot-root", bootRoot,
				"--session", "s",
				"--save-as", name,
			}, discard(), discard())
			if strings.TrimSpace(name) == "" {
				// An empty --save-as is not a refusal: it is the flag not
				// given. Nothing is saved and the boot is ordinary.
				if err != nil {
					t.Fatalf("an empty --save-as refused the boot: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("--save-as %q was accepted", name)
			}
			nothingUnder(t, bootRoot)
		})
	}
	// Nothing was written anywhere in the bundle by any of them — including,
	// for the "../" spelling, outside the bindings directory.
	if got := bindingFiles(t, bundle); !slices.Equal(got, before) {
		t.Errorf("the bindings directory holds %v, want %v", got, before)
	}
	nothingUnder(t, filepath.Join(bundle, "..", "evil.yaml"))
}

// TestABindingReplaysItsPartsAndSkills is the other half of the round trip,
// asserted on a hand-authored file rather than on one cairn wrote.
//
// The two halves are separable and only one of them is new-looking: it is
// entirely possible to write a --save-as that records parts into a file that
// boot then ignores. That is a binding which lies about what it restores, and
// it would pass every test that only ever booted what it had just saved.
func TestABindingReplaysItsPartsAndSkills(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	writeFile(t, filepath.Join(bundle, "bindings", "hand.yaml"), ""+
		"# Written by a person, not by --save-as.\n"+
		"profile: engineer\n"+
		"parts:\n"+
		"  - docs-only\n"+
		"skills:\n"+
		"  - qhealth\n"+
		fmt.Sprintf("scope: %s\n", scopeDir), 0o644)

	var stdout, stderr bytes.Buffer
	if err := runShow(ctx, []string{"hand", "--profile", bundle}, &stdout, &stderr); err != nil {
		t.Fatalf("show: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	// docs-only's own body reaches the composition, which is the part landing.
	if !strings.Contains(out, "user-facing documentation") {
		t.Errorf("the binding's part did not compose:\n%s", out)
	}
	// And the skill the binding names is in the resolved set beside the ones
	// the profiles declare.
	if got := skillsOf(t, out); !slices.Equal(got, []string{"capture-decision", "qhealth"}) {
		t.Errorf("the composed skills are %v, want the binding's beside the profile's", got)
	}
	// The binding is named as the contributor, because it is the file a reader
	// would have to edit and there is no flag to blame.
	if !strings.Contains(out, `binding "hand"`) {
		t.Errorf("the binding is not credited for the skills it contributed:\n%s", out)
	}
}

// TestABindingsPartIsNamedByItsBinding covers the diagnostic, which is the
// half of replay that is easy to leave pointing at the wrong file.
//
// A part a binding names can be absorbed by the chain exactly as a --with can,
// and the line saying so is the only thing that stops it being a silent
// no-op. Reporting it as a "--with" would send an operator looking through a
// command line they did not type.
func TestABindingsPartIsNamedByItsBinding(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)

	t.Run("a part already in the chain", func(t *testing.T) {
		writeFile(t, filepath.Join(bundle, "bindings", "absorbed.yaml"),
			"profile: engineer\nparts:\n  - base\n", 0o644)
		var stderr bytes.Buffer
		if err := runShow(ctx, []string{"absorbed", "--profile", bundle}, discard(), &stderr); err != nil {
			t.Fatalf("show: %v", err)
		}
		want := "cairn: binding \"absorbed\": part base: already in the chain, contributed nothing\n"
		if got := stderr.String(); got != want {
			t.Errorf("stderr is %q, want %q", got, want)
		}
	})

	t.Run("a part the binding and the flag both name", func(t *testing.T) {
		// The fold keeps a part where it FIRST landed, which is the binding's
		// position, so the binding is what the report has to name. Keying the
		// spelling by the part's id and letting the later write win would
		// replace a correct answer with a plausible one, and the plausible one
		// sends the reader to a flag that is not why the part is there.
		writeFile(t, filepath.Join(bundle, "bindings", "dup.yaml"),
			"profile: engineer\nparts:\n  - docs-only\n", 0o644)
		var stderr bytes.Buffer
		if err := runShow(ctx, []string{"dup", "--with", "docs-only", "--profile", bundle},
			discard(), &stderr); err != nil {
			t.Fatalf("show: %v", err)
		}
		want := "cairn: binding \"dup\": part docs-only: already in the chain, contributed nothing\n"
		if got := stderr.String(); got != want {
			t.Errorf("stderr is %q, want %q", got, want)
		}
	})

	t.Run("a part the bundle does not hold", func(t *testing.T) {
		writeFile(t, filepath.Join(bundle, "bindings", "missing.yaml"),
			"profile: engineer\nparts:\n  - gone\n", 0o644)
		err := runShow(ctx, []string{"missing", "--profile", bundle}, discard(), discard())
		if err == nil {
			t.Fatal("a binding naming a part that is not there was accepted")
		}
		if !strings.Contains(err.Error(), `binding "missing"`) {
			t.Errorf("the diagnostic does not name the binding to edit: %v", err)
		}
		if strings.Contains(err.Error(), "--with") {
			t.Errorf("the diagnostic blames a flag nobody typed: %v", err)
		}
	})
}

// TestSaveAsComposesOntoABinding. Saving from a binding saves what that boot
// composed — the binding's parts and the ones typed onto it — because there is
// one idea of "the composition" and --save-as records that one.
func TestSaveAsComposesOntoABinding(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	writeFile(t, filepath.Join(bundle, "bindings", "base-binding.yaml"),
		"profile: engineer\nparts:\n  - docs-only\nskills:\n  - qhealth\n"+
			fmt.Sprintf("scope: %s\n", scopeDir), 0o644)

	bootTree(t, ctx, filepath.Join(home, "runtime"),
		"base-binding", "--profile", bundle, "--skill", "adr", "--save-as", "grown")

	want := "profile: engineer\n" +
		"parts:\n  - docs-only\n" +
		"skills:\n  - qhealth\n  - adr\n" +
		fmt.Sprintf("scope: %s\n", scopeDir)
	if got := read(t, bundle, "bindings/grown.yaml"); got != want {
		t.Errorf("the saved binding is\n%q\nwant\n%q", got, want)
	}
}

// TestTheTerminalHasTheLastWordOverTheBinding pins the order replay puts a
// binding's parts in, which is the whole reason a binding can be composed onto
// at all.
//
// A binding is a saved composition, not a fourth kind of contributor, so it
// resolves where the composition it saved would have: ahead of whatever is
// typed onto it. Reversing the two is silent — every part still folds, every
// skill still lands — until two of them declare the same key, and then a flag
// typed at the terminal loses to a file the operator was extending.
func TestTheTerminalHasTheLastWordOverTheBinding(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	bundle := exampleBundle(t, home)
	scopeDir := filepath.Join(home, "scope")
	mustMkdir(t, scopeDir)
	for _, id := range []string{"from-the-binding", "from-the-flag"} {
		writeFile(t, filepath.Join(bundle, "profiles", id+".md"),
			fmt.Sprintf("---\nid: %s\nname: %s\n---\n", id, id), 0o644)
	}
	writeFile(t, filepath.Join(bundle, "bindings", "ordered.yaml"),
		"profile: engineer\nparts:\n  - from-the-binding\n"+
			fmt.Sprintf("scope: %s\n", scopeDir), 0o644)

	// Both parts declare a name. Closest-wins, and the terminal is closest.
	out := mustShow(t, ctx, bundle, "ordered", "--with", "from-the-flag")
	if !strings.Contains(out, "from-the-flag") {
		t.Errorf("the binding's part outranked the flag typed onto it:\n%s", out)
	}
	if strings.Contains(out, "name          from-the-binding") {
		t.Errorf("the resolved name came from the binding rather than the flag:\n%s", out)
	}

	// And the order survives the save, because what is saved is the
	// composition that resolved rather than a re-derivation of it.
	bootTree(t, ctx, filepath.Join(home, "runtime"),
		"ordered", "--profile", bundle, "--with", "from-the-flag", "--save-as", "ordered2")
	want := "profile: engineer\n" +
		"parts:\n  - from-the-binding\n  - from-the-flag\n" +
		fmt.Sprintf("scope: %s\n", scopeDir)
	if got := read(t, bundle, "bindings/ordered2.yaml"); got != want {
		t.Errorf("the saved binding is\n%q\nwant\n%q", got, want)
	}
}

// exampleBundle copies examples/bundle into a directory of its own and returns
// it.
//
// A copy, because --save-as writes into the bundle and the example one is
// checked in. The two skills the tests add are skills the example bundle does
// not ship: the point of naming them is that they are ids the profiles never
// declared, so seeing them in a boot directory means the flag or the binding
// put them there.
func exampleBundle(t *testing.T, into string) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "examples", "bundle"))
	if err != nil {
		t.Fatalf("locate the example bundle: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "profiles", "engineer.md")); err != nil {
		t.Fatalf("the example bundle is not where this test expects it: %v", err)
	}
	dst := filepath.Join(into, "bundle")
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatalf("copy the example bundle: %v", err)
	}
	for _, id := range []string{"qhealth", "adr"} {
		mustMkdir(t, filepath.Join(dst, "skills", id))
		writeFile(t, filepath.Join(dst, "skills", id, "SKILL.md"),
			fmt.Sprintf("---\nname: %s\ndescription: A skill the example bundle does not ship.\n---\n", id),
			0o644)
	}
	return dst
}

// writeProbePart adds a part to a copied bundle that declares one skill of its
// own, and the skill directory that skill needs.
//
// It is the smallest thing a part can contribute that a boot directory shows.
// The bundle's own part contributes nothing visible, which is exactly what
// makes a tree comparison over it unable to tell a composed boot from an
// uncomposed one — so a test whose claim is "the same tree" needs a part it
// can actually see.
func writeProbePart(t *testing.T, bundle, partID, skillID string) {
	t.Helper()
	writeFile(t, filepath.Join(bundle, "profiles", partID+".md"),
		fmt.Sprintf("---\nid: %s\nspec:\n  skills: [%s]\n---\n", partID, skillID), 0o644)
	mustMkdir(t, filepath.Join(bundle, "skills", skillID))
	writeFile(t, filepath.Join(bundle, "skills", skillID, "SKILL.md"),
		fmt.Sprintf("---\nname: %s\ndescription: Planted only by a part.\n---\n", skillID), 0o644)
}

// bootTree runs one boot and returns the tree it planted.
func bootTree(t *testing.T, ctx context.Context, bootRoot string, args ...string) map[string]string {
	t.Helper()
	return bootTreeErr(t, ctx, discard(), bootRoot, args...)
}

// bootTreeErr is [bootTree] with stderr handed in, for the tests that read it.
func bootTreeErr(t *testing.T, ctx context.Context, stderr io.Writer, bootRoot string, args ...string) map[string]string {
	t.Helper()
	var stdout bytes.Buffer
	var captured bytes.Buffer
	full := append([]string{"boot"}, args...)
	full = append(full, "--boot-root", bootRoot, "--session", "s")
	if err := run(ctx, full, &stdout, io.MultiWriter(stderr, &captured)); err != nil {
		t.Fatalf("boot %v: %v\nstderr: %s", args, err, captured.String())
	}
	return treeOf(t, strings.TrimSpace(stdout.String()))
}

// diffTrees fails with every difference between two boot directories rather
// than with the first, because "these two trees are the same" is the claim and
// one line of it is not enough to act on.
func diffTrees(t *testing.T, want, got map[string]string) {
	t.Helper()
	if maps.Equal(want, got) {
		return
	}
	for _, rel := range slices.Sorted(maps.Keys(want)) {
		switch g, ok := got[rel]; {
		case !ok:
			t.Errorf("the replayed boot did not write %s", rel)
		case g != want[rel]:
			t.Errorf("the replayed boot wrote a different %s:\n%s\nwant\n%s", rel, g, want[rel])
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[rel]; !ok {
			t.Errorf("the replayed boot wrote %s, which the composed boot did not", rel)
		}
	}
}

// changedFiles names every path the two trees disagree about, present in one
// and absent from the other included.
func changedFiles(t *testing.T, a, b map[string]string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, rel := range slices.Sorted(maps.Keys(a)) {
		if b[rel] != a[rel] {
			seen[rel] = true
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(b)) {
		if a[rel] != b[rel] {
			seen[rel] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// without removes the one line holding s, so that a difference of exactly one
// line can be asserted as one.
func without(text, s string) string {
	out := make([]string, 0, 8)
	for _, line := range strings.SplitAfter(text, "\n") {
		if !strings.Contains(line, s) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "")
}

// bindingFiles lists the bundle's bindings directory.
func bindingFiles(t *testing.T, bundle string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(bundle, "bindings"))
	if err != nil {
		t.Fatalf("read the bindings directory: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

// safeSegment maps a rune to one a directory name can hold, so that a test
// case named after a rejected binding name can still have a boot root of its
// own.
func safeSegment(r rune) rune {
	if r == '/' || r == os.PathSeparator || r == '.' || r == ' ' {
		return '_'
	}
	return r
}
