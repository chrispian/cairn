package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/chrispian/cairn/profile"
	"gopkg.in/yaml.v3"
)

// bindingExt is the extension a binding file is written with. One extension
// and not two: a bundle where half the bindings are .yml and half are .yaml is
// a bundle where an operator cannot guess the name of the file they are
// looking for.
const bindingExt = ".yaml"

// profileExt is the extension a profile file is written with. Anything else in
// the profiles directory is ignored rather than refused — a README beside the
// profiles is not a broken profile.
const profileExt = ".md"

// checkDir reports whether path names an existing directory, and returns it.
//
// It is the first thing [Open] does, and the reason it is separate from the
// reads below is the diagnostic. A bundle that is not there makes every read
// under it fail, and without this the operator gets the first of those — "no
// profiles directory", naming a path inside a directory that does not exist —
// instead of being told that the bundle they named is not a bundle.
func checkDir(path string) (string, error) {
	dir := strings.TrimSpace(path)
	if dir == "" {
		return "", fmt.Errorf("%w: no directory was named", ErrBundleNotFound)
	}
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("%w: %s", ErrBundleNotFound, dir)
	case err != nil:
		return "", fmt.Errorf("read the profile bundle %s: %w", dir, err)
	case !info.IsDir():
		return "", fmt.Errorf("%w: %s is not a directory", ErrBundleNotFound, dir)
	}
	return dir, nil
}

// readProfiles reads every profile file the bundle holds: the profiles
// directory, and the immediate contents of [PartsDir] inside it.
//
// The two locations fill ONE map, and that is the whole of the feature. A part
// is not a kind, so there is no second map, no second parser and no second
// namespace for a lookup to consult — everything downstream of here sees the
// flat id map it has always seen, and neither the resolver, the composer, the
// listing nor `--save-as` learns that a subdirectory exists.
//
// Order is root first and parts second, which decides nothing. It is not a
// precedence: a duplicate id is refused rather than resolved, so no traversal
// order can pick a winner and reordering these two calls would change no
// outcome but the order of the two paths in the diagnostic.
func readProfiles(root string) (map[string]profile.Profile, error) {
	dir := filepath.Join(root, ProfilesDir)
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%w: %s", ErrNoProfilesDir, dir)
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	out := make(map[string]profile.Profile, len(entries))
	// The file each id was read from, kept only so that a duplicate can name
	// both documents. It is discarded with this function: an id maps to one
	// profile or the bundle did not open, so nothing downstream has a question
	// this could answer.
	at := make(map[string]string, len(entries))
	if err := readProfileDir(dir, entries, out, at); err != nil {
		return nil, err
	}

	// parts is read through the entry the root listing already returned, not
	// by joining the name and reading it. The difference is symlinks: os.ReadDir
	// reports the directory entry's own type, so a `parts` that is a symlink to
	// a directory has IsDir false here and is passed over, where reading the
	// joined path would have followed it. A bundle is a directory of files that
	// git reviews, and a link out of the tree is content nobody reviewing the
	// bundle can see.
	//
	// Passed over silently, as every other subdirectory is. See [PartsDir].
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() != PartsDir {
			continue
		}
		sub := filepath.Join(dir, PartsDir)
		subEntries, err := os.ReadDir(sub)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", sub, err)
		}
		if err := readProfileDir(sub, subEntries, out, at); err != nil {
			return nil, err
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%w: neither %s nor %s holds a %s file",
			ErrNoProfilesDir, dir, filepath.Join(dir, PartsDir), profileExt)
	}
	return out, nil
}

// readProfileDir reads the profile files of one directory into out, recording
// where each came from in at.
//
// It descends into nothing. The root listing's own subdirectories are skipped
// here and [readProfiles] reaches the one it reads deliberately, so this is
// the same loop over both locations and neither of them can grow a third by
// accident.
func readProfileDir(dir string, entries []os.DirEntry, out map[string]profile.Profile, at map[string]string) error {
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != profileExt {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		text, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		p, err := parseProfile(string(text), entry.Name())
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if first, ok := at[p.ID]; ok {
			return fmt.Errorf("%w: %q is declared by %s and by %s — rename one, "+
				"since a profile is named by its id wherever the file sits",
				ErrDuplicateProfileID, p.ID, first, path)
		}
		at[p.ID] = path
		out[p.ID] = p
	}
	return nil
}

// ReadProfile reads one profile out of a file named directly, instead of out
// of a bundle's profiles directory.
//
// It is how a composition loads a part by path. The part is an ordinary
// profile in the ordinary format — the same frontmatter keys, the same closed
// set, the same manifest conversion, the same diagnostics. What it is not is a
// catalog entry: the file has no bundle to be listed in and no id the bundle
// knows, so the caller decides what to call it, and its extends still resolves
// against the catalog.
//
// The file is NOT held to being named after the id it declares, and that is
// the difference between this and [parseProfile].
//
// The reason is narrow and it is the whole of it: [parseProfile]'s rule is
// about the catalog's map, which is keyed by id while the listing walks file
// names, so a bundled file disagreeing with itself resolves under one spelling
// and lists under the other. That reason does not transfer, because there is no
// map here keyed by anything a profile declares — a part is keyed by the path
// it was read from, and its id is overwritten with that path by the caller. The
// check would be made and its result discarded.
//
// What the rule cost is likewise narrow, and worth being accurate about: a
// generated part that declares no id already worked, taking the file's name.
// It bit only a generator that writes an id AND picks one the tempfile it
// landed in is not named after — which is the natural thing to write, and a
// requirement to name the file after the id would be the friction the path form
// exists to remove, wearing a different hat.
//
// Any extension is read, not only [profileExt]. The bundle's directory listing
// skips a file that is not a profile because it cannot tell one from a README;
// a path names exactly one file, and refusing to read the file the operator
// pointed at because of how it is spelled would be a rule with nothing behind
// it.
func ReadProfile(path string) (*profile.Profile, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the profile %s: %w", path, err)
	}
	base := filepath.Base(path)
	p, err := parseFile(string(text), strings.TrimSuffix(base, filepath.Ext(base)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &p, nil
}

// bindingFile is one binding as its file is written. The name is the file's,
// so it is not a field.
//
// The field order is the file's order, and the file's order is the order a
// composition resolves in: the base profile, the parts merged onto its chain,
// the skills and prompts merged after those, and last the scope — which is not part of the
// composition at all but a fact about the instance. A reader who knows how
// `cairn boot` works can read the file top to bottom and be reading the same
// sequence.
//
// The keys are named after the model and not after the flags that fill them,
// which is a rule this file already followed: `profile` is the profile a
// binding boots, while boot's own `--profile` names the bundle directory. So
// the parts key is "parts" rather than "with" — `with` is a preposition
// belonging to a command line, and every key here is a noun naming something
// the binding holds.
type bindingFile struct {
	Profile string   `yaml:"profile"`
	Parts   []string `yaml:"parts,omitempty"`
	Skills  []string `yaml:"skills,omitempty"`
	Prompts []string `yaml:"prompts,omitempty"`
	Scope   string   `yaml:"scope,omitempty"`
}

// MarshalBinding renders b as the bytes of its file.
//
// It is the one thing this package does that is not reading, and it is
// deliberately not a write: the bytes are returned and the composition root
// puts them somewhere. What lives here is the format — which keys, in what
// order, spelled how — because the parser above is the other half of it and
// the two drifting is the only way a binding cairn wrote stops being a binding
// cairn reads.
//
// The rendering is what a person would type. Two-space indent, block
// sequences, no document marker, no generated header: a saved binding sits in
// the same directory as hand-authored ones, and one that announced itself as
// machine-written would invite being treated as machine-owned. It carries no
// timestamp for the same reason, and because git already knows.
//
// b.Name is not written. The file's name is the binding's name, so writing it
// inside would create the one disagreement the layout exists to prevent.
func MarshalBinding(b Binding) ([]byte, error) {
	id := strings.TrimSpace(b.ProfileID)
	if id == "" {
		return nil, fmt.Errorf("binding %q names no profile — a binding is a base profile, "+
			"the parts merged onto it, the skills and prompts it carries and a default scope", b.Name)
	}
	parts, err := listOf("binding "+b.Name, "parts", b.Parts)
	if err != nil {
		return nil, err
	}
	skills, err := listOf("binding "+b.Name, "skills", b.Skills)
	if err != nil {
		return nil, err
	}
	prompts, err := listOf("binding "+b.Name, "prompts", b.Prompts)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(bindingFile{
		Profile: id,
		Parts:   parts,
		Skills:  skills,
		Prompts: prompts,
		Scope:   strings.TrimSpace(b.Scope),
	}); err != nil {
		return nil, fmt.Errorf("render binding %q: %w", b.Name, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("render binding %q: %w", b.Name, err)
	}
	return buf.Bytes(), nil
}

// BindingPath returns the file the binding called name is read from and
// written to.
//
// It is where a name is checked, because the name and the file are the same
// fact. A name holding a separator would name a file outside the bindings
// directory, or inside a directory of it that nothing lists — either way, a
// binding that cannot be booted by the name it was saved under.
func BindingPath(root, name string) (string, error) {
	n := strings.TrimSpace(name)
	switch {
	case n == "":
		return "", fmt.Errorf("%w: no name was given", ErrBindingName)
	case strings.ContainsRune(n, '/'), strings.ContainsRune(n, filepath.Separator):
		return "", fmt.Errorf("%w: %q holds a path separator, and a binding is named by its file", ErrBindingName, n)
	case strings.HasPrefix(n, "."):
		return "", fmt.Errorf("%w: %q begins with a dot, and a binding is named by its file", ErrBindingName, n)
	}
	return filepath.Join(root, BindingsDir, n+bindingExt), nil
}

// bindingKeys is every key a binding file may hold, in the order they are
// written. It exists so that [parseBinding] can name the whole set when it
// refuses one that is not in it — an operator who mistyped a key needs to see
// the spelling that would have worked, not only that theirs was not it.
var bindingKeys = []string{"profile", "parts", "skills", "prompts", "scope"}

// parseBinding reads one binding file, refusing a key the format does not
// have.
//
// The refusal is the whole reason this is not two lines of yaml.Unmarshal. A
// binding is small, hand-edited, and edited by more than one writer, and an
// unknown key is silently discarded by every YAML decoder in its default mode
// — so `part:` for `parts:`, or `skill:` for `skills:` which differs from the
// flag that fills it by one character, produces a binding that composes
// nothing, boots cleanly, and says nothing at all. That is the exact failure
// this format's own rules are written against: a silent drop is worse than a
// refusal.
//
// The keys are checked here rather than through yaml's KnownFields because the
// diagnostic is the point. KnownFields reports "field part not found in type
// catalog.bindingFile", which names a Go type the operator cannot see and does
// not say what they could have written instead.
func parseBinding(path string, text []byte) (bindingFile, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(text, &doc); err != nil {
		return bindingFile{}, fmt.Errorf("%s: %w", path, err)
	}
	// An empty file parses to nothing at all. It is not refused here — it is a
	// binding that names no profile, and that refusal below says so in the
	// vocabulary of the format rather than in the vocabulary of the parser.
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return bindingFile{}, nil
	}
	body := doc.Content[0]
	if body.Kind != yaml.MappingNode {
		return bindingFile{}, fmt.Errorf("%s: is not a mapping — a binding is %s written one per line",
			path, strings.Join(quoteAll(bindingKeys), ", "))
	}
	// Content alternates key, value.
	for i := 0; i+1 < len(body.Content); i += 2 {
		key := body.Content[i].Value
		if !slices.Contains(bindingKeys, key) {
			return bindingFile{}, fmt.Errorf("%s:%d: no binding key named %q — a binding holds %s",
				path, body.Content[i].Line, key, strings.Join(quoteAll(bindingKeys), ", "))
		}
	}
	var declared bindingFile
	if err := body.Decode(&declared); err != nil {
		return bindingFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return declared, nil
}

// quoteAll quotes each of ss, for a diagnostic listing a closed set.
func quoteAll(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, strconv.Quote(s))
	}
	return out
}

// readBindings reads every binding file in the bundle's bindings directory.
// An absent directory is not an error: a bundle with profiles and no bindings
// boots by profile id.
//
// A binding naming a profile the bundle does not hold is refused, which is the
// one referential check the schema used to make. It is worth keeping because a
// binding is the name an operator types most and the failure is otherwise
// deferred to whenever somebody happens to boot that one name.
func readBindings(root string, profiles map[string]profile.Profile) (map[string]Binding, error) {
	dir := filepath.Join(root, BindingsDir)
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return map[string]Binding{}, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	out := make(map[string]Binding, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != bindingExt {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		declared, err := parseBinding(path, text)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(entry.Name(), bindingExt)
		id := strings.TrimSpace(declared.Profile)
		if id == "" {
			return nil, fmt.Errorf("%s: names no profile — a binding is a base profile, the parts "+
				"merged onto it, the skills and prompts it carries and a default scope", path)
		}
		if _, ok := profiles[id]; !ok {
			return nil, fmt.Errorf("%s: names profile %q, which %s holds no file for",
				path, id, filepath.Join(root, ProfilesDir))
		}
		parts, err := listOf(path, "parts", declared.Parts)
		if err != nil {
			return nil, err
		}
		skills, err := listOf(path, "skills", declared.Skills)
		if err != nil {
			return nil, err
		}
		prompts, err := listOf(path, "prompts", declared.Prompts)
		if err != nil {
			return nil, err
		}
		out[name] = Binding{
			Name:      name,
			ProfileID: id,
			Parts:     parts,
			Skills:    skills,
			Prompts:   prompts,
			Scope:     strings.TrimSpace(declared.Scope),
		}
	}
	return out, nil
}

// listOf trims one of a binding's lists and refuses an entry that names
// nothing. A list with no entries comes back nil, which under omitempty is the
// same as the empty slice it came from — the key is absent either way.
//
// An empty entry is refused rather than dropped for the reason every other
// empty value in cairn is: a list item that is there and means nothing is a
// typo, and silently composing one fewer part than the file appears to name is
// the kind of difference nobody goes looking for.
//
// Both halves of the format go through it, which is the point. A marshaller
// that dropped what the parser refuses would be the write-side version of the
// same silent difference, in the one package that owns both sides.
//
// What is NOT checked is whether a part names a profile the bundle holds. The
// profile key above is checked, and the difference is that a part may be a
// path — the same value --with takes — which cannot be resolved at Open
// without expanding variables against an environment this package has no
// business reading. The boot resolves it and says which value failed.
func listOf(path, key string, in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for i, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, fmt.Errorf("%s: %s[%d] names nothing", path, key, i)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// readScopes reads the bundle's scope alias registry. An absent file is not an
// error: aliases are optional, and a binding whose scope is a path needs none.
func readScopes(root string) (map[string]string, error) {
	path := filepath.Join(root, ScopesFile)
	text, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return map[string]string{}, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	out := map[string]string{}
	if err := yaml.Unmarshal(text, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for alias, dir := range out {
		if strings.TrimSpace(dir) == "" {
			return nil, fmt.Errorf("%s: alias %q names no directory", path, alias)
		}
	}
	return out, nil
}

// sortedKeys returns a map's keys in order, for the listings that are read
// rather than searched.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
