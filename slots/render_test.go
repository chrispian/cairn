package slots_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chrispian/cairn/profile"
	"github.com/chrispian/cairn/slots"
	"github.com/hollis-labs/agentkit/agentcontext"
	"github.com/hollis-labs/agentkit/agentcontext/resolvers"
)

// TestAFailedSlotDoesNotReadAsAnEmptyOne is the defect this package's renderer
// exists for, asserted the way it was found: two slots side by side, one that
// resolved empty and one that failed, rendered into one file.
//
// The library's own renderer emits a bare section heading for both, and in the
// file those are the same bytes. "The task service returned nothing" and "the
// task service was unreachable" lead a reader to opposite conclusions, and the
// one that is a failure is the one that reads as fact.
func TestAFailedSlotDoesNotReadAsAnEmptyOne(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-written.md")
	spec := manifest(t, slotted(`[
	  {"name":"tasks",  "section":"## Tasks",  "source":{"kind":"cmd","cmd":{"run":"true"}}},
	  {"name":"repo",   "section":"## Repo",   "source":{"kind":"inline","inline":{"content":"branch=main"}}},
	  {"name":"memory", "section":"## Memory", "source":{"kind":"static_file","static_file":{"path":"`+missing+`"}}}
	]`))

	res, err := slots.Assemble(context.Background(), spec, slots.Options{})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	tasks := section(t, res.Rendered, "## Tasks")
	memory := section(t, res.Rendered, "## Memory")
	if tasks == memory {
		t.Fatalf("the slot that resolved empty and the slot that failed rendered the same bytes %q", tasks)
	}
	if tasks != "" {
		t.Errorf("the slot that resolved empty rendered %q, want nothing under its heading", tasks)
	}
	if !strings.HasPrefix(memory, slots.UnavailableMarker) {
		t.Errorf("the slot that failed rendered %q, want it to open with %q", memory, slots.UnavailableMarker)
	}
	if !strings.Contains(memory, "never-written.md") {
		t.Errorf("the failure line does not say what failed:\n%s", memory)
	}
	if strings.Count(memory, "\n") != 0 {
		t.Errorf("the failure line is not one line:\n%s", memory)
	}

	// The slot that succeeded is untouched, and the failure did not stop the
	// assembly.
	if got := section(t, res.Rendered, "## Repo"); got != "branch=main" {
		t.Errorf("the slot that resolved rendered %q", got)
	}
}

// TestMarkFailuresKeepsWhatAResolverManaged covers a resolver that failed
// after producing something. The content is what it managed and is kept; the
// marker goes above it rather than replacing it.
func TestMarkFailuresKeepsWhatAResolverManaged(t *testing.T) {
	rendered, _ := slots.MarkFailures{}.Render([]agentcontext.SlotResult{
		{Name: "partial", Section: "## Partial", Content: "half of it", Err: errors.New("connection reset")},
	}, agentcontext.Limits{})

	if !strings.Contains(rendered, "half of it") {
		t.Errorf("what the resolver managed was dropped:\n%s", rendered)
	}
	if !strings.Contains(rendered, "connection reset") {
		t.Errorf("the failure was not reported:\n%s", rendered)
	}
	if strings.Index(rendered, slots.UnavailableMarker) > strings.Index(rendered, "half of it") {
		t.Errorf("the marker is below the content it qualifies:\n%s", rendered)
	}
}

// TestMarkFailuresDelegatesTheBudget covers the reason this renderer wraps the
// library's rather than replacing it: ordering, truncation and drop accounting
// stay the library's, so a failure line is budgeted like any other content
// instead of escaping a limit the operator set.
func TestMarkFailuresDelegatesTheBudget(t *testing.T) {
	in := []agentcontext.SlotResult{
		{Name: "first", Section: "## First", Content: strings.Repeat("x", 40)},
		{Name: "second", Section: "## Second", Err: errors.New("unreachable")},
	}
	rendered, applied := slots.MarkFailures{}.Render(in, agentcontext.Limits{MaxBytes: 30})

	if int64(len(rendered)) > 30 {
		t.Errorf("rendered %d bytes past a 30-byte budget:\n%s", len(rendered), rendered)
	}
	if len(applied.TruncatedSlots) == 0 && len(applied.DroppedSlots) == 0 {
		t.Error("the budget was applied without recording a truncation or a drop")
	}
}

// TestMarkFailuresIsAPassThroughWhenNothingFailed pins that this renderer
// changes nothing it does not have to.
func TestMarkFailuresIsAPassThroughWhenNothingFailed(t *testing.T) {
	in := []agentcontext.SlotResult{
		{Name: "a", Section: "## A", Content: "one"},
		{Name: "b", Section: "## B"},
	}
	want, wantApplied := agentcontext.DefaultRenderer{}.Render(in, agentcontext.Limits{})
	got, gotApplied := slots.MarkFailures{}.Render(in, agentcontext.Limits{})
	if got != want {
		t.Errorf("rendered %q, want the library's own %q", got, want)
	}
	if !reflect.DeepEqual(gotApplied, wantApplied) {
		t.Errorf("applied %+v, want %+v", gotApplied, wantApplied)
	}
}

// TestMarkFailuresDoesNotMutateTheCaller'sSlots would be a mouthful, so:
// TestMarkFailuresCopiesBeforeMarking covers the results the dispatcher hands
// in, which it goes on to return on ContextResult.Slots. Rewriting Content
// there would make the per-slot record disagree with what the resolver did.
func TestMarkFailuresCopiesBeforeMarking(t *testing.T) {
	in := []agentcontext.SlotResult{{Name: "memory", Section: "## Memory", Err: errors.New("unreachable")}}
	slots.MarkFailures{}.Render(in, agentcontext.Limits{})
	if in[0].Content != "" {
		t.Errorf("the caller's slot was rewritten to %q", in[0].Content)
	}
}

// TestTheKindDiagnosticNamesTheKey covers the one mistake this manifest key
// invites. SlotSource's kind field is json:"kind" and yaml:"type"; every boot
// profile in the portfolio is YAML and says type:, and a cairn manifest is
// JSON. The library never sees the manifest, so it can only say the kind is
// unknown — which points at nothing.
func TestTheKindDiagnosticNamesTheKey(t *testing.T) {
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
			_, err := slots.Assemble(context.Background(), manifest(t, slotted(tc.manifest)), slots.Options{})
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
	_, err := slots.Assemble(context.Background(),
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

// section returns the body rendered under heading, or fails if the heading is
// absent — so a test cannot pass by asserting against a section that was never
// emitted.
//
// The library separates sections with one blank line and emits a heading with
// no body for a slot that resolved empty, which is exactly the shape under
// test, so the split is on the separator and the match is on the first line.
func section(t *testing.T, rendered, heading string) string {
	t.Helper()
	for _, block := range strings.Split(rendered, "\n\n") {
		head, body, _ := strings.Cut(block, "\n")
		if head == heading {
			return strings.TrimRight(body, "\n")
		}
	}
	t.Fatalf("no %q section in:\n%s", heading, rendered)
	return ""
}
