package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/chrispian/cairn/profile"
)

// specIndent is one level of the indentation a manifest value is shown at, and
// also the margin the value sits in under its key.
const specIndent = "  "

// columnGap separates a name from its value in the two-column blocks: the
// scalar fields, and each manifest key from the profiles that declare it.
const columnGap = 2

// specNote is the spec section's note, in the shape install.Report's sections
// use: a heading, then what the entries under it mean, then the entries.
//
// The names are the whole reason this command exists. A value composed from
// three profiles looks exactly like a value one profile wrote, and nothing
// else in this document could tell them apart — see docs/plan.md §3, which
// records that a profile can no longer be read without walking its chain.
//
// The count of names is not a detail: it decides what the value below can be
// trusted to be. One name and the value is that profile's own document, which
// the cascade never re-serializes and this command only re-spaces — the case
// that matters, because spec.settings is written verbatim into the harness's
// settings document and an operator will diff the two. Two or more and it is a
// composition: profile.encodeJSON marshals a Go map, so the members are what
// the profiles declared and the order is the cascade's, at every depth. A note
// promising byte identity for both would be wrong for exactly the key it was
// written to explain.
const specNote = specIndent + "The names beside a key are the profiles in the chain that declare it. One name\n" +
	specIndent + "means the value below is that profile's own document, indented here and\n" +
	specIndent + "otherwise byte for byte what it stored. Two or more means the cascade composed\n" +
	specIndent + "it: the members are what those profiles declared, and the order is not.\n"

// runShow resolves a target and prints what it resolves to. It is the
// mitigation docs/plan.md §3 names: a manifest key is composed member by
// member across the extends chain, so a profile can no longer be read by
// reading its own row.
//
// It renders nothing. No boot directory, no installed layer, no temporary
// file; the whole output is on stdout.
//
// The exception, because "writes nothing" is the second half of this command's
// contract and an unnamed exception to it is worse than none: opening the
// database writes. store.Open creates the file and its parent directory when
// they are absent and applies the schema in an immediate transaction, so a
// show against a path that names nothing leaves a directory and an empty
// database behind — and does it before the target is looked up, so a show that
// then fails still leaves them. That is cairn's rule everywhere rather than a
// choice made here: an absent database is a usable starting state, not a
// configuration error. Nothing else on this path opens, creates or touches a
// file.
//
// An abstract profile shows, following runInstall rather than runBoot: nothing
// here is run, and the abstract root is the profile most worth reading.
//
// There is no --provider. The task this was written for named one, and it is
// deliberately absent: spec.settings is a single document rather than one per
// provider, so there is nothing for the flag to select. It arrives when
// settings is re-keyed by provider and has a target to name.
func runShow(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cairn show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		scopeFlag = fs.String("scope", "", "the scope to report, as a path or a scope alias")
		dbFlag    = fs.String("db", "", "the database path")
	)
	target, rest := splitTarget(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if target == "" {
		target = fs.Arg(0)
	} else if fs.NArg() > 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return fmt.Errorf("show takes one binding or profile, and was given %q as well", fs.Arg(0))
	}
	if target == "" || fs.NArg() > 1 {
		_, _ = fmt.Fprint(stderr, usage)
		return errors.New("show takes exactly one binding or profile")
	}

	home, _ := os.UserHomeDir()

	st, err := openStore(ctx, *dbFlag, home)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	_, profileID, declaredScope, err := lookup(ctx, st, target)
	if err != nil {
		return err
	}
	resolved, err := profile.Resolve(ctx, st, profileID)
	if err != nil {
		return err
	}

	// The resolved spec does not depend on scope — scope is an instance value
	// substituted at boot, not a thing the cascade composes. So the flag is
	// kept by making the scope one of the reported facts rather than an input
	// to them: what `cairn boot <target>` would work in, resolved through the
	// same path runBoot resolves it through, with --scope overriding the
	// binding's exactly as it does there. A flag that takes a value and
	// changes no output is worse than no flag.
	//
	// The binding's own name is not reported. lookup returns it and runBoot
	// needs it to name a directory; nothing here is named after anything, and
	// the profile line already says what the target resolved to.
	rawScope := declaredScope
	if strings.TrimSpace(*scopeFlag) != "" {
		rawScope = *scopeFlag
	}
	scopeDir, err := resolveScope(ctx, st, rawScope, home)
	if err != nil {
		// Reported, not refused, and the rule it follows is the slot rule: a
		// resolution that fails costs the reader one fact, and a command that
		// refused the whole document over it would be least usable exactly
		// when something is already wrong.
		//
		// It is specifically not runBoot's refusal, because runBoot is
		// refusing something else. scope.ErrNotDirectory exists to keep
		// scope.CheckBootDir answerable — a scope the filesystem cannot
		// identify contains nothing, which would turn the one guard cairn has
		// into a no-op. This command runs that guard on nothing: it plants no
		// directory, so there is no write for an unidentifiable scope to leave
		// unguarded.
		//
		// The declared value leads the line, ahead of the error that names
		// what it expanded to, for the reason reportSlotFailures puts it there.
		_, _ = fmt.Fprintf(stderr, "cairn: scope %q did not resolve, so none is reported: %v\n", rawScope, err)
		scopeDir = ""
	}

	declared, err := declaringProfiles(ctx, st, resolved)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(stdout, showDocument(resolved, scopeDir, declared))
	return err
}

// declaringProfiles maps each manifest key to the profiles in the chain whose
// own spec declares it, ancestor-first.
//
// It re-reads the chain rather than asking the cascade, because the cascade
// keeps no record of where a value came from: [profile.Resolve] folds ancestor
// onto descendant and returns the composition. Reading the same rows a second
// time is the cheap half of the question.
//
// It is per key and not per member. Saying that spec.slots came from two
// profiles is not the same as saying which of them contributed the slot in
// front of you, and the second answer cannot be assembled out here without a
// second copy of profile.specMergers — the table that decides what is keyed
// and by what, which that package documents as the only place a key earns a
// merge rule. Provenance per member is a change to the cascade, not to its
// caller, and it is more than this command should carry.
func declaringProfiles(ctx context.Context, l profile.Loader, resolved *profile.Resolved) (map[string][]string, error) {
	out := make(map[string][]string, len(resolved.Spec))
	for _, id := range resolved.Chain {
		p, err := l.Profile(ctx, id)
		if err != nil {
			return nil, err
		}
		if p == nil {
			return nil, fmt.Errorf("loading profile %q: %w", id, profile.ErrNilProfile)
		}
		for key := range p.Spec {
			out[key] = append(out[key], id)
		}
	}
	return out, nil
}

// showDocument renders what `cairn show` prints: the chain and the scalar
// fields as one aligned block, then the merged manifest one key at a time.
//
// The fields are a fixed list and a field with no value prints its name and
// nothing after it. A varying set of lines would leave the reader unable to
// tell a field that resolved to nothing from a field this command forgot.
//
// Resolved.Body is the field left out, and it is left out rather than
// forgotten. It is the one thing the cascade concatenates instead of composing
// by key — see profile.Resolve — so it is not what the merge rule made
// unreadable, and it is a whole persona long: printing it would bury the
// manifest this command exists to show. `cairn boot` renders it, which is
// where it is read.
//
// Keys are sorted, and so are the members of every collection the cascade
// composed — see profile.sortedValues, which says why that order is
// deterministic and why nothing may read meaning into it.
func showDocument(resolved *profile.Resolved, scopeDir string, declared map[string][]string) string {
	var b strings.Builder

	fields := [][2]string{
		{"profile", resolved.ID},
		{"chain", strings.Join(resolved.Chain, " -> ")},
		{"name", resolved.Name},
		{"description", resolved.Description},
		{"provider", resolved.Provider.String()},
		{"model", resolved.Model},
		{"abstract", strconv.FormatBool(resolved.Abstract)},
		{"scope", scopeDir},
	}
	width := 0
	for _, f := range fields {
		width = max(width, len(f[0]))
	}
	for _, f := range fields {
		writeField(&b, width, f[0], f[1])
	}

	keys := slices.Sorted(maps.Keys(resolved.Spec))
	fmt.Fprintf(&b, "\nspec (%d %s)\n", len(keys), plural(len(keys), "key"))
	if len(keys) == 0 {
		return b.String()
	}
	b.WriteString(specNote)

	width = 0
	for _, key := range keys {
		width = max(width, len(specLabel(key)))
	}
	for _, key := range keys {
		b.WriteString("\n")
		writeField(&b, width, specLabel(key), strings.Join(declared[key], ", "))
		b.WriteString(indentJSON(resolved.Spec[key]))
	}
	return b.String()
}

// writeField writes one name-and-value line, leaving no trailing space when
// the value is empty.
func writeField(b *strings.Builder, width int, name, value string) {
	if value == "" {
		b.WriteString(name + "\n")
		return
	}
	b.WriteString(pad(name, width) + value + "\n")
}

// specLabel is how a manifest key is named here, which is how the cascade's
// own diagnostics name one: profile.listByField and profile.decodeObject both
// report a key they could not compose as spec.<key>.
func specLabel(key string) string { return "spec." + key }

// pad returns s widened to width and followed by the column gap. width must be
// at least len(s), which every caller guarantees by computing it as the widest
// of the very names it then pads.
func pad(s string, width int) string {
	return s + strings.Repeat(" ", width-len(s)+columnGap)
}

// plural returns word, made plural unless n is one.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// indentJSON returns a manifest value laid out for reading, in the margin its
// key sits above.
//
// [json.Indent] is the whole transformation: it moves whitespace between
// tokens and changes nothing else, so key order, string spelling and number
// spelling all survive it. That is a narrower promise than re-encoding would
// allow, and it is the one [specNote] makes to the reader.
//
// A value that is not JSON is printed as it was stored. The store validates
// every manifest value before writing it and a merge composes valid JSON out
// of valid JSON, so this is unreachable through either — but showing what is
// there is this command's whole job, and refusing to print a value because it
// could not be laid out prettily would fail at exactly the moment the operator
// most needs to see it.
func indentJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, specIndent, specIndent); err != nil {
		return specIndent + string(raw) + "\n"
	}
	return specIndent + buf.String() + "\n"
}
