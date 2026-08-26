package slots_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/chrispian/cairn/profile"
	"github.com/chrispian/cairn/slots"
	"github.com/hollis-labs/agentkit/agentcontext"
	"github.com/hollis-labs/agentkit/agentcontext/resolvers"
)

// TestASlotThatProducedNothingRendersNothingAtAll is docs/plan.md §5 asserted
// on the bytes of the whole rendering rather than on the absence of a marker.
//
// Three slots go in: one that resolves empty, one that resolves, one that
// fails. One section comes out. The assertion is deliberately on the entire
// string, because a test that only looked for the absence of the old
// "**Unavailable.**" line would pass on a rendering that still emitted a bare
// "## Memory" heading — and a heading with nothing under it is the shape this
// renderer exists to remove.
func TestASlotThatProducedNothingRendersNothingAtAll(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "never-written.md")
	spec := manifest(t, slotted(`[
	  {"name":"tasks",  "section":"## Tasks",  "source":{"kind":"cmd","cmd":{"run":"true"}}},
	  {"name":"repo",   "section":"## Repo",   "source":{"kind":"inline","inline":{"content":"branch=main"}}},
	  {"name":"memory", "section":"## Memory", "source":{"kind":"static_file","static_file":{"path":"`+missing+`"}}}
	]`))

	res, err := slots.Assemble(t.Context(), spec, slots.Options{})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	if want := "## Repo\nbranch=main"; res.Rendered != want {
		t.Errorf("the assembly rendered\n%q\nwant only the slot that resolved\n%q", res.Rendered, want)
	}
	// Named individually so a failure says which of the two cases regressed,
	// and so the old marker cannot creep back in under a new spelling.
	for _, absent := range []string{"## Tasks", "## Memory", "never-written.md", "Unavailable"} {
		if strings.Contains(res.Rendered, absent) {
			t.Errorf("the rendering carries %q:\n%s", absent, res.Rendered)
		}
	}

	// The failure is not swallowed, it is redirected: it stays on the result
	// the caller was handed, and `cairn boot` reports it on stderr. It is the
	// operator's to see, and the operator does not read boot.md.
	var failed []string
	for _, s := range res.Slots {
		if s.Err != nil {
			failed = append(failed, s.Name)
		}
	}
	if !slices.Equal(failed, []string{"memory"}) {
		t.Errorf("the result records %v as failed, want just the memory slot", failed)
	}
}

// TestDropUnresolvedDropsWhatAFailedResolverManaged covers a resolver that
// errored after producing something. An earlier revision kept the partial
// content under a marker that said not to trust it. Without the marker there
// is nothing to say so, and half an answer presented as the whole one is worse
// than no section: the agent has no way to tell it is reading a fragment.
func TestDropUnresolvedDropsWhatAFailedResolverManaged(t *testing.T) {
	t.Parallel()

	rendered, _ := slots.DropUnresolved{}.Render([]agentcontext.SlotResult{
		{Name: "partial", Section: "## Partial", Content: "half of it", Err: errors.New("connection reset")},
		{Name: "whole", Section: "## Whole", Content: "all of it"},
	}, agentcontext.Limits{})

	if want := "## Whole\nall of it"; rendered != want {
		t.Errorf("rendered %q, want %q", rendered, want)
	}
}

// TestDropUnresolvedTreatsWhitespaceAsEmpty covers the resolver that succeeded
// and returned a newline. A section holding one blank line is a heading with
// nothing under it wearing a disguise.
func TestDropUnresolvedTreatsWhitespaceAsEmpty(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"", "\n", "   ", " \n\t\n "} {
		rendered, _ := slots.DropUnresolved{}.Render([]agentcontext.SlotResult{
			{Name: "blank", Section: "## Blank", Content: content},
		}, agentcontext.Limits{})
		if rendered != "" {
			t.Errorf("a slot holding %q rendered %q, want nothing", content, rendered)
		}
	}
}

// TestDropUnresolvedDelegatesTheBudget covers the reason this renderer wraps
// the library's rather than replacing it: ordering, truncation and drop
// accounting stay the library's, so what survives the filter is budgeted like
// any other content instead of escaping a limit the operator set.
func TestDropUnresolvedDelegatesTheBudget(t *testing.T) {
	t.Parallel()

	in := []agentcontext.SlotResult{
		{Name: "first", Section: "## First", Content: strings.Repeat("x", 40)},
		{Name: "second", Section: "## Second", Content: strings.Repeat("y", 40)},
	}
	rendered, applied := slots.DropUnresolved{}.Render(in, agentcontext.Limits{MaxBytes: 30})

	if int64(len(rendered)) > 30 {
		t.Errorf("rendered %d bytes past a 30-byte budget:\n%s", len(rendered), rendered)
	}
	if len(applied.TruncatedSlots) == 0 && len(applied.DroppedSlots) == 0 {
		t.Error("the budget was applied without recording a truncation or a drop")
	}
}

// TestDropUnresolvedIsAPassThroughWhenEverySlotResolved pins that this
// renderer changes nothing it does not have to.
func TestDropUnresolvedIsAPassThroughWhenEverySlotResolved(t *testing.T) {
	t.Parallel()

	in := []agentcontext.SlotResult{
		{Name: "a", Section: "## A", Content: "one"},
		{Name: "b", Section: "## B", Content: "two"},
	}
	want, wantApplied := agentcontext.DefaultRenderer{}.Render(in, agentcontext.Limits{})
	got, gotApplied := slots.DropUnresolved{}.Render(in, agentcontext.Limits{})
	if got != want {
		t.Errorf("rendered %q, want the library's own %q", got, want)
	}
	if !reflect.DeepEqual(gotApplied, wantApplied) {
		t.Errorf("applied %+v, want %+v", gotApplied, wantApplied)
	}
}

// TestDropUnresolvedLeavesTheCallersResultsAlone covers the slice the
// dispatcher hands in, which it goes on to return on ContextResult.Slots.
// Filtering in place there would take the failure away from the caller that
// has to report it.
func TestDropUnresolvedLeavesTheCallersResultsAlone(t *testing.T) {
	t.Parallel()

	unreachable := errors.New("unreachable")
	in := []agentcontext.SlotResult{
		{Name: "memory", Section: "## Memory", Content: "half", Err: unreachable},
		{Name: "repo", Section: "## Repo", Content: "branch=main"},
	}
	slots.DropUnresolved{}.Render(in, agentcontext.Limits{})

	if len(in) != 2 {
		t.Fatalf("the caller's slice is now %d long, want 2", len(in))
	}
	if !errors.Is(in[0].Err, unreachable) || in[0].Content != "half" {
		t.Errorf("the caller's failed slot was rewritten to %+v", in[0])
	}
}

// TestTheKindDiagnosticNamesTheKey covers the one mistake this manifest key
// invites. SlotSource's kind field is json:"kind" and yaml:"type"; every boot
// profile in the portfolio is YAML and says type:, and a cairn manifest is
// JSON. The library never sees the manifest, so it can only say the kind is
// unknown — which points at nothing.
func TestTheKindDiagnosticNamesTheKey(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ manifest, wants string }{
		"a slot copied out of a YAML profile": {
			manifest: `[{"name":"note","source":{"type":"inline","inline":{"content":"x"}}}]`,
			wants:    `"kind"`,
		},
		"a slot with no kind at all": {
			manifest: `[{"name":"note","source":{"inline":{"content":"x"}}}]`,
			wants:    `declares no "kind"`,
		},
		"a kind that is not a kind": {
			manifest: `[{"name":"note","source":{"kind":"sqlite"}}]`,
			wants:    `"sqlite"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := slots.Assemble(t.Context(), manifest(t, slotted(tc.manifest)), slots.Options{})
			if !errors.Is(err, slots.ErrSlotKind) {
				t.Fatalf("assemble = %v, want ErrSlotKind", err)
			}
			if !strings.Contains(err.Error(), `slot "note"`) {
				t.Errorf("the error does not name the slot: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("the error does not carry %q: %v", tc.wants, err)
			}
		})
	}
}

// TestTheKindDiagnosticListsWhatIsActuallyWired pins the diagnostic's list of
// kinds against the resolver map rather than against the library's constants.
//
// The library defines a skill_index kind that resolvers.Default does not wire,
// so naming it would send an operator to write a slot that then fails. If the
// library starts shipping a default resolver for a kind, this fails here
// instead of leaving the message quietly stale.
func TestTheKindDiagnosticListsWhatIsActuallyWired(t *testing.T) {
	t.Parallel()

	_, err := slots.Assemble(t.Context(),
		manifest(t, slotted(`[{"name":"note","source":{"kind":"sqlite"}}]`)), slots.Options{})
	if err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	for kind := range resolvers.Default() {
		if !strings.Contains(err.Error(), fmt.Sprintf("%q", string(kind))) {
			t.Errorf("kind %q is wired but the diagnostic does not list it: %v", kind, err)
		}
	}
	if strings.Contains(err.Error(), `"skill_index"`) {
		t.Error("the diagnostic lists skill_index, which resolvers.Default does not wire")
	}
}

// slotted wraps a slots array in the manifest object an operator stores.
func slotted(raw string) string {
	return `{"` + profile.SpecKeySlots + `": ` + raw + `}`
}
