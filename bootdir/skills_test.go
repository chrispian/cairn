package bootdir

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chrispian/cairn/profile"
)

// skillsInstance returns an instance declaring names, copied from source.
//
// The fixtures are built under the test's own temporary directory rather than
// stored under testdata. A skill tree is a .claude/skills directory holding
// SKILL.md files, which is exactly the shape an agent harness scans the
// working tree for, so the fixtures are kept out of the working tree
// altogether.
func skillsInstance(t *testing.T, source string, names ...string) *Instance {
	t.Helper()
	manifest := map[string]any{profile.SpecKeySkills: names}
	if source != "" {
		manifest[profile.SpecKeySkillsDir] = source
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode the skills manifest: %v", err)
	}
	return testInstance(t, profile.Resolved{ID: "reviewer", Spec: testSpec(t, string(encoded))})
}

// TestSkillsArePlantedAsDirectories is the reason cairn copies a tree instead
// of resolving a skill to one file. A skill is a directory — an entry file,
// references beside it, and scripts that have to still be executable — and
// flattening it would drop everything but the entry file, silently.
func TestSkillsArePlantedAsDirectories(t *testing.T) {
	source := t.TempDir()
	writeSkillTree(t, source, "code-review", map[string]string{
		SkillFileName:             "# code review\n",
		"references/checklist.md": "- read the diff\n",
		"references/deep/note.md": "a nested reference\n",
		"scripts/run.sh":          "#!/bin/sh\necho reviewing\n",
	}, "scripts/run.sh")
	writeSkillTree(t, source, "capture-decision", map[string]string{
		SkillFileName: "# capture decision\n",
	})

	inst := skillsInstance(t, source, "code-review", "capture-decision")
	files, err := RenderSkills(inst)
	if err != nil {
		t.Fatalf("RenderSkills(): %v", err)
	}

	want := []string{
		".claude/skills/code-review/SKILL.md",
		".claude/skills/code-review/references/checklist.md",
		".claude/skills/code-review/references/deep/note.md",
		".claude/skills/code-review/scripts/run.sh",
		".claude/skills/capture-decision/SKILL.md",
	}
	got := filePaths(files)
	if len(got) != len(want) {
		t.Fatalf("rendered %v, want %v", got, want)
	}
	for _, path := range want {
		if !slices.Contains(got, path) {
			t.Errorf("nothing was rendered at %s; the rendering holds %v", path, got)
		}
	}

	entry := fileByPath(t, files, ".claude/skills/code-review/SKILL.md")
	if string(entry.Content) != "# code review\n" {
		t.Errorf("SKILL.md holds %q, want %q", entry.Content, "# code review\n")
	}
	if entry.Mode != DefaultFileMode {
		t.Errorf("SKILL.md is mode %v, want %v", entry.Mode, DefaultFileMode)
	}
	script := fileByPath(t, files, ".claude/skills/code-review/scripts/run.sh")
	if script.Mode != SkillExecFileMode {
		t.Errorf("the script is mode %v, want %v — a skill's script has to still run",
			script.Mode, SkillExecFileMode)
	}
	reference := fileByPath(t, files, ".claude/skills/code-review/references/checklist.md")
	if reference.Mode != DefaultFileMode {
		t.Errorf("a reference is mode %v, want %v", reference.Mode, DefaultFileMode)
	}
}

// TestSkillsAreOrderedDeterministically states the property a rendering has to
// hold whatever the filesystem hands back: skills in the order the manifest
// declares them, files inside each in one stable order.
func TestSkillsAreOrderedDeterministically(t *testing.T) {
	source := t.TempDir()
	writeSkillTree(t, source, "zulu", map[string]string{
		SkillFileName: "z\n", "b.md": "b\n", "a.md": "a\n", "references/y.md": "y\n",
	})
	writeSkillTree(t, source, "alpha", map[string]string{SkillFileName: "a\n"})

	inst := skillsInstance(t, source, "zulu", "alpha")
	first, err := RenderSkills(inst)
	if err != nil {
		t.Fatalf("RenderSkills(): %v", err)
	}
	want := []string{
		".claude/skills/zulu/SKILL.md",
		".claude/skills/zulu/a.md",
		".claude/skills/zulu/b.md",
		".claude/skills/zulu/references/y.md",
		".claude/skills/alpha/SKILL.md",
	}
	if got := filePaths(first); !slices.Equal(got, want) {
		t.Fatalf("rendered %v, want %v", got, want)
	}
	for i := range 8 {
		again, err := RenderSkills(inst)
		if err != nil {
			t.Fatalf("RenderSkills() on render %d: %v", i, err)
		}
		if got := filePaths(again); !slices.Equal(got, want) {
			t.Fatalf("render %d produced %v, want %v", i, got, want)
		}
	}
}

// TestSkillsAreCopiedNotLinked is why the planted layer exists at all. The
// bytes are read at render time and carried in a [File], so a boot directory
// cannot reference the skill source: editing the source afterwards leaves
// every already-rendered instance as it was.
func TestSkillsAreCopiedNotLinked(t *testing.T) {
	source := t.TempDir()
	writeSkillTree(t, source, "code-review", map[string]string{SkillFileName: "the original body\n"})

	files, err := RenderSkills(skillsInstance(t, source, "code-review"))
	if err != nil {
		t.Fatalf("RenderSkills(): %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "code-review", SkillFileName),
		[]byte("rewritten after rendering\n"), 0o644); err != nil {
		t.Fatalf("rewrite the source skill: %v", err)
	}
	if got := string(files[0].Content); got != "the original body\n" {
		t.Errorf("the rendered skill is %q, want the bytes read at render time", got)
	}
}

// TestSkillsRefuseWhatAHarnessWouldNotLoad collects every way a declared skill
// stops the render. Each of them would otherwise produce a boot directory an
// agent reads as complete while a skill it was promised is missing or inert.
func TestSkillsRefuseWhatAHarnessWouldNotLoad(t *testing.T) {
	source := t.TempDir()
	writeSkillTree(t, source, "code-review", map[string]string{SkillFileName: "# code review\n"})
	writeSkillTree(t, source, "no-entry-file", map[string]string{"references/only.md": "no entry file\n"})
	if err := os.MkdirAll(filepath.Join(source, "empty"), 0o755); err != nil {
		t.Fatalf("create an empty skill directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "a-file-not-a-directory"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write a file where a skill directory would be: %v", err)
	}

	tests := []struct {
		name    string
		declare []string
		source  string
		want    error
		names   []string
	}{
		{
			name:    "a skill with no entry file",
			declare: []string{"no-entry-file"},
			source:  source,
			want:    ErrSkillContent,
			names:   []string{`"no-entry-file"`, SkillFileName, filepath.Join(source, "no-entry-file")},
		},
		{
			name:    "a skill directory holding nothing",
			declare: []string{"empty"},
			source:  source,
			want:    ErrSkillContent,
			names:   []string{`"empty"`, filepath.Join(source, "empty")},
		},
		{
			name:    "a skill that is a file",
			declare: []string{"a-file-not-a-directory"},
			source:  source,
			want:    ErrSkillContent,
			names:   []string{`"a-file-not-a-directory"`},
		},
		{
			name:    "a skill that is not there",
			declare: []string{"code-review", "no-such-skill"},
			source:  source,
			want:    ErrSkillNotFound,
			names:   []string{`"no-such-skill"`, filepath.Join(source, "no-such-skill")},
		},
		{
			name:    "a name holding a path separator",
			declare: []string{"../elsewhere/code-review"},
			source:  source,
			want:    ErrSkillName,
			names:   []string{`"../elsewhere/code-review"`},
		},
		{
			name:    "an empty name",
			declare: []string{""},
			source:  source,
			want:    ErrSkillName,
		},
		{
			name:    "a name declared twice",
			declare: []string{"code-review", "code-review"},
			source:  source,
			want:    ErrSkillName,
			names:   []string{`"code-review"`},
		},
		{
			name:    "skills declared with no skills_dir",
			declare: []string{"code-review"},
			source:  "",
			want:    ErrSkillsSource,
			names:   []string{`"code-review"`, profile.SpecKeySkillsDir},
		},
		{
			name:    "a skills_dir that is not absolute",
			declare: []string{"code-review"},
			source:  "relative/skills",
			want:    ErrSkillsSource,
			names:   []string{`"relative/skills"`},
		},
		{
			name:    "a skills_dir that is not there",
			declare: []string{"code-review"},
			source:  filepath.Join(source, "no-such-directory"),
			want:    ErrSkillsSource,
			names:   []string{filepath.Join(source, "no-such-directory")},
		},
		{
			name:    "a skills_dir that is a file",
			declare: []string{"code-review"},
			source:  filepath.Join(source, "a-file-not-a-directory"),
			want:    ErrSkillsSource,
			names:   []string{filepath.Join(source, "a-file-not-a-directory")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := skillsInstance(t, tt.source, tt.declare...)

			files, err := RenderSkills(inst)
			if !errors.Is(err, tt.want) {
				t.Fatalf("RenderSkills() error = %v, want %v; it rendered %v", err, tt.want, filePaths(files))
			}
			for _, want := range tt.names {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error %q does not name %s", err, want)
				}
			}
		})
	}
}

// TestSkillsRefuseSomethingThatIsNotAFile keeps a directory symlink out of a
// planted skill. A symlink to a regular file is read through and copied by
// value, which is the point; anything else would be planted as a file that is
// not one.
func TestSkillsRefuseSomethingThatIsNotAFile(t *testing.T) {
	source := t.TempDir()
	writeSkillTree(t, source, "code-review", map[string]string{SkillFileName: "# code review\n"})
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(source, "code-review", "linked")); err != nil {
		t.Skipf("this filesystem does not support symlinks: %v", err)
	}

	_, err := RenderSkills(skillsInstance(t, source, "code-review"))
	if !errors.Is(err, ErrSkillContent) {
		t.Fatalf("RenderSkills() error = %v, want ErrSkillContent", err)
	}
}

// TestSkillsExpandALeadingTilde covers the one form a profile is allowed to
// write a home-relative skills directory in. Cairn ships no skills, so the
// directory is always somewhere on the operator's machine, and writing it out
// in full in every profile is how it goes stale.
//
// The home it expands against is the instance's, not the process's: a renderer
// consults nothing outside the instance it was handed, which is also why this
// test does not have to touch the environment to exercise the expansion.
func TestSkillsExpandALeadingTilde(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, "skills")
	writeSkillTree(t, source, "code-review", map[string]string{SkillFileName: "# code review\n"})

	inst := skillsInstance(t, "~/skills", "code-review")
	inst.Home = home
	files, err := RenderSkills(inst)
	if err != nil {
		t.Fatalf("RenderSkills() with a home-relative skills_dir: %v", err)
	}
	if got := filePaths(files); !slices.Equal(got, []string{".claude/skills/code-review/SKILL.md"}) {
		t.Errorf("rendered %v, want the one skill under %s", got, SkillsDirName)
	}

	// With no home on the instance there is nothing to expand against, and the
	// failure says which key could not be resolved rather than resolving
	// somewhere unexpected.
	inst.Home = ""
	if _, err := RenderSkills(inst); !errors.Is(err, ErrSkillsSource) {
		t.Errorf("RenderSkills() with no home = %v, want ErrSkillsSource", err)
	}
}

// TestSkillsAreAbsentWhenNoneAreDeclared covers the profile that declares no
// skills at all, including one that declares a directory and nothing to copy
// out of it.
func TestSkillsAreAbsentWhenNoneAreDeclared(t *testing.T) {
	for _, manifest := range []string{"", `{}`, `{"skills": []}`, `{"skills": null}`,
		`{"skills": [], "skills_dir": "/nowhere"}`} {
		inst := testInstance(t, profile.Resolved{ID: "quiet", Spec: testSpec(t, manifest)})

		files, err := RenderSkills(inst)
		if err != nil {
			t.Fatalf("RenderSkills() with manifest %q: %v", manifest, err)
		}
		if len(files) != 0 {
			t.Errorf("RenderSkills() with manifest %q rendered %v, want nothing", manifest, filePaths(files))
		}
	}
}

// TestSkillsRefuseALayoutWithNoSkillsDirectory covers a layout that declares
// nowhere to plant a skill while the profile declares one to plant.
func TestSkillsRefuseALayoutWithNoSkillsDirectory(t *testing.T) {
	source := t.TempDir()
	writeSkillTree(t, source, "code-review", map[string]string{SkillFileName: "# code review\n"})
	inst := skillsInstance(t, source, "code-review")
	inst.Layout.SkillsDir = ""

	if _, err := RenderSkills(inst); !errors.Is(err, ErrProviderLayout) {
		t.Fatalf("RenderSkills() error = %v, want ErrProviderLayout", err)
	}
}

// TestSkillFileModeMapsOnlyTheExecutableBit states what survives the copy: a
// file that could be run stays runnable, and every other mode becomes the
// default, so a stray 0600 in the source cannot plant a skill the harness
// cannot read.
func TestSkillFileModeMapsOnlyTheExecutableBit(t *testing.T) {
	tests := []struct {
		source fs.FileMode
		want   fs.FileMode
	}{
		{0o644, DefaultFileMode},
		{0o600, DefaultFileMode},
		{0o400, DefaultFileMode},
		{0o755, SkillExecFileMode},
		{0o700, SkillExecFileMode},
		{0o111, SkillExecFileMode},
	}
	for _, tt := range tests {
		if got := skillFileMode(tt.source); got != tt.want {
			t.Errorf("skillFileMode(%v) = %v, want %v", tt.source, got, tt.want)
		}
	}
}
