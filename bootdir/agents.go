package bootdir

import (
	"fmt"
	"strings"
)

// renderAgents returns the instruction file every boot directory carries.
//
// It is composed of declared fields and nothing else: the profile's own name,
// description and body, followed by a block listing the fields the profile
// resolved to. Cairn writes no sentence of its own into it. What an agent is
// told belongs to whoever wrote the profile, so a paragraph nobody declared is
// a paragraph nobody can correct — the profile body is where that text goes.
//
// The file is always rendered, including for a profile that declares nothing
// at all, in which case it is empty rather than absent: a boot directory whose
// instruction file is missing looks like a render that stopped halfway.
func renderAgents(inst *Instance) ([]File, error) {
	if inst == nil || inst.Profile == nil {
		return nil, ErrNoProfile
	}
	if !inst.Layout.Agents.Declared() {
		return nil, fmt.Errorf("%w: this layout declares no path for %s", ErrProviderLayout, AgentsFileName)
	}
	return []File{{
		Path:    inst.Layout.Agents.RelPath,
		Content: agentsContent(inst),
		Mode:    inst.Layout.Agents.Mode,
	}}, nil
}

// agentsContent renders the blocks of the instruction file, separated by one
// blank line and ending in exactly one newline. A block whose fields are all
// empty is omitted along with its separator; a profile with no declared field
// at all renders no bytes.
func agentsContent(inst *Instance) []byte {
	blocks := make([]string, 0, 4)
	if title := agentsTitle(inst); title != "" {
		blocks = append(blocks, "# "+title)
	}
	if description := agentsBlock(inst.Profile.Description); description != "" {
		blocks = append(blocks, description)
	}
	if body := agentsBlock(inst.Profile.Body); body != "" {
		blocks = append(blocks, body)
	}
	if section := agentsProfileSection(inst); section != "" {
		blocks = append(blocks, section)
	}
	if len(blocks) == 0 {
		return nil
	}
	return []byte(strings.Join(blocks, "\n\n") + "\n")
}

// agentsTitle returns the heading text: the profile's name, falling back to
// its id, so that a profile with no display name is still named by something a
// reader can look up.
func agentsTitle(inst *Instance) string {
	if name := strings.TrimSpace(inst.Profile.Name); name != "" {
		return name
	}
	return strings.TrimSpace(inst.Profile.ID)
}

// agentsProfileSection lists the fields the profile resolved to, in a fixed
// order, one line per field that declares a value. Every value is a field the
// operator set or the cascade produced; nothing here is inferred. A profile
// that declares none of them renders no section rather than an empty heading.
func agentsProfileSection(inst *Instance) string {
	fields := []struct{ label, value string }{
		{"profile", inst.Profile.ID},
		{"name", inst.Profile.Name},
		{"provider", inst.Profile.Provider.String()},
		{"model", inst.Profile.Model},
		{"scope", inst.Scope},
	}
	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		if value := strings.TrimSpace(field.value); value != "" {
			lines = append(lines, "- "+field.label+": "+value)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "## Profile\n\n" + strings.Join(lines, "\n")
}

// agentsBlock returns prose as one block of the instruction file: its own
// bytes, with only the blank lines around it removed so that the separator
// between two blocks is exactly one blank line however the field was stored.
//
// Leading spaces and tabs on the first line survive, because an indented first
// line is a code block and trimming it would rewrite what the operator wrote.
// A field holding only whitespace is treated as empty.
func agentsBlock(prose string) string {
	if strings.TrimSpace(prose) == "" {
		return ""
	}
	return strings.Trim(prose, "\r\n")
}
