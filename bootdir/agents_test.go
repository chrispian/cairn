package bootdir

import (
	"strings"
	"testing"

	"github.com/chrispian/cairn/profile"
)

// TestAgentsRendersDeclaredFieldsOnly is the whole contract of the instruction
// file as four complete renderings. Every line of every want below is a field
// somebody declared: cairn contributes the heading marker, the section
// heading, and the field labels, and not one sentence of prose. A change that
// adds an authored paragraph fails all four of these at once, which is the
// point of asserting the entire file rather than a substring of it.
func TestAgentsRendersDeclaredFieldsOnly(t *testing.T) {
	tests := []struct {
		name     string
		resolved profile.Resolved
		scope    string
		want     string
	}{
		{
			name: "every field declared",
			resolved: profile.Resolved{
				ID:          "reviewer",
				Name:        "Reviewer",
				Description: "Reviews changes before they land.",
				Provider:    profile.ProviderClaude,
				Model:       "claude-sonnet-4-5",
				Body:        "You read diffs.\n\nYou say what is wrong with them.\n",
			},
			scope: "/Users/chrispian/dev/projects/cairn",
			want: "# Reviewer\n" +
				"\n" +
				"Reviews changes before they land.\n" +
				"\n" +
				"You read diffs.\n" +
				"\n" +
				"You say what is wrong with them.\n" +
				"\n" +
				"## Profile\n" +
				"\n" +
				"- profile: reviewer\n" +
				"- name: Reviewer\n" +
				"- provider: claude\n" +
				"- model: claude-sonnet-4-5\n" +
				"- scope: /Users/chrispian/dev/projects/cairn\n",
		},
		{
			name:     "a name and nothing else",
			resolved: profile.Resolved{Name: "Solo"},
			want: "# Solo\n" +
				"\n" +
				"## Profile\n" +
				"\n" +
				"- name: Solo\n",
		},
		{
			name:     "a body and nothing else",
			resolved: profile.Resolved{Body: "Just the prose the operator wrote.\n"},
			want:     "Just the prose the operator wrote.\n",
		},
		{
			name:     "nothing at all",
			resolved: profile.Resolved{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := testInstance(t, tt.resolved)
			inst.Scope = tt.scope

			files, err := RenderAgents(inst)
			if err != nil {
				t.Fatalf("RenderAgents(): %v", err)
			}
			if len(files) != 1 {
				t.Fatalf("RenderAgents() rendered %v, want exactly %s", filePaths(files), AgentsFileName)
			}
			if files[0].Path != AgentsFileName {
				t.Errorf("rendered at %q, want %q", files[0].Path, AgentsFileName)
			}
			if got := string(files[0].Content); got != tt.want {
				t.Errorf("%s is\n%q\nwant\n%q", AgentsFileName, got, tt.want)
			}
		})
	}
}

// TestAgentsFallsBackToTheProfileID keeps a profile with no display name from
// rendering a heading a reader cannot look anything up by.
func TestAgentsFallsBackToTheProfileID(t *testing.T) {
	inst := testInstance(t, profile.Resolved{ID: "base.reviewer"})

	files, err := RenderAgents(inst)
	if err != nil {
		t.Fatalf("RenderAgents(): %v", err)
	}
	want := "# base.reviewer\n\n## Profile\n\n- profile: base.reviewer\n"
	if got := string(files[0].Content); got != want {
		t.Errorf("%s is\n%q\nwant\n%q", AgentsFileName, got, want)
	}
}

// TestAgentsBodyIsVerbatim states the rule the body is held to: cairn joins
// blocks with one blank line and touches nothing else, so an indented first
// line — a code block — survives, and so does the interior spacing.
func TestAgentsBodyIsVerbatim(t *testing.T) {
	body := "    indented, which is a code block\n\ncontinued\ttext   \n\n\n"
	inst := testInstance(t, profile.Resolved{Body: body})

	files, err := RenderAgents(inst)
	if err != nil {
		t.Fatalf("RenderAgents(): %v", err)
	}
	want := "    indented, which is a code block\n\ncontinued\ttext   \n"
	if got := string(files[0].Content); got != want {
		t.Errorf("%s is\n%q\nwant\n%q", AgentsFileName, got, want)
	}
}

// TestAgentsIsAlwaysRendered is the reason the empty case above renders a file
// with no bytes rather than no file. A boot directory whose instruction file
// is missing looks like a render that stopped halfway, and the harness reading
// it says nothing.
func TestAgentsIsAlwaysRendered(t *testing.T) {
	files, err := Render(testInstance(t, profile.Resolved{}))
	if err != nil {
		t.Fatalf("Render(): %v", err)
	}
	if got := fileByPath(t, files, AgentsFileName); len(got.Content) != 0 {
		t.Errorf("%s holds %q, want no bytes", AgentsFileName, got.Content)
	}
}

// TestAgentsRendersNoCairnAuthoredVocabulary is a tripwire for the open item
// in docs/plan.md §9: the prior tree wrote cairn's own editorial voice into
// agent contracts, and until that prose is reviewed and rewritten by its
// operator, none of it is rendered from the binary. The words below are the
// ones that voice reached for. A profile that declares them still gets them —
// they are checked against a rendering of a profile that declares nothing.
func TestAgentsRendersNoCairnAuthoredVocabulary(t *testing.T) {
	inst := testInstance(t, profile.Resolved{
		ID:       "quiet",
		Name:     "Quiet",
		Provider: profile.ProviderClaude,
		Model:    "claude-sonnet-4-5",
	})
	inst.Scope = "/tmp/scope"

	files, err := RenderAgents(inst)
	if err != nil {
		t.Fatalf("RenderAgents(): %v", err)
	}
	rendered := strings.ToLower(string(files[0].Content))
	for _, word := range []string{
		"escalate", "authority", "authoritative", "reports_to", "precedence",
		"boot.md", "skill", "mcp", "dispatch", "override", "you ", "your ",
	} {
		if strings.Contains(rendered, word) {
			t.Errorf("%s holds %q, which no field of this profile declares", AgentsFileName, word)
		}
	}
}
