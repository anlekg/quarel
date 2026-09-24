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
	// Milestone 6: encrypted account backups (opened only with the recovery phrase).
	`
CREATE TABLE backups (
	user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	version    INTEGER NOT NULL,
	data       TEXT NOT NULL, -- base64 ciphertext, opaque to the server
	updated_at INTEGER NOT NULL
);
`,
	// P1 block 4: account management, public profile, blocks, presence,
	// administration log.
	`
-- One pending code per user and purpose: 'reset' (forgotten password) or
-- 'email' (address change; data = the new address).
CREATE TABLE account_codes (
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	purpose    TEXT NOT NULL CHECK (purpose IN ('reset', 'email')),
	code_hash  TEXT NOT NULL,
	data       TEXT NOT NULL DEFAULT '',
	attempts   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, purpose)
);

ALTER TABLE users ADD COLUMN bio TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN pseudo_changed_at INTEGER;
ALTER TABLE users ADD COLUMN presence TEXT NOT NULL DEFAULT 'online' CHECK (presence IN ('online', 'idle', 'dnd', 'invisible'));

CREATE TABLE avatars (
	user_id      TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	content_type TEXT NOT NULL,
	data         BLOB NOT NULL,
	updated_at   INTEGER NOT NULL
);

CREATE TABLE blocks (
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	blocked_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, blocked_id)
);
CREATE INDEX blocks_blocked ON blocks(blocked_id);

-- Operator actions (account disable/enable, key rotation). No foreign key:
-- the record outlives the account.
CREATE TABLE admin_log (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	action     TEXT NOT NULL,
	user_id    TEXT,
	reason     TEXT NOT NULL DEFAULT '',
	operator   TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
`,
	// P1 block 5: group conversations. The 1-to-1 "dms" become conversations
	// with members (same ids); their events keep their ids so pending
	// delivery receipts still work.
	`
CREATE TABLE conversations (
	id         TEXT PRIMARY KEY,
	kind       TEXT NOT NULL CHECK (kind IN ('direct', 'group')),
	name       TEXT NOT NULL DEFAULT '',
	owner_id   TEXT,
	created_at INTEGER NOT NULL
);
CREATE TABLE conversation_members (
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	joined_at       INTEGER NOT NULL,
	PRIMARY KEY (conversation_id, user_id)
);
CREATE INDEX conversation_members_user ON conversation_members(user_id);
-- One direct conversation per pair of users (user_a < user_b).
CREATE TABLE direct_pairs (
	user_a          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	user_b          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	PRIMARY KEY (user_a, user_b)
);
CREATE TABLE conv_events (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	sender_user     TEXT NOT NULL,
	sender_device   TEXT NOT NULL,
	created_at      INTEGER NOT NULL
);

INSERT INTO conversations (id, kind, created_at) SELECT id, 'direct', created_at FROM dms;
INSERT INTO conversation_members (conversation_id, user_id, joined_at)
	SELECT id, user_a, created_at FROM dms UNION ALL SELECT id, user_b, created_at FROM dms;
INSERT INTO direct_pairs (user_a, user_b, conversation_id) SELECT user_a, user_b, id FROM dms;
INSERT INTO conv_events (id, conversation_id, sender_user, sender_device, created_at)
	SELECT id, dm_id, sender_user, sender_device, created_at FROM dm_events;
DROP TABLE dm_events;
DROP TABLE dms;

-- Encrypted files of conversations: ciphertext on disk, opaque to the server.
CREATE TABLE conv_attachments (
	id              TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	uploader_id     TEXT NOT NULL,
	size            INTEGER NOT NULL,
	created_at      INTEGER NOT NULL
);

-- Privacy settings.
ALTER TABLE users ADD COLUMN share_typing INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN share_read_receipts INTEGER NOT NULL DEFAULT 1;
`,
}

// SchemaVersion is the database version this program creates and understands.
func SchemaVersion() int { return len(migrations) }

// OpenDB opens (creating if needed) the SQLite database at path and applies migrations.
func OpenDB(path string) (*sql.DB, error) { return sqlitedb.Open(path, migrations) }
