package profile

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrCycle reports that following extends arrived back at a profile the walk
// had already visited. The wrapping error names the ids in the order they were
// walked, ending with the one that closed the loop.
var ErrCycle = errors.New("extends cycle")

// ErrNilLoader reports that [Resolve] was given no [Loader] to read profiles
// through. It is a programming error rather than a configuration one.
var ErrNilLoader = errors.New("nil loader")

// ErrNilProfile reports that a [Loader] returned neither a profile nor an
// error. It is a violation of the [Loader] contract, caught here so it surfaces
// as a named error instead of a nil dereference further down.
var ErrNilProfile = errors.New("loader returned no profile and no error")

// Loader is what [Resolve] reads profiles through: one stored profile by id,
// with no cascade applied. The store implements it.
//
// A Loader that has no profile for an id returns an error — the store's
// ErrProfileNotFound — rather than a nil profile. [Resolve] propagates that
// error, so what "not found" means stays the Loader's to define.
type Loader interface {
	Profile(ctx context.Context, id string) (*Profile, error)
}

// Resolve walks id's extends chain and folds it into one [Resolved].
//
// The walk goes leaf to root and the fold goes root to leaf, so every field is
// closest-wins: the value from the nearest profile in the chain that declares
// it, where declaring means non-empty for a string and present for a top-level
// key of [Spec]. A [Spec] key is taken whole — no key is unioned, merged, or
// looked inside, and a key this package has never heard of cascades exactly
// like one it renders. Restating an ancestor's value is how a profile keeps it,
// and a key set to JSON null is how a profile clears one.
//
// [Profile.Body] is the exception: it concatenates ancestor-first, because the
// persona is additive.
//
// [Profile.Abstract] does not cascade. [Resolved.Abstract] is the leaf's own
// flag, carried rather than acted on: Resolve never refuses to resolve an
// abstract profile, because `cairn install` legitimately resolves one and only
// a direct boot has reason to object.
func Resolve(ctx context.Context, l Loader, id string) (*Resolved, error) {
	if l == nil {
		return nil, fmt.Errorf("resolving profile %q: %w", id, ErrNilLoader)
	}

	chain, ids, err := walk(ctx, l, id)
	if err != nil {
		return nil, err
	}

	out := &Resolved{
		ID:       id,
		Chain:    ids,
		Abstract: chain[len(chain)-1].Abstract,
		Spec:     Spec{},
	}

	bodies := make([]string, 0, len(chain))
	for _, p := range chain {
		if p.Name != "" {
			out.Name = p.Name
		}
		if p.Description != "" {
			out.Description = p.Description
		}
		if p.Provider != "" {
			out.Provider = p.Provider
		}
		if p.Model != "" {
			out.Model = p.Model
		}
		if body := strings.TrimSpace(p.Body); body != "" {
			bodies = append(bodies, body)
		}
		for key, raw := range p.Spec {
			out.Spec[key] = raw
		}
	}
	out.Body = strings.Join(bodies, "\n\n")

	return out, nil
}

// walk follows extends from id to the root of its chain and returns both the
// profiles and their ids, ancestor-first. A chain is linear — extends names one
// profile — so revisiting an id is always a cycle.
func walk(ctx context.Context, l Loader, id string) ([]*Profile, []string, error) {
	var (
		chain []*Profile
		ids   []string
		seen  = make(map[string]bool)
		// from is the profile whose extends named the id being loaded, empty
		// for the leaf, so a missing ancestor says who referenced it.
		from string
	)

	for cur := id; ; {
		if seen[cur] {
			return nil, nil, fmt.Errorf("resolving profile %q: %w: %s",
				id, ErrCycle, strings.Join(append(slices.Clone(ids), cur), " -> "))
		}
		seen[cur] = true
		ids = append(ids, cur)

		p, err := l.Profile(ctx, cur)
		if err != nil {
			return nil, nil, loadErr(cur, from, err)
		}
		if p == nil {
			return nil, nil, loadErr(cur, from, ErrNilProfile)
		}

		chain = append(chain, p)
		if p.Extends == "" {
			break
		}
		from, cur = cur, p.Extends
	}

	slices.Reverse(chain)
	slices.Reverse(ids)

	return chain, ids, nil
}

// loadErr wraps a failure to load id, naming the profile whose extends
// referenced it so a broken chain says where the dangling reference lives.
// An empty from means id was the profile asked for.
func loadErr(id, from string, err error) error {
	if from == "" {
		return fmt.Errorf("loading profile %q: %w", id, err)
	}
	return fmt.Errorf("loading profile %q, extended by %q: %w", id, from, err)
}
