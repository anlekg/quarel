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
	// Milestone 5: friends, end-to-end encryption keys, direct messages.
	// The server only stores public keys and opaque ciphertexts.
	`
-- One row per pair, user_a < user_b.
CREATE TABLE friendships (
	user_a     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	user_b     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	status     TEXT NOT NULL CHECK (status IN ('pending', 'accepted')),
	requester  TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (user_a, user_b)
);
CREATE INDEX friendships_b ON friendships(user_b);

-- The user's master signing key (Ed25519, public part): it signs verified devices.
CREATE TABLE master_keys (
	user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	ed25519    TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

-- Olm identity keys of each device (a device is a session).
CREATE TABLE device_keys (
	session_id       TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
	user_id          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	curve25519       TEXT NOT NULL,
	ed25519          TEXT NOT NULL,
	signature        TEXT NOT NULL, -- by the device's ed25519 key
	master_signature TEXT,          -- by the master key: the device is verified
	created_at       INTEGER NOT NULL
);
CREATE INDEX device_keys_user ON device_keys(user_id);

CREATE TABLE one_time_keys (
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	key_id     TEXT NOT NULL,
	key        TEXT NOT NULL,
	signature  TEXT NOT NULL,
	fallback   INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (session_id, key_id)
);

CREATE TABLE dms (
	id         TEXT PRIMARY KEY,
	user_a     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	user_b     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	UNIQUE (user_a, user_b)
);

-- A sent DM, kept only until every recipient device has acknowledged it.
CREATE TABLE dm_events (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	dm_id         TEXT NOT NULL REFERENCES dms(id) ON DELETE CASCADE,
	sender_user   TEXT NOT NULL,
	sender_device TEXT NOT NULL,
	created_at    INTEGER NOT NULL
);

-- Mailbox: one row per recipient device, deleted when that device acknowledges it.
CREATE TABLE inbox (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	kind          TEXT NOT NULL CHECK (kind IN ('to_device', 'dm', 'receipt')),
	sender_user   TEXT NOT NULL,
	sender_device TEXT NOT NULL,
	dm_id         TEXT,
	event_id      INTEGER,
	payload       TEXT NOT NULL,
	created_at    INTEGER NOT NULL
);
CREATE INDEX inbox_session ON inbox(session_id, id);
CREATE INDEX inbox_event ON inbox(event_id);
`,
}

// OpenDB opens (creating if needed) the SQLite database at path and applies migrations.
func OpenDB(path string) (*sql.DB, error) { return sqlitedb.Open(path, migrations) }
