package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chrispian/cairn/catalog"
)

// listSection is one block of the listing: a heading, the sentence saying what
// the rows under it are, and the rows as columns that have not been aligned
// yet. Alignment is per section — see [writeRows] — so the sections are held
// this way rather than written as they are built.
type listSection struct {
	heading string
	note    string
	rows    [][]string
}

// runList enumerates the catalog: the bindings with the directories their
// scopes resolve to, and the profiles with their descriptions.
//
// It exists for two reasons and the smaller one is the conductor's. A
// file-backed catalog with no way to list it is a directory an operator has to
// `ls` themselves, reading nine files to answer "what can I boot" — and the
// answer is not in any one of them, since a binding's scope may be an alias
// that resolves somewhere else. The other reason is that the profile which
// prints that menu into its own boot file used to do it with a SQL query, and
// the database it queried is gone.
//
// It renders nothing and writes nothing, for the reason `cairn show` does.
//
// There is no target and no --scope. A listing is of the whole catalog: one
// profile is what `cairn show` is for, and a scope is an instance value that
// no listing has an instance to resolve against.
func runList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cairn list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profileFlag := fs.String("profile", "", profileFlagUsage)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return fmt.Errorf("list takes no binding or profile, and was given %q", fs.Arg(0))
	}

	home, _ := os.UserHomeDir()
	bundle, err := bundleRoot(*profileFlag, home)
	if err != nil {
		return err
	}
	cat, err := catalog.Open(bundle)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(stdout, listDocument(cat))
	return err
}

// listDocument renders the catalog.
//
// The bundle's own path is deliberately not printed. This document is read in
// two places — a terminal, where the operator just typed the flag that chose
// the bundle, and the conductor's boot file, where a machine-specific absolute
// path would be the one line of the render that differs between two checkouts.
// `cairn show` reports the root, which is where a question about the bundle
// itself belongs.
//
// A section with no rows renders nothing at all, which is install.Report's
// rule and for its reason: a bundle with no aliases should not have to scroll
// past a heading saying so.
func listDocument(cat *catalog.Catalog) string {
	sections := []listSection{
		{
			heading: "Bindings",
			note: "The name is all `cairn boot` needs: it carries the profile and where the work is.\n" +
				specIndent + "A binding that composes says that too: the parts, skills and prompts it adds,\n" +
				specIndent + "as it declares them. `cairn show <name>` resolves what a boot will carry.",
		},
		{
			heading: "Profiles",
			note:    "Bootable by id, and each one needs a scope: a path, or an alias below.",
		},
		{
			heading: "Abstract profiles",
			note:    "Extended rather than booted. `cairn install` and `cairn show` take one; `cairn boot` refuses it.",
		},
		{
			heading: "Scope aliases",
			note:    "A short name a binding's scope, or --scope, may be written as instead of a path.",
		},
	}
	const (
		bindings = 0
		profiles = 1
		abstract = 2
		aliases  = 3
	)

	// The composition is the last column, and last is load-bearing. [writeRows]
	// measures a column across every row of the block and pads all but the last
	// one, so a binding that names six parts spends that width on its own line
	// and no other. In any earlier position it would push every other binding's
	// scope right by the same amount, and eight rows would become a wall of
	// whitespace to say something about one of them. It is also the only column
	// holding three lists rather than one value, so it is the only one whose
	// width has no bound — which is the same fact from the other side.
	//
	// The binding's own file writes scope last, and the order there is the
	// order a composition resolves in — see catalog's bindingFile. This row
	// deliberately disagrees, because a file is read top to bottom and a column
	// is measured across rows.
	for _, b := range cat.Bindings() {
		sections[bindings].rows = append(sections[bindings].rows,
			[]string{b.Name, b.ProfileID, cat.ResolvedScope(b), bindingComposition(b)})
	}
	for _, p := range cat.Profiles() {
		at := profiles
		if p.Abstract {
			at = abstract
		}
		sections[at].rows = append(sections[at].rows, []string{p.ID, p.Description})
	}
	for _, s := range cat.Scopes() {
		sections[aliases].rows = append(sections[aliases].rows, []string{s.Alias, s.Path})
	}

	var b strings.Builder
	for _, section := range sections {
		if len(section.rows) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s (%d)\n", section.heading, len(section.rows))
		b.WriteString(specIndent + section.note + "\n")
		writeRows(&b, section.rows)
	}
	return b.String()
}

// bindingComposition renders what a binding adds to the profile it boots: the
// parts merged onto that profile's chain, then the skills and prompts the boot
// directory carries. Empty for a binding that adds none of them.
//
// Ids, and not a count of them. The defect this closes is that two bindings
// booting materially different sessions rendered as one row but for the name,
// and a count reproduces that a notch further in: two bindings each adding one
// part are still one row apart, which is the same complaint about the same
// listing. The case is sharpest for prompts, because a prompt is planted as a
// command a person can type — "1 prompt" stands for an invocation without
// saying what to invoke. `cairn show <name>` resolves the whole cascade and is
// where a full account belongs; a listing is for choosing which of these to
// boot, and that choice is made on the ids.
//
// They are the binding's own ids, spelled as it wrote them. [catalog.Binding]
// keeps a part as written so that it stays true when the bundle moves, and
// expanding one here would undo that and could print an absolute path — which
// is the line [listDocument] exists not to grow. It follows that this column
// is what the binding adds and not what the boot ends up carrying: a skill the
// profile itself declares is not here, which is what the note under the
// heading spends its second sentence on.
//
// The labels are the binding file's keys rather than the flags that fill them,
// for the reason catalog's bindingFile gives for the keys themselves — they
// name what the binding holds. An operator who reads "parts: docs-only" here
// opens the file and finds "parts:" in it.
func bindingComposition(b catalog.Binding) string {
	// Declaration order, which is also resolution order: parts change what the
	// profile is, and skills and prompts are added to whatever it resolved to.
	groups := []struct {
		key string
		ids []string
	}{
		{"parts", b.Parts},
		{"skills", b.Skills},
		{"prompts", b.Prompts},
	}
	written := make([]string, 0, len(groups))
	for _, g := range groups {
		if len(g.ids) == 0 {
			continue
		}
		written = append(written, g.key+": "+strings.Join(g.ids, ", "))
	}
	// The gap between groups is the gap between columns, because with nothing
	// to the right of it there is no second boundary for it to be confused
	// with, and one spacing is one thing for a reader to learn.
	return strings.Join(written, strings.Repeat(" ", columnGap))
}

// writeRows writes one section's rows, each column widened to the widest entry
// in it, indented to sit under its heading's note.
//
// The last column is not padded and every line is trimmed on the right. A
// binding that composes nothing, or a profile with no description, would
// otherwise end in a run of spaces — invisible in a terminal, and a diff in a
// file this render is planted into.
//
// The trim does more than tidy: it is why the composition column costs nothing
// where it is not used. A binding with no parts, skills or prompts pads its
// scope to the block's width, appends nothing, and has the padding taken back
// off — so the row is byte-for-byte the row it was before the column existed.
// Every binding in the live bundle is that binding today.
func writeRows(b *strings.Builder, rows [][]string) {
	widths := make([]int, 0, 4)
	for _, row := range rows {
		for i, cell := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], len(cell))
		}
	}
	for _, row := range rows {
		line := specIndent
		for i, cell := range row {
			if i == len(row)-1 {
				line += cell
				continue
			}
			line += pad(cell, widths[i])
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
}
