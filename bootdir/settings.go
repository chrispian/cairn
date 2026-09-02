package bootdir

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/chrispian/cairn/profile"
)

// RenderSettings returns the harness settings document, laid out for reading.
//
// The content is the operator's own, verbatim after the cascade rather than
// verbatim as stored, because the settings key is a keyed collection: a chain
// whose profiles each declare part of the document renders the composition of
// them.
//
// Nothing here reads the document. Cairn models no permission mode, validates
// no tool name, and translates nothing into a rule: the settings key is the
// operator's own values and they are written as they were composed, so what
// the harness does with them is settled by the harness and by whoever wrote
// them. A rule that turns out not to enforce is a fact about the harness, not
// a defect here.
//
// The layout is [IndentJSON], which moves whitespace and nothing else — see
// its doc for why that is not the same as handing a hand-spelled document to
// Go's encoder. A manifest that declares no settings key renders no file.
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
	return []File{{
		Path:    inst.Layout.Settings.RelPath,
		Content: IndentJSON(stored),
		Mode:    inst.Layout.Settings.Mode,
	}}, nil
}

// IndentJSON returns raw laid out one element per line at [JSONIndent] per
// level, ending in the newline that makes the result a text file.
//
// [json.Indent] is the whole transformation. It moves whitespace between
// tokens and changes nothing else, so key order, string spelling and number
// spelling all survive it — which is what separates laying a document out from
// re-encoding it. Handing a hand-spelled settings document to Go's encoder
// would re-spell its strings and re-order nothing predictably; this does
// neither, and the document that comes out is the one that went in with the
// newlines put back.
//
// A value that is not JSON is returned as it was stored. Every manifest value
// is validated before the store will write it and a merge composes valid JSON
// out of valid JSON, so this is unreachable through either — but a renderer
// that dropped an artifact because it could not lay it out prettily would fail
// at exactly the moment the operator most needs to see what is there.
//
// It is exported because a check needs the same transformation: normalizing
// both sides through one function is what keeps a comparison from reporting
// whitespace as drift. See [github.com/chrispian/cairn/install.Renderer].
func IndentJSON(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	var buf bytes.Buffer
	if err := json.Indent(&buf, trimmed, "", JSONIndent); err != nil {
		buf.Reset()
		buf.Write(trimmed)
	}
	return append(buf.Bytes(), '\n')
}
