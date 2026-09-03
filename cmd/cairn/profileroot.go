package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrispian/cairn/profile"
)

// envProfileRoot names the environment variable a profile bundle's root
// reaches a manifest through. It loses to the command-line flag, as every
// other variable cairn reads a path from does.
//
// It is spelled here rather than in a library because nothing below the
// composition root knows it exists: a profile writes "$CAIRN_PROFILE_ROOT/..."
// in a value that names somewhere to read from, and the expander that answers
// it is an ordinary [profile.Expander] handed down from here. Expansion did
// not have to learn a new name, and did not.
const envProfileRoot = "CAIRN_PROFILE_ROOT"

// profileFlagUsage is the one description of --profile, so that the three
// commands that take it describe it identically.
const profileFlagUsage = "a profile bundle directory, seeded as $" + envProfileRoot

// errProfileBundle reports a --profile that does not name an existing
// directory.
var errProfileBundle = errors.New("--profile does not name a profile bundle")

// resolveProfileRoot turns --profile's value into the absolute directory
// [envProfileRoot] is seeded with. An empty value seeds nothing and returns
// the empty string, which leaves the process environment's own answer in play.
//
// Everything below is done to the flag and to nothing else. A bundle root
// arriving through the variable is passed through exactly as it was exported —
// unabsolutized, unchecked — because it is not cairn's value to rewrite, and
// the guarantees here stop at the command line. `CAIRN_PROFILE_ROOT=/gone cairn
// boot x` therefore reaches the failure the check below exists to prevent, one
// diagnostic per derived path. That is the cost of not vetting somebody else's
// environment, and it is named here rather than left for a reader to discover.
//
// Absolute, and made so here. The bundle root is a prefix on paths cairn then
// resolves somewhere else entirely — a slot's static path resolves against the
// instance's scope, not against the shell the operator typed the flag in — so
// a relative --profile would name one directory to the operator and a
// different one to every value that expands it. Here, where the operator's own
// working directory is still the right thing to resolve against, is the only
// place that can be settled.
//
// A leading "~/" expands and a "$VAR" does not. The flag came through a shell,
// which has already expanded what it was going to; expanding again would
// re-read a value the operator may have quoted deliberately. The tilde
// survives quoting and shells routinely hand it through, so it is the one form
// left to deal with — [profile.ExpandPath] with a nil lookup is exactly that
// pair.
//
// Symlinks are not resolved, where scope.Parse does resolve them. That
// difference is the difference in what the two values are for. A scope is
// compared against a directory about to be written, and os.SameFile needs a
// canonical path for the comparison to mean anything. A bundle root is a prefix
// glued onto paths the operator wrote, and canonicalizing it would put a
// spelling they never used into every diagnostic about a path that failed —
// which is the loss [profile.QuotedExpansion] exists to prevent.
//
// A bundle that does not exist is refused rather than seeded. This is not a
// rule about where a bundle may live, in the way plan §1 rules out: any
// directory is accepted, and nothing inside it is required — cairn opens no
// part of the bundle itself, only the paths a profile names inside it. What
// the check buys is where the failure lands. A root that is not there makes
// every value that expands it wrong at once, and without this the operator
// reads that as one diagnostic per derived path, none of which names the flag
// that caused them. It is install.Root.Check's reasoning about the install
// root: a missing directory here is not a starting state, it is a sign cairn
// was pointed somewhere wrong.
func resolveProfileRoot(raw, home string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	expanded, err := profile.ExpandPath(trimmed, home, nil)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("absolute path of the profile bundle %q: %w", raw, err)
	}
	info, err := os.Stat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("%w: %s", errProfileBundle, abs)
	case err != nil:
		return "", fmt.Errorf("stat the profile bundle %s: %w", abs, err)
	case !info.IsDir():
		return "", fmt.Errorf("%w: %s is not a directory", errProfileBundle, abs)
	}
	return abs, nil
}

// environment returns the lookup a manifest's variables are expanded against:
// the process environment, with [envProfileRoot] answered by the bundle the
// operator named on the command line.
//
// One value threaded from here is the whole of what --profile does. Cairn
// already expands $VAR and ${VAR} in every manifest value that names somewhere
// to read from, and already carries the lookup down from the composition root
// rather than letting anything below reach for the process's own — so a bundle
// root is a variable like any other, and seeding it needed no new expansion,
// no new manifest key and no new path rule.
//
// An empty root returns [os.Getenv] itself, so a command run without the flag
// reads the environment exactly as it did before, [envProfileRoot] included: a
// profile bundle exported into the shell keeps working, and the flag is what
// overrides it rather than the reverse.
func environment(profileRoot string) profile.Expander {
	if profileRoot == "" {
		return os.Getenv
	}
	return func(name string) string {
		if name == envProfileRoot {
			return profileRoot
		}
		return os.Getenv(name)
	}
}
