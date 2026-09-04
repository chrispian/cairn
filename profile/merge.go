package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ErrSpecMerge reports a manifest key two profiles in one chain both declare
// and that cannot be composed: the values disagree about their shape, or a
// member of a keyed collection carries nothing to key it by.
//
// It is reached only when two or more profiles declare the key. A shape a
// single profile got wrong still cascades untouched and is reported by the
// accessor that reads it, which knows what the key was for.
var ErrSpecMerge = errors.New("spec key cannot be merged")

// merger folds a descendant's value for one manifest key onto the ancestor's.
// path names the key for a diagnostic — "settings", or "install.skills" for a
// key reached inside another.
type merger interface {
	merge(path string, older, newer json.RawMessage) (json.RawMessage, error)
}

// installSkillsKey is the member of [SpecKeyInstall] that names the skills the
// installed layer plants. It is the one member of that key with a merge rule
// of its own; see [Spec.InstallSkills], which decodes it under the same name.
const installSkillsKey = "skills"

// accessDirectoriesKey is the member of [SpecKeyAccess] that names the
// directories an instance is granted. Like installSkillsKey it is the one
// member of its key with a rule of its own; see [Spec.AccessDirectories].
const accessDirectoriesKey = "directories"

// specMergers is the explicit table of keyed collections: the manifest keys
// whose members compose by key, and the key each one composes by. Every key
// that is not in this table replaces whole, closest-wins.
//
// A table, deliberately, rather than a rule inferred from the JSON in front of
// it. Cairn interprets only the manifest keys it renders and carries the rest
// untouched — see the package comment — and a rule like "any list of scalars
// unions" cannot tell [SpecKeySubagents] from an unknown key's list, or from
// the "tools" inside a [SpecKeySubagent] declaration. Inferring the behaviour
// would quietly reach inside keys this package has never heard of and break
// the promise that they survive the cascade byte for byte. So a key earns a
// merge rule by being named here, and nothing else does.
var specMergers = map[string]merger{
	// Maps, keyed by the destination path or the boot-relative path.
	SpecKeyTemplates: objectByKey{},
	SpecKeyFiles:     objectByKey{},
	SpecKeyTrees:     objectByKey{},

	// The settings documents compose at every depth: a descendant setting one
	// key under "permissions" keeps the siblings it did not mention. Arrays
	// inside them are not keyed and replace, like any other unkeyed value.
	//
	// The outermost level is the provider — see [SpecKeySettings] — and it
	// needs no rule of its own. Provider names are keys like any other, so a
	// profile declaring settings for claude alone leaves an ancestor's codex
	// document standing, and one declaring both composes both. That is the
	// same fold one level up from the one this key was already merged by, and
	// getting it for free is the whole reason the keying went at the top
	// rather than being spelled as a second mechanism.
	SpecKeySettings: objectByKey{deep: true},

	// The install namespace composes per member, and its skills are a keyed
	// collection of their own. Any other member of it replaces whole, so a
	// future install-only key needs no decision here to behave predictably.
	SpecKeyInstall: objectByKey{sub: map[string]merger{installSkillsKey: listOfIDs{}}},

	// The access namespace composes the same way, and its directories are
	// keyed by the path: a chain whose profiles each name directories grants
	// the union of them. Replacing instead would mean a profile could only
	// reach what its closest declaring ancestor reached, so extending a
	// profile to add one directory would silently drop the rest.
	SpecKeyAccess: objectByKey{sub: map[string]merger{accessDirectoriesKey: listOfIDs{}}},

	// Lists of objects, keyed by the "name" field each member carries.
	// spec.mcp is a list of agentlaunch.MCPServerSpec, not an object of
	// servers, which makes it structurally identical to spec.slots.
	SpecKeySlots: listByField{field: "name"},
	SpecKeyMCP:   listByField{field: "name"},

	// Lists whose member is its own key.
	SpecKeySkills:    listOfIDs{},
	SpecKeyPrompts:   listOfIDs{},
	SpecKeySubagents: listOfIDs{},
}

// mergeSpecKey folds a descendant's value for key onto the ancestor's already
// folded value.
//
// A key with no rule in [specMergers] returns the descendant's bytes unread,
// which is the whole-key replace every manifest key had before keyed
// collections existed and every unknown key still has.
func mergeSpecKey(key string, older, newer json.RawMessage) (json.RawMessage, error) {
	return mergeAt(key, specMergers[key], older, newer)
}

// Merge folds one more contributor onto a composed manifest value, under the
// rule [specMergers] composes key by — the same fold the cascade runs, called
// once with a value that came from somewhere other than a profile.
//
// It exists for the instance flags. `--skill a,b` contributes ids to
// [SpecKeySkills] and `--set <slot>=<value>` contributes an inline slot to
// [SpecKeySlots], and both are meant to land exactly as a part declaring the
// same thing would. Merging them here rather than beside the flag is what makes
// that literally true instead of approximately: there is one table of keyed
// collections, and a caller that reimplemented a union would be a second one.
//
// The overlay is the closer value: it replaces where the key replaces, and
// takes precedence member by member where the key is keyed. A composed value
// carrying nothing returns the overlay, and an overlay carrying nothing clears
// the key, which is the cascade's own rule for a null and not a special case
// here.
func Merge(key string, composed, overlay json.RawMessage) (json.RawMessage, error) {
	return mergeAt(key, specMergers[key], composed, overlay)
}

// MergeSettings folds an overlay onto a composed settings document under the
// rule [specMergers] composes [SpecKeySettings] by: a member the overlay
// declares replaces the one beneath it, a member it does not mention stands,
// and both compose at every depth.
//
// The document is one provider's, already selected by [Spec.Settings] — the
// caller is rendering for one target and has nothing to say to the others. So
// the rule applied here is the key's rule from the provider level down, which
// is where a harness's own vocabulary starts.
//
// It exists because the cascade is no longer the last thing to contribute to
// that document. bootdir.RenderSettings adds the directories an instance is
// granted, and folding them in through the key's own rule is what keeps one
// document from being composed two ways: a merge written beside the renderer
// would have to rediscover on its own that touching one nested key must leave
// that key's siblings standing.
//
// An overlay carrying nothing leaves composed exactly as it was, bytes
// included — this is the one place a null does not clear. Clearing is what a
// profile does to an ancestor's key, and the overlay here is not a profile: a
// renderer with nothing to contribute must leave the operator's document
// alone, not empty it. A composed value carrying nothing returns the overlay,
// so a profile that declared no settings at all still receives what cairn
// grants it.
func MergeSettings(composed, overlay json.RawMessage) (json.RawMessage, error) {
	if isUndeclared(overlay) {
		return composed, nil
	}
	return Merge(SpecKeySettings, composed, overlay)
}

// mergeAt applies m, guarding the cases where there is nothing to compose.
//
// A nil m replaces. A newer value that carries nothing — absent bytes, or the
// JSON null a profile clears an ancestor's key with — replaces, which is what
// makes null clear a whole collection. An older value that carries nothing is
// nothing to fold onto, so the newer value passes through as it was stored.
//
// That last guard is the one that looks redundant and is not. [Resolve] folds
// a key the moment anything ahead of it declared the key, and it carries a
// clearing null forward as the folded value — so a leaf redeclaring a key a
// middle profile cleared arrives here with two values that both count as
// declared. JSON null decodes into a map as an empty map rather than as an
// error, so without the guard that leaf's document would be composed against
// nothing and re-encoded in Go's spelling. Byte-identity is load-bearing:
// bootdir.RenderSettings writes spec.settings into a file, laying it out and
// changing nothing else, so what the cascade re-spells lands in a settings
// document the operator reads. Go's encoder sorts an object's keys, escapes
// <, > and & inside its strings, renumbers 0.5e3 to 500 and drops a duplicate
// key — none of which laying a document out would undo.
// Pinned by TestResolveSettingsRedeclaredAfterAMidChainClearAreByteIdentical.
//
// The plainer route to the same promise is one level up: a key exactly one
// profile in the chain declares never reaches a merger at all, because a
// merger is only ever called with two declared values. See [Spec].
func mergeAt(path string, m merger, older, newer json.RawMessage) (json.RawMessage, error) {
	if m == nil || isUndeclared(older) || isUndeclared(newer) {
		return newer, nil
	}
	return m.merge(path, older, newer)
}

// objectByKey composes a JSON object member by member: a member the descendant
// declares replaces the ancestor's member at that key, and every member it
// does not mention stands.
//
// A member set to JSON null removes it from the merged object. That is the
// same spelling that clears a whole key one level up, and it is why the closer
// of two profiles cannot restate a literal null in order to keep it: its own
// null clears. An ancestor's literal null is not lost — a descendant that
// never mentions the key leaves it standing. See docs/plan.md §3.
type objectByKey struct {
	// sub names the members that are keyed collections in their own right,
	// composed by the merger given rather than replaced. A member not named
	// here replaces.
	//
	// It applies at the top level of the key it was declared for and nowhere
	// below it: a deep merge recurses without it, so a nested member that
	// happens to share a name with one of these entries is not that entry and
	// is not composed by its rule.
	sub map[string]merger

	// deep composes any member both profiles declare as an object, all the way
	// down. It is what "keyed by every key at every depth" means for the
	// settings document; without it a descendant touching one nested key would
	// drop that key's siblings.
	deep bool
}

func (o objectByKey) merge(path string, older, newer json.RawMessage) (json.RawMessage, error) {
	oldm, err := decodeObject(path, older)
	if err != nil {
		return nil, err
	}
	newm, err := decodeObject(path, newer)
	if err != nil {
		return nil, err
	}

	out := make(map[string]json.RawMessage, len(oldm)+len(newm))
	maps.Copy(out, oldm)
	for key, value := range newm {
		if isUndeclared(value) {
			delete(out, key)
			continue
		}
		prev, seen := out[key]
		switch {
		case seen && o.sub[key] != nil:
			merged, err := mergeAt(path+"."+key, o.sub[key], prev, value)
			if err != nil {
				return nil, err
			}
			out[key] = merged
		case seen && o.deep && isObject(prev) && isObject(value):
			// Deep, and only deep. Recursing on o would carry sub down with
			// it and apply a top-level member's rule to any nested member
			// that merely shares its name.
			merged, err := objectByKey{deep: true}.merge(path+"."+key, prev, value)
			if err != nil {
				return nil, err
			}
			out[key] = merged
		default:
			out[key] = value
		}
	}
	return encodeJSON(out)
}

// listByField composes a JSON list of objects keyed by one field of each
// member — a slot by its name, an MCP server by its name.
//
// A member the descendant declares replaces the ancestor's member of the same
// name whole; a member is never merged field by field, because a member is not
// itself a keyed collection.
//
// There is no way to remove one member. A list member is identified by a field
// inside it rather than by a key above it, so the null that clears a member of
// an object has nowhere to be written — see docs/plan.md §3, which records the
// gap rather than inventing a spelling for it. The whole key still clears.
//
// One profile declaring the same key twice in its own list is refused here,
// reporting [ErrSpecMerge]. It is a malformed manifest — a template addresses
// a slot by name — and keying it last-write-wins would compose the duplicate
// away, leaving the merged value with nothing for the downstream accessor to
// catch. Detection would then stop exactly where the chain got hard to read.
//
// That leaves an asymmetry, stated rather than smoothed over. A chain where
// only one profile declares the key never reaches a merger, so its bytes are
// carried unread and the duplicate is still the accessor's to find:
// slots.ErrSlotName for spec.slots, bootdir.ErrMCPServer for spec.mcp. Both
// paths refuse. They refuse in different places, with different errors, and
// this package is the wrong place to fix that — reading a single profile's
// value to check it would end the promise that one declarer is carried byte
// for byte.
type listByField struct{ field string }

func (l listByField) merge(path string, older, newer json.RawMessage) (json.RawMessage, error) {
	keyed := make(map[string]json.RawMessage)
	for _, raw := range []json.RawMessage{older, newer} {
		members, err := decodeList(path, raw)
		if err != nil {
			return nil, err
		}
		// Per value, not across both: one member replacing another profile's
		// member of the same name is the whole point, and two members of the
		// same name inside one profile's list is the malformed manifest.
		seen := make(map[string]struct{}, len(members))
		for i, member := range members {
			key, err := l.keyOf(path, i, member)
			if err != nil {
				return nil, err
			}
			if _, duplicate := seen[key]; duplicate {
				return nil, fmt.Errorf("%w: spec.%s declares %q twice in one profile, and a merged collection holds one member per %q",
					ErrSpecMerge, path, key, l.field)
			}
			seen[key] = struct{}{}
			keyed[key] = member
		}
	}
	return encodeJSON(sortedValues(keyed))
}

// keyOf returns the value of the keying field, refusing a member that has none
// rather than composing every such member at the empty key and silently
// keeping one of them.
func (l listByField) keyOf(path string, i int, member json.RawMessage) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(member, &fields); err != nil {
		return "", fmt.Errorf("%w: spec.%s member %d is not an object, so it carries no %q to compose by",
			ErrSpecMerge, path, i, l.field)
	}
	var name string
	if raw, ok := fields[l.field]; ok {
		_ = json.Unmarshal(raw, &name)
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("%w: spec.%s member %d declares no %q, and two profiles declaring the key compose its members by that field",
			ErrSpecMerge, path, i, l.field)
	}
	return name, nil
}

// listOfIDs composes a JSON list whose member is its own key: a skill name, a
// profile id. Two profiles declaring the same id declare one member.
//
// Like [listByField] it has no spelling for removing one member, and for the
// same reason.
type listOfIDs struct{}

func (listOfIDs) merge(path string, older, newer json.RawMessage) (json.RawMessage, error) {
	keyed := make(map[string]json.RawMessage)
	for _, raw := range []json.RawMessage{older, newer} {
		members, err := decodeList(path, raw)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			compacted, err := compactJSON(path, member)
			if err != nil {
				return nil, err
			}
			keyed[string(compacted)] = compacted
		}
	}
	return encodeJSON(sortedValues(keyed))
}

// sortedValues returns a keyed collection's members ordered by key.
//
// Ordering is deterministic and nothing may depend on it. A merged collection
// is a set or a map: a template addresses a slot by name and owns document
// order through its markers, skills and subagents are sets, and the map-shaped
// keys never had an order to begin with. Sorting is what makes two renders of
// one profile identical; it is not a contract about sequence.
func sortedValues(keyed map[string]json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(keyed))
	for _, key := range slices.Sorted(maps.Keys(keyed)) {
		out = append(out, keyed[key])
	}
	return out
}

// decodeObject reads a manifest value the table says is an object, naming the
// key when it is not one.
func decodeObject(path string, raw json.RawMessage) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: spec.%s: two profiles declare it and it is not an object in both: %w",
			ErrSpecMerge, path, err)
	}
	return out, nil
}

// decodeList reads a manifest value the table says is a list, naming the key
// when it is not one.
func decodeList(path string, raw json.RawMessage) ([]json.RawMessage, error) {
	var out []json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: spec.%s: two profiles declare it and it is not a list in both: %w",
			ErrSpecMerge, path, err)
	}
	return out, nil
}

// isObject reports whether a manifest value is a JSON object, which is what
// decides whether a deep merge has another level to go down.
func isObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// compactJSON returns a value with its insignificant whitespace removed, so
// that a member's identity does not depend on how it was spelled.
func compactJSON(path string, raw json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, fmt.Errorf("%w: spec.%s: %w", ErrSpecMerge, path, err)
	}
	return json.RawMessage(buf.Bytes()), nil
}

// encodeJSON serializes a merged value with HTML escaping off.
//
// [json.Marshal] would rewrite <, > and & inside the members it carries
// through — they are [json.RawMessage], and the encoder compacts a raw message
// with escaping on. A merge composes what the operator wrote; it does not
// re-spell it.
func encodeJSON(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")), nil
}
