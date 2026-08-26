package bootdir_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/profile"
)

// TestTheInstructionFileCarriesNoManifestValue is a regression guard on the
// one place a rendered field would do real damage.
//
// AGENTS.md exists to be read into a model's context, and the boot directory
// holding it is handed to a harness. spec.mcp[].env is where an MCP server's
// API keys live. A "render every declared field" instruction file — the
// obvious next step from the block it does render — would write those keys
// into the one artifact whose whole purpose is to be read aloud.
//
// The rule is therefore narrower than "do not render secrets", which nobody
// can enforce, and is instead structural: the instruction file renders the
// profile's scalar columns and its body, and never a manifest value. The
// manifest is where the operator puts things meant for a program; a program
// reads .mcp.json.
func TestTheInstructionFileCarriesNoManifestValue(t *testing.T) {
	const secret = "sk-live-do-not-render-me"

	inst := instance(t, &profile.Resolved{
		ID:       "engineer",
		Name:     "Engineer",
		Provider: profile.ProviderClaude,
		Model:    "opus",
		Body:     "engineer persona",
		Spec: profile.Spec{
			"mcp": json.RawMessage(`[{"name":"vanta","command":"vanta-mcp",` +
				`"env":{"VANTA_TOKEN":"` + secret + `"},"args":["--token","` + secret + `"]}]`),
			"settings": json.RawMessage(`{"env":{"ANTHROPIC_API_KEY":"` + secret + `"}}`),
			"files":    json.RawMessage(`{"notes/creds.md":"` + secret + `"}`),
		},
	})

	files, err := bootdir.Render(inst)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var sawInstruction, sawMCP bool
	for _, f := range files {
		switch f.Path {
		case inst.Layout.Agents.RelPath:
			sawInstruction = true
			if strings.Contains(string(f.Content), secret) {
				t.Errorf("%s carries a manifest value:\n%s", f.Path, f.Content)
			}
			if strings.Contains(string(f.Content), "vanta") {
				t.Errorf("%s names a declared MCP server; it renders scalar columns only:\n%s", f.Path, f.Content)
			}
		case inst.Layout.MCP.RelPath:
			sawMCP = true
			if !strings.Contains(string(f.Content), secret) {
				t.Errorf("%s did not carry the declared server env, so this test proves nothing:\n%s",
					f.Path, f.Content)
			}
		}
	}
	if !sawInstruction {
		t.Fatal("no instruction file was rendered, so the assertion above never ran")
	}
	if !sawMCP {
		t.Fatal("no MCP config was rendered, so the value under test never reached the boot directory")
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
