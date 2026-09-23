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
	// Milestone 3: roles, permissions, channel overrides, bans.
	// Role 1 is @everyone; 3091 = view_channel | send_messages | create_invite | connect | speak.
	`
CREATE TABLE roles (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL,
	color       INTEGER NOT NULL DEFAULT 0,
	position    INTEGER NOT NULL,
	permissions INTEGER NOT NULL DEFAULT 0,
	mentionable INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL
);
INSERT INTO roles (id, name, position, permissions, created_at) VALUES (1, '@everyone', 0, 3091, 0);

CREATE TABLE member_roles (
	member_id TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
	role_id   INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
	PRIMARY KEY (member_id, role_id)
);

-- target_id is a role ID (as text) or a member ID; no foreign key, cleaned up by the code.
CREATE TABLE channel_overrides (
	channel_id  INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	target_type TEXT NOT NULL CHECK (target_type IN ('role', 'member')),
	target_id   TEXT NOT NULL,
	allow       INTEGER NOT NULL DEFAULT 0,
	deny        INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (channel_id, target_type, target_id)
);

-- Bans target the member row, i.e. the portable identity (issuer, subject).
CREATE TABLE bans (
	member_id  TEXT PRIMARY KEY REFERENCES members(id),
	reason     TEXT NOT NULL DEFAULT '',
	banned_by  TEXT REFERENCES members(id),
	created_at INTEGER NOT NULL
);

CREATE TABLE message_role_mentions (
	message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	role_id    INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
	PRIMARY KEY (message_id, role_id)
);
`,
}

// OpenDB opens (creating if needed) the SQLite database at path and applies migrations.
func OpenDB(path string) (*sql.DB, error) { return sqlitedb.Open(path, migrations) }
