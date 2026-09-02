package bootdir

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestSubstituteReplacesWhatWasDeclared is the whole of the mechanism: a slot
// marker becomes that slot's section, a value marker becomes that value, and
// the text around them is untouched.
func TestSubstituteReplacesWhatWasDeclared(t *testing.T) {
	const template = "# <!-- cairn:value profile -->\n" +
		"\n" +
		"prose the operator wrote\n" +
		"\n" +
		"<!-- cairn:slot repo -->\n"
	sections := map[string]string{"repo": "## Repository\n\non branch main"}
	values := map[string]string{"profile": "engineer"}

	got, err := Substitute(template, sections, values)
	if err != nil {
		t.Fatalf("Substitute(): %v", err)
	}
	want := "# engineer\n\nprose the operator wrote\n\n## Repository\n\non branch main\n"
	if got != want {
		t.Errorf("Substitute() =\n%q\nwant\n%q", got, want)
	}
}

// TestSubstituteOmitsASlotThatFilledNothing is docs/plan.md §5 carried into a
// world where the heading lives in a template.
//
// A slot that failed to resolve and one that resolved empty both arrive here as
// an empty section, and the marker takes the heading with it because the
// heading came back from the slot rather than being written around the marker.
// A template holding "## Memory" above the marker would keep that heading; that
// is why the heading belongs to the slot.
func TestSubstituteOmitsASlotThatFilledNothing(t *testing.T) {
	const template = "before\n\n<!-- cairn:slot memory -->\n\nafter\n"

	for name, sections := range map[string]map[string]string{
		"a slot that resolved empty": {"memory": ""},
		"a slot never declared":      {},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Substitute(template, sections, nil)
			if err != nil {
				t.Fatalf("Substitute(): %v", err)
			}
			if want := "before\n\n\n\nafter\n"; got != want {
				t.Errorf("Substitute() = %q, want %q", got, want)
			}
			if strings.Contains(got, "cairn:") {
				t.Errorf("Substitute() left a marker behind: %q", got)
			}
		})
	}
}

// TestSubstituteRepeatsAMarkerEveryTimeItAppears covers a name used twice. A
// template may want one section in two places, and substituting only the first
// would leave a marker in a file an agent reads.
func TestSubstituteRepeatsAMarkerEveryTimeItAppears(t *testing.T) {
	got, err := Substitute(
		"<!-- cairn:value profile -->/<!-- cairn:value profile -->",
		nil, map[string]string{"profile": "eng"})
	if err != nil {
		t.Fatalf("Substitute(): %v", err)
	}
	if got != "eng/eng" {
		t.Errorf("Substitute() = %q, want both markers replaced", got)
	}
}

// TestSubstituteToleratesTheSpellingsAnOperatorWrites covers the whitespace a
// marker is written with, which nobody should have to think about.
func TestSubstituteToleratesTheSpellingsAnOperatorWrites(t *testing.T) {
	for _, marker := range []string{
		"<!-- cairn:value profile -->",
		"<!--cairn:value profile-->",
		"<!--   cairn:value    profile   -->",
	} {
		got, err := Substitute(marker, nil, map[string]string{"profile": "eng"})
		if err != nil {
			t.Fatalf("Substitute(%q): %v", marker, err)
		}
		if got != "eng" {
			t.Errorf("Substitute(%q) = %q, want %q", marker, got, "eng")
		}
	}
}

// TestSubstituteRefusesAMarkerItCannotActOn covers every malformed form.
//
// A marker in cairn's own namespace that cairn does not understand is refused
// rather than left in place. Leaving it would plant the marker's own text in a
// file an agent reads, which is the one outcome worse than failing.
func TestSubstituteRefusesAMarkerItCannotActOn(t *testing.T) {
	for name, marker := range map[string]string{
		"no verb and no name":  "<!-- cairn: -->",
		"a verb and no name":   "<!-- cairn:slot -->",
		"a verb cairn has not": "<!-- cairn:section repo -->",
		"three words":          "<!-- cairn:slot repo extra -->",
		"a value that is not":  "<!-- cairn:value tenant -->",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Substitute(marker, nil, nil); !errors.Is(err, ErrMarker) {
				t.Fatalf("Substitute(%q) = %v, want ErrMarker", marker, err)
			}
		})
	}
}

// TestMarkersIgnoreWhatIsNotCairns states the other half of the namespace rule.
// A comment that is not cairn's, and an @-import the harness resolves for
// itself, both survive into the output untouched.
func TestMarkersIgnoreWhatIsNotCairns(t *testing.T) {
	const template = "<!-- an ordinary comment -->\n@AGENTS.md\n${NOT_EXPANDED_HERE}\n{{ notATemplate }}\n"

	markers, err := Markers(template)
	if err != nil {
		t.Fatalf("Markers(): %v", err)
	}
	if len(markers) != 0 {
		t.Errorf("Markers() found %v in text holding none of cairn's", markers)
	}
	got, err := Substitute(template, nil, nil)
	if err != nil {
		t.Fatalf("Substitute(): %v", err)
	}
	if got != template {
		t.Errorf("Substitute() rewrote text it does not own:\n%q", got)
	}
}

// TestMarkersAreOneLine pins the property that keeps a marker findable without
// a parser: Go's "." does not match a newline, so a comment spanning lines is
// not a marker and is left alone.
func TestMarkersAreOneLine(t *testing.T) {
	const template = "<!-- cairn:slot\nrepo -->\n"

	markers, err := Markers(template)
	if err != nil {
		t.Fatalf("Markers(): %v", err)
	}
	if len(markers) != 0 {
		t.Errorf("Markers() read a marker across a line break: %v", markers)
	}
}

// TestUnfilledReportsOnlySlots covers what the operator hears about. A declared
// slot that filled nothing left the document shorter than it reads and is worth
// saying; an empty value is a fact about the instance — a boot with no scope —
// and is not.
func TestUnfilledReportsOnlySlots(t *testing.T) {
	const template = "<!-- cairn:slot filled -->\n<!-- cairn:slot empty -->\n<!-- cairn:value scope -->\n"

	// "scope" is declared as a slot as well as being a value name. Without it
	// the value marker names nothing in sections, reads as undeclared under the
	// two-value lookup, and stays silent whether or not the verb check above is
	// there — which would leave this test's own claim pinned by nothing.
	unfilled, err := Unfilled(template, map[string]string{
		"filled": "## Here\n\ncontent",
		"empty":  "",
		"scope":  "",
	})
	if err != nil {
		t.Fatalf("Unfilled(): %v", err)
	}
	if len(unfilled) != 1 || unfilled[0].Name != "empty" {
		t.Fatalf("Unfilled() = %v, want only the declared slot that filled nothing", unfilled)
	}
	if unfilled[0].Text != "<!-- cairn:slot empty -->" {
		t.Errorf("the report does not quote the marker: %q", unfilled[0].Text)
	}
}

// TestUnfilledIsSilentAboutASlotNobodyDeclared is the property that lets one
// shared template carry every marker any profile might fill.
//
// A name absent from sections names a slot this manifest never declared, which
// is not a fault: the profile simply does not use that block. Reporting it would
// print a line per unused marker and bury the case the warning exists for — a
// slot that was supposed to fill and did not. Distinguishing the two rests
// entirely on the two-value lookup, since a single-value one reads "" for both.
func TestUnfilledIsSilentAboutASlotNobodyDeclared(t *testing.T) {
	const template = "<!-- cairn:slot declared -->\n<!-- cairn:slot undeclared -->\n"

	unfilled, err := Unfilled(template, map[string]string{"declared": ""})
	if err != nil {
		t.Fatalf("Unfilled(): %v", err)
	}
	if len(unfilled) != 1 || unfilled[0].Name != "declared" {
		t.Fatalf("Unfilled() = %v, want only the declared slot", unfilled)
	}
}

// TestValueNamesIsClosedAndSorted pins the set a marker may name. It is closed
// so that an unknown value can be refused rather than silently substituting
// nothing, and sorted so a diagnostic lists it the same way twice.
func TestValueNamesIsClosedAndSorted(t *testing.T) {
	names := ValueNames()
	if !slices.IsSorted(names) {
		t.Errorf("ValueNames() = %v, which is not sorted", names)
	}
	want := []string{"binding", "model", "profile", "provider", "scope", "session"}
	if !slices.Equal(names, want) {
		t.Errorf("ValueNames() = %v, want %v", names, want)
	}
	// A fresh slice, so a caller cannot shrink the set an unknown name is
	// checked against.
	names[0] = "rewritten"
	if ValueNames()[0] == "rewritten" {
		t.Error("ValueNames() returns a slice a caller can rewrite")
	}
	// Deliberately absent, and each for its own reason: both are absolute
	// paths into one machine rather than facts about a profile.
	for _, absent := range []string{"boot_dir", "home"} {
		if slices.Contains(ValueNames(), absent) {
			t.Errorf("ValueNames() carries %q", absent)
		}
	}
}
