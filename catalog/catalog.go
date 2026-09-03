// Package catalog is Cairn's profile bundle: a directory of files, read into
// memory whole at the start of a command.
//
// The catalog is the store. A profile is a markdown file with YAML
// frontmatter, a binding is a small YAML file, and git is the review surface —
// so there is nothing to seed, nothing to migrate, and no second copy of the
// operator's profiles to keep in step with the first.
//
// Reading is all this package does. It creates no directory and no file, and
// it does not treat an absent bundle as a starting state to be conjured: a
// read that finds nothing says what it was looking for and where, which is the
// one thing a command pointed at the wrong bundle needs to hear.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chrispian/cairn/profile"
)

// DirName is the base name of the configuration directory a bundle defaults
// to. It names the contents rather than a consumer of them: several tools may
// read the same profiles, so the directory is "agents" rather than "cairn".
const DirName = "agents"

// ProfilesDir is the bundle subdirectory holding one markdown file per
// profile.
const ProfilesDir = "profiles"

// BindingsDir is the bundle subdirectory holding one YAML file per binding.
const BindingsDir = "bindings"

// ScopesFile is the bundle file holding the scope alias registry, if there is
// one. It is at the bundle root rather than inside [BindingsDir], where every
// file is one binding named after itself.
//
// The registry is on its way out — a binding is to carry its directory as a
// path — so this file has a shorter future than the rest of the layout, and it
// is deliberately one file rather than a directory of them.
const ScopesFile = "scopes.yaml"

// ErrBundleNotFound reports that the bundle directory is absent, or is not a
// directory.
var ErrBundleNotFound = errors.New("profile bundle not found")

// ErrNoProfilesDir reports a bundle with no profiles directory in it. A
// directory holding no profiles is not an empty catalog, it is a sign cairn
// was pointed somewhere that is not a bundle.
var ErrNoProfilesDir = errors.New("profile bundle holds no profiles directory")

// ErrProfileNotFound reports that no profile file exists for an id.
var ErrProfileNotFound = errors.New("profile not found")

// ErrBindingNotFound reports that no binding file exists for a name.
var ErrBindingNotFound = errors.New("binding not found")

// ErrScopeNotFound reports that the scope registry names no such alias.
var ErrScopeNotFound = errors.New("scope alias not found")

// ErrNoHome reports that the bundle path fell back to the home directory and
// no home directory is known.
var ErrNoHome = errors.New("home directory unknown")

// Binding is one file of the bindings directory: a name an operator boots by,
// the profile it resolves to, and the scope that boot works in.
type Binding struct {
	// Name is the binding's identity — what `cairn boot` is given. It is the
	// file's base name, so a binding cannot disagree with what it is called.
	Name string

	// ProfileID is the profile this binding boots.
	ProfileID string

	// Scope is where that boot works. It is a scope alias when one exists by
	// that name and a directory path otherwise, so an operator who has not
	// declared an alias is not obliged to. Empty means no declared scope.
	Scope string
}

// Scope is one entry of the scope registry: a short alias for a directory.
type Scope struct {
	// Alias is the name a binding's scope may be written as.
	Alias string

	// Path is the directory the alias names.
	Path string
}

// Catalog is one bundle, read.
//
// Everything is read at [Open] and nothing is read after it. A command resolves
// a chain, a subagent's profile and a binding's scope from the same snapshot,
// so a file edited mid-command cannot make one lookup disagree with the next.
type Catalog struct {
	root string

	profiles map[string]profile.Profile
	bindings map[string]Binding
	scopes   map[string]string

	// The listing orders, sorted at Open. They are held rather than recomputed
	// so that [Catalog.Profiles] and its two siblings are reads and not sorts.
	profileIDs   []string
	bindingNames []string
	scopeAliases []string
}

// DefaultRoot returns the bundle directory: envRoot when it is set,
// $XDG_CONFIG_HOME/agents when that is set, and $HOME/.config/agents
// otherwise. It reports [ErrNoHome] only when it actually needs a home.
//
// Every input is passed rather than read, so nothing here consults the process
// environment on its own. The name of the variable envRoot came from stays at
// the composition root, which is the only place that knows cairn has flags.
func DefaultRoot(envRoot, xdgConfigHome, home string) (string, error) {
	if p := strings.TrimSpace(envRoot); p != "" {
		return p, nil
	}
	if x := strings.TrimSpace(xdgConfigHome); x != "" {
		return filepath.Join(x, DirName), nil
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("%w: pass --profile to say where the profile bundle is", ErrNoHome)
	}
	return filepath.Join(home, ".config", DirName), nil
}

// Open reads the bundle rooted at root.
//
// An absent root, an absent profiles directory, an unparseable profile and a
// binding naming a profile that is not there are all refusals, and each names
// the file it was reading. Failing at Open rather than at the first lookup is
// what makes the diagnostic useful: the operator hears that the bundle is
// wrong, instead of hearing that the profile they asked for is missing from a
// bundle that was never read.
func Open(root string) (*Catalog, error) {
	dir, err := checkDir(root)
	if err != nil {
		return nil, err
	}
	c := &Catalog{root: dir}

	if c.profiles, err = readProfiles(dir); err != nil {
		return nil, err
	}
	if c.scopes, err = readScopes(dir); err != nil {
		return nil, err
	}
	if c.bindings, err = readBindings(dir, c.profiles); err != nil {
		return nil, err
	}

	c.profileIDs = sortedKeys(c.profiles)
	c.bindingNames = sortedKeys(c.bindings)
	c.scopeAliases = sortedKeys(c.scopes)
	return c, nil
}

// Root returns the directory this catalog was read from, absolute if the
// caller's path was.
func (c *Catalog) Root() string { return c.root }

// Profile returns the profile stored under id, or an error wrapping
// [ErrProfileNotFound] when no such file exists. It implements
// [profile.Loader].
//
// The context is the interface's and is not consulted: the bundle was read
// before this was called, so there is no work here to cancel.
func (c *Catalog) Profile(_ context.Context, id string) (*profile.Profile, error) {
	p, ok := c.profiles[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("load profile %q from %s: %w", id, filepath.Join(c.root, ProfilesDir), ErrProfileNotFound)
	}
	return &p, nil
}

// Profiles returns every profile in the bundle, ordered by id.
func (c *Catalog) Profiles() []profile.Profile {
	out := make([]profile.Profile, 0, len(c.profileIDs))
	for _, id := range c.profileIDs {
		out = append(out, c.profiles[id])
	}
	return out
}

// Binding returns the binding stored under name, or an error wrapping
// [ErrBindingNotFound] when no such file exists.
func (c *Catalog) Binding(name string) (*Binding, error) {
	b, ok := c.bindings[strings.TrimSpace(name)]
	if !ok {
		return nil, fmt.Errorf("load binding %q from %s: %w", name, filepath.Join(c.root, BindingsDir), ErrBindingNotFound)
	}
	return &b, nil
}

// Bindings returns every binding in the bundle, ordered by name.
func (c *Catalog) Bindings() []Binding {
	out := make([]Binding, 0, len(c.bindingNames))
	for _, name := range c.bindingNames {
		out = append(out, c.bindings[name])
	}
	return out
}

// Scope returns the directory path alias names, or an error wrapping
// [ErrScopeNotFound] when the registry does not name it.
func (c *Catalog) Scope(alias string) (string, error) {
	path, ok := c.scopes[strings.TrimSpace(alias)]
	if !ok {
		return "", fmt.Errorf("load scope %q from %s: %w", alias, filepath.Join(c.root, ScopesFile), ErrScopeNotFound)
	}
	return path, nil
}

// Scopes returns every scope alias in the bundle, ordered by alias.
func (c *Catalog) Scopes() []Scope {
	out := make([]Scope, 0, len(c.scopeAliases))
	for _, alias := range c.scopeAliases {
		out = append(out, Scope{Alias: alias, Path: c.scopes[alias]})
	}
	return out
}

// ResolvedScope returns where a binding's boot works: the alias's directory
// when the scope names one, and the scope itself otherwise. A binding that
// declares no scope resolves to the empty string.
//
// It is the listing's answer and not the boot's. `cairn boot` expands "~/" and
// checks that the directory exists — see scope.Parse — and a listing that did
// either would refuse to print a catalog because one binding points somewhere
// that is not there today.
func (c *Catalog) ResolvedScope(b Binding) string {
	declared := strings.TrimSpace(b.Scope)
	if declared == "" {
		return ""
	}
	if path, ok := c.scopes[declared]; ok {
		return path
	}
	return declared
}
