package catalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chrispian/cairn/profile"
	"gopkg.in/yaml.v3"
)

// frontmatterFence is the line that opens a profile's frontmatter and the line
// that closes it.
const frontmatterFence = "---"

// frontmatterLineOffset turns a line number inside the frontmatter into a line
// number in the file. The frontmatter's first line is the file's second,
// because the file's first is the fence.
const frontmatterLineOffset = 1

// frontmatterKeys are the keys a profile's frontmatter may carry, in the order
// a diagnostic names them.
//
// It is a closed set, and that is the difference between this and spec. A
// manifest key cairn has never heard of is carried untouched, because the
// manifest is somebody else's vocabulary; a frontmatter key cairn has never
// heard of is a typo, and the file is now the only copy — nothing downstream
// will notice that "descripton" left the profile without a description, or
// that a misspelled "spec" left it without a manifest at all.
var frontmatterKeys = []string{
	"id", "extends", "abstract", "name", "description", "provider", "model", "spec",
}

// parseProfile reads one profile file. stem is the file's base name without
// its extension, which is the profile's id.
func parseProfile(text, stem string) (profile.Profile, error) {
	front, body, err := splitFrontmatter(text)
	if err != nil {
		return profile.Profile{}, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return profile.Profile{}, fmt.Errorf("frontmatter: %w", err)
	}
	root := mappingOf(&doc)
	if root == nil {
		return profile.Profile{}, errors.New("the frontmatter is not a mapping")
	}

	p := profile.Profile{ID: stem, Spec: profile.Spec{}}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		var err error
		switch key.Value {
		case "id":
			err = value.Decode(&p.ID)
		case "extends":
			err = value.Decode(&p.Extends)
		case "abstract":
			err = value.Decode(&p.Abstract)
		case "name":
			err = value.Decode(&p.Name)
		case "description":
			err = value.Decode(&p.Description)
		case "provider":
			var name string
			if err = value.Decode(&name); err == nil {
				p.Provider = profile.Provider(name)
			}
		case "model":
			err = value.Decode(&p.Model)
		case "spec":
			p.Spec, err = decodeSpec(value)
		default:
			return profile.Profile{}, fmt.Errorf("line %d: %q is not a frontmatter key — they are %s",
				key.Line+frontmatterLineOffset, key.Value, strings.Join(frontmatterKeys, ", "))
		}
		if err != nil {
			return profile.Profile{}, fmt.Errorf("line %d: %s: %w", key.Line+frontmatterLineOffset, key.Value, err)
		}
	}

	// The id and the file name are one fact written twice, and this is where
	// they are held to agreeing. The file name wins nothing: a profile whose
	// frontmatter says otherwise is refused rather than quietly renamed,
	// because an extends chain names a profile by id and a bundle where the
	// two spellings differ resolves one and lists the other.
	if strings.TrimSpace(p.ID) != stem {
		return profile.Profile{}, fmt.Errorf("the frontmatter id is %q and the file is named %q: rename one to match the other",
			p.ID, stem+profileExt)
	}
	p.Body = body
	return p, nil
}

// splitFrontmatter separates a profile file's YAML frontmatter from the prose
// after it.
//
// The fences are whole lines, found rather than counted. Splitting on the
// first three occurrences of "---" — which is what the seeder this replaces
// did — cuts a profile in half at the first horizontal rule in its prose, or
// at a YAML value that happens to hold one.
//
// The body is trimmed and given back one trailing newline, or nothing at all
// when it is blank. It is prose that gets concatenated with other profiles'
// prose in the cascade, so leading and trailing blank lines are the file's
// formatting rather than the author's content.
func splitFrontmatter(text string) (front, body string, err error) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || fenceLine(lines[0]) != frontmatterFence {
		return "", "", errors.New("no YAML frontmatter: a profile opens with a --- line")
	}
	for i := 1; i < len(lines); i++ {
		if fenceLine(lines[i]) != frontmatterFence {
			continue
		}
		// The line break before the closing fence belongs to the frontmatter,
		// and putting it back is not cosmetic: a block scalar's last line ends
		// at that break, and a YAML document that stops without one clips the
		// trailing newline off the value. A `body: |` written in a subagent
		// declaration would silently lose the newline it was authored with.
		front = strings.Join(lines[1:i], "\n") + "\n"
		if prose := strings.TrimSpace(strings.Join(lines[i+1:], "\n")); prose != "" {
			body = prose + "\n"
		}
		return front, body, nil
	}
	return "", "", errors.New("the frontmatter is never closed: there is no second --- line")
}

// fenceLine returns a line with the trailing whitespace a fence may carry
// removed, the carriage return of a CRLF file included.
func fenceLine(line string) string { return strings.TrimRight(line, " \t\r") }

// mappingOf returns the mapping node a decoded document holds, or nil for
// anything else — an empty document, a document holding a list, a document
// holding a bare scalar.
func mappingOf(doc *yaml.Node) *yaml.Node {
	node := doc
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}
