// Package store is Cairn's sqlite database: the profiles it resolves, the
// bindings that name them, and the scope aliases those bindings point at.
//
// It is deliberately small. Three tables, no migrations beyond the one that
// creates them, and no column that exists to record something Cairn does not
// render. The schema is not the model.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/hollis-labs/go-sqlite/sqlitekit"
)

// EnvDB names the environment variable that overrides the database path. It
// loses to the command-line flag.
const EnvDB = "CAIRN_DB"

// EnvXDGConfigHome names the XDG base-directory variable for user
// configuration. When set, the default path is $XDG_CONFIG_HOME/agents.
const EnvXDGConfigHome = "XDG_CONFIG_HOME"

// DirName is the base name of the configuration directory the database sits
// in. It names the contents rather than a consumer of them: several tools may
// read the same profiles, so the directory is "agents" rather than "cairn".
const DirName = "agents"

// FileName is the database's file name inside [DirName].
const FileName = "cairn.db"

// TimeLayout is how a timestamp column is stored: RFC 3339 in UTC, to
// nanosecond precision, so the text sorts in chronological order.
const TimeLayout = time.RFC3339Nano

// ErrNoHome reports that the database path fell back to the home directory and
// no home directory is known.
var ErrNoHome = errors.New("home directory unknown")

// ErrProfileNotFound reports that no profile row exists for an id.
var ErrProfileNotFound = errors.New("profile not found")

// ErrBindingNotFound reports that no binding row exists for a name.
var ErrBindingNotFound = errors.New("binding not found")

// ErrScopeNotFound reports that no scope row exists for an alias.
var ErrScopeNotFound = errors.New("scope alias not found")

// ErrInvalidKey reports a profile id, binding name, or scope alias that is
// empty or is only whitespace.
var ErrInvalidKey = errors.New("invalid key")

// Binding is one row of the bindings table: a name an operator boots by, the
// profile it resolves to, and the scope that boot works in.
type Binding struct {
	// Name is the binding's primary key — what `cairn boot` is given.
	Name string

	// ProfileID is the profile this binding boots.
	ProfileID string

	// Scope is where that boot works. It is a scope alias when one exists by
	// that name and a directory path otherwise, so an operator who has not
	// declared an alias is not obliged to. Empty means no declared scope.
	Scope string
}

// Scope is one row of the scopes table: a short alias for a directory.
type Scope struct {
	// Alias is the scope's primary key.
	Alias string

	// Path is the directory the alias names.
	Path string
}

// Store is an open database.
//
// It holds one connection: Cairn is a single-user command-line tool that opens
// the database, does one thing, and exits, so a reader pool would buy nothing
// and a second connection would only make SQLITE_BUSY reachable.
type Store struct {
	db *sql.DB

	// now is the clock timestamp columns are stamped from. Tests replace it;
	// nothing else does.
	now func() time.Time
}

// DefaultPath returns the database path: the value of [EnvDB] when it is set,
// $XDG_CONFIG_HOME/agents/cairn.db when that is set, and
// $HOME/.config/agents/cairn.db otherwise. It reports [ErrNoHome] only when it
// actually needs a home.
//
// Every input is passed rather than read, so nothing here consults the process
// environment on its own.
func DefaultPath(envDB, xdgConfigHome, home string) (string, error) {
	if p := strings.TrimSpace(envDB); p != "" {
		return p, nil
	}
	if x := strings.TrimSpace(xdgConfigHome); x != "" {
		return filepath.Join(x, DirName, FileName), nil
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("%w: set %s to say where the database lives", ErrNoHome, EnvDB)
	}
	return filepath.Join(home, ".config", DirName, FileName), nil
}

// Open opens the database at path, creating the file, its parent directory,
// and the schema if any of them are absent.
//
// An absent database is not a configuration error: an empty one is a usable
// starting state, and the operator has to be able to write the first profile
// into something.
func Open(ctx context.Context, path string) (*Store, error) {
	// WriterOptions rather than the opener's own default: OpenSingle falls
	// back to DefaultOptions, which leaves _txlock unset, and txutil's
	// immediate-transaction contract is carried by the DSN rather than by the
	// BEGIN. Without it every transaction below would quietly be deferred.
	db, err := sqlitekit.OpenSingle(ctx, path, sqlitekit.OpenOptions{
		Options:         sqlitekit.WriterOptions(),
		CreateParentDir: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	s := &Store{db: db, now: time.Now}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB returns the underlying handle. It exists for tests and for a caller that
// needs a query this package does not offer; ordinary use goes through the
// methods.
func (s *Store) DB() *sql.DB { return s.db }

// SetClock replaces the clock timestamp columns are stamped from. It exists so
// a test can assert on a stored timestamp; production callers leave it alone.
func (s *Store) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}
