package bootdir

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/chrispian/cairn/profile"
)

// TestSettingsAreWrittenVerbatim is the assertion behind docs/plan.md §6.
// Cairn models no permission mode, validates no tool name and translates
// nothing: the settings key is the operator's document, and the rendering is
// that document. Every one below is deliberately odd and every one is valid
// JSON, because the shape of a settings document belongs to the harness that
// reads it and to whoever wrote it.
//
// Verbatim is asserted by compacting the render and comparing that to the
// stored bytes compacted — [json.Compact] rather than a second call to the
// [json.Indent] under test, so the two sides are not the same function
// agreeing with itself. What survives that round trip is everything the
// operator could have written: a duplicated key, an unescaped angle bracket, a
// number spelled 0.5e3, a document that is not an object at all.
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

		files, err := RenderSettings(inst)
		if err != nil {
			t.Fatalf("RenderSettings() with %s: %v", document, err)
		}
		if len(files) != 1 {
			t.Fatalf("RenderSettings() with %s wrote %v, want one file", document, filePaths(files))
		}
		if want := inst.Layout.Settings.RelPath; files[0].Path != want {
			t.Errorf("rendered at %q, want %q", files[0].Path, want)
		}
		if got, want := compactForTest(t, files[0].Content), compactForTest(t, []byte(document)); got != want {
			t.Errorf("the settings document compacts to\n%s\nwant\n%s", got, want)
		}
		if content := files[0].Content; !bytes.HasSuffix(content, []byte("}\n")) &&
			!bytes.HasSuffix(content, []byte("]\n")) && !bytes.HasSuffix(content, []byte("\n")) {
			t.Errorf("the settings document does not end in a newline: %q", content)
		}
	}
}

// compactForTest removes a document's insignificant whitespace, reporting a
// document that is not JSON rather than hiding it.
func compactForTest(t *testing.T, raw []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, bytes.TrimSpace(raw)); err != nil {
		t.Fatalf("compact %q: %v", raw, err)
	}
	return buf.String()
}

// TestSettingsAreLaidOut covers the one edit the renderer makes. The operator
// reads this file and diffs it, and the harness rewrites it at this width, so
// a document arriving compact is written out one key per line.
func TestSettingsAreLaidOut(t *testing.T) {
	inst := testInstance(t, profile.Resolved{
		ID:   "reviewer",
		Spec: profile.Spec{profile.SpecKeySettings: []byte(`{"model":"opus","tui":"fullscreen"}`)},
	})

	files, err := RenderSettings(inst)
	if err != nil {
		t.Fatalf("RenderSettings(): %v", err)
	}
	want := "{\n  \"model\": \"opus\",\n  \"tui\": \"fullscreen\"\n}\n"
	if string(files[0].Content) != want {
		t.Errorf("the settings document is %q, want %q", files[0].Content, want)
	}
}

// TestSettingsKeepASingleTrailingNewline covers a stored document that already
// ends in one: it is not given a second.
func TestSettingsKeepASingleTrailingNewline(t *testing.T) {
	inst := testInstance(t, profile.Resolved{
		ID:   "reviewer",
		Spec: profile.Spec{profile.SpecKeySettings: []byte("{\"model\": \"opus\"}\n")},
	})

	files, err := RenderSettings(inst)
	if err != nil {
		t.Fatalf("RenderSettings(): %v", err)
	}
	if want := "{\n  \"model\": \"opus\"\n}\n"; string(files[0].Content) != want {
		t.Errorf("the settings document is %q, want %q", files[0].Content, want)
	}
}

// TestSettingsAreAbsentWhenUndeclared keeps cairn from planting a settings
// file for a profile that asked for none. An empty document is a settings
// document, and a harness reads it as one.
func TestSettingsAreAbsentWhenUndeclared(t *testing.T) {
	for _, manifest := range []string{"", `{}`, `{"settings": null}`} {
		inst := testInstance(t, profile.Resolved{ID: "quiet", Spec: testSpec(t, manifest)})

		files, err := RenderSettings(inst)
		if err != nil {
			t.Fatalf("RenderSettings() with manifest %q: %v", manifest, err)
		}
		if len(files) != 0 {
			t.Errorf("RenderSettings() with manifest %q wrote %v, want no file",
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

	if _, err := RenderSettings(inst); err == nil {
		t.Fatal("RenderSettings() with no declared settings path returned no error")
	}
}
