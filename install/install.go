// Package install renders the installed layer — the configuration a harness
// reads for every session on the machine, rather than for one boot directory
// — from the same profile a boot directory is rendered from.
//
// It is the same artifacts at different paths. The renderers are
// [github.com/chrispian/cairn/bootdir]'s, run over an [bootdir.Instance] whose
// [bootdir.Layout] names the installed paths, so the two layers cannot drift
// apart the way two copies of a renderer would. What is not shared is what
// only makes sense per session: a boot file assembled from slots, the MCP
// servers a boot directory declares, and the arbitrary paths spec.files
// plants.
//
// # cairn install is human-executed
//
// Every agent working on Cairn runs under the very directory this package
// writes. An agent that runs the install rewrites its own live configuration
// mid-session. Nothing in this package is safe to invoke to "check that it
// works"; [Check] against a fixture root is.
package install

import (
	"errors"
	"io/fs"
	"strconv"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/profile"
)

// ClaudeDirName is the directory, relative to the install root, that Claude
// Code's installed layer is rendered into.
const ClaudeDirName = ".claude"

// SettingsFileName is the name, inside a provider directory, of the settings
// document the harness reads.
const SettingsFileName = "settings.json"

// SkillsDirName is the directory, inside a provider directory, the declared
// skills are copied into.
const SkillsDirName = "skills"

// StagingPattern is the [os.MkdirTemp] pattern for the directory a render is
// staged in before it is moved into place. It is created inside the install
// root so that every move is a rename within one filesystem.
const StagingPattern = ".cairn-install-*"

// ErrNoRoot reports that the install root is not set.
var ErrNoRoot = errors.New("install root is not set")

// ErrRootNotAbsolute reports an install root that is not an absolute path.
var ErrRootNotAbsolute = errors.New("install root is not an absolute path")

// ErrRootNotFound reports that the install root does not exist. Unlike the
// database, Cairn never creates it: the installed layer goes inside a home
// directory that already exists, and creating one would mean cairn had
// resolved the wrong path.
var ErrRootNotFound = errors.New("install root not found")

// ErrRootNotDirectory reports an install root that exists and is not a
// directory.
var ErrRootNotDirectory = errors.New("install root is not a directory")

// ErrNoProfile reports that a [Layer] carries no resolved profile.
var ErrNoProfile = errors.New("layer has no resolved profile")

// File is one rendered file of the installed layer, with a path relative to
// the install root. It is [bootdir.File] because it is the same artifact.
type File = bootdir.File

// Layer is one render of the installed layer: where it goes, what it is
// rendered from, and the material rendering needs that the profile does not
// carry. Everything that varies is here, so nothing below reads the
// environment.
type Layer struct {
	// Root is the directory the provider directories are written beneath. The
	// zero Root is an error, never a default.
	Root Root

	// Profile is the resolved profile the layer is rendered from.
	//
	// It is normally abstract — the installed layer is usually the root of the
	// cascade — and that is not checked. Refusing an abstract profile here
	// would refuse the profile this package mostly exists to render; `cairn
	// boot` is where a direct boot of one is refused.
	Profile *profile.Resolved

	// Home is the operator's home directory, used to expand a manifest path
	// written with a leading "~/". Carried rather than read, for the reason
	// [bootdir.Instance].Home is.
	Home string
}

// Result is what one [Install] wrote.
type Result struct {
	// Root is the install root the files were written beneath.
	Root string

	// Files are the paths written, relative to the root and slash-separated,
	// in render order. It is the manifest [Check] diffs against disk, and the
	// set its sweep treats as cairn's.
	Files []string
}

// Renderer produces one artifact of the installed layer.
//
// It carries the same [bootdir.Renderer] the boot directory runs, plus where
// the artifact lands and whether it is a tree — see [Renderer.Tree].
type Renderer struct {
	// Artifact names what this renderer produces, for diagnostics and for
	// reading the registration list. It is a label relative to the provider
	// directory, not a path.
	Artifact string

	// Tree reports that Artifact names a directory this renderer fills rather
	// than one file.
	//
	// It exists so that [Check] derives the directories cairn owns from this
	// list rather than from what a particular render produced. The difference
	// is whether an orphan is found: a profile declaring no skills renders
	// nothing into the skills directory, and a sweep scoped to the render
	// would stop looking at that directory in exactly the case where something
	// was left behind.
	Tree bool

	// Render is the boot-directory renderer this artifact is produced by. The
	// instance it is handed carries a [bootdir.Layout] naming the installed
	// paths, so the same function serves both layers.
	Render func(inst *bootdir.Instance) ([]File, error)
}

// ClaudeRenderers returns the artifacts of Claude Code's installed layer, in
// render order.
//
// It is deliberately shorter than a boot directory's.
//
// There is no boot file: slots are resolved when an instance is materialized,
// and the installed layer is not. There is no MCP configuration: plan §6 drops
// the audit that used to guard it, and user-level MCP is not a file in this
// directory.
//
// There are no spec.files, and that one follows from where the two layers
// write rather than from anyone's taste. A boot directory is created fresh and
// refuses to plant if it already exists, so an arbitrary path→content map can
// only ever land on empty ground. The installed layer writes into a directory
// that already exists and is full of the operator's live state, where the same
// map lands on whatever is already there. It compounds with the sweep:
// rendering spec.files here would make cairn start claiming ownership of
// arbitrary paths in a home directory for orphan-reporting purposes, and
// having claimed them, report on them.
//
// The caller receives a fresh slice it may modify.
func ClaudeRenderers() []Renderer {
	return []Renderer{
		{Artifact: bootdir.AgentsFileName, Render: bootdir.RenderAgents},
		{Artifact: pointerFileName, Render: bootdir.RenderPointer},
		{Artifact: SettingsFileName, Render: bootdir.RenderSettings},
		{Artifact: SkillsDirName, Render: bootdir.RenderSkills, Tree: true},
	}
}

// pointerFileName is the harness's own instruction file inside the provider
// directory. It never carries content of its own — see
// [bootdir.PointerFileContent].
const pointerFileName = "CLAUDE.md"

// ClaudeLayout returns the [bootdir.Layout] the installed layer is rendered
// through: the same artifact names a boot directory uses, at the paths the
// harness reads them from beneath the install root.
//
// The boot and MCP artifacts are deliberately undeclared. A renderer handed an
// undeclared path for content the profile declared reports it rather than
// dropping it, which is why those two are not in [ClaudeRenderers] either.
func ClaudeLayout() bootdir.Layout {
	return bootdir.Layout{
		Provider:  profile.ProviderClaude,
		Agents:    bootdir.Artifact{RelPath: ClaudeDirName + "/" + bootdir.AgentsFileName},
		Pointer:   bootdir.Artifact{RelPath: ClaudeDirName + "/" + pointerFileName},
		Settings:  bootdir.Artifact{RelPath: ClaudeDirName + "/" + SettingsFileName},
		SkillsDir: ClaudeDirName + "/" + SkillsDirName,
	}
}

// GeneratedMarker returns the one line the installed instruction file opens
// with: the command that wrote it and the profile it came from.
//
// Plan §9 holds cairn's own prose out of rendered agent files until the
// operator has reviewed it, and the test that separates this from what §9
// guards is whether the operator could have written the same sentence in a
// profile body. "Escalate to your reports_to" — yes, trivially, and cairn
// taking that sentence is precisely what §9 is about. "These bytes were
// rendered by cairn install from profile X" — no. A profile body cannot
// truthfully assert its own rendering provenance; the claim would be false the
// moment the same body rendered anywhere else. Only the renderer knows. §9
// prohibits cairn making claims about an agent's conduct or about what content
// means; this is cairn describing its own action, which it is the sole
// authority on.
//
// It states the fact and stops: no advice about what to edit instead, and
// nothing about which session sees it. It is an HTML comment so that an
// operator opening the file in an editor sees it while it stays quiet in an
// agent's context.
//
// The boot directory gets no marker. It is created fresh, disposable, and
// never hand-edited with an expectation of persistence, so the line would be
// noise in every session's context with no reader to serve.
func GeneratedMarker(profileID string) string {
	return "<!-- Generated by `cairn install` from profile " + strconv.Quote(profileID) + ". -->"
}

// DirMode is the mode a directory of the installed layer is created with.
const DirMode fs.FileMode = 0o755
