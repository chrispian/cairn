package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestMarshalBindingWritesWhatAPersonWouldType is the format contract.
//
// Bindings are hand-edited, and by more than one writer, so what --save-as
// leaves behind has to be a file a person would have written: the keys named
// after the model, in the order a composition resolves; two-space block
// sequences, which is where a comment about one part can go; and nothing that
// announces the file as machine-written, because a saved binding lives in the
// same directory as hand-authored ones and being told which is which would
// invite treating them differently.
func TestMarshalBindingWritesWhatAPersonWouldType(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Binding
		want string
	}{{
		name: "a binding that composes nothing is the two lines it always was",
		in:   Binding{Name: "eng", ProfileID: "engineer", Scope: "cairn"},
		want: "profile: engineer\nscope: cairn\n",
	}, {
		// The key order is the order a composition resolves in, so a reader
		// can read the file top to bottom and be reading the sequence
		// `cairn boot` runs. Prompts sit beside skills because they compose
		// at the same point, and after them because --skill is registered
		// first.
		name: "every list is a block sequence, in composition order",
		in: Binding{
			Name:      "w2",
			ProfileID: "writer",
			Parts:     []string{"docs-only", "nanite-conventions"},
			Skills:    []string{"qhealth", "adr"},
			Prompts:   []string{"handoff", "reset-scope"},
			Scope:     "nanite",
		},
		want: "profile: writer\n" +
			"parts:\n" +
			"  - docs-only\n" +
			"  - nanite-conventions\n" +
			"skills:\n" +
			"  - qhealth\n" +
			"  - adr\n" +
			"prompts:\n" +
			"  - handoff\n" +
			"  - reset-scope\n" +
			"scope: nanite\n",
	}, {
		// An empty list marshalled as "parts: []" would be a saved binding
		// that reads differently from every hand-authored one beside it, for
		// no reason its reader could see.
		name: "an empty list is absent rather than empty",
		in:   Binding{Name: "w", ProfileID: "writer", Parts: []string{}},
		want: "profile: writer\n",
	}, {
		// The scope key is where a value that YAML reads as something else
		// turns up, because it is the one an operator types a path into.
		name: "a scope that would not round-trip bare is quoted",
		in:   Binding{Name: "n", ProfileID: "writer", Scope: "~"},
		want: "profile: writer\nscope: \"~\"\n",
	}, {
		name: "a home-relative scope is not quoted, because it does not need to be",
		in:   Binding{Name: "n", ProfileID: "writer", Scope: "~/dev/projects/cairn"},
		want: "profile: writer\nscope: ~/dev/projects/cairn\n",
	}, {
		name: "a variable in a scope survives verbatim",
		in:   Binding{Name: "n", ProfileID: "writer", Scope: "$CAIRN_PROFILE_ROOT/../work"},
		want: "profile: writer\nscope: $CAIRN_PROFILE_ROOT/../work\n",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MarshalBinding(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("the file is\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestMarshalBindingReproducesTheHandAuthoredExamples is the format claim
// stated as something checkable rather than as taste.
//
// The bindings in examples/bundle were written by hand. Reading them and
// rendering them back gets the same bytes — the prose comments aside, which no
// marshaller can invent — which is what it means for a saved binding to look
// like a hand-authored one. It is also the closest thing to a fixture-free
// check that the parser and the marshaller agree: every key one writes, the
// other reads back into the value it came from.
func TestMarshalBindingReproducesTheHandAuthoredExamples(t *testing.T) {
	root := filepath.Join("..", "examples", "bundle")
	cat, err := Open(root)
	if err != nil {
		t.Fatalf("open the example bundle: %v", err)
	}
	bindings := cat.Bindings()
	if len(bindings) == 0 {
		t.Fatal("the example bundle holds no bindings, so this test asserts nothing")
	}
	composed := 0
	for _, b := range bindings {
		t.Run(b.Name, func(t *testing.T) {
			text, err := os.ReadFile(filepath.Join(root, BindingsDir, b.Name+bindingExt))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			got, err := MarshalBinding(b)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if want := uncommented(string(text)); string(got) != want {
				t.Errorf("rendering the hand-authored file gives\n%q\nwant\n%q", got, want)
			}
		})
		if len(b.Parts) > 0 || len(b.Skills) > 0 {
			composed++
		}
	}
	// At least one of them composes something, so that the block-sequence half
	// of the format is exercised by a file a person actually wrote rather than
	// only by the table above.
	if composed == 0 {
		t.Error("no example binding names a part or a skill, so the list keys are untested here")
	}
}

// TestABindingRoundTripsThroughItsOwnFormat pins the two halves of the format
// against each other. They live in one package precisely so that they cannot
// drift, and this is what says they have not.
func TestABindingRoundTripsThroughItsOwnFormat(t *testing.T) {
	root := t.TempDir()
	writeProfileFile(t, root, "writer", "---\nid: writer\n---\n")
	writeProfileFile(t, root, "docs-only", "---\nid: docs-only\n---\n")

	want := Binding{
		Name:      "w2",
		ProfileID: "writer",
		Parts:     []string{"docs-only"},
		Skills:    []string{"qhealth", "adr"},
		Prompts:   []string{"handoff"},
		Scope:     "~/dev/projects/cairn",
	}
	text, err := MarshalBinding(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path, err := BindingPath(root, want.Name)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	writeFile(t, path, string(text))

	cat, err := Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, err := cat.Binding("w2")
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	if got.ProfileID != want.ProfileID || got.Scope != want.Scope ||
		!slices.Equal(got.Parts, want.Parts) || !slices.Equal(got.Skills, want.Skills) ||
		!slices.Equal(got.Prompts, want.Prompts) {
		t.Errorf("the binding read back is %+v, want %+v", *got, want)
	}
}

// TestBindingPathRefusesANameThatIsNotAFileName. A binding's name is its
// file's base name, so a name that cannot be one is a binding nothing could
// boot — and, for the "../" spelling, a write outside the bundle the operator
// pointed at.
func TestBindingPathRefusesANameThatIsNotAFileName(t *testing.T) {
	for _, name := range []string{"", "   ", "a/b", "../evil", ".", "..", ".hidden"} {
		if _, err := BindingPath("/bundle", name); !errors.Is(err, ErrBindingName) {
			t.Errorf("BindingPath(%q) returned %v, want ErrBindingName", name, err)
		}
	}
	got, err := BindingPath("/bundle", "  eng-docs  ")
	if err != nil {
		t.Fatalf("BindingPath: %v", err)
	}
	if want := filepath.Join("/bundle", BindingsDir, "eng-docs.yaml"); got != want {
		t.Errorf("BindingPath returned %q, want %q", got, want)
	}
}

// TestABindingListEntryThatNamesNothingIsRefusedOnWriteToo is the other half
// of the test below, and the half that is easy to leave out.
//
// A marshaller that quietly dropped what the parser refuses would be the
// write-side version of the same silent difference — one fewer part in the
// file than the caller asked for — in the one package that owns both sides.
func TestABindingListEntryThatNamesNothingIsRefusedOnWriteToo(t *testing.T) {
	for _, in := range []Binding{
		{Name: "w", ProfileID: "writer", Parts: []string{"docs-only", "  "}},
		{Name: "w", ProfileID: "writer", Skills: []string{""}},
	} {
		if _, err := MarshalBinding(in); err == nil {
			t.Errorf("MarshalBinding(%+v) wrote a list with an entry that names nothing", in)
		}
	}
}

// TestABindingListEntryThatNamesNothingIsRefused. An entry that is there and
// means nothing is a typo, and composing one fewer part than the file appears
// to name is a difference nobody would go looking for.
func TestABindingListEntryThatNamesNothingIsRefused(t *testing.T) {
	for _, key := range []string{"parts", "skills"} {
		t.Run(key, func(t *testing.T) {
			root := t.TempDir()
			writeProfileFile(t, root, "writer", "---\nid: writer\n---\n")
			writeFile(t, filepath.Join(root, BindingsDir, "w.yaml"),
				"profile: writer\n"+key+":\n  - \"\"\n")
			_, err := Open(root)
			if err == nil {
				t.Fatal("a binding holding an empty list entry was read")
			}
			if !strings.Contains(err.Error(), key+"[0] names nothing") {
				t.Errorf("the refusal does not name the entry: %v", err)
			}
		})
	}
}

// TestABindingKeyTheFormatDoesNotHaveIsRefused is the format's own rule turned
// on the format.
//
// Every YAML decoder discards an unknown key by default, so `part:` for
// `parts:` — or `skill:` for `skills:`, which differs from the flag that fills
// it by one character — would produce a binding that composes nothing, boots
// cleanly and says nothing at all. That is the silent drop this whole design
// refuses everywhere else, in the one file handed to other tools as a
// contract.
func TestABindingKeyTheFormatDoesNotHaveIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"a singular parts key", "profile: writer\npart:\n  - docs-only\n", `no binding key named "part"`},
		{"a key named after the flag", "profile: writer\nskill:\n  - q\n", `no binding key named "skill"`},
		{"the singular of the newest key", "profile: writer\nprompt:\n  - handoff\n", `no binding key named "prompt"`},
		{"a key from another format", "profile: writer\nwith: [docs-only]\n", `no binding key named "with"`},
		{"a document that is not a mapping", "- writer\n", "is not a mapping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeProfileFile(t, root, "writer", "---\nid: writer\n---\n")
			writeProfileFile(t, root, "docs-only", "---\nid: docs-only\n---\n")
			writeFile(t, filepath.Join(root, BindingsDir, "w.yaml"), tc.body)
			_, err := Open(root)
			if err == nil {
				t.Fatal("a binding holding a key the format does not have was read")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal is %v, want it to carry %q", err, tc.want)
			}
			// And it says what could have been written instead, which is the
			// whole reason this is not yaml's own KnownFields error.
			for _, key := range bindingKeys {
				if !strings.Contains(err.Error(), strconv.Quote(key)) {
					t.Errorf("the refusal does not name the key %q an operator could have written: %v", key, err)
				}
			}
		})
	}
}

// TestAnEmptyBindingFileIsRefusedInTheFormatsOwnWords. An empty file is a
// binding that names no profile, and saying so beats a parser-level complaint
// about a document with no content.
func TestAnEmptyBindingFileIsRefusedInTheFormatsOwnWords(t *testing.T) {
	root := t.TempDir()
	writeProfileFile(t, root, "writer", "---\nid: writer\n---\n")
	writeFile(t, filepath.Join(root, BindingsDir, "w.yaml"), "")
	_, err := Open(root)
	if err == nil {
		t.Fatal("an empty binding file was read")
	}
	if !strings.Contains(err.Error(), "names no profile") {
		t.Errorf("the refusal is %v, want it in the vocabulary of the format", err)
	}
}

// uncommented strips the comment lines and blank lines from a binding file, so
// that what a marshaller could produce is what it is compared against.
func uncommented(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
