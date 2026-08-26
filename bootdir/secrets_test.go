package bootdir_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/profile"
)

// TestATemplateCannotReachAManifestValue is a regression guard on the one place
// a substituted value would do real damage.
//
// A template exists to be read into a model's context, and the boot directory
// holding it is handed to a harness. spec.mcp[].env is where an MCP server's
// API keys live, and spec.settings holds whatever the operator put there. A
// marker vocabulary wide enough to reach a manifest key — anything shaped like
// "substitute spec.mcp" — would let a template pull those into the one artifact
// whose whole purpose is to be read aloud.
//
// The rule is therefore structural rather than a promise about secrets, which
// nobody can enforce: there are exactly two marker verbs, one substitutes a
// slot the operator declared a source for, and the other substitutes one of the
// closed set in [bootdir.ValueNames]. Neither can name a manifest key. This
// test writes a template that tries every value there is and asserts the
// secret does not appear.
func TestATemplateCannotReachAManifestValue(t *testing.T) {
	const secret = "sk-live-do-not-render-me"

	// A template asking for every value cairn will substitute, plus a slot.
	var markers strings.Builder
	for _, name := range bootdir.ValueNames() {
		fmt.Fprintf(&markers, "%s: <!-- cairn:value %s -->\n", name, name)
	}
	markers.WriteString("<!-- cairn:slot note -->\n")

	inst := instance(t, &profile.Resolved{
		ID:       "engineer",
		Name:     "Engineer",
		Provider: profile.ProviderClaude,
		Model:    "opus",
		Body:     "engineer persona",
		Spec: profile.Spec{
			"mcp": json.RawMessage(`[{"name":"vanta","command":"vanta-mcp",` +
				`"env":{"VANTA_TOKEN":"` + secret + `"},"args":["--token","` + secret + `"]}]`),
			"settings":  json.RawMessage(`{"env":{"ANTHROPIC_API_KEY":"` + secret + `"}}`),
			"templates": json.RawMessage(`{"AGENTS.md": "declared, and resolved onto the instance"}`),
		},
	})
	inst.Templates = map[string]string{bootdir.AgentsFileName: markers.String()}
	inst.Sections = map[string]string{"note": "## Note\n\nnothing secret"}
	inst.Values = map[string]string{
		"binding": "eng", "profile": "engineer", "provider": "claude",
		"model": "opus", "scope": "/repo", "session": "s1",
	}

	files, err := bootdir.Render(inst)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var sawTemplate, sawMCP bool
	for _, f := range files {
		switch f.Path {
		case bootdir.AgentsFileName:
			sawTemplate = true
			if strings.Contains(string(f.Content), secret) {
				t.Errorf("%s carries a manifest value:\n%s", f.Path, f.Content)
			}
			if strings.Contains(string(f.Content), "vanta") {
				t.Errorf("%s names a declared MCP server:\n%s", f.Path, f.Content)
			}
		case inst.Layout.MCP.RelPath:
			sawMCP = true
			if !strings.Contains(string(f.Content), secret) {
				t.Errorf("%s did not carry the declared server env, so this test proves nothing:\n%s",
					f.Path, f.Content)
			}
		}
	}
	if !sawTemplate {
		t.Fatal("no template was rendered, so the assertion above never ran")
	}
	if !sawMCP {
		t.Fatal("no MCP config was rendered, so the value under test never reached the boot directory")
	}
}

// TestAMarkerNamingAManifestKeyIsRefused is the other half: a template that
// tries to reach a manifest key does not silently render the marker's own text
// into an agent's context. The cairn: prefix is this package's namespace, so a
// marker inside it that cairn does not understand is a mistake rather than
// someone else's syntax.
func TestAMarkerNamingAManifestKeyIsRefused(t *testing.T) {
	for _, marker := range []string{
		"<!-- cairn:value mcp -->",
		"<!-- cairn:value settings -->",
		"<!-- cairn:spec mcp -->",
		"<!-- cairn:value -->",
	} {
		if _, err := bootdir.Substitute(marker, nil, nil); err == nil {
			t.Errorf("Substitute(%q) was accepted, want a refusal", marker)
		}
	}
}

// instance builds a claude instance around a resolved profile.
func instance(t *testing.T, resolved *profile.Resolved) *bootdir.Instance {
	t.Helper()
	layout, err := bootdir.LayoutFor(profile.ProviderClaude)
	if err != nil {
		t.Fatalf("claude layout: %v", err)
	}
	return &bootdir.Instance{Dir: t.TempDir(), Layout: layout, Profile: resolved}
}
