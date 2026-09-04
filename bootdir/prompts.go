package bootdir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/chrispian/cairn/profile"
)

// ErrPromptsSource reports that prompts were declared but the directory to
// read them from is unusable: the manifest names none, the one it names is not
// absolute once a leading "~/" is expanded, or it is not a directory. Cairn
// ships no prompts, so a profile that declares one has to say where it lives.
var ErrPromptsSource = errors.New("prompts directory is unusable")

// ErrPromptName reports a declared name that cannot name a file beneath the
// prompts source: it is empty, it is "." or "..", it holds a path separator,
// it carries the extension cairn appends, or the same name is declared twice.
var ErrPromptName = errors.New("invalid prompt name")

// ErrPromptNotFound reports that a declared prompt has no file in the prompts
// source. It stops the render rather than omitting the prompt, because the
// command it would have planted answers "Unknown command" and nothing says
// why.
var ErrPromptNotFound = errors.New("prompt not found")

// ErrPromptContent reports a prompt that cannot be planted: it is not a
// regular file, or it substitutes away to nothing.
var ErrPromptContent = errors.New("unusable prompt content")

// PromptFileExt is the extension a prompt file carries, in the bundle and in
// the boot directory both. A name declares neither of them — spec.prompts
// holds "handoff", and the file is handoff.md at each end.
const PromptFileExt = ".md"

// promptKind describes the prompt collection for the shared content helpers.
var promptKind = contentKind{
	key:       profile.SpecKeyPrompts,
	dirKey:    profile.SpecKeyPromptsDir,
	plural:    "prompts",
	entry:     "prompt file",
	nameErr:   ErrPromptName,
	sourceErr: ErrPromptsSource,
}

// renderPrompts returns one file per prompt spec.prompts declares, substituted
// like any other template and planted under the layout's prompts directory.
//
// A prompt is [RenderSkills] for content a person invokes. The declaration
// cascades the same way, composes the same way, and resolves against a
// directory the manifest names for the same reason — so what is here is only
// the part that is about prompts: one flat file per name, substituted, at a
// path the harness reads as a namespaced command.
//
// It is a template and not a copy, which is the whole difference from a skill.
// The text goes through [Substitute], so a prompt carries `cairn:slot` and
// `cairn:value` markers and reaches the boot directory already knowing its
// scope, its session and its profile. Nothing new renders it: this is the same
// substitution spec.templates gets, over text read from a file instead of out
// of the manifest.
//
// A profile declaring no prompts renders nothing and reports no error. One
// declaring a prompt that cannot be planted reports [ErrPromptsSource],
// [ErrPromptName], [ErrPromptNotFound] or [ErrPromptContent], each naming the
// prompt and the path it was read from.
//
// It is unexported, where [RenderSkills] is not. The difference is who calls
// them: the installed layer renders skills through [RenderInstallSkills] and
// so needs the boot one on the same boundary, while nothing outside this
// package renders prompts — `cairn install` deliberately plants none. That is
// [renderSubagents]'s situation exactly, and this follows it.
//
// The output is deterministic: the prompts in the order the resolved manifest
// carries them, which for a key the cascade composed is sorted — see
// [profile.Resolve]. Nothing here depends on which, because the planted paths
// are one per name.
func renderPrompts(inst *Instance) ([]File, error) {
	if inst == nil || inst.Profile == nil {
		return nil, ErrNoProfile
	}
	declared, err := inst.Profile.Spec.Prompts()
	if err != nil {
		return nil, err
	}
	if len(declared) == 0 {
		return nil, nil
	}
	target := strings.TrimSpace(inst.Layout.PromptsDir)
	if target == "" {
		return nil, fmt.Errorf(
			"%w: spec.%s declares %s, but this layout declares no prompts directory",
			ErrProviderLayout, profile.SpecKeyPrompts, quotedNames(declared))
	}
	dir, err := inst.Profile.Spec.PromptsDir()
	if err != nil {
		return nil, err
	}
	source, err := contentSource(dir, promptKind, declared, inst.Home, inst.Env)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(declared))
	files := make([]File, 0, len(declared))
	for _, raw := range declared {
		name := strings.TrimSpace(raw)
		if err := checkContentName(name, promptKind); err != nil {
			return nil, err
		}
		// The one mistake the shared check has no opinion about, and the one
		// an operator is most likely to make: a prompt is a file with an
		// extension, and writing the extension into the name would be read
		// from handoff.md.md. The not-found diagnostic would name that path
		// and be perfectly accurate about a path nobody wrote.
		// EqualFold, because the filesystems this runs on mostly are. On APFS
		// "handoff.MD" names the same file as "handoff.md", so a
		// case-sensitive guard would let it through to be read from
		// "handoff.MD.md" — which is the diagnostic about a path nobody wrote
		// that this guard exists to prevent, arriving in a different spelling.
		if strings.EqualFold(filepath.Ext(name), PromptFileExt) {
			return nil, fmt.Errorf(
				"%w: spec.%s declares %q; a prompt is named without its %s, so that would be read from %s",
				ErrPromptName, profile.SpecKeyPrompts, name, PromptFileExt,
				filepath.Join(source, name+PromptFileExt))
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("%w: spec.%s declares %q twice",
				ErrPromptName, profile.SpecKeyPrompts, name)
		}
		seen[name] = struct{}{}

		file, err := renderPrompt(inst, source, target, name)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// emptiedBy says why a prompt rendered nothing: it held nothing to begin with,
// or every marker in it stood for nothing, named.
//
// The distinction is the operator's next move. An empty file is a file to
// write; a file emptied by substitution is a slot that did not resolve, and
// that failure is already on stderr under a name this can be matched to.
func emptiedBy(text string) string {
	markers, err := Markers(text)
	if err != nil || len(markers) == 0 {
		return "holds nothing"
	}
	named := make([]string, 0, len(markers))
	for _, m := range markers {
		one := fmt.Sprintf("%s %q", m.Verb, m.Name)
		if !slices.Contains(named, one) {
			named = append(named, one)
		}
	}
	return "substitutes away to nothing — " + strings.Join(named, ", ") + " stood for nothing"
}

// renderPrompt reads the prompt named name under source, substitutes it, and
// returns it as one file under target.
func renderPrompt(inst *Instance, source, target, name string) (File, error) {
	from := filepath.Join(source, name+PromptFileExt)
	// Stat and not Lstat, so a symlink to a regular file is read by value. That
	// is the skills copier's behaviour and this follows it rather than
	// narrowing it here: a bundle that keeps one prompt somewhere else and
	// links to it is a bundle, and refusing the link would be a rule invented
	// for the newer collection alone.
	info, err := os.Stat(from)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// A dangling symlink lands here too, and "nothing at <path>" would be
		// wrong about it in the way that costs the most: there IS something
		// there, and an operator told to create the file finds one already in
		// the way. So the link is named, as the skills copier names it.
		if link, readErr := os.Readlink(from); readErr == nil {
			return File{}, fmt.Errorf("%w: prompt %q at %s is a symlink to %s, which does not exist",
				ErrPromptContent, name, from, link)
		}
		return File{}, fmt.Errorf("%w: spec.%s declares %q, which is not in %s: nothing at %s",
			ErrPromptNotFound, profile.SpecKeyPrompts, name, source, from)
	case err != nil:
		return File{}, fmt.Errorf("stat prompt %q at %s: %w", name, from, err)
	case !info.Mode().IsRegular():
		return File{}, fmt.Errorf("%w: prompt %q at %s is not a regular file",
			ErrPromptContent, name, from)
	}
	text, err := os.ReadFile(from)
	if err != nil {
		return File{}, fmt.Errorf("read prompt %q at %s: %w", name, from, err)
	}
	rendered, err := Substitute(string(text), inst.Sections, inst.Values)
	if err != nil {
		return File{}, fmt.Errorf("prompt %q at %s: %w", name, from, err)
	}
	// A prompt that substitutes away to nothing is refused, where a template
	// that does renders no file. The difference is what each one is addressed
	// by. A template names its own destination and a profile that leaves one
	// empty has simply not written that document; a prompt is named in a list
	// and invoked by that name, so an empty one plants nothing and answers
	// "Unknown command" at the moment it is typed — which is the failure the
	// skills renderer refuses an empty skill to prevent, arriving later.
	//
	// It is worth being explicit about what this does to the slot rule. A slot
	// that fails to resolve is survivable everywhere else in cairn — the
	// section is dropped and the boot carries on — and a prompt made only of
	// such a slot is the one place that failure reaches the exit status. That
	// is the cost of refusing rather than planting nothing, and it is why the
	// message names the markers rather than only reporting the emptiness: the
	// slot failure is already on stderr, and this is what matches the two up.
	if strings.TrimSpace(rendered) == "" {
		return File{}, fmt.Errorf("%w: prompt %q at %s %s, so /%s:%s would answer "+
			"\"Unknown command\"", ErrPromptContent, name, from, emptiedBy(string(text)),
			PromptNamespace, name)
	}
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}
	return File{Path: path.Join(target, name+PromptFileExt), Content: []byte(rendered)}, nil
}
