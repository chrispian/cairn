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
	"strings"
	"time"

	"github.com/chrispian/cairn/bootdir"
	"github.com/chrispian/cairn/profile"
	"github.com/chrispian/cairn/scope"
	"github.com/chrispian/cairn/slots"
	"github.com/chrispian/cairn/store"
	"github.com/hollis-labs/agentkit/agentcontext"
)

const usage = `cairn assembles files and writes them into a directory.

usage:
  cairn boot <binding|profile> [flags]   materialize a boot directory, print its path
  cairn install [--check]                render the installed layer

flags for boot:
  --scope <path|alias>   the directory the instance works in; overrides the binding's
  --db <path>            the database; defaults to $CAIRN_DB, else $XDG_CONFIG_HOME/agents/cairn.db
  --boot-root <path>     where boot directories are planted; defaults to $CAIRN_BOOT_ROOT
  --session <name>       the session segment; defaults to a UTC timestamp and a random suffix
`

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "cairn: %v\n", err)
		os.Exit(1)
	}
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
		return runInstall(args[1:])
	case "-h", "--help", "help":
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	default:
		_, _ = fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// runInstall reports that the installed layer is not rendered yet. It is a
// separate function so that the day it does something, nothing above it
// changes.
//
// cairn install is human-executed, permanently: every agent working on Cairn
// runs under ~/.claude, and an agent running install rewrites its own live
// configuration mid-session.
func runInstall(args []string) error {
	fs := flag.NewFlagSet("cairn install", flag.ContinueOnError)
	check := fs.Bool("check", false, "re-render, diff against disk, report drift")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *check {
		return errors.New("install --check is not implemented yet — see docs/plan.md §8 step 7")
	}
	return errors.New("install is not implemented yet — see docs/plan.md §8 step 7")
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

	dbPath := *dbFlag
	if strings.TrimSpace(dbPath) == "" {
		var err error
		dbPath, err = store.DefaultPath(os.Getenv(store.EnvDB), os.Getenv(store.EnvXDGConfigHome), home)
		if err != nil {
			return err
		}
	}
	st, err := store.Open(ctx, dbPath)
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
	boot := ""
	if assembled != nil {
		boot = assembled.Rendered
		reportSlotFailures(stderr, assembled)
	}

	inst := &bootdir.Instance{
		Dir:     dir,
		Layout:  layout,
		Home:    home,
		Profile: resolved,
		Scope:   scopeDir,
		Boot:    boot,
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

// reportSlotFailures prints every non-required slot that failed to resolve.
//
// The library records such a failure on the slot instead of blocking the
// assembly, which is the behaviour Cairn wants — one unreachable endpoint
// should not stop a boot. It is not a behaviour that should be silent: the
// boot directory is written either way, and the operator has no other way to
// learn that a section of the boot file is missing.
func reportSlotFailures(stderr io.Writer, res *agentcontext.ContextResult) {
	for _, s := range res.Slots {
		if s.Err != nil {
			_, _ = fmt.Fprintf(stderr, "cairn: slot %q did not resolve: %v\n", s.Name, s.Err)
		}
	}
}
