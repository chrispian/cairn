// Command cairn assembles files and writes them into a directory.
//
// It reads a profile from its own store, resolves it through an extends
// cascade, and materializes a boot directory a CLI coding agent can be
// launched from. It prints the path and exits; a human launches.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/install"
	"github.com/chrispian/cairn/profile"
	"github.com/chrispian/cairn/scope"
	"github.com/chrispian/cairn/slots"
	"github.com/chrispian/cairn/store"
	"github.com/hollis-labs/agentkit/agentcontext"
)

const usage = `cairn assembles files and writes them into a directory.

usage:
  cairn boot <binding|profile> [flags]      materialize a boot directory, print its path
  cairn install <binding|profile> [flags]   render the installed layer
  cairn show <binding|profile> [flags]      print what the profile resolves to

flags for boot:
  --scope <path|alias>   the directory the instance works in; overrides the binding's
  --boot-root <path>     where boot directories are planted; defaults to $CAIRN_BOOT_ROOT
  --session <name>       the session segment; defaults to a UTC timestamp and a random suffix

flags for install:
  --check                re-render, diff against disk, report drift, write nothing
  --root <path>          where the installed layer goes; defaults to the home directory

flags for show:
  --scope <path|alias>   the scope to report, as boot would resolve it; overrides the binding's

flags for all three:
  --db <path>            the database; defaults to $CAIRN_DB, else $XDG_CONFIG_HOME/agents/cairn.db

cairn install is human-executed. Every agent working under ~/.claude that runs
it rewrites its own live configuration mid-session.

cairn show renders nothing — no boot directory, no installed layer. An extends
chain composes keyed collections member by member, so what a profile resolves
to is held in no one row, and this is where it is read. Opening the database
still creates one if it is absent, as every command does.
`

func main() {
	err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	if err == nil {
		return
	}
	// Drift is a finding, not a failure: `install --check` has already printed
	// its report to stdout, and writing it again to stderr as an error would
	// say the same thing twice in two voices.
	var code exitCode
	if errors.As(err, &code) {
		os.Exit(int(code))
	}
	fmt.Fprintf(os.Stderr, "cairn: %v\n", err)
	os.Exit(1)
}

// run is main's body with its inputs and outputs passed in, so that the
// command is testable without a process.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return errors.New("no command")
	}
	switch args[0] {
	case "boot":
		return runBoot(ctx, args[1:], stdout, stderr)
	case "install":
		return runInstall(ctx, args[1:], stdout, stderr)
	case "show":
		return runShow(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	default:
		_, _ = fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// exitCode is an error that carries only a process exit status. It is how
// `install --check` reports drift: drift is a finding, not a failure, so the
// report goes to stdout and nothing is written to stderr, but the status has
// to be non-zero for a shell to branch on it.
type exitCode int

// Error implements error.
func (c exitCode) Error() string { return fmt.Sprintf("exit status %d", int(c)) }

// runInstall renders the installed layer, or checks it against disk.
//
// cairn install is human-executed, permanently. Every agent working on Cairn
// runs under the directory this writes; an agent that runs it rewrites its own
// live configuration mid-session. Nothing here enforces that — plan §1 rules
// out validation whose only job is to stop the operator doing what the
// operator meant — so the convention is documented and not policed.
func runInstall(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cairn install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		check    = fs.Bool("check", false, "re-render, diff against disk, report drift, write nothing")
		dbFlag   = fs.String("db", "", "the database path")
		rootFlag = fs.String("root", "", "the directory the installed layer is written beneath")
	)
	target, rest := splitTarget(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if target == "" {
		target = fs.Arg(0)
	} else if fs.NArg() > 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return fmt.Errorf("install takes one binding or profile, and was given %q as well", fs.Arg(0))
	}
	if target == "" || fs.NArg() > 1 {
		_, _ = fmt.Fprint(stderr, usage)
		return errors.New("install takes exactly one binding or profile")
	}

	home, _ := os.UserHomeDir()

	st, err := openStore(ctx, *dbFlag, home)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	_, profileID, _, err := lookup(ctx, st, target)
	if err != nil {
		return err
	}
	// No abstract check. The installed layer is normally rendered from the
	// abstract root of the cascade, and refusing one here would refuse the
	// profile this command mostly exists to render. `cairn boot` is where a
	// direct boot of an abstract profile is refused — plan §7.
	resolved, err := profile.Resolve(ctx, st, profileID)
	if err != nil {
		return err
	}

	dir := *rootFlag
	if strings.TrimSpace(dir) == "" {
		if strings.TrimSpace(home) == "" {
			return fmt.Errorf("%w: pass --root to say where the installed layer goes", install.ErrNoRoot)
		}
		dir = home
	}
	root, err := install.NewRoot(dir)
	if err != nil {
		return err
	}
	// Templates resolve here for the reason they do in a boot: a template may
	// name a source, and reading one is I/O. No slots are resolved — see
	// install.layerInstance — so a template's slot markers substitute nothing
	// in this layer.
	templates, err := slots.ResolveEntries(ctx, resolved.Spec, profile.SpecKeyTemplates,
		slots.Options{Env: os.Getenv})
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}
	// Slots resolve here too, restricted to the kinds whose answer changes only
	// when the operator changes something. Without them an installed template
	// renders a skeleton; with the rest of them a check would run the profile's
	// commands and report drift on every invocation.
	assembled, err := slots.Assemble(ctx, resolved.Spec, slots.Options{
		Deterministic: true,
		Env:           os.Getenv,
		Provenance: agentcontext.ProvenanceInput{
			LineageAlias: target,
			ProfileID:    resolved.ID,
		},
	})
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}
	if assembled != nil {
		expansions, err := slots.Expansions(resolved.Spec, profile.SpecKeySlots, os.Getenv)
		if err != nil {
			return fmt.Errorf("profile %q: %w", resolved.ID, err)
		}
		reportSlotFailures(stderr, assembled, expansions)
	}
	sections, err := slots.Sections(assembled)
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}
	// The slots this layer does not resolve at all, named once rather than as
	// one puzzling empty section per marker.
	skipped, err := slots.Nondeterministic(resolved.Spec)
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}
	if len(skipped) > 0 {
		_, _ = fmt.Fprintf(stderr,
			"cairn: the installed layer renders no section for %s: it resolves only %s, "+
				"because a check re-renders and anything else would report drift on every run\n",
			strings.Join(skipped, ", "), kindList(slots.DeterministicKinds()))
	}

	lay := &install.Layer{
		Root:      root,
		Profile:   resolved,
		Home:      home,
		Env:       os.Getenv,
		Templates: templates,
		Sections:  sections,
		Values: instanceValues(map[string]string{
			"binding":  target,
			"profile":  resolved.ID,
			"provider": resolved.Provider.String(),
			"model":    resolved.Model,
		}),
	}

	if *check {
		report, err := install.Check(lay)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprint(stdout, report.String()); err != nil {
			return err
		}
		if code := report.ExitCode(); code != 0 {
			return exitCode(code)
		}
		return nil
	}

	res, err := install.Install(lay)
	if err != nil {
		return err
	}
	for _, rel := range res.Files {
		if _, err := fmt.Fprintln(stdout, filepath.Join(res.Root, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}

// kindList renders slot kinds for a diagnostic.
func kindList(kinds []agentcontext.SlotSourceKind) string {
	quoted := make([]string, 0, len(kinds))
	for _, k := range kinds {
		quoted = append(quoted, string(k))
	}
	return strings.Join(quoted, ", ")
}

// openStore resolves the database path and opens it. Both commands need it and
// both resolve it the same way: the flag, then CAIRN_DB, then XDG, then home.
func openStore(ctx context.Context, flagValue, home string) (*store.Store, error) {
	path := flagValue
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = store.DefaultPath(os.Getenv(store.EnvDB), os.Getenv(store.EnvXDGConfigHome), home)
		if err != nil {
			return nil, err
		}
	}
	return store.Open(ctx, path)
}

// runBoot materializes one boot directory and prints its path.
func runBoot(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cairn boot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		scopeFlag = fs.String("scope", "", "the directory the instance works in, as a path or a scope alias")
		dbFlag    = fs.String("db", "", "the database path")
		rootFlag  = fs.String("boot-root", "", "where boot directories are planted")
		sessFlag  = fs.String("session", "", "the session segment")
	)
	target, rest := splitTarget(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if target == "" {
		target = fs.Arg(0)
	} else if fs.NArg() > 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return fmt.Errorf("boot takes one binding or profile, and was given %q as well", fs.Arg(0))
	}
	if target == "" || fs.NArg() > 1 {
		_, _ = fmt.Fprint(stderr, usage)
		return errors.New("boot takes exactly one binding or profile")
	}

	home, _ := os.UserHomeDir()

	st, err := openStore(ctx, *dbFlag, home)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	name, profileID, declaredScope, err := lookup(ctx, st, target)
	if err != nil {
		return err
	}

	resolved, err := profile.Resolve(ctx, st, profileID)
	if err != nil {
		return err
	}
	if resolved.Abstract {
		return fmt.Errorf("profile %q is abstract: it exists to be extended, not booted", resolved.ID)
	}

	layout, err := bootdir.LayoutFor(resolved.Provider)
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}

	rawScope := declaredScope
	if strings.TrimSpace(*scopeFlag) != "" {
		rawScope = *scopeFlag
	}
	scopeDir, err := resolveScope(ctx, st, rawScope, home)
	if err != nil {
		return err
	}

	root := *rootFlag
	if strings.TrimSpace(root) == "" {
		root, err = bootdir.DefaultRoot(os.Getenv(bootdir.EnvBootRoot), home)
		if err != nil {
			return err
		}
	}
	session := *sessFlag
	if strings.TrimSpace(session) == "" {
		session, err = bootdir.NewSession(time.Now(), nil)
		if err != nil {
			return err
		}
	}
	dir, err := bootdir.Location{Root: root, Name: name, Session: session}.Dir()
	if err != nil {
		return err
	}

	// The one validation scope carries, and it guards this write.
	if err := scope.CheckBootDir(scopeDir, dir); err != nil {
		return err
	}

	// Slots resolve here rather than inside a renderer: resolving one runs
	// commands and makes requests, and a renderer may do neither.
	assembled, err := slots.Assemble(ctx, resolved.Spec, slots.Options{
		// The one place cairn reads the environment. Everything below is
		// handed the lookup rather than reaching for it, so a renderer and a
		// resolver expand the same manifest the same way and neither has a
		// hidden input.
		Env: os.Getenv,
		// Scope is the instance's working directory, and the workdir is what
		// workdir-relative slot paths resolve against. They are the same
		// directory, and joining them is this composition root's call to make
		// — package slots is handed the value and asks no questions about it.
		Workdir: scopeDir,
		// No budget. Read that as "cairn imposes none", not as "nothing here
		// can get large": http_text, http_json, role_summary and static_dir
		// each enforce a resolver-side cap and report Truncated, but cmd and
		// static_file do not — cmd clamps its duration and never its output
		// size. A cmd slot is therefore the one genuinely unbounded path into
		// boot.md, and bounding it is the operator's business for now.
		Provenance: agentcontext.ProvenanceInput{
			LineageAlias: name,
			ProfileID:    resolved.ID,
		},
	})
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}
	if assembled != nil {
		// The declared form of an expanded path or URL, which only cairn ever
		// held: the resolver was handed the expansion and can name nothing
		// else. Without this a slot written "$AGENT_DOCS/process.md" with the
		// variable unset reports a failure to open "/process.md", and the
		// operator searches for a path nobody typed.
		expansions, err := slots.Expansions(resolved.Spec, profile.SpecKeySlots, os.Getenv)
		if err != nil {
			return fmt.Errorf("profile %q: %w", resolved.ID, err)
		}
		reportSlotFailures(stderr, assembled, expansions)
	}
	// One rendered section per declared slot, addressed by name. The assembled
	// rendering the library returns is discarded: a template decides what order
	// sections appear in and whether they appear at all, so what is wanted is
	// each section on its own.
	sections, err := slots.Sections(assembled)
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}

	// Files resolve here for the same reason slots do, and unlike a slot a
	// source that fails fails the boot: a missing section is degraded context,
	// a missing file is a hole at a path the profile promised.
	planted, err := slots.ResolveFiles(ctx, resolved.Spec,
		slots.Options{Workdir: scopeDir, Env: os.Getenv})
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}

	// A template's text resolves the same way a file's does, and for the same
	// reason: a profile keeps its prose in a file more often than in the
	// database, and reading one is I/O.
	templates, err := slots.ResolveEntries(ctx, resolved.Spec, profile.SpecKeyTemplates,
		slots.Options{Workdir: scopeDir, Env: os.Getenv})
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}

	// Subagent declarations resolve here for the same reason slots and files
	// do, and it is the third form the reason takes: naming a subagent means
	// reading another profile out of the store and walking its extends chain,
	// and a renderer does no I/O.
	subagents, err := resolveSubagents(ctx, st, resolved)
	if err != nil {
		return err
	}

	inst := &bootdir.Instance{
		Dir:       dir,
		Layout:    layout,
		Home:      home,
		Env:       os.Getenv,
		Profile:   resolved,
		Scope:     scopeDir,
		Files:     planted,
		Subagents: subagents,
		Templates: templates,
		Sections:  sections,
		Values: instanceValues(map[string]string{
			"binding":  name,
			"profile":  resolved.ID,
			"provider": resolved.Provider.String(),
			"model":    resolved.Model,
			"scope":    scopeDir,
			"session":  session,
		}),
	}
	// Reported before the write rather than after it, so that an operator
	// reading stderr sees the missing block named beside the slot failure that
	// explains it.
	if err := reportUnfilledMarkers(stderr, templates, sections); err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}
	files, err := bootdir.Render(inst)
	if err != nil {
		return err
	}
	if _, err := bootdir.PlantFiles(ctx, dir, files); err != nil {
		return err
	}

	// The path is the whole output of the command, so a write that fails is
	// reported rather than dropped — and it names the directory, which by now
	// exists, so the failure does not also lose it.
	if _, err := fmt.Fprintln(stdout, dir); err != nil {
		return fmt.Errorf("the boot directory was written to %s but its path could not be printed: %w", dir, err)
	}
	return nil
}

// splitTarget lifts a leading positional argument out of args so that flags may
// be written on either side of it. Go's flag package stops parsing at the first
// non-flag argument, which would otherwise make `cairn boot eng --db x` read
// --db and its value as two more positionals.
//
// An empty target means the first argument was a flag, and the positional is
// whatever the flag set has left over.
func splitTarget(args []string) (target string, rest []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// lookup resolves a boot target to the name its boot directory is planted
// under, the profile it boots, and the scope it declares.
//
// A binding is tried first: it is the name an operator boots by, and a profile
// of the same id is the fallback rather than an ambiguity, because Cairn is a
// single-operator tool and the operator who named both meant the binding.
func lookup(ctx context.Context, st *store.Store, target string) (name, profileID, declaredScope string, err error) {
	b, err := st.Binding(ctx, target)
	switch {
	case err == nil:
		return b.Name, b.ProfileID, b.Scope, nil
	case !errors.Is(err, store.ErrBindingNotFound):
		return "", "", "", err
	}
	if _, err := st.Profile(ctx, target); err != nil {
		if errors.Is(err, store.ErrProfileNotFound) {
			return "", "", "", fmt.Errorf("no binding and no profile named %q", target)
		}
		return "", "", "", err
	}
	return target, target, "", nil
}

// resolveScope turns a declared scope into a directory. A value that could not
// be a path is looked up as a scope alias; anything else is taken as a path, so
// an operator who has not declared an alias is not obliged to.
//
// The path-like test is what keeps the two from crossing. Trying the alias
// table first for every value would mean that declaring an alias named "src"
// silently retargets any binding whose scope is the literal relative path
// "src" — unlikely, and one predicate to make impossible. A scope that moves
// on its own is the kind of bug that eats an afternoon.
func resolveScope(ctx context.Context, st *store.Store, raw, home string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if !pathLike(trimmed) {
		switch path, err := st.Scope(ctx, trimmed); {
		case err == nil:
			return scope.Parse(path, home)
		case !errors.Is(err, store.ErrScopeNotFound):
			return "", err
		}
	}
	return scope.Parse(trimmed, home)
}

// pathLike reports whether raw is spelled like a path rather than like a bare
// alias: it holds a separator, begins with "~", or is absolute.
func pathLike(raw string) bool {
	return strings.ContainsRune(raw, '/') ||
		strings.ContainsRune(raw, filepath.Separator) ||
		strings.HasPrefix(raw, "~") ||
		filepath.IsAbs(raw)
}

// instanceValues returns the values a template may substitute, checked against
// [bootdir.ValueNames] so that a value added here and not there — or the
// reverse — fails at the composition root rather than as a marker that
// substitutes nothing.
func instanceValues(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for _, name := range bootdir.ValueNames() {
		out[name] = values[name]
	}
	return out
}

// reportUnfilledMarkers prints every marker whose slot was declared and then
// filled nothing — it failed to resolve, or it resolved empty. It leaves the
// template shorter than it reads, and nothing in the resulting file says so.
//
// A marker naming a slot no profile declared is not reported. See
// [github.com/chrispian/cairn/bootdir.Unfilled] for why the two are told apart.
//
// It is a report and not a refusal, matching the slot rule it follows from: a
// section that is not there is degraded context and the agent asks its tools.
// The operator hears about it because they are the only one who can fix it.
//
// The destinations are walked in sorted order so that two boots of one profile
// report in the same order.
func reportUnfilledMarkers(stderr io.Writer, templates, sections map[string]string) error {
	dests := make([]string, 0, len(templates))
	for dest := range templates {
		dests = append(dests, dest)
	}
	sort.Strings(dests)
	for _, dest := range dests {
		unfilled, err := bootdir.Unfilled(templates[dest], sections)
		if err != nil {
			return fmt.Errorf("%s: %w", dest, err)
		}
		for _, marker := range unfilled {
			_, _ = fmt.Fprintf(stderr, "cairn: %s: slot %q filled nothing, so %s renders no section\n",
				dest, marker.Name, dest)
		}
	}
	return nil
}

// reportSlotFailures prints every non-required slot that failed to resolve,
// with the manifest value behind it when expansion changed one.
//
// The library records such a failure on the slot instead of blocking the
// assembly, which is the behaviour Cairn wants — one unreachable endpoint
// should not stop a boot. It is not a behaviour that should be silent: the
// boot directory is written either way, and the operator has no other way to
// learn that a section is missing.
//
// The library's message is passed through untouched and the declared form is
// added ahead of it, so the line reads cause first and consequence second: what
// the operator wrote, what was tried, then what went wrong with it. Expansion runs before the request is built, so a resolver
// only ever saw the expanded value: the two halves together are what the
// operator asked for and what was tried. A slot whose value expansion did not
// change reads exactly as it did before.
func reportSlotFailures(stderr io.Writer, res *agentcontext.ContextResult, expansions map[string]string) {
	for _, s := range res.Slots {
		if s.Err == nil {
			continue
		}
		note := ""
		if expanded := expansions[s.Name]; expanded != "" {
			note = expanded + ": "
		}
		_, _ = fmt.Fprintf(stderr, "cairn: slot %q did not resolve: %s%v\n", s.Name, note, s.Err)
	}
}
