package slots

import (
	"os"

	"github.com/hollis-labs/agentkit/agentcontext"
)

// Expander returns the value of an environment variable. Nil means the process
// environment.
//
// It is a parameter rather than a direct read so that a test can state what the
// environment held, and so that the one place cairn reads the environment
// during a render is named.
type Expander func(name string) string

// expand returns s with $VAR and ${VAR} replaced by look's answers. An unset
// name expands to nothing, which is [os.Expand]'s behaviour and the behaviour
// of every shell an operator writing one of these paths has used.
func expand(s string, look Expander) string {
	if s == "" {
		return s
	}
	if look == nil {
		look = os.Getenv
	}
	return os.Expand(s, look)
}

// expandSources returns specs with $VAR and ${VAR} expanded in the two fields
// that name somewhere to read from: a static path and an HTTP URL.
//
// It is those two and nothing else, deliberately. A profile has to say where a
// service lives without hardcoding a host that differs between machines, and
// without cairn growing a second configuration file to hold one. Expanding a
// command line as well would make the environment able to rewrite what runs,
// which is a larger promise than "say where to read from" — a cmd slot already
// runs through a shell that does its own expansion, so nothing is lost by
// leaving it alone.
//
// The specs are copied rather than rewritten: the caller's manifest is what a
// diagnostic quotes, and an error naming an expanded value would name something
// the operator never wrote.
func expandSources(specs []agentcontext.SlotSpec, look Expander) []agentcontext.SlotSpec {
	out := make([]agentcontext.SlotSpec, len(specs))
	copy(out, specs)
	for i := range out {
		out[i].Source = expandSource(out[i].Source, look)
	}
	return out
}

// expandSource is [expandSources] for one source.
func expandSource(src agentcontext.SlotSource, look Expander) agentcontext.SlotSource {
	src.StaticFile.Path = expand(src.StaticFile.Path, look)
	src.StaticDir.Path = expand(src.StaticDir.Path, look)
	src.HTTPText.URL = expand(src.HTTPText.URL, look)
	src.HTTPJSON.URL = expand(src.HTTPJSON.URL, look)
	return src
}
