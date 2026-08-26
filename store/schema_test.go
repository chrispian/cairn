package store

import (
	"testing"
)

func TestSchemaCreatesEveryTable(t *testing.T) {
	s, _ := openStore(t)

	rows, err := s.DB().QueryContext(t.Context(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sqlite_master: %v", err)
	}

	want := []string{"bindings", "profiles", "scopes"}
	if len(got) != len(want) {
		t.Fatalf("tables = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tables = %v, want %v", got, want)
		}
	}
}

func TestSchemaColumns(t *testing.T) {
	s, _ := openStore(t)

	tables := map[string][]string{
		"profiles": {"id", "extends", "abstract", "name", "description", "provider", "model", "body", "spec", "created_at", "updated_at"},
		"bindings": {"name", "profile_id", "scope"},
		"scopes":   {"alias", "path"},
	}

	for table, want := range tables {
		t.Run(table, func(t *testing.T) {
			rows, err := s.DB().QueryContext(t.Context(), `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
			if err != nil {
				t.Fatalf("pragma_table_info(%q): %v", table, err)
			}
			defer func() { _ = rows.Close() }()

			var got []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Fatalf("scan column name: %v", err)
				}
				got = append(got, name)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterate columns: %v", err)
			}
			if len(got) != len(want) {
				t.Fatalf("columns = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("columns = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	s, _ := openStore(t)

	if err := s.PutScope(t.Context(), Scope{Alias: "cairn", Path: "/cairn"}); err != nil {
		t.Fatalf("PutScope: %v", err)
	}
	// Applying the schema again is what a second Open does, and it must not
	// disturb what is already stored.
	if err := s.migrate(t.Context()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	got, err := s.Scope(t.Context(), "cairn")
	if err != nil {
		t.Fatalf("Scope after re-migrate: %v", err)
	}
	if want := "/cairn"; got != want {
		t.Fatalf("Scope after re-migrate = %q, want %q", got, want)
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	s, _ := openStore(t)

	// The bindings foreign key is the schema's only referential rule, and it
	// is on: the DSN sets foreign_keys(1).
	_, err := s.DB().ExecContext(t.Context(),
		`INSERT INTO bindings (name, profile_id, scope) VALUES ('eng', 'nobody', '')`)
	if err == nil {
		t.Fatal("inserted a binding pointing at a profile that does not exist")
	}

	if err := s.PutProfile(t.Context(), profileFixture("engineer")); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}
	if err := s.PutBinding(t.Context(), Binding{Name: "eng", ProfileID: "engineer"}); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}
	if err := s.DeleteProfile(t.Context(), "engineer"); err == nil {
		t.Fatal("deleted a profile a binding still points at")
	}
}
