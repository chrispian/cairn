package bootdir

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/chrispian/cairn/profile"
)

// TestFilesRenderSortedAndVerbatim states both halves of the escape hatch. The
// order is sorted because a manifest holds these in a map, which has none, and
// a rendering has to be the same twice. The content is untouched because cairn
// does not know what any of it is for.
func TestFilesRenderSortedAndVerbatim(t *testing.T) {
	manifest := `{"files": {
		"notes/zulu.md":   "zulu\n",
		"alpha.md":        "alpha, with no trailing newline",
		"notes/alpha.md":  "nested alpha\n",
		"deeply/nested/x": "not markdown at all\r\n\tand not reformatted\n",
		"b.md":            ""
	}}`
	inst := testInstance(t, profile.Resolved{ID: "reviewer", Spec: testSpec(t, manifest)})

	files, err := renderFiles(inst)
	if err != nil {
		t.Fatalf("renderFiles(): %v", err)
	}
	want := []string{"alpha.md", "b.md", "deeply/nested/x", "notes/alpha.md", "notes/zulu.md"}
	if got := filePaths(files); !slices.Equal(got, want) {
		t.Fatalf("rendered %v, want %v", got, want)
	}
	for _, tt := range []struct{ path, content string }{
		{"alpha.md", "alpha, with no trailing newline"},
		{"b.md", ""},
		{"deeply/nested/x", "not markdown at all\r\n\tand not reformatted\n"},
		{"notes/zulu.md", "zulu\n"},
	} {
		if got := string(fileByPath(t, files, tt.path).Content); got != tt.content {
			t.Errorf("%s holds %q, want %q", tt.path, got, tt.content)
		}
	}
}

// TestFilesAreAbsentWhenNoneAreDeclared covers the profile that declares no
// files key at all.
func TestFilesAreAbsentWhenNoneAreDeclared(t *testing.T) {
	for _, manifest := range []string{"", `{}`, `{"files": {}}`, `{"files": null}`} {
		inst := testInstance(t, profile.Resolved{ID: "quiet", Spec: testSpec(t, manifest)})

		files, err := renderFiles(inst)
		if err != nil {
			t.Fatalf("renderFiles() with manifest %q: %v", manifest, err)
		}
		if len(files) != 0 {
			t.Errorf("renderFiles() with manifest %q rendered %v, want nothing", manifest, filePaths(files))
		}
	}
}

// TestFilesRefuseAPathThatCannotNameItself covers the one bad path [Render]
// cannot report usefully: its diagnostic quotes the offending path, and an
// empty one leaves nothing to quote.
func TestFilesRefuseAPathThatCannotNameItself(t *testing.T) {
	inst := testInstance(t, profile.Resolved{ID: "reviewer", Spec: testSpec(t, `{"files": {"": "x"}}`)})

	_, err := renderFiles(inst)
	if !errors.Is(err, ErrArtifactPath) {
		t.Fatalf("renderFiles() error = %v, want ErrArtifactPath", err)
	}
	if !strings.Contains(err.Error(), profile.SpecKeyFiles) {
		t.Errorf("the error %q does not name the manifest key it came from", err)
	}
}

// TestFilesLeavePathSafetyToRender states where the second opinion lives. The
// renderer emits the path the manifest declared; [Render] is what decides
// whether it can name something inside the boot directory, and it asks that of
// every renderer.
func TestFilesLeavePathSafetyToRender(t *testing.T) {
	for _, bad := range []string{"../escape.md", "/absolute.md", "notes/../../escape.md"} {
		manifest := `{"files": {"` + bad + `": "x"}}`
		inst := testInstance(t, profile.Resolved{ID: "reviewer", Spec: testSpec(t, manifest)})

		if _, err := renderFiles(inst); err != nil {
			t.Errorf("renderFiles() with %q returned %v; path safety is Render's question", bad, err)
		}
		_, err := Render(inst)
		if !errors.Is(err, ErrArtifactPath) {
			t.Errorf("Render() with %q returned error %v, want ErrArtifactPath", bad, err)
		}
		if err != nil && !strings.Contains(err.Error(), bad) {
			t.Errorf("the error %q does not name %q", err, bad)
		}
	}
}

// TestFilesCarryAMalformedManifestOut leaves the operator's own JSON to the
// package that decodes it.
func TestFilesCarryAMalformedManifestOut(t *testing.T) {
	inst := testInstance(t, profile.Resolved{ID: "reviewer", Spec: testSpec(t, `{"files": ["not", "a map"]}`)})

	if _, err := renderFiles(inst); err == nil {
		t.Fatal("renderFiles() with a malformed files key returned no error")
	}
}
