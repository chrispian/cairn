package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/profile"
)

// fixtureSpec decodes a manifest written inline in a test into a
// [profile.Spec], so that a test declares the JSON an operator would have
// stored rather than a map of pre-marshalled values.
func fixtureSpec(t *testing.T, manifest string) profile.Spec {
	t.Helper()
	if manifest == "" {
		return nil
	}
	var spec profile.Spec
	if err := json.Unmarshal([]byte(manifest), &spec); err != nil {
		t.Fatalf("decode the manifest %s: %v", manifest, err)
	}
	return spec
}

// fixtureLayer returns a layer rooted at a fresh temporary directory.
//
// Every test in this package roots there and nowhere else. `cairn install`
// writes the configuration the session running these tests is itself reading,
// so a test that defaulted to a real home directory would rewrite it.
func fixtureLayer(t *testing.T, resolved profile.Resolved) *Layer {
	t.Helper()
	root, err := NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot on a temporary directory: %v", err)
	}
	return &Layer{Root: root, Profile: &resolved}
}

// fixtureSkill writes one skill directory under root: files maps
// slash-separated paths inside the skill to their contents, and every path
// named in executable is written with its executable bit set.
func fixtureSkill(t *testing.T, root, name string, files map[string]string, executable ...string) {
	t.Helper()
	for rel, content := range files {
		dest := filepath.Join(root, name, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatalf("create the directory for %s: %v", dest, err)
		}
		mode := fs.FileMode(0o644)
		if slices.Contains(executable, rel) {
			mode = 0o755
		}
		if err := os.WriteFile(dest, []byte(content), mode); err != nil {
			t.Fatalf("write %s: %v", dest, err)
		}
		// os.WriteFile's mode is masked by the umask, and the executable bit is
		// the property under test.
		if err := os.Chmod(dest, mode); err != nil {
			t.Fatalf("set the mode on %s: %v", dest, err)
		}
	}
}

// renderedPaths returns the paths of files, in render order.
func renderedPaths(files []File) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

// renderedFile returns the rendered file at path.
func renderedFile(t *testing.T, files []File, path string) File {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("no file was rendered at %q; the render holds %v", path, renderedPaths(files))
	return File{}
}

// declaredManifest returns a manifest declaring every key cairn renders
// somewhere, including the three the installed layer deliberately does not:
// slots, mcp, and files.
func declaredManifest(skillsDir string) string {
	return fmt.Sprintf(`{
	  "slots":      [{"name": "memory", "source": {"kind": "inline", "value": "remembered"}}],
	  "mcp":        [{"name": "memory", "command": "memoryd", "args": ["--stdio"]}],
	  "skills":     ["code-review"],
	  "skills_dir": %s,
	  "settings":   {"model": "opus"},
	  "files":      {"notes/todo.md": "do the thing"}
	}`, strconv.Quote(skillsDir))
}

// fullyDeclaredLayer returns a layer whose profile declares every renderable
// manifest key, with one multi-file skill on disk to copy.
func fullyDeclaredLayer(t *testing.T) *Layer {
	t.Helper()
	skillsDir := t.TempDir()
	fixtureSkill(t, skillsDir, "code-review", map[string]string{
		"SKILL.md":            "# Code review\n",
		"references/style.md": "prefer clarity\n",
		"run.sh":              "#!/bin/sh\necho review\n",
	}, "run.sh")
	return fixtureLayer(t, profile.Resolved{
		ID:       "base",
		Name:     "Base",
		Provider: profile.ProviderClaude,
		Body:     "the operator's prose",
		Spec:     fixtureSpec(t, declaredManifest(skillsDir)),
	})
}

// TestRenderFullyDeclaredProfileRendersTheInstalledArtifactsAndNothingElse
// pins the whole output contract of the installed layer, in render order.
//
// The negative half is the point. The profile declares slots, MCP servers and
// spec.files, all of which a boot directory renders. A boot directory is
// created fresh and refuses to plant over one that exists, so an arbitrary
// path-to-content map can only ever land on empty ground; the installed layer
// writes into a directory already full of the operator's live state, where the
// same map lands on whatever is already there.
func TestRenderFullyDeclaredProfileRendersTheInstalledArtifactsAndNothingElse(t *testing.T) {
	files, err := Render(fullyDeclaredLayer(t))
	if err != nil {
		t.Fatalf("Render a fully declared profile: %v", err)
	}
	want := []string{
		".claude/AGENTS.md",
		".claude/CLAUDE.md",
		".claude/settings.json",
		".claude/skills/code-review/SKILL.md",
		".claude/skills/code-review/references/style.md",
		".claude/skills/code-review/run.sh",
	}
	if got := renderedPaths(files); !slices.Equal(got, want) {
		t.Errorf("Render produced\n\t%v\nwant\n\t%v", got, want)
	}
	for _, absent := range []string{"boot.md", ".mcp.json", "notes/todo.md"} {
		for _, f := range files {
			if f.Path == absent || strings.HasSuffix(f.Path, "/"+absent) {
				t.Errorf("Render produced %q; the installed layer renders no %s", f.Path, absent)
			}
		}
	}
}

// TestRenderEveryPathIsInsideTheProviderDirectory states the containment rule
// as its own assertion, so that a renderer emitting a path at the install
// root — beside the operator's shell configuration — fails here and not only
// as a surprising entry in the list above.
func TestRenderEveryPathIsInsideTheProviderDirectory(t *testing.T) {
	files, err := Render(fullyDeclaredLayer(t))
	if err != nil {
		t.Fatalf("Render a fully declared profile: %v", err)
	}
	for _, f := range files {
		if !strings.HasPrefix(f.Path, ClaudeDirName+"/") {
			t.Errorf("Render produced %q, which is not under %q", f.Path, ClaudeDirName)
		}
	}
}

// TestCheckInsideReportsAPathOutsideTheProviderDirectory exercises the
// containment check directly. Nothing reachable through [Render] can produce
// such a path today, which is exactly why the check has to be tested from
// here: the case it guards is a future change to a shared renderer.
func TestCheckInsideReportsAPathOutsideTheProviderDirectory(t *testing.T) {
	for _, escape := range []string{"AGENTS.md", ".claudex/AGENTS.md", ".claude"} {
		files := []File{{Path: ClaudeDirName + "/AGENTS.md"}, {Path: escape}}
		err := checkInside(files, ClaudeDirName)
		if !errors.Is(err, ErrUnexpectedArtifact) {
			t.Errorf("checkInside with %q = %v, want ErrUnexpectedArtifact", escape, err)
		}
		if err != nil && !strings.Contains(err.Error(), escape) {
			t.Errorf("checkInside with %q reported %v, which does not name the path", escape, err)
		}
	}
}

// TestRenderPointerIsTheIncludeAndNothingElse holds the pointer file to its
// whole contract: one include of the instruction file, byte for byte, with no
// generated-file marker. The marker would be three times the file and would
// sit in the document that points at the one carrying it.
func TestRenderPointerIsTheIncludeAndNothingElse(t *testing.T) {
	files, err := Render(fullyDeclaredLayer(t))
	if err != nil {
		t.Fatalf("Render a fully declared profile: %v", err)
	}
	pointer := renderedFile(t, files, ".claude/CLAUDE.md")
	if string(pointer.Content) != bootdir.PointerFileContent {
		t.Errorf(".claude/CLAUDE.md rendered %q, want %q", pointer.Content, bootdir.PointerFileContent)
	}
}

// TestRenderInstructionFileOpensWithTheGeneratedMarker checks the marker is
// the first line, that a blank line separates it from the content, and that
// the content the shared renderer produced is still there behind it.
func TestRenderInstructionFileOpensWithTheGeneratedMarker(t *testing.T) {
	files, err := Render(fullyDeclaredLayer(t))
	if err != nil {
		t.Fatalf("Render a fully declared profile: %v", err)
	}
	agents := renderedFile(t, files, ".claude/AGENTS.md")
	wantPrefix := GeneratedMarker("base") + "\n\n"
	if !strings.HasPrefix(string(agents.Content), wantPrefix) {
		t.Fatalf(".claude/AGENTS.md rendered\n%s\nwant it to open with\n%s", agents.Content, wantPrefix)
	}
	if !strings.Contains(string(agents.Content), `"base"`) {
		t.Errorf(".claude/AGENTS.md does not name the profile it was rendered from:\n%s", agents.Content)
	}
	if !strings.Contains(string(agents.Content), "the operator's prose") {
		t.Errorf(".claude/AGENTS.md lost the profile body behind the marker:\n%s", agents.Content)
	}
}

// TestRenderSettingsCarriesNoGeneratedMarker holds the settings document to
// the harness's schema. JSON has no comment syntax, and a top-level key cairn
// invented would be cairn editing a document whose shape belongs to Claude
// Code.
func TestRenderSettingsCarriesNoGeneratedMarker(t *testing.T) {
	files, err := Render(fullyDeclaredLayer(t))
	if err != nil {
		t.Fatalf("Render a fully declared profile: %v", err)
	}
	settings := renderedFile(t, files, ".claude/settings.json")
	if string(settings.Content) != "{\"model\": \"opus\"}\n" {
		t.Errorf(".claude/settings.json rendered %q, want the stored bytes and a newline", settings.Content)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(settings.Content, &decoded); err != nil {
		t.Fatalf(".claude/settings.json is not valid JSON: %v", err)
	}
}

// TestBootDirectoryInstructionFileCarriesNoGeneratedMarker guards the marker
// against leaking into package bootdir.
//
// A boot directory is created fresh, disposable, and never hand-edited with an
// expectation of persistence, so the line would be noise in every session's
// context with no reader to serve. This asserts it from outside, because the
// mechanism that keeps it out is that install applies the marker after
// bootdir's renderer has returned — which stops holding the moment someone
// moves it one package down.
func TestBootDirectoryInstructionFileCarriesNoGeneratedMarker(t *testing.T) {
	layout, err := bootdir.LayoutFor(profile.ProviderClaude)
	if err != nil {
		t.Fatalf("bootdir.LayoutFor(%q): %v", profile.ProviderClaude, err)
	}
	resolved := profile.Resolved{ID: "base", Name: "Base", Provider: profile.ProviderClaude, Body: "prose"}
	files, err := bootdir.Render(&bootdir.Instance{
		Dir:     filepath.Join(t.TempDir(), "boot"),
		Layout:  layout,
		Profile: &resolved,
	})
	if err != nil {
		t.Fatalf("bootdir.Render: %v", err)
	}
	agents := renderedFile(t, files, bootdir.AgentsFileName)
	if bytes.Contains(agents.Content, []byte("Generated by")) {
		t.Errorf("a boot directory's %s carries a generated-file marker:\n%s",
			bootdir.AgentsFileName, agents.Content)
	}
}

// TestRenderAbstractProfile holds plan §7: the installed layer is normally
// rendered from the abstract root of the cascade, so Render must not refuse
// one. `cairn boot` is what refuses a direct boot.
func TestRenderAbstractProfile(t *testing.T) {
	lay := fixtureLayer(t, profile.Resolved{
		ID:       "base",
		Name:     "Base",
		Abstract: true,
		Provider: profile.ProviderClaude,
		Body:     "the abstract root",
	})
	files, err := Render(lay)
	if err != nil {
		t.Fatalf("Render an abstract profile: %v", err)
	}
	want := []string{".claude/AGENTS.md", ".claude/CLAUDE.md"}
	if got := renderedPaths(files); !slices.Equal(got, want) {
		t.Fatalf("Render an abstract profile produced %v, want %v", got, want)
	}
}

// TestRenderProfileDeclaringNothing checks that a manifest with no keys at all
// renders the unconditional artifacts and reports no error. A profile is not
// obliged to declare anything.
func TestRenderProfileDeclaringNothing(t *testing.T) {
	lay := fixtureLayer(t, profile.Resolved{ID: "bare", Provider: profile.ProviderClaude})
	files, err := Render(lay)
	if err != nil {
		t.Fatalf("Render a profile declaring nothing: %v", err)
	}
	want := []string{".claude/AGENTS.md", ".claude/CLAUDE.md"}
	if got := renderedPaths(files); !slices.Equal(got, want) {
		t.Fatalf("Render a profile declaring nothing produced %v, want %v", got, want)
	}
}

// TestMarkGeneratedDropsAnInstructionFileWithNoContent checks that an
// instruction file rendering to no bytes stays absent rather than becoming a
// file holding a marker and nothing else. A file claiming cairn rendered
// something, when cairn rendered nothing, is worse than an absent one.
//
// It is asserted against the wrapper rather than through [Render], because no
// profile reachable through Render can produce it: the instruction file lists
// the provider the profile resolved to, and a profile with no provider is
// refused before any renderer runs. That makes this a guard on the wrapper's
// own rule, which is where the rule lives.
func TestMarkGeneratedDropsAnInstructionFileWithNoContent(t *testing.T) {
	empty := func(_ *bootdir.Instance) ([]File, error) {
		return []File{{Path: ".claude/AGENTS.md"}}, nil
	}
	files, err := markGenerated(empty, "base")(nil)
	if err != nil {
		t.Fatalf("markGenerated over an empty render: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("markGenerated produced %v, want no file at all", renderedPaths(files))
	}
}

// TestMarkGeneratedPropagatesTheRendererError checks the wrapper does not
// swallow what the shared renderer reported.
func TestMarkGeneratedPropagatesTheRendererError(t *testing.T) {
	want := errors.New("the renderer failed")
	failing := func(_ *bootdir.Instance) ([]File, error) { return nil, want }
	if _, err := markGenerated(failing, "base")(nil); !errors.Is(err, want) {
		t.Errorf("markGenerated over a failing render = %v, want %v", err, want)
	}
}

// TestMarkGeneratedDoesNotAliasTheRenderedContent checks the marker is
// prepended into a fresh buffer. A renderer returning a slice backed by an
// array it also holds — a skill's file bytes, say — must not have that array
// written through.
func TestMarkGeneratedDoesNotAliasTheRenderedContent(t *testing.T) {
	original := []byte("# Base\n")
	render := func(_ *bootdir.Instance) ([]File, error) {
		return []File{{Path: ".claude/AGENTS.md", Content: original}}, nil
	}
	files, err := markGenerated(render, "base")(nil)
	if err != nil {
		t.Fatalf("markGenerated: %v", err)
	}
	if string(original) != "# Base\n" {
		t.Errorf("markGenerated wrote through the renderer's own bytes: %q", original)
	}
	if !strings.HasSuffix(string(files[0].Content), "# Base\n") {
		t.Errorf("markGenerated lost the rendered content: %q", files[0].Content)
	}
}

// TestRenderMultiFileSkillLandsAsADirectoryTree holds plan §5: a skill is a
// directory, and the executable bit on a script inside it is load-bearing.
func TestRenderMultiFileSkillLandsAsADirectoryTree(t *testing.T) {
	files, err := Render(fullyDeclaredLayer(t))
	if err != nil {
		t.Fatalf("Render a fully declared profile: %v", err)
	}
	entry := renderedFile(t, files, ".claude/skills/code-review/SKILL.md")
	if string(entry.Content) != "# Code review\n" {
		t.Errorf("SKILL.md rendered %q", entry.Content)
	}
	reference := renderedFile(t, files, ".claude/skills/code-review/references/style.md")
	if string(reference.Content) != "prefer clarity\n" {
		t.Errorf("references/style.md rendered %q", reference.Content)
	}
	script := renderedFile(t, files, ".claude/skills/code-review/run.sh")
	if script.Mode != bootdir.SkillExecFileMode {
		t.Errorf("run.sh rendered with mode %v, want %v", script.Mode, bootdir.SkillExecFileMode)
	}
}

// TestRenderExpandsTheSkillsDirectoryAgainstTheLayerHome checks that a
// manifest path written with a leading "~/" is expanded against the home the
// layer carries, and not against the process's own environment.
func TestRenderExpandsTheSkillsDirectoryAgainstTheLayerHome(t *testing.T) {
	home := t.TempDir()
	skillsDir := filepath.Join(home, "skills")
	fixtureSkill(t, skillsDir, "capture-decision", map[string]string{"SKILL.md": "# Capture\n"})

	lay := fixtureLayer(t, profile.Resolved{
		ID:       "base",
		Provider: profile.ProviderClaude,
		Spec:     fixtureSpec(t, `{"skills": ["capture-decision"], "skills_dir": "~/skills"}`),
	})
	lay.Home = home

	files, err := Render(lay)
	if err != nil {
		t.Fatalf("Render with a home-relative skills directory: %v", err)
	}
	skill := renderedFile(t, files, ".claude/skills/capture-decision/SKILL.md")
	if string(skill.Content) != "# Capture\n" {
		t.Errorf("SKILL.md rendered %q", skill.Content)
	}
}

// TestRenderWithoutAProfile checks that a layer carrying no resolved profile
// is reported rather than dereferenced.
func TestRenderWithoutAProfile(t *testing.T) {
	root, err := NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot on a temporary directory: %v", err)
	}
	for name, lay := range map[string]*Layer{
		"a nil layer": nil,
		"no profile":  {Root: root},
	} {
		if _, err := Render(lay); !errors.Is(err, ErrNoProfile) {
			t.Errorf("Render with %s = %v, want ErrNoProfile", name, err)
		}
	}
}

// TestRenderWithoutARoot checks that the zero root is an error rather than a
// default. There is no directory cairn installs into unless one was named.
func TestRenderWithoutARoot(t *testing.T) {
	resolved := profile.Resolved{ID: "base", Provider: profile.ProviderClaude}
	if _, err := Render(&Layer{Profile: &resolved}); !errors.Is(err, ErrNoRoot) {
		t.Errorf("Render with the zero root = %v, want ErrNoRoot", err)
	}
}

// TestRenderUnsupportedProvider checks that a harness with no installed layout
// is refused rather than rendered through Claude Code's.
func TestRenderUnsupportedProvider(t *testing.T) {
	for _, p := range []profile.Provider{profile.ProviderCodex, profile.ProviderOpenCode, ""} {
		lay := fixtureLayer(t, profile.Resolved{ID: "base", Provider: p})
		if _, err := Render(lay); !errors.Is(err, bootdir.ErrUnsupportedProvider) {
			t.Errorf("Render for provider %q = %v, want bootdir.ErrUnsupportedProvider", p, err)
		}
	}
}

// TestRenderReportsAFailedRenderer checks that a renderer's own error reaches
// the caller naming what it was rendering.
func TestRenderReportsAFailedRenderer(t *testing.T) {
	lay := fixtureLayer(t, profile.Resolved{
		ID:       "base",
		Provider: profile.ProviderClaude,
		Spec: fixtureSpec(t, fmt.Sprintf(
			`{"skills": ["code-review"], "skills_dir": %s}`,
			strconv.Quote(filepath.Join(t.TempDir(), "absent")))),
	})
	_, err := Render(lay)
	if !errors.Is(err, bootdir.ErrSkillsSource) {
		t.Fatalf("Render with a missing skills directory = %v, want bootdir.ErrSkillsSource", err)
	}
	if !strings.Contains(err.Error(), SkillsDirName) {
		t.Errorf("the error does not name the artifact being rendered: %v", err)
	}
}

// TestPlanterForClaude checks the registration list a check reads to learn
// which directories cairn owns, including the tree flag that is the difference
// between finding an orphan and not.
func TestPlanterForClaude(t *testing.T) {
	renderers, layout, err := PlanterFor(profile.ProviderClaude)
	if err != nil {
		t.Fatalf("PlanterFor(%q): %v", profile.ProviderClaude, err)
	}
	if layout.Provider != profile.ProviderClaude {
		t.Errorf("PlanterFor returned a layout for %q", layout.Provider)
	}
	if layout.Boot.Declared() || layout.MCP.Declared() {
		t.Errorf("the installed layout declares a boot or MCP path: %+v", layout)
	}
	want := []struct {
		artifact string
		tree     bool
	}{
		{bootdir.AgentsFileName, false},
		{pointerFileName, false},
		{SettingsFileName, false},
		{SkillsDirName, true},
	}
	if len(renderers) != len(want) {
		t.Fatalf("PlanterFor returned %d renderers, want %d", len(renderers), len(want))
	}
	for i, w := range want {
		if renderers[i].Artifact != w.artifact || renderers[i].Tree != w.tree {
			t.Errorf("renderer %d is %q (tree %v), want %q (tree %v)",
				i, renderers[i].Artifact, renderers[i].Tree, w.artifact, w.tree)
		}
		if renderers[i].Render == nil {
			t.Errorf("renderer %q carries no render function", renderers[i].Artifact)
		}
	}
}

// TestPlanterForUnsupportedProvider checks that the caller receives an error
// rather than an empty layout it might render through.
func TestPlanterForUnsupportedProvider(t *testing.T) {
	for _, p := range []profile.Provider{profile.ProviderCodex, profile.ProviderOpenCode, "", "nonesuch"} {
		if _, _, err := PlanterFor(p); !errors.Is(err, bootdir.ErrUnsupportedProvider) {
			t.Errorf("PlanterFor(%q) = %v, want bootdir.ErrUnsupportedProvider", p, err)
		}
	}
}

// TestPlanterForReturnsAFreshSlice checks the documented promise that a caller
// may modify what it receives.
func TestPlanterForReturnsAFreshSlice(t *testing.T) {
	first, _, err := PlanterFor(profile.ProviderClaude)
	if err != nil {
		t.Fatalf("PlanterFor(%q): %v", profile.ProviderClaude, err)
	}
	first[0].Artifact = "mutated"
	second, _, err := PlanterFor(profile.ProviderClaude)
	if err != nil {
		t.Fatalf("PlanterFor(%q): %v", profile.ProviderClaude, err)
	}
	if second[0].Artifact != bootdir.AgentsFileName {
		t.Errorf("PlanterFor returned a shared slice: the second call sees %q", second[0].Artifact)
	}
}
