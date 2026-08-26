package bootdir

import (
	"testing"

	"github.com/chrispian/cairn/profile"
)

// TestBootIsAbsentWhenNothingWasAssembled states the rule that an empty boot
// file is not written. A file holding nothing would read as a slot that
// resolved to nothing, which is a different fact from a profile that declared
// no slots at all.
func TestBootIsAbsentWhenNothingWasAssembled(t *testing.T) {
	for _, assembled := range []string{"", "   ", "\n\n"} {
		inst := testInstance(t, profile.Resolved{ID: "quiet"})
		inst.Boot = assembled

		files, err := renderBoot(inst)
		if err != nil {
			t.Fatalf("renderBoot() with %q assembled: %v", assembled, err)
		}
		if len(files) != 0 {
			t.Errorf("renderBoot() with %q assembled wrote %v, want no file",
				assembled, filePaths(files))
		}
	}
}

// TestBootIsWrittenVerbatim holds the boot file to the bytes the slot assembly
// produced. The content is another package's output; reformatting it here
// would make a boot directory disagree with the assembly it was rendered from.
//
// The one byte this package adds is the final newline. The assembler emits a
// section heading with no body for a slot that resolved empty, so whether the
// assembled string ends in a newline depends on which slot happened to come
// last — which is not a property a file's last byte should have.
func TestBootIsWrittenVerbatim(t *testing.T) {
	cases := map[string]struct{ assembled, want string }{
		"a trailing newline is kept as one": {
			assembled: "## repo\n\nbranch=main\n",
			want:      "## repo\n\nbranch=main\n",
		},
		"a missing trailing newline is added": {
			assembled: "## repo\n\nbranch=main",
			want:      "## repo\n\nbranch=main\n",
		},
		"an empty last slot still ends the file in a newline": {
			assembled: "## repo\n\nbranch=main\n\n## tasks",
			want:      "## repo\n\nbranch=main\n\n## tasks\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			inst := testInstance(t, profile.Resolved{ID: "reviewer"})
			inst.Boot = tc.assembled

			files, err := renderBoot(inst)
			if err != nil {
				t.Fatalf("renderBoot(): %v", err)
			}
			if len(files) != 1 {
				t.Fatalf("renderBoot() wrote %v, want exactly one file", filePaths(files))
			}
			if want := inst.Layout.Boot.RelPath; files[0].Path != want {
				t.Errorf("rendered at %q, want %q", files[0].Path, want)
			}
			if got := string(files[0].Content); got != tc.want {
				t.Errorf("the boot file is %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBootRefusesToDropAssembledContent covers the layout that declares no
// boot path while content was assembled anyway. Writing nothing would discard
// work that already ran commands and made requests, and say so nowhere.
func TestBootRefusesToDropAssembledContent(t *testing.T) {
	inst := testInstance(t, profile.Resolved{ID: "reviewer"})
	inst.Boot = "assembled"
	inst.Layout.Boot = Artifact{}

	if _, err := renderBoot(inst); err == nil {
		t.Fatal("renderBoot() with no declared boot path returned no error")
	}
}
