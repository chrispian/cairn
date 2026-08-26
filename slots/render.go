package slots

import (
	"fmt"
	"strings"

	"github.com/hollis-labs/agentkit/agentcontext"
)

// UnavailableMarker opens the line a failed slot renders in place of the
// content it did not produce. It is a fixed string so that a reader — or a
// grep — can find every section of a boot file that reports a failure.
const UnavailableMarker = "**Unavailable.**"

// MarkFailures is an [agentcontext.Renderer] that renders a one-line
// provenance for a slot whose resolver failed, then delegates everything else
// to the renderer it wraps.
//
// It exists because the library's own renderer emits a bare section heading
// for a slot that resolved empty and a bare section heading for a slot that
// failed, and in the rendered file those are the same bytes. The two mean
// opposite things: "the memory service returned nothing" and "the memory
// service was unreachable" lead a reader to opposite conclusions, and the one
// that is a failure is the one that reads as fact.
//
// The failure is already reported to the operator on stderr. This is for the
// agent, which reads the file and not the terminal.
//
// Substituting content and delegating is deliberate. Ordering, budget
// enforcement, truncation and drop accounting all stay the wrapped renderer's,
// so the failure line is budgeted like any other content rather than escaping
// a limit the operator set.
type MarkFailures struct {
	// Inner renders the marked results. Nil means
	// [agentcontext.DefaultRenderer].
	Inner agentcontext.Renderer
}

// Render implements [agentcontext.Renderer].
func (m MarkFailures) Render(slots []agentcontext.SlotResult, limits agentcontext.Limits) (string, agentcontext.LimitsApplied) {
	marked := make([]agentcontext.SlotResult, len(slots))
	copy(marked, slots)
	for i := range marked {
		if marked[i].Err == nil {
			continue
		}
		// A resolver that failed and still produced content keeps it: the
		// content is what it managed, and the marker goes above it.
		line := FailureLine(marked[i].Err)
		if marked[i].Content == "" {
			marked[i].Content = line
			continue
		}
		marked[i].Content = line + "\n" + marked[i].Content
	}

	inner := m.Inner
	if inner == nil {
		inner = agentcontext.DefaultRenderer{}
	}
	return inner.Render(marked, limits)
}

// FailureLine returns the one line a failed slot renders: the marker, then the
// resolver's own error.
//
// It states what happened and stops. It does not tell the reader what to
// conclude or what to do about it — a boot file carries what a profile
// declared, and an instruction cairn invented is one nobody can correct.
// Newlines in the error are folded so that the line stays one line.
func FailureLine(err error) string {
	if err == nil {
		return ""
	}
	return UnavailableMarker + " " + strings.Join(strings.Fields(err.Error()), " ")
}

// Ensure MarkFailures satisfies the renderer contract at compile time.
var _ agentcontext.Renderer = MarkFailures{}

// wiredKinds are the slot kinds a manifest may declare and expect to resolve,
// in the order a diagnostic lists them.
//
// It is the kinds [resolvers.Default] wires, not the kinds the library
// defines. skill_index is a ninth thing the library knows and this wiring does
// not carry, so naming it in a diagnostic would send an operator to write a
// slot that then fails. A test pins the list against the resolver map, so a
// kind the library starts shipping a default resolver for turns up as a
// failing test rather than as a diagnostic that has quietly gone stale.
var wiredKinds = []agentcontext.SlotSourceKind{
	agentcontext.SlotSourceKindStaticFile,
	agentcontext.SlotSourceKindStaticDir,
	agentcontext.SlotSourceKindInline,
	agentcontext.SlotSourceKindCmd,
	agentcontext.SlotSourceKindHTTPText,
	agentcontext.SlotSourceKindHTTPJSON,
	agentcontext.SlotSourceKindRoleSummary,
}

// kindList renders wiredKinds for a diagnostic.
func kindList() string {
	quoted := make([]string, 0, len(wiredKinds))
	for _, k := range wiredKinds {
		quoted = append(quoted, fmt.Sprintf("%q", string(k)))
	}
	return strings.Join(quoted, ", ")
}
