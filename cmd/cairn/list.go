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
			note:    "The name is all `cairn boot` needs: it carries the profile and where the work is.",
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

	for _, b := range cat.Bindings() {
		sections[bindings].rows = append(sections[bindings].rows,
			[]string{b.Name, b.ProfileID, cat.ResolvedScope(b)})
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

// writeRows writes one section's rows, each column widened to the widest entry
// in it, indented to sit under its heading's note.
//
// The last column is not padded and every line is trimmed on the right. A
// binding that declares no scope, or a profile with no description, would
// otherwise end in a run of spaces — invisible in a terminal, and a diff in a
// file this render is planted into.
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
