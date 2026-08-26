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

// AgentsFileName is the template destination the installed layer renders its
// instruction file from. Cairn declares the name rather than reading it from a
// provider's BootDirSpec: it is the one artifact whose name is the same for
// every harness.
//
// It is not a file cairn insists on. A boot directory renders whatever
// templates a profile declares, at whatever paths it declares them; this name
// matters only where a layout has to map a destination onto a path of its own.
const AgentsFileName = "AGENTS.md"

// PointerFileName is the harness's own instruction file. Cairn declares the
// name because a layout has to know which template destination lands there;
// what the file holds is the profile's, like every other template.
const PointerFileName = "CLAUDE.md"

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
// Three of its artifacts — Pointer, MCP and Settings — are taken from
// go-providers' BootDirSpec, which is the library that owns each harness's
// on-disk convention. Cairn takes the path and the mode from there and
// supplies its own content: the spec's own render functions are never called,
// because some of them have side effects on the operator's real home
// directory.
//
// Agents, Skills and Subagents are Cairn's, not the provider's. No BootDirSpec
// declares any of them.
type Layout struct {
	// Provider is the harness this layout describes.
	Provider profile.Provider

	// Agents is where a template declared for [AgentsFileName] is written.
	Agents Artifact

	// Pointer is where a template declared for [PointerFileName] is written.
	// Undeclared when the harness reads [AgentsFileName] directly.
	Pointer Artifact

	// MCP is the MCP server configuration.
	MCP Artifact

	// Settings is the harness settings document, written verbatim from the
	// profile's manifest.
	Settings Artifact

	// SkillsDir is the directory declared skills are planted under, one
	// directory per skill.
	SkillsDir string

	// SubagentsDir is the directory subagent definitions are planted under,
	// one file per named profile. Like SkillsDir it is cairn's, not the
	// provider's: no BootDirSpec declares it.
	SubagentsDir string

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
	declared, err := artifacts(spec, PointerFileName, ".mcp.json", ".claude/settings.json")
	if err != nil {
		return Layout{}, fmt.Errorf("claude: %w", err)
	}
	return Layout{
		Provider:      profile.ProviderClaude,
		Agents:        Artifact{RelPath: AgentsFileName},
		Pointer:       declared[PointerFileName],
		MCP:           declared[".mcp.json"],
		Settings:      declared[".claude/settings.json"],
		SkillsDir:     SkillsDirName,
		SubagentsDir:  SubagentsDirName,
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
