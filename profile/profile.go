// Package profile holds the stored profile, the resolved profile a boot
// directory is rendered from, and the extends cascade between them.
//
// A profile is one row of the profiles table plus an opaque JSON rendering
// manifest. Cairn interprets only the manifest keys it renders and carries the
// rest untouched, so a key this package has never heard of survives the
// cascade and reaches whatever does know it.
package profile

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hollis-labs/agentkit/agentcontext"
	"github.com/hollis-labs/agentkit/agentlaunch"
)

// Provider names the CLI coding agent harness a profile's boot directory is
// rendered for. It is the profiles table's provider column.
type Provider string

const (
	// ProviderClaude is the Claude Code CLI.
	ProviderClaude Provider = "claude"

	// ProviderCodex is the Codex CLI.
	ProviderCodex Provider = "codex"

	// ProviderOpenCode is the opencode CLI.
	ProviderOpenCode Provider = "opencode"
)

// Valid reports whether p names a harness Cairn knows a layout for. The empty
// provider is not valid: a profile that declares no harness is a configuration
// error rather than a default.
func (p Provider) Valid() bool {
	switch p {
	case ProviderClaude, ProviderCodex, ProviderOpenCode:
		return true
	}
	return false
}

// String returns p as its stored string.
func (p Provider) String() string { return string(p) }

// Spec is a profile's rendering manifest: opaque JSON held as its top-level
// keys, of which Cairn interprets only the ones it renders.
//
// Holding the values as [json.RawMessage] is what makes an unknown key
// harmless. The cascade merges whole keys without looking inside them, and a
// key nothing renders is written back out exactly as it arrived.
type Spec map[string]json.RawMessage

// Manifest keys Cairn renders. Every other key in a [Spec] is carried and
// ignored — see the package comment.
const (
	// SpecKeySlots holds a list of agentcontext.SlotSpec assembled into
	// boot.md.
	SpecKeySlots = "slots"

	// SpecKeyMCP holds a list of agentlaunch.MCPServerSpec rendered into
	// .mcp.json.
	SpecKeyMCP = "mcp"

	// SpecKeySkills holds the names of the skill directories copied into the
	// boot directory's skills tree.
	SpecKeySkills = "skills"

	// SpecKeySkillsDir holds the directory those skill names are copied from.
	// Cairn ships no skills, so a profile declaring skills has to say where
	// they live.
	SpecKeySkillsDir = "skills_dir"

	// SpecKeySettings holds the provider settings document, written verbatim.
	SpecKeySettings = "settings"

	// SpecKeyFiles maps boot-directory-relative paths to their contents.
	SpecKeyFiles = "files"
)

// Profile is one stored profile: a row of the profiles table, with its
// manifest decoded into a [Spec] and nothing else interpreted.
//
// It is the input to the cascade, not the thing a boot directory is rendered
// from — see [Resolved].
type Profile struct {
	// ID is the profile's primary key.
	ID string

	// Extends is the id of the profile this one inherits from, or empty at the
	// root of a chain.
	Extends string

	// Abstract marks a profile that exists to be extended rather than booted.
	// It does not cascade: a concrete profile extending an abstract one is
	// still concrete.
	Abstract bool

	// Name is the profile's display name.
	Name string

	// Description is a one-line summary.
	Description string

	// Provider is the harness the boot directory is rendered for.
	Provider Provider

	// Model is the model identifier handed to that harness.
	Model string

	// Body is the profile's prose. It is the one field the cascade
	// concatenates rather than overwrites.
	Body string

	// Spec is the rendering manifest.
	Spec Spec

	// CreatedAt and UpdatedAt are the row's timestamps. Nothing renders them.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Resolved is a profile after the extends cascade: the fields a renderer
// reads, with every ancestor already folded in. Rendering never cascades
// again.
type Resolved struct {
	// ID is the profile the cascade was resolved for — the leaf of the chain.
	ID string

	// Chain is every profile id the cascade walked, ancestor-first, ending at
	// ID. It is provenance: nothing renders from it.
	Chain []string

	// Abstract is the leaf profile's own flag, carried so a caller can refuse
	// to boot one. It is not inherited.
	Abstract bool

	// Name, Description, Provider and Model are the closest declared value of
	// each field in the chain.
	Name        string
	Description string
	Provider    Provider
	Model       string

	// Body is every profile's body concatenated ancestor-first.
	Body string

	// Spec is the merged manifest: for each top-level key, the value from the
	// closest profile in the chain that declares it.
	Spec Spec
}

// Slots returns the slot specifications under [SpecKeySlots]. A manifest
// declaring no slots returns nil and no error.
func (s Spec) Slots() ([]agentcontext.SlotSpec, error) {
	var out []agentcontext.SlotSpec
	if err := s.decode(SpecKeySlots, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MCP returns the MCP server definitions under [SpecKeyMCP]. A manifest
// declaring none returns nil and no error.
func (s Spec) MCP() ([]agentlaunch.MCPServerSpec, error) {
	var out []agentlaunch.MCPServerSpec
	if err := s.decode(SpecKeyMCP, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Skills returns the skill names under [SpecKeySkills]. A manifest declaring
// none returns nil and no error.
func (s Spec) Skills() ([]string, error) {
	var out []string
	if err := s.decode(SpecKeySkills, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SkillsDir returns the directory under [SpecKeySkillsDir] that skill names
// are resolved against. A manifest declaring none returns the empty string and
// no error; whether that is a problem depends on whether any skill was
// declared, which is the skills renderer's question.
func (s Spec) SkillsDir() (string, error) {
	var out string
	if err := s.decode(SpecKeySkillsDir, &out); err != nil {
		return "", err
	}
	return out, nil
}

// Settings returns the provider settings document under [SpecKeySettings]
// exactly as it was stored, and whether the key was declared at all. The bytes
// are not reformatted: the manifest is what the operator wrote.
func (s Spec) Settings() (json.RawMessage, bool) {
	raw, ok := s[SpecKeySettings]
	if !ok || len(raw) == 0 {
		return nil, false
	}
	return raw, true
}

// Files returns the path-to-content map under [SpecKeyFiles]. A manifest
// declaring none returns nil and no error.
func (s Spec) Files() (map[string]string, error) {
	var out map[string]string
	if err := s.decode(SpecKeyFiles, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// decode unmarshals the value at key into dest. A key that is absent, or
// present as JSON null, leaves dest untouched and reports no error — a
// manifest is not obliged to declare anything.
func (s Spec) decode(key string, dest any) error {
	raw, ok := s[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("spec key %q: %w", key, err)
	}
	return nil
}
