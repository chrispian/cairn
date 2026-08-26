// Package bootdir renders a resolved profile into the files of one boot
// directory and writes them there.
//
// Rendering and writing are separate on purpose. [Render] produces every file
// in memory, so a render that fails writes nothing; [Plant] stages the whole
// tree beside the target and moves it into place with one rename, so a boot
// directory is either complete or absent. A half-built boot directory is one
// an agent might boot from.
package bootdir

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/chrispian/cairn/profile"
	goprovider "github.com/hollis-labs/go-providers/provider"
)

// AgentsFileName is the instruction file every boot directory carries. Cairn
// declares it rather than reading it from a provider's BootDirSpec: it is the
// one artifact whose name is the same for every harness, which is what makes
// the pointer file below possible.
const AgentsFileName = "AGENTS.md"

// PointerFileContent is the entire content of a provider's pointer file — a
// one-line include of [AgentsFileName] and nothing else, so that a reader who
// opens either file knows which one carries the content.
const PointerFileContent = "@" + AgentsFileName + "\n"

// SkillsDirName is the directory, relative to the boot directory root,
// declared skills are planted into. No provider's BootDirSpec declares it;
// it is Claude Code's on-disk convention, one directory per skill.
const SkillsDirName = ".claude/skills"

// SkillFileName is the file a skill directory must hold for a harness to load
// the skill at all.
const SkillFileName = "SKILL.md"

// DefaultFileMode is the mode a rendered file is written with when its [File]
// carries none.
const DefaultFileMode fs.FileMode = 0o644

// DefaultDirMode is the mode the boot directory and every directory inside it
// is created with.
const DefaultDirMode fs.FileMode = 0o755

// ErrUnsupportedProvider reports that no layout is implemented for a profile's
// provider, or that the profile declares none at all.
var ErrUnsupportedProvider = errors.New("unsupported provider")

// ErrProviderLayout reports that a provider's BootDirSpec no longer declares
// an artifact Cairn renders for it, so Cairn would be writing to a path the
// harness has stopped reading. It is an error rather than a fallback, because
// the failure it prevents is silent.
var ErrProviderLayout = errors.New("provider no longer declares an artifact cairn renders")

// Artifact is one file of a boot directory: where the harness reads it from,
// and the mode it is written with.
type Artifact struct {
	// RelPath is the path relative to the boot directory root,
	// slash-separated. Empty means the layout does not carry this artifact.
	RelPath string

	// Mode is the permission mode. Zero means [DefaultFileMode].
	Mode fs.FileMode
}

// Declared reports whether a carries a path at all.
func (a Artifact) Declared() bool { return a.RelPath != "" }

// Layout is where one provider's harness reads each boot-directory artifact
// from.
//
// Four of its artifacts — Pointer, Boot, MCP and Settings — are taken from
// go-providers' BootDirSpec, which is the library that owns each harness's
// on-disk convention. Cairn takes the path and the mode from there and
// supplies its own content: the spec's own render functions are never called,
// because some of them have side effects on the operator's real home
// directory.
//
// Agents and Skills are Cairn's, not the provider's. No BootDirSpec declares
// either.
type Layout struct {
	// Provider is the harness this layout describes.
	Provider profile.Provider

	// Agents is the instruction file, always [AgentsFileName].
	Agents Artifact

	// Pointer is the harness's own instruction file, holding
	// [PointerFileContent]. Undeclared when the harness reads
	// [AgentsFileName] directly.
	Pointer Artifact

	// Boot is the file the assembled slots are written to.
	Boot Artifact

	// MCP is the MCP server configuration.
	MCP Artifact

	// Settings is the harness settings document, written verbatim from the
	// profile's manifest.
	Settings Artifact

	// SkillsDir is the directory declared skills are planted under, one
	// directory per skill.
	SkillsDir string

	// CwdPreference is where the harness expects to be invoked, and
	// ProjectDirArg is its flag pattern for granting access to the scope
	// directory. Both come from the provider's BootDirSpec. Cairn does not
	// launch anything, so nothing here reads them; they are carried so that
	// the caller printing a boot directory can also print how to open it.
	CwdPreference goprovider.CwdPreference
	ProjectDirArg string
}

// LayoutFor returns the [Layout] for a resolved profile's provider.
//
// Claude Code is the only harness implemented. Codex and opencode report
// [ErrUnsupportedProvider] rather than falling back to a layout that would
// write another harness's files, and so does a profile that declares no
// provider at all.
func LayoutFor(p profile.Provider) (Layout, error) {
	switch p {
	case profile.ProviderClaude:
		return claudeLayout()
	case "":
		return Layout{}, fmt.Errorf("%w: the resolved profile declares no provider", ErrUnsupportedProvider)
	default:
		return Layout{}, fmt.Errorf("%w: %q", ErrUnsupportedProvider, p)
	}
}

// claudeLayout derives the Claude Code layout from that adapter's BootDirSpec.
func claudeLayout() (Layout, error) {
	spec := goprovider.NewClaudeAdapter().BootDirSpec()
	declared, err := artifacts(spec, "CLAUDE.md", "boot.md", ".mcp.json", ".claude/settings.json")
	if err != nil {
		return Layout{}, fmt.Errorf("claude: %w", err)
	}
	return Layout{
		Provider:      profile.ProviderClaude,
		Agents:        Artifact{RelPath: AgentsFileName},
		Pointer:       declared["CLAUDE.md"],
		Boot:          declared["boot.md"],
		MCP:           declared[".mcp.json"],
		Settings:      declared[".claude/settings.json"],
		SkillsDir:     SkillsDirName,
		CwdPreference: spec.CwdPreference,
		ProjectDirArg: spec.ProjectDirArg,
	}, nil
}

// artifacts looks each wanted path up in spec's planted files and returns them
// carrying the mode the spec declares. A path the spec no longer declares
// reports [ErrProviderLayout]: the harness has moved the file, and writing to
// the old path would leave a boot directory that looks complete and is not.
//
// The spec's PlantedFile.Render functions are deliberately never invoked. At
// least one of them writes to the operator's real home directory when handed a
// boot directory, and Cairn renders its own content for every path here
// anyway.
func artifacts(spec goprovider.BootDirSpec, want ...string) (map[string]Artifact, error) {
	byPath := make(map[string]goprovider.PlantedFile, len(spec.PlantedFiles))
	for _, pf := range spec.PlantedFiles {
		byPath[pf.RelPath] = pf
	}
	out := make(map[string]Artifact, len(want))
	for _, rel := range want {
		pf, ok := byPath[rel]
		if !ok {
			return nil, fmt.Errorf("%w: %q is not in its BootDirSpec", ErrProviderLayout, rel)
		}
		out[rel] = Artifact{RelPath: pf.RelPath, Mode: pf.Mode}
	}
	return out, nil
}
