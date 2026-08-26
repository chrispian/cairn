// Package slots resolves a profile manifest's slot declarations into the
// content a boot directory's boot.md is written from.
//
// The resolving is not Cairn's. agentkit's agentcontext owns the slot kinds,
// the resolvers, the per-slot timeouts and headers, the byte and token
// budgets, the per-slot provenance, and the determinism contract — including
// the rule that a non-required slot whose resolver fails records the failure
// on its result instead of blocking the assembly. What lives here is the
// wiring between a [profile.Spec] and that library, and deliberately nothing
// more: three portfolio libraries each grew their own context pipeline before
// agentcontext was extracted to end exactly that.
package slots

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chrispian/cairn/profile"
	"github.com/hollis-labs/agentkit/agentcontext"
	"github.com/hollis-labs/agentkit/agentcontext/resolvers"
)

// ErrSlotKind reports a slot whose declared kind is missing or is not one the
// library recognizes. It exists so the failure names the slot and the key,
// which the library's own sentinel cannot: it is reached from a request that
// no longer knows how the manifest was spelled.
var ErrSlotKind = errors.New("invalid slot kind")

// Options carries what varies between two materializations.
type Options struct {
	// Workdir is what workdir-relative slot paths resolve against — the
	// instance's scope directory, or empty for none.
	Workdir string

	// Limits caps the assembled output. Zero means unlimited.
	Limits agentcontext.Limits

	// Provenance is the request-level attribution threaded onto the result.
	Provenance agentcontext.ProvenanceInput

	// Provider overrides the assembled ContextProvider. Nil means the default
	// wiring: agentcontext.NewProvider(resolvers.Default(), MarkFailures{}).
	Provider agentcontext.ContextProvider
}

// Assemble resolves a profile manifest's slots into the boot file's content.
//
// A manifest that declares no slots — the key absent, null, or an empty list —
// returns a nil result and a nil error. That is not a failure: it means there
// is no boot file to write, and the caller renders none.
//
// Otherwise the declared slots are handed to the provider exactly as the
// manifest carried them, in declared order. Nothing here filters, re-orders,
// validates or normalises them; [agentcontext.ContextRequest.Validate] does
// that inside Assemble, and doing it twice would only let the two answers
// drift. Errors are wrapped to name the manifest they came from and stay
// [errors.Is]-comparable against the library's sentinels.
func Assemble(ctx context.Context, spec profile.Spec, opts Options) (*agentcontext.ContextResult, error) {
	declared, err := spec.Slots()
	if err != nil {
		return nil, wrap(err)
	}
	if len(declared) == 0 {
		return nil, nil
	}
	if err := checkKinds(spec[profile.SpecKeySlots], declared); err != nil {
		return nil, wrap(err)
	}

	provider := opts.Provider
	if provider == nil {
		if provider, err = defaultProvider(); err != nil {
			return nil, wrap(err)
		}
	}

	result, err := provider.Assemble(ctx, agentcontext.ContextRequest{
		Slots:      declared,
		Limits:     opts.Limits,
		Workdir:    opts.Workdir,
		Provenance: opts.Provenance,
	})
	if err != nil {
		return nil, wrap(err)
	}
	return result, nil
}

// checkKinds reports a slot whose kind the library will refuse, naming the
// slot and the key rather than leaving the caller with the library's bare
// "unknown slot kind".
//
// The mistake it exists for is specific and near-certain to be made.
// [agentcontext.SlotSource]'s kind field is tagged `json:"kind"` and
// `yaml:"type"`. Every boot profile in the portfolio is YAML and writes
// `type:`; a cairn manifest is JSON and must write "kind". Copying a slot out
// of a working YAML profile therefore produces a slot with no kind at all, and
// the library — which never sees the manifest — can only say that the kind is
// unknown.
func checkKinds(raw json.RawMessage, declared []agentcontext.SlotSpec) error {
	var sources []struct {
		Source map[string]json.RawMessage `json:"source"`
	}
	// A manifest that will not re-decode is not this check's problem: it
	// decoded once already to produce declared, and the kind check below still
	// runs without the raw source objects.
	_ = json.Unmarshal(raw, &sources)

	for i, slot := range declared {
		if slot.Source.Kind.Valid() {
			continue
		}
		named := fmt.Sprintf("slot %d", i)
		if slot.Name != "" {
			named = fmt.Sprintf("slot %q", slot.Name)
		}
		if slot.Source.Kind != "" {
			return fmt.Errorf("%w: %s declares kind %q; the kinds are %s",
				ErrSlotKind, named, slot.Source.Kind, kindList())
		}
		if i < len(sources) {
			if wrong, ok := sources[i].Source["type"]; ok {
				return fmt.Errorf(
					"%w: %s declares no \"kind\", but its source has a \"type\" of %s — "+
						"a slot is written in YAML as `type:` and in a cairn manifest, which is JSON, as \"kind\"",
					ErrSlotKind, named, wrong)
			}
		}
		return fmt.Errorf("%w: %s declares no \"kind\"; the kinds are %s",
			ErrSlotKind, named, kindList())
	}
	return nil
}

// defaultProvider builds the provider [Assemble] uses when [Options] names
// none: the seven app-neutral resolvers, and [MarkFailures] over the library's
// own renderer so that a slot which failed does not read as a slot which was
// empty.
//
// resolvers.WithSkillIndex is deliberately not wired. Per the MVP plan the
// eighth kind gets added when a profile needs one, and none does yet; until
// then a manifest declaring a skill_index slot fails with
// [agentcontext.ErrUnknownSlotKind], which is the honest answer. This is a
// decision, not an oversight.
func defaultProvider() (agentcontext.ContextProvider, error) {
	return agentcontext.NewProvider(resolvers.Default(), MarkFailures{})
}

// wrap names the manifest an error came out of without hiding it: the
// library's sentinels stay reachable through errors.Is.
func wrap(err error) error {
	return fmt.Errorf("assemble profile manifest slots: %w", err)
}
