package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chrispian/cairn/profile"
	"github.com/hollis-labs/go-sqlite/txutil"
)

// profileColumns is the profiles table in its stored order. Every read selects
// it and [scanProfile] consumes it, so the two cannot drift apart.
const profileColumns = `id, extends, abstract, name, description, provider, model, body, spec, created_at, updated_at`

// emptySpec is what an absent manifest is stored as. The column is NOT NULL
// and a profile that declares nothing still has a manifest — an empty one.
const emptySpec = `{}`

// Profile returns the profile stored under id, or an error wrapping
// [ErrProfileNotFound] when no such row exists.
func (s *Store) Profile(ctx context.Context, id string) (*profile.Profile, error) {
	if err := checkKey("profile id", id); err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM profiles WHERE id = ?`, id)
	p, err := scanProfile(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("load profile %q: %w", id, ErrProfileNotFound)
	case err != nil:
		return nil, fmt.Errorf("load profile %q: %w", id, err)
	}
	return &p, nil
}

// Profiles returns every stored profile, ordered by id.
func (s *Store) Profiles(ctx context.Context) ([]profile.Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+profileColumns+` FROM profiles ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []profile.Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("list profiles: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	return out, nil
}

// PutProfile writes p, inserting it when its id is new and updating every
// column but created_at when it is not.
//
// A row's created_at survives an update, and updated_at is refreshed on every
// write. On an insert, a non-zero CreatedAt is kept as given so an import can
// carry the history it arrived with; a zero one is stamped from the store's
// clock.
func (s *Store) PutProfile(ctx context.Context, p profile.Profile) error {
	if err := checkKey("profile id", p.ID); err != nil {
		return err
	}
	spec, err := encodeSpec(p.Spec)
	if err != nil {
		return fmt.Errorf("put profile %q: %w", p.ID, err)
	}

	now := s.now().UTC()
	created := now
	if !p.CreatedAt.IsZero() {
		created = p.CreatedAt.UTC()
	}

	// An upsert rather than INSERT OR REPLACE: REPLACE deletes the existing
	// row first, which trips the bindings foreign key on any profile an
	// operator has already bound a name to.
	const stmt = `
INSERT INTO profiles (` + profileColumns + `)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  extends     = excluded.extends,
  abstract    = excluded.abstract,
  name        = excluded.name,
  description = excluded.description,
  provider    = excluded.provider,
  model       = excluded.model,
  body        = excluded.body,
  spec        = excluded.spec,
  updated_at  = excluded.updated_at`

	err = txutil.WithImmediate(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, stmt,
			p.ID,
			p.Extends,
			boolToInt(p.Abstract),
			p.Name,
			p.Description,
			p.Provider.String(),
			p.Model,
			p.Body,
			spec,
			created.Format(TimeLayout),
			now.Format(TimeLayout),
		)
		return err
	})
	if err != nil {
		return fmt.Errorf("put profile %q: %w", p.ID, err)
	}
	return nil
}

// DeleteProfile removes the profile stored under id. It returns an error
// wrapping [ErrProfileNotFound] when there was no such row.
func (s *Store) DeleteProfile(ctx context.Context, id string) error {
	if err := checkKey("profile id", id); err != nil {
		return err
	}
	err := txutil.WithImmediate(ctx, s.db, func(tx *sql.Tx) error {
		return deleteOne(ctx, tx, `DELETE FROM profiles WHERE id = ?`, id, ErrProfileNotFound)
	})
	if err != nil {
		return fmt.Errorf("delete profile %q: %w", id, err)
	}
	return nil
}

// Binding returns the binding stored under name, or an error wrapping
// [ErrBindingNotFound] when no such row exists.
func (s *Store) Binding(ctx context.Context, name string) (*Binding, error) {
	if err := checkKey("binding name", name); err != nil {
		return nil, err
	}
	var b Binding
	err := s.db.
		QueryRowContext(ctx, `SELECT name, profile_id, scope FROM bindings WHERE name = ?`, name).
		Scan(&b.Name, &b.ProfileID, &b.Scope)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("load binding %q: %w", name, ErrBindingNotFound)
	case err != nil:
		return nil, fmt.Errorf("load binding %q: %w", name, err)
	}
	return &b, nil
}

// Bindings returns every stored binding, ordered by name.
func (s *Store) Bindings(ctx context.Context) ([]Binding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, profile_id, scope FROM bindings ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Binding
	for rows.Next() {
		var b Binding
		if err := rows.Scan(&b.Name, &b.ProfileID, &b.Scope); err != nil {
			return nil, fmt.Errorf("list bindings: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list bindings: %w", err)
	}
	return out, nil
}

// PutBinding writes b, inserting it when its name is new and replacing the
// profile and scope it points at when it is not.
//
// A binding naming a profile that does not exist is refused by the schema's
// foreign key. That is the only referential check; nothing here re-implements
// it.
func (s *Store) PutBinding(ctx context.Context, b Binding) error {
	if err := checkKey("binding name", b.Name); err != nil {
		return err
	}
	const stmt = `
INSERT INTO bindings (name, profile_id, scope)
VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
  profile_id = excluded.profile_id,
  scope      = excluded.scope`

	err := txutil.WithImmediate(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, stmt, b.Name, b.ProfileID, b.Scope)
		return err
	})
	if err != nil {
		return fmt.Errorf("put binding %q: %w", b.Name, err)
	}
	return nil
}

// DeleteBinding removes the binding stored under name. It returns an error
// wrapping [ErrBindingNotFound] when there was no such row.
func (s *Store) DeleteBinding(ctx context.Context, name string) error {
	if err := checkKey("binding name", name); err != nil {
		return err
	}
	err := txutil.WithImmediate(ctx, s.db, func(tx *sql.Tx) error {
		return deleteOne(ctx, tx, `DELETE FROM bindings WHERE name = ?`, name, ErrBindingNotFound)
	})
	if err != nil {
		return fmt.Errorf("delete binding %q: %w", name, err)
	}
	return nil
}

// Scope returns the directory path alias names, or an error wrapping
// [ErrScopeNotFound] when no such row exists.
func (s *Store) Scope(ctx context.Context, alias string) (string, error) {
	if err := checkKey("scope alias", alias); err != nil {
		return "", err
	}
	var path string
	err := s.db.
		QueryRowContext(ctx, `SELECT path FROM scopes WHERE alias = ?`, alias).
		Scan(&path)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("load scope %q: %w", alias, ErrScopeNotFound)
	case err != nil:
		return "", fmt.Errorf("load scope %q: %w", alias, err)
	}
	return path, nil
}

// Scopes returns every stored scope alias, ordered by alias.
func (s *Store) Scopes(ctx context.Context) ([]Scope, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT alias, path FROM scopes ORDER BY alias`)
	if err != nil {
		return nil, fmt.Errorf("list scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Scope
	for rows.Next() {
		var sc Scope
		if err := rows.Scan(&sc.Alias, &sc.Path); err != nil {
			return nil, fmt.Errorf("list scopes: %w", err)
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list scopes: %w", err)
	}
	return out, nil
}

// PutScope writes sc, inserting it when its alias is new and replacing the
// path when it is not.
func (s *Store) PutScope(ctx context.Context, sc Scope) error {
	if err := checkKey("scope alias", sc.Alias); err != nil {
		return err
	}
	const stmt = `
INSERT INTO scopes (alias, path)
VALUES (?, ?)
ON CONFLICT(alias) DO UPDATE SET path = excluded.path`

	err := txutil.WithImmediate(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, stmt, sc.Alias, sc.Path)
		return err
	})
	if err != nil {
		return fmt.Errorf("put scope %q: %w", sc.Alias, err)
	}
	return nil
}

// DeleteScope removes the scope stored under alias. It returns an error
// wrapping [ErrScopeNotFound] when there was no such row.
func (s *Store) DeleteScope(ctx context.Context, alias string) error {
	if err := checkKey("scope alias", alias); err != nil {
		return err
	}
	err := txutil.WithImmediate(ctx, s.db, func(tx *sql.Tx) error {
		return deleteOne(ctx, tx, `DELETE FROM scopes WHERE alias = ?`, alias, ErrScopeNotFound)
	})
	if err != nil {
		return fmt.Errorf("delete scope %q: %w", alias, err)
	}
	return nil
}

// deleteOne runs a single-key delete and reports missing when it removed
// nothing.
func deleteOne(ctx context.Context, tx *sql.Tx, stmt, key string, missing error) error {
	res, err := tx.ExecContext(ctx, stmt, key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return missing
	}
	return nil
}

// checkKey rejects a primary key that is empty or is only whitespace. It is
// the whole of Cairn's key validation: what else a key may contain is the
// operator's business.
func checkKey(what, key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%s %q: %w", what, key, ErrInvalidKey)
	}
	return nil
}

// rowScanner is the part of *sql.Row and *sql.Rows [scanProfile] needs, so one
// scan serves both the single-row read and the listing.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanProfile reads one profiles row in [profileColumns] order.
func scanProfile(sc rowScanner) (profile.Profile, error) {
	var (
		p        profile.Profile
		abstract int64
		provider string
		spec     string
		created  string
		updated  string
	)
	err := sc.Scan(
		&p.ID,
		&p.Extends,
		&abstract,
		&p.Name,
		&p.Description,
		&provider,
		&p.Model,
		&p.Body,
		&spec,
		&created,
		&updated,
	)
	if err != nil {
		return profile.Profile{}, err
	}

	p.Abstract = abstract != 0
	p.Provider = profile.Provider(provider)

	if p.Spec, err = decodeSpec(spec); err != nil {
		return profile.Profile{}, fmt.Errorf("profile %q: %w", p.ID, err)
	}
	if p.CreatedAt, err = parseTime(created); err != nil {
		return profile.Profile{}, fmt.Errorf("profile %q created_at: %w", p.ID, err)
	}
	if p.UpdatedAt, err = parseTime(updated); err != nil {
		return profile.Profile{}, fmt.Errorf("profile %q updated_at: %w", p.ID, err)
	}
	return p, nil
}

// encodeSpec renders a manifest as the text of the spec column.
//
// The object is assembled by hand, key by key, rather than handed to
// [json.Marshal]: marshalling compacts and HTML-escapes what a
// [json.RawMessage] hands back, and a key Cairn does not know has to come back
// out byte for byte as it went in. Keys are written in sorted order so the
// same manifest always stores the same text.
func encodeSpec(spec profile.Spec) (string, error) {
	if len(spec) == 0 {
		return emptySpec, nil
	}

	keys := make([]string, 0, len(spec))
	for k := range spec {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		value := bytes.TrimSpace(spec[k])
		if len(value) == 0 {
			value = []byte("null")
		}
		if !json.Valid(value) {
			return "", fmt.Errorf("spec key %q: value is not JSON", k)
		}
		name, err := json.Marshal(k)
		if err != nil {
			return "", fmt.Errorf("spec key %q: %w", k, err)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(name)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.String(), nil
}

// decodeSpec reads the spec column. Empty text and an empty object both decode
// to an empty manifest that is not nil, and every key is carried through
// whether or not Cairn renders it.
func decodeSpec(text string) (profile.Spec, error) {
	spec := profile.Spec{}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || trimmed == emptySpec {
		return spec, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &spec); err != nil {
		return nil, fmt.Errorf("decode spec: %w", err)
	}
	if spec == nil {
		// The column held JSON null, which unmarshals a map to nil.
		spec = profile.Spec{}
	}
	return spec, nil
}

// parseTime reads a timestamp column. Empty text is the zero time rather than
// an error: an operator writing rows by hand is not obliged to stamp them.
func parseTime(text string) (time.Time, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return time.Time{}, nil
	}
	return time.Parse(TimeLayout, trimmed)
}

// boolToInt renders a Go bool for an INTEGER column.
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
