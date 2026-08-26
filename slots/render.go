package slots

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hollis-labs/agentkit/agentcontext"
)

// DropUnresolved is an [agentcontext.Renderer] that removes every slot which
// produced no content — whether because its resolver failed or because it
// resolved to nothing — and delegates what remains to the renderer it wraps.
//
// The library's own renderer emits a bare section heading for both cases, so a
// boot file ends up carrying headings with nothing under them. Cairn omits the
// section instead, which is also what Tether's boot template does.
//
// An earlier revision rendered a failure marker in place of the missing
// content, to keep "the service returned nothing" distinguishable from "the
// service was unreachable". That was wrong on both counts. It is cairn
// authoring prose into the agent's context, which this package has no standing
// to do — a boot file carries what a profile declared, and a sentence cairn
// invented is one nobody can correct. And the distinction it preserved is not
// the agent's to act on: current truth comes from the tools, not from a
// snapshot, so an agent that needs the data asks for it.
//
// The failure is not swallowed. It stays on the [agentcontext.SlotResult] the
// caller receives, and the caller reports it on stderr — to the operator, who
// can fix it. It simply does not reach the file.
//
// Filtering and delegating is deliberate. Ordering, budget enforcement,
// truncation and drop accounting all stay the wrapped renderer's.
type DropUnresolved struct {
	// Inner renders the surviving results. Nil means
	// [agentcontext.DefaultRenderer].
	Inner agentcontext.Renderer
}

// Render implements [agentcontext.Renderer].
//
// A slot survives when its resolver did not fail and it produced content that
// is not entirely whitespace. Partial content from a failed resolver is
// dropped with the rest of it: what a resolver managed before failing is not
// something to hand an agent as though it were the whole answer.
func (d DropUnresolved) Render(slots []agentcontext.SlotResult, limits agentcontext.Limits) (string, agentcontext.LimitsApplied) {
	kept := make([]agentcontext.SlotResult, 0, len(slots))
	for _, s := range slots {
		if s.Err != nil || strings.TrimSpace(s.Content) == "" {
			continue
		}
		kept = append(kept, s)
	}

	inner := d.Inner
	if inner == nil {
		inner = agentcontext.DefaultRenderer{}
	}
	return inner.Render(kept, limits)
}

// Ensure DropUnresolved satisfies the renderer contract at compile time.
var _ agentcontext.Renderer = DropUnresolved{}

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

// kindDiagnostic reports a source whose declared kind is missing or is not one
// the library recognizes, in a message that names where the source was
// written. A valid kind returns nil.
//
// It is shared because the mistake is shared: a slot and a files entry both
// carry an [agentcontext.SlotSource], and an operator copying either out of a
// YAML boot profile makes the same substitution. named is what the caller
// calls the offending source — a slot by its name, a files entry by its path.
// source is the raw JSON object the manifest held for it, or nil when the
// manifest could not be re-read; it is consulted only to spot the "type" key,
// so a nil map costs the hint and nothing else.
func kindDiagnostic(named string, kind agentcontext.SlotSourceKind, source map[string]json.RawMessage) error {
	if kind.Valid() {
		return nil
	}
	if kind != "" {
		return fmt.Errorf("%w: %s declares kind %q; the kinds are %s",
			ErrSlotKind, named, kind, kindList())
	}
	if wrong, ok := source["type"]; ok {
		return fmt.Errorf(
			"%w: %s declares no \"kind\", but declares a \"type\" of %s — "+
				"a source is written in YAML as `type:` and in a cairn manifest, which is JSON, as \"kind\"",
			ErrSlotKind, named, wrong)
	}
	return fmt.Errorf("%w: %s declares no \"kind\"; the kinds are %s",
		ErrSlotKind, named, kindList())
}

// kindList renders wiredKinds for a diagnostic.
func kindList() string {
	quoted := make([]string, 0, len(wiredKinds))
	for _, k := range wiredKinds {
		quoted = append(quoted, fmt.Sprintf("%q", string(k)))
	}
	return strings.Join(quoted, ", ")
}
