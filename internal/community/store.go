package community

import (
	"database/sql"

	"github.com/anlekg/quarel/internal/sqlitedb"
)

// migrations: never edit an existing entry, only append. Timestamps are Unix milliseconds.
var migrations = []string{
	`
CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

-- A member is identified by (issuer, subject): the portable identity.
-- Rows are kept when a member leaves (left_at) so their messages keep an author.
CREATE TABLE members (
	id        TEXT PRIMARY KEY,
	issuer    TEXT NOT NULL,
	subject   TEXT NOT NULL,
	handle    TEXT NOT NULL,
	nickname  TEXT,
	is_owner  INTEGER NOT NULL DEFAULT 0,
	joined_at INTEGER NOT NULL,
	left_at   INTEGER,
	UNIQUE (issuer, subject)
);

CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	member_id  TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_member ON sessions(member_id);

CREATE TABLE invites (
	code       TEXT PRIMARY KEY,
	creator_id TEXT REFERENCES members(id) ON DELETE SET NULL,
	max_uses   INTEGER,          -- NULL: unlimited
	uses       INTEGER NOT NULL DEFAULT 0,
	expires_at INTEGER,          -- NULL: never
	created_at INTEGER NOT NULL
);

CREATE TABLE channels (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	type       TEXT NOT NULL CHECK (type IN ('text', 'voice', 'category')),
	name       TEXT NOT NULL,
	topic      TEXT NOT NULL DEFAULT '',
	parent_id  INTEGER REFERENCES channels(id) ON DELETE SET NULL,
	position   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE messages (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	channel_id       INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	author_id        TEXT NOT NULL REFERENCES members(id),
	content          TEXT NOT NULL,
	mention_everyone INTEGER NOT NULL DEFAULT 0,
	created_at       INTEGER NOT NULL,
	edited_at        INTEGER
);
CREATE INDEX messages_channel ON messages(channel_id, id);

CREATE TABLE message_mentions (
	message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	member_id  TEXT NOT NULL REFERENCES members(id),
	PRIMARY KEY (message_id, member_id)
);
`,
}

// OpenDB opens (creating if needed) the SQLite database at path and applies migrations.
func OpenDB(path string) (*sql.DB, error) { return sqlitedb.Open(path, migrations) }
