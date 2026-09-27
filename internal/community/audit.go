package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The audit log records moderation and configuration actions: who did what
// to whom, and why. Readable with view_audit_log; entries older than
// auditRetention are deleted.

const auditRetention = 90 * 24 * time.Hour

// Audit actions (stable API values).
const (
	auditMemberKick       = "member_kick"
	auditMemberBan        = "member_ban"
	auditMemberUnban      = "member_unban"
	auditMemberTimeout    = "member_timeout"
	auditMemberTimeoutEnd = "member_timeout_remove"
	auditMemberRoleAdd    = "member_role_add"
	auditMemberRoleRemove = "member_role_remove"
	auditMessagesDelete   = "messages_delete" // someone else's messages (one, bulk or purge)
	auditRoleCreate       = "role_create"
	auditRoleUpdate       = "role_update"
	auditRoleDelete       = "role_delete"
	auditChannelCreate    = "channel_create"
	auditChannelUpdate    = "channel_update"
	auditChannelDelete    = "channel_delete"
	auditOverrideUpdate   = "override_update"
	auditOverrideDelete   = "override_delete"
	auditServerUpdate     = "server_update"
	auditOwnerTransfer    = "owner_transfer"
	auditOwnerReset       = "owner_reset"   // by the host, from the administration page
	auditInviteDelete     = "invite_delete" // someone else's invite
	auditBotCreate        = "bot_create"
	auditBotDelete        = "bot_delete"
	auditBotTokenReset    = "bot_token_reset"
	auditVoiceMute        = "voice_mute"
	auditVoiceDeafen      = "voice_deafen"
	auditVoiceMove        = "voice_move"
	auditVoiceDisconnect  = "voice_disconnect"
	auditVoiceSpeaker     = "voice_speaker"
	auditEmojiCreate      = "emoji_create"
	auditEmojiDelete      = "emoji_delete"
)

type auditEntry struct {
	ID        int64          `json:"id"`
	ActorID   *string        `json:"actor_id"`
	Action    string         `json:"action"`
	TargetID  *string        `json:"target_id"` // member, role, channel or bot ID depending on the action
	Reason    string         `json:"reason"`
	Details   map[string]any `json:"details"`
	CreatedAt time.Time      `json:"created_at"`
	// Names of the members involved, even if they left since (for display).
	ActorName  string `json:"actor_name,omitempty"`
	TargetName string `json:"target_name,omitempty"`
}

// audit records an action. q may be a transaction (never s.db inside one).
// Failures are logged, not returned: the action itself already happened.
func (s *Server) audit(ctx context.Context, q querier, actor, action, target, reason string, details map[string]any) {
	if action == "" {
		return
	}
	if details == nil {
		details = map[string]any{}
	}
	d, _ := json.Marshal(details)
	var actorArg, targetArg any // NULL actor: the host (administration page)
	if actor != "" {
		actorArg = actor
	}
	if target != "" {
		targetArg = target
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO audit_log (actor_id, action, target_id, reason, details, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		actorArg, action, targetArg, reason, string(d), s.nowMs()); err != nil {
		s.logErr("writing the audit log", err)
	}
}

// handleAuditLog: GET /v1/audit-log?limit=&before=&action=&actor_id=&target_id=, newest first.
func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requirePerm(r, permViewAuditLog); err != nil {
		writeErr(w, r, err)
		return
	}
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_limit", "limit must be 1-100"))
			return
		}
		limit = n
	}
	where, args := `1`, []any{}
	if v := q.Get("before"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_before", "before must be an entry ID"))
			return
		}
		where, args = where+` AND id < ?`, append(args, id)
	}
	for _, f := range []struct{ param, col string }{{"action", "action"}, {"actor_id", "actor_id"}, {"target_id", "target_id"}} {
		if v := q.Get(f.param); v != "" {
			where, args = where+` AND `+f.col+` = ?`, append(args, v)
		}
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, actor_id, action, target_id, reason, details, created_at FROM audit_log
		WHERE `+where+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []auditEntry{}
	for rows.Next() {
		var e auditEntry
		var actor, target sql.NullString
		var details string
		var created int64
		if err := rows.Scan(&e.ID, &actor, &e.Action, &target, &e.Reason, &details, &created); err != nil {
			writeErr(w, r, err)
			return
		}
		if actor.Valid {
			e.ActorID = &actor.String
		}
		if target.Valid {
			e.TargetID = &target.String
		}
		json.Unmarshal([]byte(details), &e.Details)
		e.CreatedAt = fromMs(created)
		list = append(list, e)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, r, err)
		return
	}
	rows.Close()
	names := map[string]string{}
	name := func(id *string) string {
		if id == nil {
			return ""
		}
		if n, ok := names[*id]; ok {
			return n
		}
		var handle string
		var nick sql.NullString
		if s.db.QueryRowContext(r.Context(), `SELECT handle, nickname FROM members WHERE id = ?`, *id).Scan(&handle, &nick) == nil {
			names[*id], _, _ = strings.Cut(handle, "@")
			if handle == "" {
				names[*id] = deletedName
			}
			if nick.Valid && nick.String != "" {
				names[*id] = nick.String
			}
		} else {
			names[*id] = ""
		}
		return names[*id]
	}
	for i := range list {
		list[i].ActorName = name(list[i].ActorID)
		list[i].TargetName = name(list[i].TargetID) // "" when the target is not a member (role, channel…)
	}
	writeJSON(w, http.StatusOK, list)
}

// Housekeeping runs the periodic cleanups: unsent attachments, orphan files,
// expired audit entries and sessions.
func (s *Server) Housekeeping(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE created_at < ?`, s.now().Add(-auditRetention).UnixMilli()); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.nowMs()); err != nil {
		return err
	}
	if err := s.ArchiveThreads(ctx); err != nil {
		return err
	}
	return s.CleanupAttachments(ctx)
}
