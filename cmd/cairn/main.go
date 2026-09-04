// Command cairn assembles files and writes them into a directory.
//
// It reads a profile out of a bundle directory, resolves it through an extends
// cascade, and materializes a boot directory a CLI coding agent can be
// launched from. It prints the path — or, with --json, one object describing
// the boot — and exits; a human launches.
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
	"github.com/chrispian/cairn/catalog"
	"github.com/chrispian/cairn/install"
	"github.com/chrispian/cairn/profile"
	"github.com/chrispian/cairn/scope"
	"github.com/chrispian/cairn/slots"
	"github.com/hollis-labs/agentkit/agentcontext"
)

const usage = `cairn assembles files and writes them into a directory.

usage:
  cairn boot <binding|profile> [flags]      materialize a boot directory, print its path
  cairn install <binding|profile> [flags]   render the installed layer
  cairn show <binding|profile> [flags]      print what the profile resolves to
  cairn list [flags]                        enumerate the catalog

flags for boot and show:
  --with <part>          a profile merged after the extends chain resolves, closest-wins
                         and in the order given. Repeatable. A part is an ordinary
                         profile, so anything composable is also bootable and inspectable
                         on its own. A value holding a path separator, or beginning with
                         ".", "~" or "$", names a file; anything else is a catalog id, so
                         a part in the current directory is written ./part.md.
                         A profile the resolution has already reached — the target,
                         anything it extends, or a part named earlier — is folded once
                         where it first landed, so naming it again adds nothing and does
                         not move it: a part brings what it adds, and never reverts what
                         a profile closer to it already settled. Such a part is named on
                         stderr, so a flag that changed nothing is not silent about it
  --skill <a,b,c>        a skill the boot directory carries, added to the ones the profile
                         resolves to. Comma-separated and repeatable, the two forms
                         equivalent and composing. Additive only: nothing in cairn removes
                         a member of a collection keyed by its own id, so a session that
                         wants fewer skills boots a different profile
  --prompt <a,b,c>       a prompt the boot directory carries, added to the ones the profile
                         resolves to, planted as a command the operator invokes by name:
                         /boot:<name>. Comma-separated and repeatable, the two forms
                         equivalent and composing. Additive only, for the reason --skill is.
                         Cairn plants the file and prints the boot directory; nothing
                         fires a prompt, and a person types the command
  --set <slot>=<value>   an inline literal for a named slot, merged last. Repeatable. It
                         replaces a declared slot of that name whole, section included,
                         exactly as a part declaring that slot would

flags for boot:
  --scope <path|alias>   the directory the instance works in; overrides the binding's
  --boot-root <path>     where boot directories are planted; defaults to $CAIRN_BOOT_ROOT,
                         else ~/.local/state/cairn/boot
  --session <name>       the session segment; defaults to a UTC timestamp and a random suffix
  --json                 print one JSON object describing the boot instead of the bare path
  --save-as <name>       write this composition to the bundle as a new binding of that
                         name, so the same boot is reachable by name. The parts, the
                         skills, the prompts and the scope are saved as they were
                         written; --set values are not, because a binding names what to
                         compose and an inline value is content — each one dropped is
                         named on stderr, and this boot still has it. A composition
                         holding a path member is refused rather than saved short —
                         whether the path was typed as
                         --with or came from the binding being composed onto: a binding
                         must be reproducible by name, and a path is a handle to
                         something that may not be there later. A relative --scope is
                         saved as the directory it resolved to, since a binding records
                         no working directory to read one against. An existing binding is
                         never overwritten

flags for install:
  --check                re-render, diff against disk, report drift, write nothing
  --root <path>          where the installed layer goes; defaults to the home directory

flags for show:
  --scope <path|alias>   the scope to report, as boot would resolve it; overrides the binding's

flags for all four:
  --profile <dir>        the profile bundle — the directory the catalog is read from,
                         holding profiles/ and bindings/. Defaults to $CAIRN_PROFILE_ROOT,
                         else $XDG_CONFIG_HOME/agents, else ~/.config/agents.
                         $CAIRN_PROFILE_ROOT expands to it in every manifest value that
                         names somewhere to read from, so a profile says
                         $CAIRN_PROFILE_ROOT/templates/agents.md and the bundle relocates
                         without edits

cairn install is human-executed. Every agent working under ~/.claude that runs
it rewrites its own live configuration mid-session.

cairn show and cairn install --check render nothing and write nothing — no boot
directory, no installed layer, and no part of the bundle they read. A read that
finds nothing says which bundle it was reading and where.
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
	case "list":
		return runList(args[1:], stdout, stderr)
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
		check       = fs.Bool("check", false, "re-render, diff against disk, report drift, write nothing")
		rootFlag    = fs.String("root", "", "the directory the installed layer is written beneath")
		profileFlag = fs.String("profile", "", profileFlagUsage)
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

	// The bundle this command reads, and the environment every value in its
	// manifest expands against. They are one value: the catalog is the store,
	// so the directory the profile came out of is the directory
	// $CAIRN_PROFILE_ROOT names — see [bundleRoot] and [environment].
	bundle, err := bundleRoot(*profileFlag, home)
	if err != nil {
		return err
	}
	env := environment(bundle)

	cat, err := catalog.Open(bundle)
	if err != nil {
		return err
	}

	// The binding's composition is deliberately not replayed, and neither is
	// its scope. install renders the machine-wide layer every session loads,
	// and a per-launch composition has no meaning there — which is the same
	// reason install takes none of --with, --skill, --prompt or --set.
	tgt, err := lookup(ctx, cat, target)
	if err != nil {
		return err
	}
	profileID := tgt.profileID
	if len(tgt.parts) > 0 || len(tgt.skills) > 0 || len(tgt.prompts) > 0 {
		// Said out loud rather than left to be noticed. The decision above is
		// the right one and the silence was not: `cairn show <binding>` and
		// `cairn boot <binding>` both report a composition that this command
		// renders nothing of, so an operator comparing the two has no way to
		// tell a deliberate omission from a bug.
		_, _ = fmt.Fprintf(stderr,
			"cairn: binding %q composes %d part(s), %d skill(s) and %d prompt(s), and install "+
				"renders none of them — the installed layer is what every session loads, "+
				"not one launch.\n",
			tgt.name, len(tgt.parts), len(tgt.skills), len(tgt.prompts))
	}
	// No abstract check. The installed layer is normally rendered from the
	// abstract root of the cascade, and refusing one here would refuse the
	// profile this command mostly exists to render. `cairn boot` is where a
	// direct boot of an abstract profile is refused — plan §7.
	resolved, err := profile.Resolve(ctx, cat, profileID)
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
		slots.Options{Env: env})
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}
	// Slots resolve here too, restricted to the kinds whose answer changes only
	// when the operator changes something. Without them an installed template
	// renders a skeleton; with the rest of them a check would run the profile's
	// commands and report drift on every invocation.
	assembled, err := slots.Assemble(ctx, resolved.Spec, slots.Options{
		Deterministic: true,
		Env:           env,
		Provenance: agentcontext.ProvenanceInput{
			LineageAlias: target,
			ProfileID:    resolved.ID,
		},
	})
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}
	if assembled != nil {
		expansions, err := slots.Expansions(resolved.Spec, profile.SpecKeySlots, env)
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
		Env:       env,
		Templates: templates,
		Sections:  sections,
		Values: instanceValues(map[string]string{
			"binding":  target,
			"profile":  resolved.ID,
			"provider": resolved.Provider.String(),
			"model":    resolved.Model,
		}),
	}

	// Reported before the render, and reported for a check as well as a write.
	// A marker that stood for nothing can empty a template to the point where
	// no file is written at all, and this layer's pointer document is a
	// declared include of the instruction file beside it: lose the instruction
	// file and the pointer resolves to nothing, silently, which is the exact
	// outcome making the pointer a template was meant to prevent.
	//
	// A check catches that on a root that already carries the file. It claims
	// the paths its renderers can produce rather than the ones one render did
	// — plan §7 — so an instruction file that stopped rendering is an orphan,
	// and the check exits non-zero naming it. What it cannot catch is the
	// first install into a root that never held the file: nothing on disk to
	// orphan, nothing rendered to diff, "In sync". That is the case this line
	// is for, and it is reported for a check as well as a write because a
	// check is where an operator goes to ask whether the layer is right.
	renderers, layout, err := install.PlanterFor(resolved.Provider)
	if err != nil {
		return err
	}
	if err := reportUnfilledMarkers(stderr, installedTemplates(templates, renderers, layout), sections); err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
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

// bootTarget is what the argument to boot, show or install resolved to: the
// name a boot directory is planted under, and everything the catalog knows
// about what that name composes.
//
// A profile named directly and a binding naming it produce the same shape,
// with the composition fields empty for the profile. That is what keeps
// runBoot from branching on which one it was given: a binding is a saved
// composition, so replaying one is the same code path as typing it.
type bootTarget struct {
	// name is what the boot directory is planted under, and what the
	// `cairn:value binding` marker fills.
	name string

	// profileID is the profile the composition resolves from.
	profileID string

	// parts, skills and prompts are the composition the binding saved, empty
	// for a profile named directly. They are as the binding's file spells
	// them.
	parts   []string
	skills  []string
	prompts []string

	// scope is the declared scope, an alias or a path, before --scope
	// overrides it and before either is resolved.
	scope string
}

// lookup resolves a boot target.
//
// A binding is tried first: it is the name an operator boots by, and a profile
// of the same id is the fallback rather than an ambiguity, because Cairn is a
// single-operator tool and the operator who named both meant the binding.
func lookup(ctx context.Context, cat *catalog.Catalog, name string) (bootTarget, error) {
	b, err := cat.Binding(name)
	switch {
	case err == nil:
		return bootTarget{name: b.Name, profileID: b.ProfileID, parts: b.Parts,
			skills: b.Skills, prompts: b.Prompts, scope: b.Scope}, nil
	case !errors.Is(err, catalog.ErrBindingNotFound):
		return bootTarget{}, err
	}
	if _, err := cat.Profile(ctx, name); err != nil {
		if errors.Is(err, catalog.ErrProfileNotFound) {
			return bootTarget{}, fmt.Errorf("%s: no binding and no profile named %q", cat.Root(), name)
		}
		return bootTarget{}, err
	}
	return bootTarget{name: name, profileID: name}, nil
}

// runBoot materializes one boot directory and prints its path, or --json and
// prints one object describing it.
//
// Without --json stdout is the bare path and nothing else. That is the seam
// this command is, and --json does not move it: a caller that wants a path
// keeps getting exactly a path, and one that wants the rest asks for it.
func runBoot(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cairn boot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		scopeFlag   = fs.String("scope", "", "the directory the instance works in, as a path or a scope alias")
		rootFlag    = fs.String("boot-root", "", "where boot directories are planted")
		sessFlag    = fs.String("session", "", "the session segment")
		jsonFlag    = fs.Bool("json", false, "print one JSON object describing the boot instead of the bare path")
		profileFlag = fs.String("profile", "", profileFlagUsage)
		saveAsFlag  = fs.String("save-as", "", saveAsFlagUsage)
	)
	var compose composition
	compose.bind(fs)
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

	// The bundle this command reads, and the environment every value in its
	// manifest expands against. They are one value: the catalog is the store,
	// so the directory the profile came out of is the directory
	// $CAIRN_PROFILE_ROOT names — see [bundleRoot] and [environment].
	bundle, err := bundleRoot(*profileFlag, home)
	if err != nil {
		return err
	}
	env := environment(bundle)

	cat, err := catalog.Open(bundle)
	if err != nil {
		return err
	}

	tgt, err := lookup(ctx, cat, target)
	if err != nil {
		return err
	}
	name := tgt.name

	// A binding is a saved composition, so booting one replays it: its parts,
	// skills and prompts go ahead of whatever was typed, which is what makes the file
	// --save-as writes a record of the boot rather than a description of it.
	// A binding that saved its parts and then booted without them would be a
	// file that lies about what it restores.
	compose.replay(tgt)

	// The composition resolves through the same call whether or not anything
	// was composed: --with, --skill, --prompt and --set contribute nothing
	// when they were not given, and a second code path for the plain case is a second
	// place for the two to disagree about what a boot resolves to.
	resolved, _, err := compose.resolve(ctx, cat, home, env, tgt.profileID)
	if err != nil {
		return err
	}
	// Reported before anything is written, beside the other things an operator
	// hears about a resolution rather than a render.
	compose.reportAbsorbedParts(stderr, resolved)
	// The target's own leaf decides this, never a part — a part is a fragment
	// and may well be abstract. See profile.ResolveComposition.
	if resolved.Abstract {
		return fmt.Errorf("profile %q is abstract: it exists to be extended, not booted", resolved.ID)
	}

	layout, err := bootdir.LayoutFor(resolved.Provider)
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}

	rawScope := tgt.scope
	if strings.TrimSpace(*scopeFlag) != "" {
		rawScope = *scopeFlag
	}
	scopeDir, err := resolveScope(cat, rawScope, home)
	if err != nil {
		return err
	}

	// Checked here and written at the end. Every refusal a --save-as can raise
	// is knowable now, and raising it now is what keeps an operator who
	// mistyped a binding name from also having a boot directory planted for
	// them. Both spellings of the scope go down, because which one is recorded
	// is a decision rather than a lookup — see [savedScope].
	save, err := newBindingSave(ctx, strings.TrimSpace(*saveAsFlag), cat, tgt, &compose, rawScope, scopeDir)
	if err != nil {
		return err
	}

	bootRoot := *rootFlag
	if strings.TrimSpace(bootRoot) == "" {
		bootRoot, err = bootdir.DefaultRoot(os.Getenv(bootdir.EnvBootRoot), home)
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
	dir, err := bootdir.Location{Root: bootRoot, Name: name, Session: session}.Dir()
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
		// The lookup, handed down rather than reached for, so a renderer and a
		// resolver expand the same manifest the same way and neither has a
		// hidden input. What an environment answers is decided in one place —
		// [environment] — and nowhere below; the reads themselves happen
		// wherever a value is expanded, through the closure it returned.
		Env: env,
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
		expansions, err := slots.Expansions(resolved.Spec, profile.SpecKeySlots, env)
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
		slots.Options{Workdir: scopeDir, Env: env})
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}

	// A template's text resolves the same way a file's does, and for the same
	// reason: a profile keeps its prose in a file more often than in the
	// database, and reading one is I/O.
	templates, err := slots.ResolveEntries(ctx, resolved.Spec, profile.SpecKeyTemplates,
		slots.Options{Workdir: scopeDir, Env: env})
	if err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}

	// Subagent declarations resolve here for the same reason slots and files
	// do, and it is the third form the reason takes: naming a subagent means
	// reading another profile out of the catalog and walking its extends
	// chain, and a renderer does no I/O.
	subagents, err := resolveSubagents(ctx, cat, resolved)
	if err != nil {
		return err
	}

	inst := &bootdir.Instance{
		Dir:       dir,
		Layout:    layout,
		Home:      home,
		Env:       env,
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
	if err := reportUnfilledMarkers(stderr, bootTemplates(templates), sections); err != nil {
		return fmt.Errorf("profile %q: %w", resolved.ID, err)
	}
	files, err := bootdir.Render(inst)
	if err != nil {
		return err
	}
	if _, err := bootdir.PlantFiles(ctx, dir, files); err != nil {
		return err
	}

	// After the boot, because a binding worth reusing is one that booted, and
	// before the path is printed, because the path is the last thing this
	// command says.
	if save != nil {
		if err := save.write(stderr); err != nil {
			return fmt.Errorf("the boot directory was written to %s but the binding was not: %w", dir, err)
		}
	}

	// Whichever form it takes, this is the whole output of the command, so a
	// write that fails is reported rather than dropped — and it names the
	// directory, which by now exists, so the failure does not also lose it.
	out := dir + "\n"
	if *jsonFlag {
		out, err = bootDocument(dir, layout, scopeDir, files)
		if err != nil {
			return fmt.Errorf("the boot directory was written to %s but it could not be described: %w", dir, err)
		}
	}
	if _, err := fmt.Fprint(stdout, out); err != nil {
		return fmt.Errorf("the boot directory was written to %s but its path could not be printed: %w", dir, err)
	}
	return nil
}

// splitTarget lifts a leading positional argument out of args so that flags may
// be written on either side of it. Go's flag package stops parsing at the first
// non-flag argument, which would otherwise make `cairn boot eng --profile x`
// read --profile and its value as two more positionals.
//
// An empty target means the first argument was a flag, and the positional is
// whatever the flag set has left over.
func splitTarget(args []string) (target string, rest []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
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
func resolveScope(cat *catalog.Catalog, raw, home string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if !pathLike(trimmed) {
		switch path, err := cat.Scope(trimmed); {
		case err == nil:
			return scope.Parse(path, home)
		case !errors.Is(err, catalog.ErrScopeNotFound):
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

// instanceValues returns the values a template may substitute: a key for each
// name in [bootdir.ValueNames] and no others, so that a value wired here under
// a name cairn does not fill is dropped at the composition root rather than
// carried into a render.
//
// It is the second of two mechanisms and not the only one. Substitution fills a
// value marker only from [bootdir.ValueNames] too, so a key that got past here
// would still render nothing — see [github.com/chrispian/cairn/bootdir.Substitute].
// This narrowing stays because it is the cheaper place to be right: the map
// this builds is handed to a library that renders whatever it is given for
// every artifact, not only templates, and the manifest values it would be
// carrying are the ones spec.mcp keeps API keys in.
func instanceValues(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for _, name := range bootdir.ValueNames() {
		out[name] = values[name]
	}
	return out
}

// reportUnfilledMarkers prints every marker that stood for nothing an operator
// would want to hear about: a slot that was declared and then filled nothing —
// it failed to resolve, or it resolved empty — and a value cairn cannot fill for
// any profile. Either leaves the template shorter than it reads, and nothing in
// the resulting file says so.
//
// A marker naming a slot no profile declared is not reported, and neither is a
// value cairn knows that is empty for this instance. See
// [github.com/chrispian/cairn/bootdir.Unfilled] for why each is told from the
// case beside it.
//
// It is a report and not a refusal, matching the slot rule it follows from: a
// section that is not there is degraded context and the agent asks its tools.
// The operator hears about it because they are the only one who can fix it.
//
// The set of values is named once at the end rather than on every line. A
// template that misspells one value usually carries the marker more than once,
// and a hundred and fifty characters of set repeated behind each occurrence is
// the noise this function stays quiet about undeclared slots to avoid.
//
// One line per name per destination, for the same reason. Neither line says
// where in the file the marker was, so a second line for the same name in the
// same file carries nothing the first did not.
//
// Each template carries two names because a diagnostic and a refusal are read
// for different reasons. A marker that stood for nothing names the path the
// file lands at, which is what an operator looks for and fails to find. A
// marker that would not parse names the manifest key, which is what they open
// to fix it — the path is an output that this run will never produce, and
// naming a file an operator cannot find is worse than not naming one. In a boot
// directory the two are the same string; in the installed layer they are not.
//
// The slice is sorted here, so a caller may hand one over in any order and two
// boots of one profile still report in the same one.
func reportUnfilledMarkers(stderr io.Writer, templates []reportedTemplate, sections map[string]string) error {
	sort.Slice(templates, func(i, j int) bool { return templates[i].path < templates[j].path })
	unfillable := false
	for _, tmpl := range templates {
		dest := tmpl.path
		unfilled, err := bootdir.Unfilled(tmpl.text, sections)
		if err != nil {
			return fmt.Errorf("spec.%s %q: %w", profile.SpecKeyTemplates, tmpl.key, err)
		}
		said := make(map[reportedMarker]bool, len(unfilled))
		for _, marker := range unfilled {
			key := reportedMarker{verb: marker.Verb, name: marker.Name}
			if said[key] {
				continue
			}
			said[key] = true
			switch marker.Verb {
			case bootdir.MarkerVerbSlot:
				_, _ = fmt.Fprintf(stderr, "cairn: %s: slot %q filled nothing, so %s renders no section\n",
					dest, marker.Name, dest)
			case bootdir.MarkerVerbValue:
				unfillable = true
				_, _ = fmt.Fprintf(stderr, "cairn: %s: value %q is not one cairn fills, so %s renders nothing where it stands\n",
					dest, marker.Name, dest)
			}
		}
	}
	if unfillable {
		_, _ = fmt.Fprintf(stderr, "cairn: the values cairn fills are %s\n", quotedValueNames())
	}
	return nil
}

// reportedMarker is one reported marker's identity, for telling a repeat from a
// new finding. It is the verb and the name and deliberately not the marker's
// text, so two spellings of one marker are one finding.
type reportedMarker struct {
	verb string
	name string
}

// quotedValueNames renders the value set for a diagnostic, quoted the way every
// other set-naming diagnostic cairn prints renders one.
//
// It builds its own slice rather than rewriting the one it was given.
// [bootdir.ValueNames] does return a fresh slice every call, but that is a
// promise made in another package and invisible here, and this function has no
// reason to need it.
func quotedValueNames() string {
	names := bootdir.ValueNames()
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, fmt.Sprintf("%q", name))
	}
	return strings.Join(quoted, ", ")
}

// reportedTemplate is one template a report walks: the manifest key it was
// declared under, the path the file lands at, and its text.
type reportedTemplate struct {
	key  string
	path string
	text string
}

// bootTemplates lists a boot directory's templates for a report. A boot
// directory writes each template at the destination it was declared under, so
// the key and the path are one string and there is nothing to map.
func bootTemplates(templates map[string]string) []reportedTemplate {
	out := make([]reportedTemplate, 0, len(templates))
	for dest, text := range templates {
		out = append(out, reportedTemplate{key: dest, path: dest, text: text})
	}
	return out
}

// installedTemplates lists the installed layer's templates for a report, paired
// with the path this layer writes each at and dropping every destination it
// does not render.
//
// The installed layer renders two of the manifest's destinations and writes
// them beneath a provider directory of its own, so a report naming the
// manifest's own key would send an operator looking for "AGENTS.md" when the
// file is at ".claude/AGENTS.md", and one walking every destination would name
// files this layer never writes at all. The registration list decides which
// destinations are in play and the layout decides where each one lands, which
// is the same pair [github.com/chrispian/cairn/install.Render] renders from.
func installedTemplates(templates map[string]string, renderers []install.Renderer, layout bootdir.Layout) []reportedTemplate {
	paths := map[string]bootdir.Artifact{
		bootdir.AgentsFileName:  layout.Agents,
		bootdir.PointerFileName: layout.Pointer,
	}
	out := make([]reportedTemplate, 0, len(paths))
	for _, r := range renderers {
		artifact, rendered := paths[r.Artifact]
		if !rendered || !artifact.Declared() {
			continue
		}
		if text, declared := templates[r.Artifact]; declared {
			out = append(out, reportedTemplate{key: r.Artifact, path: artifact.RelPath, text: text})
		}
	}
	return out
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
