package install

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/profile"
)

// ErrUnexpectedArtifact reports that a renderer produced a path outside the
// provider directory the installed layer is rendered into.
//
// It stops the render rather than correcting it. The renderers are shared with
// the boot directory, so a change to one of their paths would otherwise
// relocate a file somewhere else inside the operator's home — quietly, in the
// one layer that is not disposable. A render that refuses is a render the
// operator can see; a render that moved a file is one they find later.
var ErrUnexpectedArtifact = errors.New("rendered artifact is outside the provider directory")

// harness is one provider's installed layer: the directory it is written into,
// the artifacts it holds, and the layout naming their paths.
//
// It is one value rather than three lookups so that the directory and the
// paths in the layout cannot be answered from different switch statements and
// disagree.
type harness struct {
	// dir is the provider directory relative to the install root.
	dir string

	// renderers are the artifacts, in render order.
	renderers []Renderer

	// layout is where the shared renderers read each artifact's path from.
	layout bootdir.Layout
}

// harnessFor returns the installed layer of one provider.
//
// Claude Code is the only harness implemented. Codex, opencode, and a profile
// declaring no provider at all report [bootdir.ErrUnsupportedProvider] rather
// than falling back to a layout that would write one harness's files into
// another's directory.
func harnessFor(p profile.Provider) (harness, error) {
	switch p {
	case profile.ProviderClaude:
		return harness{dir: ClaudeDirName, renderers: ClaudeRenderers(), layout: ClaudeLayout()}, nil
	case "":
		return harness{}, fmt.Errorf("%w: the resolved profile declares no provider",
			bootdir.ErrUnsupportedProvider)
	default:
		return harness{}, fmt.Errorf("%w: %q", bootdir.ErrUnsupportedProvider, p)
	}
}

// PlanterFor returns the renderers and layout for a provider, in render order,
// reporting [bootdir.ErrUnsupportedProvider] for a harness the installed layer
// has none for.
//
// It answers what a provider claims without rendering it, which is what a
// check needs: a profile declaring no skills renders nothing into the skills
// directory, and the question of whether something was left behind there can
// only be asked from the registration list. The caller receives a fresh slice
// it may modify.
func PlanterFor(p profile.Provider) ([]Renderer, bootdir.Layout, error) {
	h, err := harnessFor(p)
	if err != nil {
		return nil, bootdir.Layout{}, err
	}
	return h.renderers, h.layout, nil
}

// Render returns every file of the installed layer, paths relative to the
// install root, in render order. It writes nothing.
//
// The files come from [bootdir.RenderWith] over the provider's registered
// renderers, so the installed layer is the boot directory's renderers run
// against a layout naming different paths. That is also where every path is
// validated and a collision between two artifacts is refused: this package
// restates neither, because a second opinion on where a write may land is a
// second implementation to keep in step.
//
// What it adds is a containment check — see [ErrUnexpectedArtifact] — and the
// generated-file marker on the instruction file.
//
// An abstract profile renders. The installed layer is normally rendered from
// the abstract root of the cascade (plan §7), so refusing one here would
// refuse the profile this package mostly exists to render. It is `cairn boot`
// that refuses a direct boot of one.
//
// It touches the filesystem only to read the skills the profile names, and it
// never reads the install root, so a caller can diff a render against disk
// without going near a live configuration.
//
// Errors wrap [ErrNoProfile], [ErrNoRoot], [bootdir.ErrUnsupportedProvider],
// [bootdir.ErrArtifactPath], [bootdir.ErrDuplicatePath],
// [ErrUnexpectedArtifact], or come from a renderer unchanged.
func Render(lay *Layer) ([]File, error) {
	if lay == nil || lay.Profile == nil {
		return nil, ErrNoProfile
	}
	if lay.Root.IsZero() {
		return nil, ErrNoRoot
	}
	h, err := harnessFor(lay.Profile.Provider)
	if err != nil {
		return nil, err
	}
	files, err := bootdir.RenderWith(bootRenderers(h.renderers, lay.Profile.ID), layerInstance(lay, h.layout))
	if err != nil {
		return nil, err
	}
	if err := checkInside(files, h.dir); err != nil {
		return nil, err
	}
	return files, nil
}

// layerInstance returns the boot-directory instance the shared renderers are
// run over: the layer's resolved profile, the operator's home, and a layout
// naming the installed paths.
//
// Dir, Scope and Boot are deliberately zero, and each for its own reason. The
// installed layer is not a materialized instance — it is written beneath a
// root rather than into a directory of its own, so there is no Dir. Nothing
// has declared a scope at install time, because a scope belongs to one session
// and this layer is read by all of them. And Boot is empty because slots are
// resolved when an instance is materialized: rendering them here would run the
// operator's commands and requests at install time and freeze one moment's
// answers into every later session.
func layerInstance(lay *Layer, layout bootdir.Layout) *bootdir.Instance {
	return &bootdir.Instance{
		Layout:  layout,
		Profile: lay.Profile,
		Home:    lay.Home,
	}
}

// bootRenderers maps the installed layer's renderers onto the ones
// [bootdir.RenderWith] runs, wrapping the instruction file's so that it opens
// with the generated-file marker.
//
// The instruction file is matched by its artifact name rather than by its
// suffix. A later markdown artifact should not acquire a marker because it
// happens to end in ".md" and nobody noticed: whether a file carries one is a
// decision, and this is where it is written down.
func bootRenderers(renderers []Renderer, profileID string) []bootdir.Renderer {
	out := make([]bootdir.Renderer, 0, len(renderers))
	for _, r := range renderers {
		render := r.Render
		if r.Artifact == bootdir.AgentsFileName {
			render = markGenerated(render, profileID)
		}
		out = append(out, bootdir.Renderer{Artifact: r.Artifact, Render: render})
	}
	return out
}

// markGenerated wraps the instruction file's renderer so its artifact opens
// with [GeneratedMarker] and one blank line, ahead of the content the shared
// renderer produced.
//
// The marker is applied here rather than in package bootdir because only the
// installed layer has a reader for it. It is also applied to this artifact
// alone. The pointer file is not marked: its entire content is a one-line
// include of the instruction file, so a marker would be three times the file
// and would sit in the document that points at the one carrying it. Nobody
// should add it there for symmetry. The settings document is not marked
// either, because JSON has no comment syntax and a top-level key cairn
// invented would be cairn editing a schema that belongs to the harness.
//
// A renderer that produced no bytes still produces no file. An instruction
// file holding a marker and nothing else would claim cairn had rendered
// something when it had rendered nothing.
func markGenerated(render func(inst *bootdir.Instance) ([]File, error), profileID string) func(inst *bootdir.Instance) ([]File, error) {
	prefix := []byte(GeneratedMarker(profileID) + "\n\n")
	return func(inst *bootdir.Instance) ([]File, error) {
		files, err := render(inst)
		if err != nil {
			return nil, err
		}
		marked := make([]File, 0, len(files))
		for _, f := range files {
			if len(f.Content) == 0 {
				continue
			}
			content := make([]byte, 0, len(prefix)+len(f.Content))
			content = append(content, prefix...)
			f.Content = append(content, f.Content...)
			marked = append(marked, f)
		}
		return marked, nil
	}
}

// checkInside reports the first rendered path that is not beneath dir.
//
// The renderers are the boot directory's, and their paths come from a layout
// this package supplies, so the two are only in step for as long as both stay
// that way. Checking rather than trusting is what turns a change in one of
// them into a failed install instead of a file appearing somewhere else in the
// operator's home.
func checkInside(files []File, dir string) error {
	prefix := dir + "/"
	for _, f := range files {
		if !strings.HasPrefix(f.Path, prefix) {
			return fmt.Errorf("%w: %q is not under %q", ErrUnexpectedArtifact, f.Path, dir)
		}
	}
	return nil
}
