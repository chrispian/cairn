package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

// readProfiles reads every profile file in the bundle's profiles directory.
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
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != profileExt {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		p, err := parseProfile(string(text), entry.Name())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out[p.ID] = p
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: %s holds no %s file", ErrNoProfilesDir, dir, profileExt)
	}
	return out, nil
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

// bindingFile is one binding as its file is written: the profile it boots and
// the scope that boot works in. The name is the file's, so it is not a field.
type bindingFile struct {
	Profile string `yaml:"profile"`
	Scope   string `yaml:"scope"`
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
		var declared bindingFile
		if err := yaml.Unmarshal(text, &declared); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		name := strings.TrimSuffix(entry.Name(), bindingExt)
		id := strings.TrimSpace(declared.Profile)
		if id == "" {
			return nil, fmt.Errorf("%s: names no profile — a binding is a profile plus a scope", path)
		}
		if _, ok := profiles[id]; !ok {
			return nil, fmt.Errorf("%s: names profile %q, which %s holds no file for",
				path, id, filepath.Join(root, ProfilesDir))
		}
		out[name] = Binding{Name: name, ProfileID: id, Scope: strings.TrimSpace(declared.Scope)}
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
