package bootdir

import (
	"testing"

	"github.com/chrispian/cairn/profile"
)

// TestSettingsAreWrittenVerbatim is the assertion behind docs/plan.md §6.
// Cairn models no permission mode, validates no tool name and translates
// nothing: the settings key is the operator's bytes, and the rendering is
// those bytes. Every document below is deliberately odd and every one of them
// is valid JSON, because the shape of a settings document belongs to the
// harness that reads it and to whoever wrote it.
func TestSettingsAreWrittenVerbatim(t *testing.T) {
	documents := []string{
		`{"permissions":{"allow":["Bash(git status:*)","Read(//tmp/**)"],"deny":[]},"model":"opus"}`,
		"{\n\t\"outer\" : {\n\n\t\t\"inner\":[1,2,3]\n\t},\n  \"spaced\"   :   true\n}",
		`{"unknown_to_every_harness": {"nested": {"deeply": [null, false, 0.5e3]}}}`,
		`{"unicode": "héllo é <b>&amp;</b> — ✓", "emptyObject": {}, "emptyList": []}`,
		`{"duplicate": 1, "duplicate": 2}`,
		`{"permissionMode": "a mode no harness has ever heard of"}`,
		`[]`,
		`"a settings document that is a bare string"`,
		`17`,
	}

	for _, document := range documents {
		manifest := `{"settings": ` + document + `}`
		inst := testInstance(t, profile.Resolved{ID: "reviewer", Spec: testSpec(t, manifest)})

		files, err := renderSettings(inst)
		if err != nil {
			t.Fatalf("renderSettings() with %s: %v", document, err)
		}
		if len(files) != 1 {
			t.Fatalf("renderSettings() with %s wrote %v, want one file", document, filePaths(files))
		}
		if want := inst.Layout.Settings.RelPath; files[0].Path != want {
			t.Errorf("rendered at %q, want %q", files[0].Path, want)
		}
		if want := document + "\n"; string(files[0].Content) != want {
			t.Errorf("the settings document is\n%q\nwant\n%q", files[0].Content, want)
		}
	}
}

// TestSettingsKeepASingleTrailingNewline covers the one edit the renderer
// makes: a document already ending in a newline is not given a second one.
func TestSettingsKeepASingleTrailingNewline(t *testing.T) {
	inst := testInstance(t, profile.Resolved{
		ID:   "reviewer",
		Spec: profile.Spec{profile.SpecKeySettings: []byte("{\"model\": \"opus\"}\n")},
	})

	files, err := renderSettings(inst)
	if err != nil {
		t.Fatalf("renderSettings(): %v", err)
	}
	if want := "{\"model\": \"opus\"}\n"; string(files[0].Content) != want {
		t.Errorf("the settings document is %q, want %q", files[0].Content, want)
	}
}

// TestSettingsAreAbsentWhenUndeclared keeps cairn from planting a settings
// file for a profile that asked for none. An empty document is a settings
// document, and a harness reads it as one.
func TestSettingsAreAbsentWhenUndeclared(t *testing.T) {
	for _, manifest := range []string{"", `{}`, `{"settings": null}`} {
		inst := testInstance(t, profile.Resolved{ID: "quiet", Spec: testSpec(t, manifest)})

		files, err := renderSettings(inst)
		if err != nil {
			t.Fatalf("renderSettings() with manifest %q: %v", manifest, err)
		}
		if len(files) != 0 {
			t.Errorf("renderSettings() with manifest %q wrote %v, want no file",
				manifest, filePaths(files))
		}
	}
}

// TestSettingsRefuseToBeDropped covers a layout declaring no settings path
// while the manifest declares settings. Rendering nothing would silently
// discard the one artifact the operator wrote out by hand.
func TestSettingsRefuseToBeDropped(t *testing.T) {
	inst := testInstance(t, profile.Resolved{
		ID:   "reviewer",
		Spec: testSpec(t, `{"settings": {"model": "opus"}}`),
	})
	inst.Layout.Settings = Artifact{}

	if _, err := renderSettings(inst); err == nil {
		t.Fatal("renderSettings() with no declared settings path returned no error")
	}
}
