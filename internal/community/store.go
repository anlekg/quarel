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
	// P1 (messages, channels, moderation, voice). New permissions for @everyone:
	// add_reactions (1<<13) | attach_files (1<<14) | stream (1<<12) = 28672.
	// Thread and announcement channels are flags on text channels: the type
	// CHECK constraint cannot change without rebuilding the table, which would
	// cascade-delete messages while foreign keys are on.
	`
UPDATE roles SET permissions = permissions | 28672 WHERE id = 1;
ALTER TABLE roles ADD COLUMN hoist INTEGER NOT NULL DEFAULT 0;

ALTER TABLE channels ADD COLUMN announcement INTEGER NOT NULL DEFAULT 0;
ALTER TABLE channels ADD COLUMN thread INTEGER NOT NULL DEFAULT 0;      -- parent_id is then the text channel
ALTER TABLE channels ADD COLUMN thread_starter INTEGER;                 -- message the thread started from
CREATE UNIQUE INDEX channels_thread_starter ON channels (thread_starter) WHERE thread_starter IS NOT NULL;

ALTER TABLE messages ADD COLUMN reply_to INTEGER;
ALTER TABLE messages ADD COLUMN pinned_at INTEGER;
ALTER TABLE messages ADD COLUMN pinned_by TEXT;

CREATE TABLE reactions (
	message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	member_id  TEXT NOT NULL REFERENCES members(id),
	emoji      TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (message_id, member_id, emoji)
);

-- Files on disk under data/attachments/<id>; message_id is NULL until the message is sent.
CREATE TABLE attachments (
	id           TEXT PRIMARY KEY,
	message_id   INTEGER REFERENCES messages(id) ON DELETE CASCADE,
	channel_id   INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	uploader_id  TEXT NOT NULL REFERENCES members(id),
	filename     TEXT NOT NULL,
	content_type TEXT NOT NULL,
	size         INTEGER NOT NULL,
	created_at   INTEGER NOT NULL
);
CREATE INDEX attachments_message ON attachments(message_id);

CREATE TABLE link_previews (
	message_id  INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	url         TEXT NOT NULL,
	title       TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	site_name   TEXT NOT NULL DEFAULT '',
	image_url   TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (message_id, url)
);

CREATE TABLE read_states (
	member_id  TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
	channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	last_read  INTEGER NOT NULL,
	PRIMARY KEY (member_id, channel_id)
);

-- channel_id 0 = the member's default for the whole server.
CREATE TABLE notification_settings (
	member_id   TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
	channel_id  INTEGER NOT NULL,
	level       TEXT NOT NULL CHECK (level IN ('default', 'all', 'mentions', 'none')),
	muted_until INTEGER,
	PRIMARY KEY (member_id, channel_id)
);

-- Full-text search (accent-insensitive), kept in sync by triggers.
CREATE VIRTUAL TABLE messages_fts USING fts5(content, content='messages', content_rowid='id', tokenize='unicode61 remove_diacritics 2');
INSERT INTO messages_fts(messages_fts) VALUES ('rebuild');
CREATE TRIGGER messages_fts_insert AFTER INSERT ON messages BEGIN
	INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER messages_fts_delete AFTER DELETE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.id, old.content);
END;
CREATE TRIGGER messages_fts_update AFTER UPDATE OF content ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.id, old.content);
	INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;

-- Moderation.
ALTER TABLE members ADD COLUMN timeout_until INTEGER;
CREATE TABLE audit_log (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	actor_id   TEXT,
	action     TEXT NOT NULL,
	target_id  TEXT,
	reason     TEXT NOT NULL DEFAULT '',
	details    TEXT NOT NULL DEFAULT '{}',
	created_at INTEGER NOT NULL
);
`,
	// P1 block 2: rules screen, phone verification, bots, audit log index.
	// phone_hash is a keyed hash of the number (never the number itself); it
	// stays on the member row after a ban so the same phone cannot come back.
	`
ALTER TABLE members ADD COLUMN rules_accepted_at INTEGER;
ALTER TABLE members ADD COLUMN phone_hash TEXT;
CREATE UNIQUE INDEX members_phone ON members (phone_hash) WHERE phone_hash IS NOT NULL;
ALTER TABLE members ADD COLUMN bot INTEGER NOT NULL DEFAULT 0;
CREATE TABLE bot_tokens (
	member_id  TEXT PRIMARY KEY REFERENCES members(id) ON DELETE CASCADE,
	token_hash TEXT NOT NULL UNIQUE,
	created_at INTEGER NOT NULL
);
CREATE INDEX audit_log_created ON audit_log (created_at);
`,
	// P1 block 3: server mute and deafen, kept across voice sessions.
	`
ALTER TABLE members ADD COLUMN voice_mute INTEGER NOT NULL DEFAULT 0;
ALTER TABLE members ADD COLUMN voice_deaf INTEGER NOT NULL DEFAULT 0;
`,
	// 6 (audit, 2026-09-26): the device key of each session, so that a
	// session its Identity service ended (logout, revoked device, password
	// change…) ends here too.
	`
ALTER TABLE sessions ADD COLUMN device_key TEXT;
`,
	// 7 (P2, 2026-09-27): incoming webhooks; each posts as its own hidden member.
	`
CREATE TABLE webhooks (
	id         TEXT PRIMARY KEY,
	channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	member_id  TEXT NOT NULL REFERENCES members(id),
	name       TEXT NOT NULL,
	token_hash TEXT NOT NULL UNIQUE,
	created_by TEXT REFERENCES members(id) ON DELETE SET NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX webhooks_channel ON webhooks (channel_id);
`,
}

// SchemaVersion is the database version this program creates and understands.
func SchemaVersion() int { return len(migrations) }

// OpenDB opens (creating if needed) the SQLite database at path and applies migrations.
func OpenDB(path string) (*sql.DB, error) { return sqlitedb.Open(path, migrations) }
