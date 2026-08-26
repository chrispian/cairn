#!/usr/bin/env bash
# Seed a Cairn store with a small, realistic profile set.
#
#   ./examples/seed.sh [db-path]
#
# Cairn ships no profiles and has no authoring command yet, so profiles are
# rows. This script is the worked example of writing them by hand.
set -euo pipefail

DB="${1:-${CAIRN_DB:-$HOME/.config/agents/cairn.db}}"
CAIRN="${CAIRN:-cairn}"
NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Where your skill packages live: one directory per skill, each holding a
# SKILL.md. Cairn copies whole trees, so references/ and scripts come along.
SKILLS_DIR="${SKILLS_DIR:-$HOME/dev/chrispian/agent-setup/skills}"

mkdir -p "$(dirname "$DB")"

# The schema Cairn creates on first open. Applying it here means seed.sh can
# run before any cairn command has.
sqlite3 "$DB" <<'SQL'
CREATE TABLE IF NOT EXISTS profiles (
  id TEXT PRIMARY KEY, extends TEXT NOT NULL DEFAULT '', abstract INTEGER NOT NULL DEFAULT 0,
  name TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '', spec TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS bindings (
  name TEXT PRIMARY KEY, profile_id TEXT NOT NULL REFERENCES profiles(id),
  scope TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS scopes (alias TEXT PRIMARY KEY, path TEXT NOT NULL);
SQL

# ---------------------------------------------------------------------------
# base — abstract, the root of the cascade and the profile `install` renders.
#
# The body is IDENTITY AND POINTERS, not rules. Keep it short: every line here
# lands in every session's context whether or not it is relevant.
# ---------------------------------------------------------------------------
sqlite3 "$DB" <<SQL
INSERT OR REPLACE INTO profiles
  (id, extends, abstract, name, description, provider, model, body, spec, created_at, updated_at)
VALUES (
  'base', '', 1, 'Base', 'The floor every profile extends.', 'claude', '',
  'You are working with Chrispian on his machine.

Conventions live in the project you are scoped to — read its own AGENTS.md
before you change anything in it.',
  json('{
    "skills_dir": "$SKILLS_DIR",
    "settings": {
      "permissions": { "defaultMode": "acceptEdits" }
    }
  }'),
  '$NOW', '$NOW');
SQL

# ---------------------------------------------------------------------------
# engineer — a working profile. Slots are the interesting part: they resolve
# at boot, so boot.md carries a live snapshot rather than a stale paragraph.
# ---------------------------------------------------------------------------
sqlite3 "$DB" <<SQL
INSERT OR REPLACE INTO profiles
  (id, extends, abstract, name, description, provider, model, body, spec, created_at, updated_at)
VALUES (
  'engineer', 'base', 0, 'Engineer', 'Implements one task end to end.', 'claude', '',
  'You implement one task end to end.',
  json('{
    "skills": ["capture-decision"],
    "slots": [
      { "name": "git",
        "section": "## Repository",
        "source": { "kind": "cmd", "cmd": { "run": "git status --short --branch", "timeout": 5000000000 } } },
      { "name": "recent",
        "section": "## Recent commits",
        "source": { "kind": "cmd", "cmd": { "run": "git log --oneline -10", "timeout": 5000000000 } } }
    ],
    "mcp": [
      { "name": "mux", "command": "mux",
        "args": ["mcp", "--proxy", "--servers", "tesseract,torque"] }
    ]
  }'),
  '$NOW', '$NOW');
SQL

# Bindings name a profile plus a default scope. Scope may be an alias or a
# literal path; an alias is anything that does not look like a path.
sqlite3 "$DB" <<SQL
INSERT OR REPLACE INTO scopes (alias, path) VALUES
  ('cairn', '$HOME/dev/projects/cairn'),
  ('nanite', '$HOME/dev/hollis-labs/apps/nanite');
INSERT OR REPLACE INTO bindings (name, profile_id, scope) VALUES
  ('eng',        'engineer', 'cairn'),
  ('eng-nanite', 'engineer', 'nanite');
SQL

echo "seeded $DB"
sqlite3 -header -column "$DB" \
  "SELECT b.name AS binding, b.profile_id AS profile, COALESCE(s.path, b.scope) AS scope
     FROM bindings b LEFT JOIN scopes s ON s.alias = b.scope ORDER BY b.name;"
