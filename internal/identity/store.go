package identity

import (
	"database/sql"

	"github.com/anlekg/quarel/internal/sqlitedb"
)

// migrations are applied in order; the index+1 is stored in PRAGMA user_version.
// Never edit an existing entry, only append.
var migrations = []string{
	`
CREATE TABLE users (
	id                TEXT PRIMARY KEY,
	email             TEXT NOT NULL UNIQUE,
	pseudo            TEXT NOT NULL,
	pseudo_norm       TEXT NOT NULL UNIQUE,
	password_hash     TEXT NOT NULL,
	email_verified_at INTEGER,
	totp_secret       TEXT,
	totp_enabled_at   INTEGER,
	totp_last_step    INTEGER NOT NULL DEFAULT 0,
	disabled_at       INTEGER,
	created_at        INTEGER NOT NULL
);

CREATE TABLE email_codes (
	user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	code_hash  TEXT NOT NULL,
	attempts   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);

CREATE TABLE sessions (
	id           TEXT PRIMARY KEY,
	user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	token_hash   TEXT NOT NULL UNIQUE,
	device_name  TEXT NOT NULL,
	device_key   TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE backup_codes (
	user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	code_hash TEXT NOT NULL,
	used_at   INTEGER,
	PRIMARY KEY (user_id, code_hash)
);
`,
	`
CREATE TABLE auth_failures (
	key TEXT NOT NULL,
	at  INTEGER NOT NULL
);
CREATE INDEX auth_failures_key_at ON auth_failures(key, at);
`,
}

// OpenDB opens (creating if needed) the SQLite database at path and applies migrations.
func OpenDB(path string) (*sql.DB, error) { return sqlitedb.Open(path, migrations) }
