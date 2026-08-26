package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chrispian/cairn/profile"
)

// profileFixture is a minimally populated profile with the given id.
func profileFixture(id string) profile.Profile {
	return profile.Profile{
		ID:          id,
		Name:        "Profile " + id,
		Description: "fixture",
		Provider:    profile.ProviderClaude,
		Model:       "sonnet",
		Body:        "body of " + id,
	}
}

func TestProfileRoundTrip(t *testing.T) {
	s, clock := openStore(t)

	want := profile.Profile{
		ID:          "engineer",
		Extends:     "",
		Abstract:    true,
		Name:        "Engineer",
		Description: "writes the code",
		Provider:    profile.ProviderCodex,
		Model:       "gpt-5",
		Body:        "You write Go.\n",
		Spec: profile.Spec{
			profile.SpecKeySkills: json.RawMessage(`["code-review","capture-decision"]`),
			profile.SpecKeyFiles:  json.RawMessage(`{"notes/todo.md":"one\ntwo\n"}`),
		},
	}
	if err := s.PutProfile(t.Context(), want); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}

	got, err := s.Profile(t.Context(), "engineer")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}

	if got.ID != want.ID || got.Extends != want.Extends || got.Abstract != want.Abstract {
		t.Fatalf("identity round trip = %+v, want %+v", got, want)
	}
	if got.Name != want.Name || got.Description != want.Description {
		t.Fatalf("name/description round trip = %q/%q, want %q/%q", got.Name, got.Description, want.Name, want.Description)
	}
	if got.Provider != want.Provider || got.Model != want.Model {
		t.Fatalf("provider/model round trip = %q/%q, want %q/%q", got.Provider, got.Model, want.Provider, want.Model)
	}
	if got.Body != want.Body {
		t.Fatalf("body round trip = %q, want %q", got.Body, want.Body)
	}
	if !got.CreatedAt.Equal(clock.now()) || !got.UpdatedAt.Equal(clock.now()) {
		t.Fatalf("timestamps = %s / %s, want both %s", got.CreatedAt, got.UpdatedAt, clock.now())
	}
	for key, raw := range want.Spec {
		if gotRaw := string(got.Spec[key]); gotRaw != string(raw) {
			t.Fatalf("spec key %q round trip = %s, want %s", key, gotRaw, raw)
		}
	}
}

func TestProfileSpecCarriesUnknownKeysVerbatim(t *testing.T) {
	s, _ := openStore(t)

	// A key nothing in Cairn renders, holding nested structure of every JSON
	// shape. It has to come back byte for byte.
	const unknown = `{"budget":{"tokens":8000,"ratio":0.25},"tags":["a","b"],"enabled":true,"absent":null,"note":"a <b> & c"}`

	want := profile.Profile{
		ID: "carrier",
		Spec: profile.Spec{
			profile.SpecKeySlots: json.RawMessage(`[{"id":"repo","kind":"static_file"}]`),
			"telemetry":          json.RawMessage(unknown),
			"zz_last":            json.RawMessage(`"trailing"`),
		},
	}
	if err := s.PutProfile(t.Context(), want); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}

	got, err := s.Profile(t.Context(), "carrier")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if len(got.Spec) != len(want.Spec) {
		t.Fatalf("spec has %d keys, want %d: %v", len(got.Spec), len(want.Spec), got.Spec)
	}
	if raw := string(got.Spec["telemetry"]); raw != unknown {
		t.Fatalf("unknown key round trip =\n\t%s\nwant\n\t%s", raw, unknown)
	}
	if raw := string(got.Spec["zz_last"]); raw != `"trailing"` {
		t.Fatalf("unknown scalar key round trip = %s, want %q", raw, `"trailing"`)
	}

	// And a second write of what was read back stores the same text, so a
	// read/write cycle is not a slow rewrite of the operator's manifest.
	if err := s.PutProfile(t.Context(), *got); err != nil {
		t.Fatalf("PutProfile of the read-back profile: %v", err)
	}
	again, err := s.Profile(t.Context(), "carrier")
	if err != nil {
		t.Fatalf("Profile after rewrite: %v", err)
	}
	if raw := string(again.Spec["telemetry"]); raw != unknown {
		t.Fatalf("unknown key after rewrite =\n\t%s\nwant\n\t%s", raw, unknown)
	}
}

func TestProfileEmptySpecStoresEmptyObject(t *testing.T) {
	s, _ := openStore(t)

	for _, tt := range []struct {
		name string
		spec profile.Spec
	}{
		{name: "nil spec", spec: nil},
		{name: "empty spec", spec: profile.Spec{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := profileFixture("empty")
			p.Spec = tt.spec
			if err := s.PutProfile(t.Context(), p); err != nil {
				t.Fatalf("PutProfile: %v", err)
			}

			var stored string
			row := s.DB().QueryRowContext(t.Context(), `SELECT spec FROM profiles WHERE id = ?`, "empty")
			if err := row.Scan(&stored); err != nil {
				t.Fatalf("read spec column: %v", err)
			}
			if stored != "{}" {
				t.Fatalf("stored spec = %q, want %q", stored, "{}")
			}

			got, err := s.Profile(t.Context(), "empty")
			if err != nil {
				t.Fatalf("Profile: %v", err)
			}
			if got.Spec == nil {
				t.Fatal("decoded spec is nil, want an empty non-nil manifest")
			}
			if len(got.Spec) != 0 {
				t.Fatalf("decoded spec = %v, want empty", got.Spec)
			}
		})
	}
}

func TestProfileDecodesEmptySpecColumn(t *testing.T) {
	s, _ := openStore(t)

	// A row written by hand, as §7 of the plan expects: the spec column is
	// empty rather than "{}".
	_, err := s.DB().ExecContext(t.Context(),
		`INSERT INTO profiles (id, spec, created_at, updated_at) VALUES (?, '', '', '')`, "handwritten")
	if err != nil {
		t.Fatalf("insert hand-written row: %v", err)
	}

	got, err := s.Profile(t.Context(), "handwritten")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if got.Spec == nil || len(got.Spec) != 0 {
		t.Fatalf("spec = %v, want an empty non-nil manifest", got.Spec)
	}
	if !got.CreatedAt.IsZero() || !got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps = %s / %s, want both zero", got.CreatedAt, got.UpdatedAt)
	}
}

func TestPutProfileUpdatePreservesCreatedAt(t *testing.T) {
	s, clock := openStore(t)
	inserted := clock.now()

	if err := s.PutProfile(t.Context(), profileFixture("engineer")); err != nil {
		t.Fatalf("PutProfile insert: %v", err)
	}

	clock.advance(90 * time.Minute)
	updated := profileFixture("engineer")
	updated.Body = "rewritten"
	// A CreatedAt on the way in does not overwrite what the row already has.
	updated.CreatedAt = time.Date(1999, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := s.PutProfile(t.Context(), updated); err != nil {
		t.Fatalf("PutProfile update: %v", err)
	}

	got, err := s.Profile(t.Context(), "engineer")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if !got.CreatedAt.Equal(inserted) {
		t.Fatalf("created_at = %s, want the insert's %s", got.CreatedAt, inserted)
	}
	if !got.UpdatedAt.Equal(clock.now()) {
		t.Fatalf("updated_at = %s, want %s", got.UpdatedAt, clock.now())
	}
	if got.Body != "rewritten" {
		t.Fatalf("body = %q, want the updated one", got.Body)
	}

	profiles, err := s.Profiles(t.Context())
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("an update wrote %d rows, want 1", len(profiles))
	}
}

func TestPutProfileInsertKeepsSuppliedCreatedAt(t *testing.T) {
	s, clock := openStore(t)

	imported := time.Date(2024, time.March, 4, 5, 6, 7, 0, time.UTC)
	p := profileFixture("imported")
	p.CreatedAt = imported
	if err := s.PutProfile(t.Context(), p); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}

	got, err := s.Profile(t.Context(), "imported")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if !got.CreatedAt.Equal(imported) {
		t.Fatalf("created_at = %s, want the imported %s", got.CreatedAt, imported)
	}
	if !got.UpdatedAt.Equal(clock.now()) {
		t.Fatalf("updated_at = %s, want the clock's %s", got.UpdatedAt, clock.now())
	}
}

func TestPutProfileUpdateSurvivesAnExistingBinding(t *testing.T) {
	s, _ := openStore(t)

	if err := s.PutProfile(t.Context(), profileFixture("engineer")); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}
	if err := s.PutBinding(t.Context(), Binding{Name: "eng", ProfileID: "engineer"}); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}

	// A bound profile is still editable: the write is an update, not a
	// delete-and-reinsert that would trip the foreign key.
	updated := profileFixture("engineer")
	updated.Model = "opus"
	if err := s.PutProfile(t.Context(), updated); err != nil {
		t.Fatalf("PutProfile over a bound profile: %v", err)
	}

	b, err := s.Binding(t.Context(), "eng")
	if err != nil {
		t.Fatalf("Binding after the update: %v", err)
	}
	if b.ProfileID != "engineer" {
		t.Fatalf("binding profile_id = %q, want %q", b.ProfileID, "engineer")
	}
}

func TestProfilesOrderedByID(t *testing.T) {
	s, _ := openStore(t)

	for _, id := range []string{"zeta", "alpha", "Mid", "beta"} {
		if err := s.PutProfile(t.Context(), profileFixture(id)); err != nil {
			t.Fatalf("PutProfile(%q): %v", id, err)
		}
	}

	got, err := s.Profiles(t.Context())
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	want := []string{"Mid", "alpha", "beta", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Profiles returned %d rows, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("Profiles[%d].ID = %q, want %q (full order %v)", i, got[i].ID, id, ids(got))
		}
		if got[i].Spec == nil {
			t.Fatalf("Profiles[%d].Spec is nil, want an empty non-nil manifest", i)
		}
	}
}

// ids reports the ids of profiles in order, for a failure message.
func ids(profiles []profile.Profile) []string {
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p.ID)
	}
	return out
}

func TestDeleteProfile(t *testing.T) {
	s, _ := openStore(t)

	if err := s.PutProfile(t.Context(), profileFixture("doomed")); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}
	if err := s.DeleteProfile(t.Context(), "doomed"); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}

	_, err := s.Profile(t.Context(), "doomed")
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("Profile after delete = %v, want ErrProfileNotFound", err)
	}
	if err := s.DeleteProfile(t.Context(), "doomed"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("second DeleteProfile = %v, want ErrProfileNotFound", err)
	}
}

func TestBindingRoundTrip(t *testing.T) {
	s, _ := openStore(t)

	if err := s.PutProfile(t.Context(), profileFixture("engineer")); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}
	if err := s.PutProfile(t.Context(), profileFixture("reviewer")); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}

	want := Binding{Name: "eng", ProfileID: "engineer", Scope: "cairn"}
	if err := s.PutBinding(t.Context(), want); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}
	got, err := s.Binding(t.Context(), "eng")
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if *got != want {
		t.Fatalf("Binding = %+v, want %+v", *got, want)
	}

	// A second write of the same name repoints it rather than adding a row.
	repointed := Binding{Name: "eng", ProfileID: "reviewer", Scope: ""}
	if err := s.PutBinding(t.Context(), repointed); err != nil {
		t.Fatalf("PutBinding update: %v", err)
	}
	got, err = s.Binding(t.Context(), "eng")
	if err != nil {
		t.Fatalf("Binding after update: %v", err)
	}
	if *got != repointed {
		t.Fatalf("Binding after update = %+v, want %+v", *got, repointed)
	}

	if err := s.DeleteBinding(t.Context(), "eng"); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if _, err := s.Binding(t.Context(), "eng"); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("Binding after delete = %v, want ErrBindingNotFound", err)
	}
	if err := s.DeleteBinding(t.Context(), "eng"); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("second DeleteBinding = %v, want ErrBindingNotFound", err)
	}
}

func TestBindingsOrderedByName(t *testing.T) {
	s, _ := openStore(t)

	if err := s.PutProfile(t.Context(), profileFixture("engineer")); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}
	for _, name := range []string{"ship", "audit", "review"} {
		if err := s.PutBinding(t.Context(), Binding{Name: name, ProfileID: "engineer"}); err != nil {
			t.Fatalf("PutBinding(%q): %v", name, err)
		}
	}

	got, err := s.Bindings(t.Context())
	if err != nil {
		t.Fatalf("Bindings: %v", err)
	}
	want := []string{"audit", "review", "ship"}
	if len(got) != len(want) {
		t.Fatalf("Bindings returned %d rows, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("Bindings[%d].Name = %q, want %q", i, got[i].Name, name)
		}
	}
}

func TestScopeRoundTrip(t *testing.T) {
	s, _ := openStore(t)

	if err := s.PutScope(t.Context(), Scope{Alias: "cairn", Path: "/dev/projects/cairn"}); err != nil {
		t.Fatalf("PutScope: %v", err)
	}
	got, err := s.Scope(t.Context(), "cairn")
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	if want := "/dev/projects/cairn"; got != want {
		t.Fatalf("Scope = %q, want %q", got, want)
	}

	if err := s.PutScope(t.Context(), Scope{Alias: "cairn", Path: "/dev/cairn"}); err != nil {
		t.Fatalf("PutScope update: %v", err)
	}
	got, err = s.Scope(t.Context(), "cairn")
	if err != nil {
		t.Fatalf("Scope after update: %v", err)
	}
	if want := "/dev/cairn"; got != want {
		t.Fatalf("Scope after update = %q, want %q", got, want)
	}

	if err := s.DeleteScope(t.Context(), "cairn"); err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}
	if _, err := s.Scope(t.Context(), "cairn"); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("Scope after delete = %v, want ErrScopeNotFound", err)
	}
	if err := s.DeleteScope(t.Context(), "cairn"); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("second DeleteScope = %v, want ErrScopeNotFound", err)
	}
}

func TestScopesOrderedByAlias(t *testing.T) {
	s, _ := openStore(t)

	for _, sc := range []Scope{
		{Alias: "notes", Path: "/notes"},
		{Alias: "cairn", Path: "/cairn"},
		{Alias: "tether", Path: "/tether"},
	} {
		if err := s.PutScope(t.Context(), sc); err != nil {
			t.Fatalf("PutScope(%q): %v", sc.Alias, err)
		}
	}

	got, err := s.Scopes(t.Context())
	if err != nil {
		t.Fatalf("Scopes: %v", err)
	}
	want := []Scope{
		{Alias: "cairn", Path: "/cairn"},
		{Alias: "notes", Path: "/notes"},
		{Alias: "tether", Path: "/tether"},
	}
	if len(got) != len(want) {
		t.Fatalf("Scopes returned %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Scopes[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestNotFoundSentinels(t *testing.T) {
	s, _ := openStore(t)

	if _, err := s.Profile(t.Context(), "absent"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("Profile = %v, want ErrProfileNotFound", err)
	}
	if _, err := s.Binding(t.Context(), "absent"); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("Binding = %v, want ErrBindingNotFound", err)
	}
	if _, err := s.Scope(t.Context(), "absent"); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("Scope = %v, want ErrScopeNotFound", err)
	}
	if err := s.DeleteProfile(t.Context(), "absent"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("DeleteProfile = %v, want ErrProfileNotFound", err)
	}
	if err := s.DeleteBinding(t.Context(), "absent"); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("DeleteBinding = %v, want ErrBindingNotFound", err)
	}
	if err := s.DeleteScope(t.Context(), "absent"); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("DeleteScope = %v, want ErrScopeNotFound", err)
	}
}

func TestNotFoundErrorNamesTheKey(t *testing.T) {
	s, _ := openStore(t)

	_, err := s.Profile(t.Context(), "engineer")
	if err == nil {
		t.Fatal("Profile on an empty database returned no error")
	}
	if want := `"engineer"`; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not name %s", err, want)
	}
}

func TestBlankKeysAreInvalid(t *testing.T) {
	s, _ := openStore(t)

	for i, blank := range []string{"", " ", "\t", "\n  \n"} {
		t.Run(fmt.Sprintf("blank %d", i), func(t *testing.T) {
			calls := map[string]error{
				"Profile":       errOf2(s.Profile(t.Context(), blank)),
				"PutProfile":    s.PutProfile(t.Context(), profile.Profile{ID: blank}),
				"DeleteProfile": s.DeleteProfile(t.Context(), blank),
				"Binding":       errOf2(s.Binding(t.Context(), blank)),
				"PutBinding":    s.PutBinding(t.Context(), Binding{Name: blank, ProfileID: "engineer"}),
				"DeleteBinding": s.DeleteBinding(t.Context(), blank),
				"Scope":         errOf2(s.Scope(t.Context(), blank)),
				"PutScope":      s.PutScope(t.Context(), Scope{Alias: blank, Path: "/somewhere"}),
				"DeleteScope":   s.DeleteScope(t.Context(), blank),
			}
			for name, err := range calls {
				if !errors.Is(err, ErrInvalidKey) {
					t.Errorf("%s(%q) = %v, want ErrInvalidKey", name, blank, err)
				}
			}
		})
	}
}

// errOf2 drops the value of a two-result call so a blank-key table can hold
// reads and writes side by side.
func errOf2[T any](_ T, err error) error { return err }

func TestPutBindingRequiresAnExistingProfile(t *testing.T) {
	s, _ := openStore(t)

	err := s.PutBinding(t.Context(), Binding{Name: "eng", ProfileID: "nobody"})
	if err == nil {
		t.Fatal("PutBinding against a missing profile returned no error")
	}
	if !strings.Contains(err.Error(), `"eng"`) {
		t.Fatalf("error %q does not name the binding", err)
	}
}

func TestEncodeSpecIsStable(t *testing.T) {
	spec := profile.Spec{
		"zulu":  json.RawMessage(`1`),
		"alpha": json.RawMessage(`{"b":2}`),
		"mike":  json.RawMessage(`  "spaced"  `),
		"empty": nil,
	}

	got, err := encodeSpec(spec)
	if err != nil {
		t.Fatalf("encodeSpec: %v", err)
	}
	want := `{"alpha":{"b":2},"empty":null,"mike":"spaced","zulu":1}`
	if got != want {
		t.Fatalf("encodeSpec = %s, want %s", got, want)
	}

	// Sorted keys mean the same manifest always stores the same text.
	again, err := encodeSpec(spec)
	if err != nil {
		t.Fatalf("encodeSpec second call: %v", err)
	}
	if again != got {
		t.Fatalf("encodeSpec is not stable: %s then %s", got, again)
	}
}

func TestEncodeSpecRejectsNonJSON(t *testing.T) {
	_, err := encodeSpec(profile.Spec{"broken": json.RawMessage(`{"unclosed":`)})
	if err == nil {
		t.Fatal("encodeSpec accepted a value that is not JSON")
	}
	if !strings.Contains(err.Error(), `"broken"`) {
		t.Fatalf("error %q does not name the offending key", err)
	}
}

func TestDecodeSpecNullColumn(t *testing.T) {
	spec, err := decodeSpec("null")
	if err != nil {
		t.Fatalf("decodeSpec: %v", err)
	}
	if spec == nil || len(spec) != 0 {
		t.Fatalf("decodeSpec(\"null\") = %v, want an empty non-nil manifest", spec)
	}
}
