package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// baseTime is the instant the test clock starts at. Nothing depends on the
// value beyond its being fixed.
var baseTime = time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)

// testClock is a clock a test moves by hand, so a stored timestamp is an
// assertion rather than a race with the wall clock.
type testClock struct {
	t time.Time
}

func (c *testClock) now() time.Time { return c.t }

func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// openStore opens a store on a fresh database under t.TempDir and returns it
// with the clock driving its timestamp columns.
func openStore(t *testing.T) (*Store, *testClock) {
	t.Helper()
	clock := &testClock{t: baseTime}
	s := openStoreAt(t, filepath.Join(t.TempDir(), "cairn.db"))
	s.SetClock(clock.now)
	return s, clock
}

// openStoreAt opens a store at path and closes it when the test ends.
func openStoreAt(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func TestOpenCreatesFileAndParentDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "config", "agents", FileName)

	s := openStoreAt(t, path)

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("parent directory not created: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("database file not created: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("database path is a directory")
	}
	if s.DB() == nil {
		t.Fatal("DB() returned nil on an open store")
	}
}

func TestOpenCreatesSchema(t *testing.T) {
	s, _ := openStore(t)

	// An empty database is a usable starting state: every listing answers
	// before anything has been written.
	profiles, err := s.Profiles(t.Context())
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("Profiles on a fresh database = %d rows, want 0", len(profiles))
	}
	bindings, err := s.Bindings(t.Context())
	if err != nil {
		t.Fatalf("Bindings: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("Bindings on a fresh database = %d rows, want 0", len(bindings))
	}
	scopes, err := s.Scopes(t.Context())
	if err != nil {
		t.Fatalf("Scopes: %v", err)
	}
	if len(scopes) != 0 {
		t.Fatalf("Scopes on a fresh database = %d rows, want 0", len(scopes))
	}
}

func TestOpenExistingDatabaseKeepsItsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	first, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.PutScope(t.Context(), Scope{Alias: "cairn", Path: "/dev/projects/cairn"}); err != nil {
		t.Fatalf("PutScope: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	second := openStoreAt(t, path)
	got, err := second.Scope(t.Context(), "cairn")
	if err != nil {
		t.Fatalf("Scope after reopen: %v", err)
	}
	if want := "/dev/projects/cairn"; got != want {
		t.Fatalf("Scope after reopen = %q, want %q", got, want)
	}
}

func TestOpenSamePathTwiceConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	writer := openStoreAt(t, path)
	reader := openStoreAt(t, path)

	if err := writer.PutScope(t.Context(), Scope{Alias: "notes", Path: "/notes"}); err != nil {
		t.Fatalf("PutScope on the first handle: %v", err)
	}
	got, err := reader.Scope(t.Context(), "notes")
	if err != nil {
		t.Fatalf("Scope on the second handle: %v", err)
	}
	if want := "/notes"; got != want {
		t.Fatalf("Scope on the second handle = %q, want %q", got, want)
	}
}

func TestCloseIsSafeTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	var nilStore *Store
	if err := nilStore.Close(); err != nil {
		t.Fatalf("Close on a nil store: %v", err)
	}
}

func TestSetClockIgnoresNil(t *testing.T) {
	s, clock := openStore(t)
	s.SetClock(nil)

	if err := s.PutProfile(t.Context(), profileFixture("base")); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}
	got, err := s.Profile(t.Context(), "base")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if !got.UpdatedAt.Equal(clock.now()) {
		t.Fatalf("updated_at = %s, want the clock set before SetClock(nil) at %s", got.UpdatedAt, clock.now())
	}
}

func TestDefaultPath(t *testing.T) {
	tests := []struct {
		name          string
		envDB         string
		xdgConfigHome string
		home          string
		want          string
		wantErr       error
	}{
		{
			name:          "env wins over everything",
			envDB:         "/tmp/explicit.db",
			xdgConfigHome: "/xdg",
			home:          "/home/chrispian",
			want:          "/tmp/explicit.db",
		},
		{
			name:          "xdg wins over home",
			xdgConfigHome: "/xdg",
			home:          "/home/chrispian",
			want:          filepath.Join("/xdg", DirName, FileName),
		},
		{
			name: "home is the fallback",
			home: "/home/chrispian",
			want: filepath.Join("/home/chrispian", ".config", DirName, FileName),
		},
		{
			name:  "whitespace is not a value",
			envDB: "   ",
			home:  "/home/chrispian",
			want:  filepath.Join("/home/chrispian", ".config", DirName, FileName),
		},
		{
			name:    "no home at all",
			wantErr: ErrNoHome,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DefaultPath(tt.envDB, tt.xdgConfigHome, tt.home)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("DefaultPath error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DefaultPath: %v", err)
			}
			if got != tt.want {
				t.Fatalf("DefaultPath = %q, want %q", got, tt.want)
			}
		})
	}
}
