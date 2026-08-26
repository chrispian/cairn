package slots

import (
	"github.com/chrispian/cairn/profile"
	"github.com/hollis-labs/agentkit/agentcontext"
)

// Expander is [profile.Expander]: the environment a manifest's variables are
// expanded against, supplied by the composition root so that nothing here
// reads the process environment on its own.
type Expander = profile.Expander

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
	src.StaticFile.Path = profile.ExpandEnv(src.StaticFile.Path, look)
	src.StaticDir.Path = profile.ExpandEnv(src.StaticDir.Path, look)
	src.HTTPText.URL = profile.ExpandEnv(src.HTTPText.URL, look)
	src.HTTPJSON.URL = profile.ExpandEnv(src.HTTPJSON.URL, look)
	return src
}
