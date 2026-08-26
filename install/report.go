package install

import (
	"fmt"
	"slices"
	"strings"
)

// Exit codes for `cairn install --check`. They are here rather than in the
// command layer because they are part of the check's contract: whatever wires
// the check up branches on them, and the meaning has to be the same wherever
// it is wired.
const (
	// exitClean reports that every path cairn claims holds what the render
	// produces.
	exitClean = 0

	// exitDrift reports that the check ran and found something.
	exitDrift = 1
)

// reportSection is one status as [Report.String] prints it: the heading, and
// the sentence saying what the status means for the paths under it.
type reportSection struct {
	status  Status
	heading string
	note    string
}

// reportSections are the statuses a report is printed in, in the order they
// are printed: what differs from the render first, what matches last. It is
// the order an operator reads for — the findings are at the top, and the list
// of files that are fine does not have to be scrolled past to reach them.
func reportSections() []reportSection {
	return []reportSection{
		{StatusMissing, "Missing", "cairn renders these and there is nothing at the path."},
		{StatusModified, "Modified", "cairn renders these and the bytes on disk are different."},
		{StatusNotAFile, "Not a file", "cairn renders a file at these paths and something else is there."},
		{StatusUnreadable, "Unreadable", "these could not be read, so nothing is known about them."},
		{StatusOrphan, "Orphan", "cairn claims these paths and this render does not produce them."},
		{StatusMatch, "Match", "these are on disk with the render's bytes."},
	}
}

// ByStatus returns every entry carrying status, in path order — the order
// [Report.String] prints them in.
//
// [Report.Entries] keeps the order the check produced its findings in. This
// sorts, so that two runs over the same tree read identically whatever order a
// directory happened to be listed in.
func (r *Report) ByStatus(status Status) []Entry {
	if r == nil {
		return nil
	}
	var out []Entry
	for _, entry := range r.Entries {
		if entry.Status == status {
			out = append(out, entry)
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// Count returns how many entries carry status.
func (r *Report) Count(status Status) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, entry := range r.Entries {
		if entry.Status == status {
			n++
		}
	}
	return n
}

// Clean reports whether every path the check looked at holds what the render
// produces.
//
// Every status other than [StatusMatch] makes a report unclean, orphans
// included. An orphan is a path cairn claims — one of its own file paths, or
// anything inside a directory it fills whole — that this render does not
// produce: what a profile leaves behind when it stops declaring something. The
// sweep exists to find them, so a verdict that ignored them would ignore half
// the check.
func (r *Report) Clean() bool {
	if r == nil {
		return true
	}
	for _, entry := range r.Entries {
		if entry.Status != StatusMatch {
			return false
		}
	}
	return true
}

// ExitCode returns the code a command reporting r exits with: zero when the
// report is clean, non-zero otherwise.
//
// A check that could not run has no report at all and exits on its error
// instead, so this never reports that case.
func (r *Report) ExitCode() int {
	if r.Clean() {
		return exitClean
	}
	return exitDrift
}

// Summary returns the one-line count of what the check found, findings first
// and matches last. A status nothing carries is left out rather than printed
// as a zero.
func (r *Report) Summary() string {
	if r == nil {
		return "no report"
	}
	parts := make([]string, 0, len(reportSections()))
	for _, section := range reportSections() {
		if n := r.Count(section.status); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, section.status))
		}
	}
	line := "nothing checked"
	if len(parts) > 0 {
		line = strings.Join(parts, " · ")
	}
	if r.Root != "" {
		return r.Root + ": " + line
	}
	return line
}

// String renders the whole report for a terminal: the summary line, then one
// section per status that has entries, then the verdict.
//
// Paths are sorted within each section and every entry's detail is printed
// under it, so the report says what was found and where. It says nothing about
// what to do next beyond naming what the check itself did and did not touch.
func (r *Report) String() string {
	if r == nil {
		return "no report\n"
	}
	var b strings.Builder
	b.WriteString(r.Summary())
	b.WriteString("\n")
	for _, section := range reportSections() {
		writeSection(&b, section, r.ByStatus(section.status))
	}
	b.WriteString("\n")
	b.WriteString(r.verdict())
	return b.String()
}

// verdict is the last line: whether the layer matches the render, and what the
// check did about it, which is nothing.
func (r *Report) verdict() string {
	if r.Clean() {
		return "In sync. Every file cairn renders is on disk with the render's bytes.\n"
	}
	return "Out of sync. `--check` reports and repairs nothing: no path above was created, " +
		"rewritten or deleted.\n"
}

// writeSection renders one heading, its note, and its entries. A section with
// no entries renders nothing at all, so a clean report is short.
func writeSection(b *strings.Builder, section reportSection, entries []Entry) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s (%d)\n", section.heading, len(entries))
	b.WriteString("  " + section.note + "\n")
	for _, entry := range entries {
		b.WriteString("  " + entry.Path + "\n")
		if entry.Detail != "" {
			b.WriteString("      " + entry.Detail + "\n")
		}
	}
}
