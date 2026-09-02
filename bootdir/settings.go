package bootdir

import (
	"bytes"
	"fmt"

	"github.com/chrispian/cairn/profile"
)

// RenderSettings returns the harness settings document, verbatim after the
// cascade.
//
// Verbatim after the cascade rather than verbatim as stored, because the
// settings key is a keyed collection: a chain whose profiles each declare part
// of the document renders the composition of them. A document exactly one
// profile declared is still byte for byte what the operator wrote — the
// cascade never re-serializes a key only one profile in the chain declares —
// and that is what keeps a hand-spelled settings document out of Go's encoder.
//
// Nothing here reads the document. Cairn models no permission mode, validates
// no tool name, and translates nothing into a rule: the settings key is the
// operator's own bytes and it is written as-is, so what the harness does with
// them is settled by the harness and by whoever wrote them. A rule that turns
// out not to enforce is a fact about the harness, not a defect here.
//
// The only edit is a trailing newline where the stored bytes lack one, so that
// the planted file is a text file. A manifest that declares no settings key
// renders no file.
func RenderSettings(inst *Instance) ([]File, error) {
	if inst == nil || inst.Profile == nil {
		return nil, ErrNoProfile
	}
	stored, declared := inst.Profile.Spec.Settings()
	if !declared {
		return nil, nil
	}
	if !inst.Layout.Settings.Declared() {
		return nil, fmt.Errorf(
			"%w: spec.%s is declared, but this layout declares no path for a settings document",
			ErrProviderLayout, profile.SpecKeySettings)
	}
	content := bytes.Clone(stored)
	if n := len(content); n == 0 || content[n-1] != '\n' {
		content = append(content, '\n')
	}
	return []File{{
		Path:    inst.Layout.Settings.RelPath,
		Content: content,
		Mode:    inst.Layout.Settings.Mode,
	}}, nil
}
