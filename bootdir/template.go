package bootdir

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// MarkerVerbSlot names a marker that stands for one of the manifest's declared
// slots, substituted with that slot's rendered section.
const MarkerVerbSlot = "slot"

// MarkerVerbValue names a marker that stands for one value of the instance
// being materialized — see [ValueNames].
const MarkerVerbValue = "value"

// ErrMarker reports a marker cairn cannot act on: it names no verb or no
// target, it names a verb that is not [MarkerVerbSlot] or [MarkerVerbValue],
// or it names a value that is not one this instance carries.
//
// A malformed marker is refused rather than left in place. The cairn: prefix is
// this package's namespace, so a marker inside it that cairn does not
// understand is a mistake in a template rather than someone else's syntax — and
// leaving it alone would plant the marker's own text in a file an agent reads.
var ErrMarker = errors.New("invalid template marker")

// markerPattern matches any marker in cairn's namespace, capturing everything
// between the prefix and the close so that a malformed one is caught here
// rather than silently left in the output.
//
// It is an HTML comment for four reasons. It is invisible in rendered markdown
// and unmistakable in source, so a template reads as a document rather than as
// code. It is already this repository's comment idiom — see
// [github.com/chrispian/cairn/install.GeneratedMarker]. A harness resolving
// "@name" imports strips HTML comments before it looks for them, so a marker
// can never be read as one. And there is nowhere in it to put a conditional,
// which is the property that keeps a template a substitution target rather than
// a program.
//
// Go's "." does not match a newline, so a marker is one line by construction.
var markerPattern = regexp.MustCompile(`<!--\s*cairn:(.*?)-->`)

// Marker is one occurrence of a marker in a template.
type Marker struct {
	// Verb is [MarkerVerbSlot] or [MarkerVerbValue].
	Verb string

	// Name is the slot or value the marker stands for.
	Name string

	// Text is the marker exactly as it was written, for a diagnostic that has
	// to quote it.
	Text string
}

// ValueNames returns the instance values a template may substitute, sorted.
//
// The set is closed and cairn knows every member, which is why a marker naming
// something else is refused rather than omitted: an undeclared slot might be
// one an operator means to add, and an unknown value can only be a typo.
//
// Deliberately absent: the boot directory's own path and the operator's home.
// Both are absolute paths into one machine rather than facts about a profile,
// and the first is the directory the file is being written into, which whatever
// reads it already knows.
func ValueNames() []string {
	return []string{"binding", "model", "profile", "provider", "scope", "session"}
}

// Markers returns every marker in text, in the order they appear.
//
// It is the only scanner. Substitution runs through it and so does the caller
// that reports a marker which stood for nothing, because two scanners that
// disagreed would report one set of markers and substitute another.
func Markers(text string) ([]Marker, error) {
	found := markerPattern.FindAllStringSubmatch(text, -1)
	out := make([]Marker, 0, len(found))
	for _, match := range found {
		marker, err := parseMarker(match[0], match[1])
		if err != nil {
			return nil, err
		}
		out = append(out, marker)
	}
	return out, nil
}

// parseMarker reads one marker's body — the text between "cairn:" and the
// comment close — into a [Marker].
func parseMarker(text, body string) (Marker, error) {
	fields := strings.Fields(body)
	if len(fields) != 2 {
		return Marker{}, fmt.Errorf(
			"%w: %s names %s, and a marker is a verb and one name: %s",
			ErrMarker, text, countedFields(fields), markerForms())
	}
	verb, name := fields[0], fields[1]
	switch verb {
	case MarkerVerbSlot:
		return Marker{Verb: verb, Name: name, Text: text}, nil
	case MarkerVerbValue:
		if !slices.Contains(ValueNames(), name) {
			return Marker{}, fmt.Errorf("%w: %s names no value this instance carries; the values are %s",
				ErrMarker, text, quotedNames(ValueNames()))
		}
		return Marker{Verb: verb, Name: name, Text: text}, nil
	default:
		return Marker{}, fmt.Errorf("%w: %s declares the verb %q; the verbs are %q and %q",
			ErrMarker, text, verb, MarkerVerbSlot, MarkerVerbValue)
	}
}

// Substitute returns text with every marker replaced.
//
// A slot marker becomes that slot's rendered section, which is the heading and
// the content together or nothing at all: a slot that failed to resolve and one
// that resolved empty were both dropped before this ran, so a marker standing
// for either leaves no heading behind. That is the same rule the boot file
// followed when it was the only place a slot could land, and moving the heading
// into the template is exactly where it would have been lost — see
// [github.com/chrispian/cairn/slots.Sections].
//
// A marker naming a slot the manifest never declared substitutes nothing, and
// the caller reports it. It is not an error here because it is the same
// omission as a slot that resolved to nothing, and the file is the same file
// either way.
//
// A value marker becomes that value, which may legitimately be empty — a boot
// with no declared scope substitutes nothing and the template reads as though
// the marker were not there.
//
// Substitution does not look at where in the document a marker sits. A marker
// inside a fenced code block is substituted like any other, so a template that
// documents this syntax has to avoid writing a live one.
func Substitute(text string, sections, values map[string]string) (string, error) {
	markers, err := Markers(text)
	if err != nil {
		return "", err
	}
	for _, marker := range markers {
		replacement := values[marker.Name]
		if marker.Verb == MarkerVerbSlot {
			replacement = sections[marker.Name]
		}
		text = strings.Replace(text, marker.Text, replacement, 1)
	}
	return text, nil
}

// Unfilled returns the markers in text that stood for nothing: a slot the
// manifest did not declare, and a slot that resolved to nothing. Both are
// reported so an operator can tell a missing block from one they removed.
//
// Values are not reported. An empty value is a fact about the instance — a boot
// with no scope — rather than something that went wrong.
func Unfilled(text string, sections map[string]string) ([]Marker, error) {
	markers, err := Markers(text)
	if err != nil {
		return nil, err
	}
	var out []Marker
	for _, marker := range markers {
		if marker.Verb != MarkerVerbSlot {
			continue
		}
		if sections[marker.Name] == "" {
			out = append(out, marker)
		}
	}
	return out, nil
}

// countedFields describes what a malformed marker held, for a diagnostic that
// has to say what was wrong with it rather than only that something was.
func countedFields(fields []string) string {
	switch len(fields) {
	case 0:
		return "nothing"
	case 1:
		return fmt.Sprintf("only %q", fields[0])
	default:
		return fmt.Sprintf("%d words", len(fields))
	}
}

// markerForms renders the two legal marker spellings for a diagnostic.
func markerForms() string {
	return fmt.Sprintf("<!-- cairn:%s NAME --> or <!-- cairn:%s NAME -->",
		MarkerVerbSlot, MarkerVerbValue)
}
