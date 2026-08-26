// Package profile holds the stored profile, the resolved profile a boot
// directory is rendered from, and the extends cascade between them.
//
// A profile is one row of the profiles table plus an opaque JSON rendering
// manifest. Cairn interprets only the manifest keys it renders and carries the
// rest untouched, so a key this package has never heard of survives the
// cascade and reaches whatever does know it.
package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// SpecKeyTemplates maps boot-directory-relative paths to the template
	// text rendered into them. A template's value takes the same two shapes a
	// files value does — a literal, or a source resolved at materialization.
	SpecKeyTemplates = "templates"

	// SpecKeyTrees maps boot-directory-relative paths to the source directory
	// copied there whole.
	SpecKeyTrees = "trees"

	// SpecKeySubagents holds the ids of the profiles rendered as subagent
	// definitions in the boot directory. It is declared by the profile being
	// booted, and names other profiles.
	SpecKeySubagents = "subagents"

	// SpecKeySubagent holds one profile's own declaration of the definition it
	// is rendered as when another profile names it under [SpecKeySubagents].
	// It is an opaque map, carried into the definition's frontmatter the way
	// [SpecKeySettings] is carried into the settings document.
	SpecKeySubagent = "subagent"
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

// Subagents returns the profile ids under [SpecKeySubagents]. A manifest
// declaring none returns nil and no error.
func (s Spec) Subagents() ([]string, error) {
	var out []string
	if err := s.decode(SpecKeySubagents, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Subagent returns this profile's own subagent declaration under
// [SpecKeySubagent] exactly as it was stored, and whether the key was declared
// at all. The bytes are not read here: what they hold is the renderer's
// question, and whether they are wanted at all is the question of whether some
// other profile named this one.
//
// A key set to JSON null reads as undeclared, matching [Spec.Settings] and the
// cascade, where null is how a profile clears a key an ancestor declared.
func (s Spec) Subagent() (json.RawMessage, bool) {
	raw, ok := s[SpecKeySubagent]
	if !ok || isUndeclared(raw) {
		return nil, false
	}
	return raw, true
}

// Settings returns the provider settings document under [SpecKeySettings]
// exactly as it was stored, and whether the key was declared at all. The bytes
// are not reformatted: the manifest is what the operator wrote.
//
// A key set to JSON null reads as undeclared, matching the other accessors and
// the cascade, where null is how a profile clears a key an ancestor declared.
func (s Spec) Settings() (json.RawMessage, bool) {
	raw, ok := s[SpecKeySettings]
	if !ok || isUndeclared(raw) {
		return nil, false
	}
	return raw, true
}

// Files returns the path-to-content map under [SpecKeyFiles]. A manifest
// declaring none returns nil and no error.
func (s Spec) Files() (map[string]FileEntry, error) {
	return s.entries(SpecKeyFiles)
}

// entries decodes a manifest key holding a path-to-[FileEntry] map. Two keys
// carry that shape — see [Spec.Files] and [Spec.Templates] — and decoding them
// through one function is what keeps the two legal value forms the same in
// both.
func (s Spec) entries(key string) (map[string]FileEntry, error) {
	var raw map[string]json.RawMessage
	if err := s.decode(key, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]FileEntry, len(raw))
	for rel, v := range raw {
		entry, err := parseFileEntry(v)
		if err != nil {
			return nil, fmt.Errorf("spec key %q: %q: %w", key, rel, err)
		}
		out[rel] = entry
	}
	return out, nil
}

// Templates returns the path-to-template map under [SpecKeyTemplates]. A
// manifest declaring none returns nil and no error.
//
// A value takes the same two shapes a files value does, and for the same
// reason: a template is text, and text a profile already knows is a literal
// while text that lives on disk is a source. What separates the two keys is
// what happens to the text afterwards — a template's markers are substituted
// and a file's bytes are not.
func (s Spec) Templates() (map[string]FileEntry, error) {
	return s.entries(SpecKeyTemplates)
}

// Trees returns the destination-to-source map under [SpecKeyTrees]. A manifest
// declaring none returns nil and no error.
//
// Each value names a directory copied whole to the destination. It is not a
// slot source: a static_dir source concatenates the files it finds into one
// string, which is right for a slot and destroys a directory.
func (s Spec) Trees() (map[string]string, error) {
	var out map[string]string
	if err := s.decode(SpecKeyTrees, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// FileEntry is one value of the files manifest key. A value is either a
// literal string, planted verbatim, or a slot source resolved at
// materialization the same way a slot is.
//
// Both forms exist because both are needed. A literal covers content the
// profile already knows. A source covers content that is only true at boot —
// the case the portfolio's other planters all have: torque plants
// tasks/<id>/task.md, task.json and a per-task process.md rendered from live
// task state, and static path-to-content cannot express that.
//
// Exactly one field is set.
type FileEntry struct {
	// Literal is the content, when the manifest gave a string.
	Literal string

	// Source resolves to the content, when the manifest gave an object. Nil
	// means the entry is a literal.
	Source *agentcontext.SlotSource
}

// IsSource reports whether the entry must be resolved before it can be
// planted.
func (e FileEntry) IsSource() bool { return e.Source != nil }

// parseFileEntry reads one files value in either form. A JSON string is a
// literal; a JSON object is a slot source. Anything else is refused by name,
// because the two legal shapes are easy to describe and a silent coercion here
// would plant the wrong bytes at a path the profile promised.
func parseFileEntry(raw json.RawMessage) (FileEntry, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return FileEntry{}, errors.New("the value is empty")
	}
	switch trimmed[0] {
	case '"':
		var lit string
		if err := json.Unmarshal(trimmed, &lit); err != nil {
			return FileEntry{}, err
		}
		return FileEntry{Literal: lit}, nil
	case '{':
		var src agentcontext.SlotSource
		if err := json.Unmarshal(trimmed, &src); err != nil {
			return FileEntry{}, err
		}
		return FileEntry{Source: &src}, nil
	default:
		return FileEntry{}, fmt.Errorf(
			"the value is neither a string nor a source object — a files entry is either the content itself or %s",
			`{"kind": "...", ...}`)
	}
}

// Expander returns the value of an environment variable, or the empty string
// for one that is not set.
//
// It is a parameter rather than a direct read so that nothing below the
// composition root consults the process environment on its own. A renderer
// reads nothing outside the instance it was handed — the same rule that puts
// the operator's home on the instance rather than in the renderer that expands
// a "~/" — and an environment is the same kind of hidden input.
type Expander func(name string) string

// ExpandEnv returns s with $VAR and ${VAR} replaced by look's answers, and a
// name that is not set replaced by nothing, which is [os.Expand]'s behaviour
// and every shell's.
//
// A nil look expands nothing and returns s unchanged. That is deliberate: a
// caller that was handed no environment leaves the operator's own text in
// place, so a diagnostic quotes what they wrote rather than what an
// unconfigured expansion made of it.
func ExpandEnv(s string, look Expander) string {
	if s == "" || look == nil {
		return s
	}
	return os.Expand(s, look)
}

// ExpandPath returns a manifest path with its variables expanded and a leading
// "~" replaced by home.
//
// The order is variables first. A variable holding a home-relative path — an
// AGENT_HOME of "~/agents" — then has its tilde expanded too, where expanding
// the tilde first would leave it in the middle of the result. Anything else
// beginning with "~", "~user/x" included, is returned untouched and left to the
// caller's absolute-path check, so an unexpanded form fails by naming itself
// rather than by resolving somewhere unexpected.
//
// A path that needs home and has none returns [ErrNoHomeForPath]. Every other
// judgement — whether the result is absolute, whether it exists, whether it is
// the right kind of thing — belongs to the caller, which knows what the path
// was for.
func ExpandPath(raw string, home string, look Expander) (string, error) {
	expanded := ExpandEnv(raw, look)
	if expanded != "~" && !strings.HasPrefix(expanded, "~/") {
		return expanded, nil
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("%w: %q", ErrNoHomeForPath, expanded)
	}
	return filepath.Join(home, strings.TrimPrefix(expanded, "~")), nil
}

// QuotedExpansion names a manifest value for a diagnostic: what the operator
// wrote, and what it expanded to when the two differ.
//
// Naming only the expansion is the failure this exists to prevent. A profile
// writing "$ROOT/docs" with ROOT unset expands to "/docs", which is absolute
// and passes every check but the last, and an error quoting "/docs" sends the
// operator looking for a path they never wrote instead of at the variable they
// did not set.
//
// It is one function so that every diagnostic about an expanded value reads the
// same way, whether the value was a tree's source, a skills directory, or the
// path inside a slot.
func QuotedExpansion(declared, expanded string) string {
	if declared == expanded {
		return fmt.Sprintf("%q", declared)
	}
	return fmt.Sprintf("%q, which expanded to %q", declared, expanded)
}

// ErrNoHomeForPath reports a manifest path written with a leading "~/" where no
// home directory is known.
var ErrNoHomeForPath = errors.New("no home directory to expand a path against")

// isUndeclared reports whether a manifest value carries nothing: absent bytes,
// or the JSON null a profile clears an ancestor's key with.
func isUndeclared(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// decode unmarshals the value at key into dest. A key that is absent, or
// present as JSON null, leaves dest untouched and reports no error — a
// manifest is not obliged to declare anything.
func (s Spec) decode(key string, dest any) error {
	raw, ok := s[key]
	if !ok || isUndeclared(raw) {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("spec key %q: %w", key, err)
	}
	return nil
}
