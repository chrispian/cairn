package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hollis-labs/go-sqlite/txutil"
)

// Schema is the whole database. Every statement is idempotent, so applying it
// to an existing database is a no-op and applying it to an empty file creates
// it.
//
// There is no migration table and no version column. A schema this small is
// changed by adding another idempotent statement below; the day that stops
// being enough is the day it earns a migration table, and not before.
const Schema = `
CREATE TABLE IF NOT EXISTS profiles (
  id          TEXT PRIMARY KEY,
  extends     TEXT NOT NULL DEFAULT '',
  abstract    INTEGER NOT NULL DEFAULT 0,
  name        TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  provider    TEXT NOT NULL DEFAULT '',
  model       TEXT NOT NULL DEFAULT '',
  body        TEXT NOT NULL DEFAULT '',
  spec        TEXT NOT NULL DEFAULT '{}',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS bindings (
  name       TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  scope      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS scopes (
  alias TEXT PRIMARY KEY,
  path  TEXT NOT NULL
);
`

// migrate applies [Schema]. It runs in one immediate transaction so that two
// cairn processes opening a fresh database at the same moment cannot each
// half-create it.
func (s *Store) migrate(ctx context.Context) error {
	err := txutil.WithImmediate(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, Schema)
		return err
	})
	if err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
