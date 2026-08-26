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
	"fmt"

	"github.com/chrispian/cairn/profile"
	"github.com/hollis-labs/agentkit/agentcontext"
	"github.com/hollis-labs/agentkit/agentcontext/resolvers"
)

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
	// wiring: agentcontext.NewProvider(resolvers.Default(), DefaultRenderer{}).
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

// defaultProvider builds the provider [Assemble] uses when [Options] names
// none: the seven app-neutral resolvers and the library's own renderer.
//
// resolvers.WithSkillIndex is deliberately not wired. Per the MVP plan the
// eighth kind gets added when a profile needs one, and none does yet; until
// then a manifest declaring a skill_index slot fails with
// [agentcontext.ErrUnknownSlotKind], which is the honest answer. This is a
// decision, not an oversight.
func defaultProvider() (agentcontext.ContextProvider, error) {
	return agentcontext.NewProvider(resolvers.Default(), agentcontext.DefaultRenderer{})
}

// wrap names the manifest an error came out of without hiding it: the
// library's sentinels stay reachable through errors.Is.
func wrap(err error) error {
	return fmt.Errorf("assemble profile manifest slots: %w", err)
}
