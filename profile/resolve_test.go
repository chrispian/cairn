package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// errFakeNotFound stands in for the store's ErrProfileNotFound. The cascade
// must propagate whatever its loader reports, so the tests use a sentinel of
// their own rather than depending on package store.
var errFakeNotFound = errors.New("fake loader: no such profile")

// fakeLoader is an in-memory [Loader]: the profiles a test declares, by id.
type fakeLoader map[string]*Profile

func (f fakeLoader) Profile(_ context.Context, id string) (*Profile, error) {
	p, ok := f[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errFakeNotFound, id)
	}
	return p, nil
}

// nilLoader violates the [Loader] contract by reporting neither a profile nor
// an error.
type nilLoader struct{}

func (nilLoader) Profile(context.Context, string) (*Profile, error) { return nil, nil }

// ctxKey is the type of the value the context-propagation test plants.
type ctxKey struct{}

// ctxLoader records the context value it is called with, so a test can prove
// the caller's context reaches every load rather than being dropped.
type ctxLoader struct {
	inner fakeLoader
	saw   []string
}

func (c *ctxLoader) Profile(ctx context.Context, id string) (*Profile, error) {
	v, _ := ctx.Value(ctxKey{}).(string)
	c.saw = append(c.saw, v)
	return c.inner.Profile(ctx, id)
}

func spec(t *testing.T, kv map[string]string) Spec {
	t.Helper()
	s := Spec{}
	for k, v := range kv {
		if !json.Valid([]byte(v)) {
			t.Fatalf("spec key %q: fixture is not valid JSON: %s", k, v)
		}
		s[k] = json.RawMessage(v)
	}
	return s
}

func resolveOK(t *testing.T, l Loader, id string) *Resolved {
	t.Helper()
	got, err := Resolve(t.Context(), l, id)
	if err != nil {
		t.Fatalf("Resolve(%q) = error %v, want no error", id, err)
	}
	return got
}

func TestResolveSingleProfile(t *testing.T) {
	t.Parallel()

	l := fakeLoader{"solo": {
		ID:          "solo",
		Name:        "Solo",
		Description: "stands alone",
		Provider:    ProviderClaude,
		Model:       "opus",
		Body:        "the body",
		Spec:        spec(t, map[string]string{"skills": `["review"]`}),
	}}

	got := resolveOK(t, l, "solo")

	if got.ID != "solo" {
		t.Errorf("ID = %q, want %q", got.ID, "solo")
	}
	if !slices.Equal(got.Chain, []string{"solo"}) {
		t.Errorf("Chain = %v, want [solo]", got.Chain)
	}
	if got.Name != "Solo" || got.Description != "stands alone" {
		t.Errorf("Name/Description = %q/%q", got.Name, got.Description)
	}
	if got.Provider != ProviderClaude || got.Model != "opus" {
		t.Errorf("Provider/Model = %q/%q", got.Provider, got.Model)
	}
	if got.Body != "the body" {
		t.Errorf("Body = %q, want %q", got.Body, "the body")
	}
	if string(got.Spec["skills"]) != `["review"]` {
		t.Errorf("Spec[skills] = %s", got.Spec["skills"])
	}
}

func TestResolveClosestWinsPerField(t *testing.T) {
	t.Parallel()

	// Each field is declared at a different depth, and two of them are
	// declared twice, so every assertion below distinguishes closest-wins from
	// root-wins and from leaf-only.
	l := fakeLoader{
		"root": {
			ID: "root", Name: "root name", Description: "root desc",
			Provider: ProviderCodex, Model: "root model",
		},
		"mid": {
			ID: "mid", Extends: "root",
			Description: "mid desc", Model: "mid model",
		},
		"leaf": {
			ID: "leaf", Extends: "mid",
			Model: "leaf model",
		},
	}

	got := resolveOK(t, l, "leaf")

	if !slices.Equal(got.Chain, []string{"root", "mid", "leaf"}) {
		t.Errorf("Chain = %v, want [root mid leaf]", got.Chain)
	}
	for _, tc := range []struct {
		field, got, want string
	}{
		{"Name", got.Name, "root name"},              // declared only at the root
		{"Description", got.Description, "mid desc"}, // root and mid; mid is closer
		{"Provider", got.Provider.String(), "codex"}, // declared only at the root
		{"Model", got.Model, "leaf model"},           // all three; the leaf is closest
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.field, tc.got, tc.want)
		}
	}
	if got.ID != "leaf" {
		t.Errorf("ID = %q, want leaf", got.ID)
	}
}

func TestResolveEmptyFieldInheritsRatherThanBlanking(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {
			ID: "root", Name: "root name", Description: "root desc",
			Provider: ProviderClaude, Model: "root model",
			Spec: spec(t, map[string]string{"skills": `["review"]`}),
		},
		// Declares nothing at all: an empty field means "not declared".
		"leaf": {ID: "leaf", Extends: "root"},
	}

	got := resolveOK(t, l, "leaf")

	if got.Name != "root name" || got.Description != "root desc" {
		t.Errorf("Name/Description = %q/%q, want the root's", got.Name, got.Description)
	}
	if got.Provider != ProviderClaude || got.Model != "root model" {
		t.Errorf("Provider/Model = %q/%q, want the root's", got.Provider, got.Model)
	}
	if string(got.Spec["skills"]) != `["review"]` {
		t.Errorf("Spec[skills] = %s, want the root's", got.Spec["skills"])
	}
}

func TestResolveSpecKeyWinsWhole(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			"skills":   `["alpha","beta"]`,
			"settings": `{"kept":1,"replaced":2}`,
			"files":    `{"root.md":"root"}`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			"skills":   `["gamma"]`,
			"settings": `{"replaced":9}`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	// A list is replaced, not unioned: the ancestor's entries are gone.
	if s := string(got.Spec["skills"]); s != `["gamma"]` {
		t.Errorf("Spec[skills] = %s, want [\"gamma\"] — the ancestor's list must not be unioned in", s)
	}
	// An object is replaced, not merged: the ancestor's other keys are gone.
	if s := string(got.Spec["settings"]); s != `{"replaced":9}` {
		t.Errorf("Spec[settings] = %s, want {\"replaced\":9} — the ancestor's object must not be merged in", s)
	}
	// A key the leaf does not declare is inherited whole.
	if s := string(got.Spec["files"]); s != `{"root.md":"root"}` {
		t.Errorf("Spec[files] = %s, want the root's", s)
	}

	// The stored profiles are untouched, and the merged map is the resolved
	// profile's own.
	got.Spec["scribbled"] = json.RawMessage(`true`)
	if _, ok := l["leaf"].Spec["scribbled"]; ok {
		t.Error("writing to Resolved.Spec reached the stored profile's Spec")
	}
	if s := string(l["root"].Spec["skills"]); s != `["alpha","beta"]` {
		t.Errorf("the root profile's Spec[skills] changed to %s", s)
	}
}

func TestResolveUnknownSpecKeyCascadesLikeAKnownOne(t *testing.T) {
	t.Parallel()

	const carried = `{"nested":{"deep":[1,2,{"three":true}]},"unicode":"café"}`

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			"skills":       `["alpha"]`,
			"telemetry":    `{"sink":"root"}`,
			"root_only":    carried,
			"also_unknown": `[1,2,3]`,
		})},
		"mid": {ID: "mid", Extends: "root"},
		"leaf": {ID: "leaf", Extends: "mid", Spec: spec(t, map[string]string{
			"skills":    `["gamma"]`,
			"telemetry": `{"sink":"leaf"}`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	// The unknown key behaves exactly like the rendered one beside it.
	if s := string(got.Spec["telemetry"]); s != `{"sink":"leaf"}` {
		t.Errorf("Spec[telemetry] = %s, want the leaf's", s)
	}
	if s := string(got.Spec["skills"]); s != `["gamma"]` {
		t.Errorf("Spec[skills] = %s, want the leaf's", s)
	}
	// An unknown key nothing overrides survives the walk byte for byte.
	if s := string(got.Spec["root_only"]); s != carried {
		t.Errorf("Spec[root_only] = %s, want %s", s, carried)
	}
	if s := string(got.Spec["also_unknown"]); s != `[1,2,3]` {
		t.Errorf("Spec[also_unknown] = %s, want [1,2,3]", s)
	}
	if len(got.Spec) != 4 {
		t.Errorf("Spec has %d keys (%v), want 4", len(got.Spec), specKeys(got.Spec))
	}
}

func TestResolveSpecKeySetToNullOverrides(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			"slots": `[{"name":"role","source":{"kind":"inline","inline":{"content":"hi"}}}]`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{"slots": `null`})},
	}

	got := resolveOK(t, l, "leaf")

	if s := string(got.Spec[SpecKeySlots]); s != "null" {
		t.Errorf("Spec[slots] = %s, want null", s)
	}
	slots, err := got.Spec.Slots()
	if err != nil {
		t.Fatalf("Slots() = error %v", err)
	}
	if len(slots) != 0 {
		t.Errorf("Slots() = %v, want none", slots)
	}
}

func TestResolveSpecIsNeverNil(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root"},
		"leaf": {ID: "leaf", Extends: "root"},
	}

	got := resolveOK(t, l, "leaf")

	if got.Spec == nil {
		t.Fatal("Spec is nil for a chain that declares no manifest")
	}
	if len(got.Spec) != 0 {
		t.Errorf("Spec = %v, want empty", got.Spec)
	}
}

func TestResolveBodyConcatenatesAncestorFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bodies []string // root first
		want   string
	}{
		{
			name:   "ancestor first, blank line between",
			bodies: []string{"root body", "mid body", "leaf body"},
			want:   "root body\n\nmid body\n\nleaf body",
		},
		{
			name:   "an empty body in the middle does not double the separator",
			bodies: []string{"root body", "", "leaf body"},
			want:   "root body\n\nleaf body",
		},
		{
			name:   "a whitespace-only body counts as empty",
			bodies: []string{"root body", "  \n\t ", "leaf body"},
			want:   "root body\n\nleaf body",
		},
		{
			name:   "an empty root",
			bodies: []string{"", "mid body", "leaf body"},
			want:   "mid body\n\nleaf body",
		},
		{
			name:   "an empty leaf",
			bodies: []string{"root body", "mid body", ""},
			want:   "root body\n\nmid body",
		},
		{
			name:   "an all-empty chain",
			bodies: []string{"", "", ""},
			want:   "",
		},
		{
			name:   "surrounding blank lines are normalised away",
			bodies: []string{"\n\nroot body\n\n\n", "\nmid body\n", "leaf body\n"},
			want:   "root body\n\nmid body\n\nleaf body",
		},
		{
			name:   "blank lines inside a body are preserved",
			bodies: []string{"root para\n\nroot para two", "leaf body"},
			want:   "root para\n\nroot para two\n\nleaf body",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			l := fakeLoader{}
			var prev string
			var leaf string
			for i, body := range tc.bodies {
				id := fmt.Sprintf("p%d", i)
				l[id] = &Profile{ID: id, Extends: prev, Body: body}
				prev, leaf = id, id
			}

			got := resolveOK(t, l, leaf)

			if got.Body != tc.want {
				t.Errorf("Body = %q, want %q", got.Body, tc.want)
			}
		})
	}
}

func TestResolveAbstractIsTheLeafsOwn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                       string
		rootAbstract, leafAbstract bool
		want                       bool
	}{
		{name: "abstract root, concrete leaf", rootAbstract: true, leafAbstract: false, want: false},
		{name: "concrete root, abstract leaf", rootAbstract: false, leafAbstract: true, want: true},
		{name: "both abstract", rootAbstract: true, leafAbstract: true, want: true},
		{name: "neither abstract", rootAbstract: false, leafAbstract: false, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			l := fakeLoader{
				"root": {ID: "root", Abstract: tc.rootAbstract},
				"leaf": {ID: "leaf", Extends: "root", Abstract: tc.leafAbstract},
			}

			// Resolving an abstract profile is never itself an error: install
			// resolves one, and only a direct boot has reason to object.
			got := resolveOK(t, l, "leaf")

			if got.Abstract != tc.want {
				t.Errorf("Abstract = %v, want %v", got.Abstract, tc.want)
			}
		})
	}
}

func TestResolveAbstractLeafResolvesWithoutError(t *testing.T) {
	t.Parallel()

	l := fakeLoader{"base": {ID: "base", Abstract: true, Name: "Base", Body: "shared"}}

	got := resolveOK(t, l, "base")

	if !got.Abstract {
		t.Error("Abstract = false, want true")
	}
	if got.Body != "shared" || got.Name != "Base" {
		t.Errorf("Body/Name = %q/%q, want shared/Base", got.Body, got.Name)
	}
}

func TestResolveCycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		loader  fakeLoader
		id      string
		wantIDs string
	}{
		{
			name:    "a profile extending itself",
			loader:  fakeLoader{"ouroboros": {ID: "ouroboros", Extends: "ouroboros"}},
			id:      "ouroboros",
			wantIDs: "ouroboros -> ouroboros",
		},
		{
			name: "a three profile loop",
			loader: fakeLoader{
				"a": {ID: "a", Extends: "b"},
				"b": {ID: "b", Extends: "c"},
				"c": {ID: "c", Extends: "a"},
			},
			id:      "a",
			wantIDs: "a -> b -> c -> a",
		},
		{
			name: "a chain that walks into a loop",
			loader: fakeLoader{
				"leaf": {ID: "leaf", Extends: "a"},
				"a":    {ID: "a", Extends: "b"},
				"b":    {ID: "b", Extends: "a"},
			},
			id:      "leaf",
			wantIDs: "leaf -> a -> b -> a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Resolve(t.Context(), tc.loader, tc.id)

			if got != nil {
				t.Errorf("Resolve returned %+v, want nil alongside the error", got)
			}
			if !errors.Is(err, ErrCycle) {
				t.Fatalf("Resolve error = %v, want one wrapping ErrCycle", err)
			}
			if !strings.Contains(err.Error(), tc.wantIDs) {
				t.Errorf("Resolve error = %q, want it to name the walk order %q", err, tc.wantIDs)
			}
		})
	}
}

func TestResolveMissingAncestorNamesTheReferencingProfile(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"leaf": {ID: "leaf", Extends: "mid"},
		"mid":  {ID: "mid", Extends: "ghost"},
	}

	_, err := Resolve(t.Context(), l, "leaf")

	if !errors.Is(err, errFakeNotFound) {
		t.Fatalf("Resolve error = %v, want the loader's error propagated", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `"ghost"`) {
		t.Errorf("Resolve error = %q, want it to name the missing profile", msg)
	}
	if !strings.Contains(msg, `"mid"`) {
		t.Errorf("Resolve error = %q, want it to name the profile that referenced it", msg)
	}
}

func TestResolveMissingLeaf(t *testing.T) {
	t.Parallel()

	_, err := Resolve(t.Context(), fakeLoader{}, "absent")

	if !errors.Is(err, errFakeNotFound) {
		t.Fatalf("Resolve error = %v, want the loader's error propagated", err)
	}
	if !strings.Contains(err.Error(), `"absent"`) {
		t.Errorf("Resolve error = %q, want it to name the profile asked for", err)
	}
	// Nothing referenced the leaf, so there is nobody to blame for it.
	if strings.Contains(err.Error(), "extended by") {
		t.Errorf("Resolve error = %q, want no referencing profile named", err)
	}
}

func TestResolveNilLoader(t *testing.T) {
	t.Parallel()

	_, err := Resolve(t.Context(), nil, "leaf")

	if !errors.Is(err, ErrNilLoader) {
		t.Fatalf("Resolve error = %v, want one wrapping ErrNilLoader", err)
	}
}

func TestResolveLoaderReturningNothing(t *testing.T) {
	t.Parallel()

	_, err := Resolve(t.Context(), nilLoader{}, "leaf")

	if !errors.Is(err, ErrNilProfile) {
		t.Fatalf("Resolve error = %v, want one wrapping ErrNilProfile", err)
	}
}

func TestResolvePassesTheCallersContext(t *testing.T) {
	t.Parallel()

	l := &ctxLoader{inner: fakeLoader{
		"root": {ID: "root"},
		"leaf": {ID: "leaf", Extends: "root"},
	}}
	ctx := context.WithValue(t.Context(), ctxKey{}, "carried")

	if _, err := Resolve(ctx, l, "leaf"); err != nil {
		t.Fatalf("Resolve = error %v", err)
	}

	if !slices.Equal(l.saw, []string{"carried", "carried"}) {
		t.Errorf("loader saw context values %v, want the caller's on every load", l.saw)
	}
}

func TestResolveDeepChain(t *testing.T) {
	t.Parallel()

	// The walk is bounded by the cycle check alone, so a chain longer than any
	// arbitrary limit resolves.
	const depth = 500

	l := fakeLoader{}
	for i := range depth {
		id := fmt.Sprintf("p%d", i)
		var extends string
		if i > 0 {
			extends = fmt.Sprintf("p%d", i-1)
		}
		l[id] = &Profile{ID: id, Extends: extends, Model: id}
	}
	leaf := fmt.Sprintf("p%d", depth-1)

	got := resolveOK(t, l, leaf)

	if len(got.Chain) != depth {
		t.Errorf("Chain has %d ids, want %d", len(got.Chain), depth)
	}
	if got.Chain[0] != "p0" || got.Chain[depth-1] != leaf {
		t.Errorf("Chain runs %q..%q, want p0..%s", got.Chain[0], got.Chain[depth-1], leaf)
	}
	if got.Model != leaf {
		t.Errorf("Model = %q, want the leaf's %q", got.Model, leaf)
	}
}

// specKeys returns s's keys sorted, for a readable failure message.
func specKeys(s Spec) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
