package community

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/anlekg/quarel/internal/httpapi"
)

const (
	chanText         = "text"
	chanVoice        = "voice"
	chanCategory     = "category"
	chanAnnouncement = "announcement" // text channel where posting also needs manage_messages
	chanThread       = "thread"       // text channel attached to a message; parent_id is the text channel

	targetRole   = "role"
	targetMember = "member"
)

// override adjusts permissions in one channel (or category) for a role or a member.
type override struct {
	Type     string   `json:"type"` // role | member
	TargetID string   `json:"id"`   // role ID (as a string) or member ID
	Allow    []string `json:"allow"`
	Deny     []string `json:"deny"`
	allow    perm
	deny     perm
}

func (o override) roleID() int64 {
	id, _ := strconv.ParseInt(o.TargetID, 10, 64)
	return id
}

type channel struct {
	ID            int64      `json:"id"`
	Type          string     `json:"type"` // text | voice | category | announcement | thread
	Name          string     `json:"name"`
	Topic         string     `json:"topic"`
	ParentID      *int64     `json:"parent_id"`
	Position      int64      `json:"position"`
	ThreadStarter *int64     `json:"thread_starter,omitempty"` // threads: message they started from
	Overrides     []override `json:"overrides"`
}

// messaging reports whether the channel holds messages.
func (c *channel) messaging() bool {
	return c.Type == chanText || c.Type == chanAnnouncement || c.Type == chanThread
}

func scanChannel(sc interface{ Scan(...any) error }) (*channel, error) {
	c := channel{Overrides: []override{}}
	var parent, starter sql.NullInt64
	var announcement, thread bool
	if err := sc.Scan(&c.ID, &c.Type, &c.Name, &c.Topic, &parent, &c.Position, &announcement, &thread, &starter); err != nil {
		return nil, err
	}
	if parent.Valid {
		c.ParentID = &parent.Int64
	}
	if starter.Valid {
		c.ThreadStarter = &starter.Int64
	}
	switch {
	case thread:
		c.Type = chanThread
	case announcement:
		c.Type = chanAnnouncement
	}
	return &c, nil
}

const channelCols = `id, type, name, topic, parent_id, position, announcement, thread, thread_starter`

// loadOverrides attaches overrides to channels (all of them if channelID is 0).
func loadOverrides(ctx context.Context, q querier, byID map[int64]*channel, channelID int64) error {
	rows, err := q.QueryContext(ctx, `SELECT channel_id, target_type, target_id, allow, deny FROM channel_overrides
		WHERE ? = 0 OR channel_id = ? ORDER BY channel_id, target_type, target_id`, channelID, channelID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int64
		var o override
		if err := rows.Scan(&cid, &o.Type, &o.TargetID, &o.allow, &o.deny); err != nil {
			return err
		}
		o.Allow, o.Deny = o.allow.names(), o.deny.names()
		if c := byID[cid]; c != nil {
			c.Overrides = append(c.Overrides, o)
		}
	}
	return rows.Err()
}

func channelByID(ctx context.Context, q querier, id int64) (*channel, error) {
	c, err := scanChannel(q.QueryRowContext(ctx, `SELECT `+channelCols+` FROM channels WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(http.StatusNotFound, "not_found", "no such channel")
	}
	if err != nil {
		return nil, err
	}
	return c, loadOverrides(ctx, q, map[int64]*channel{c.ID: c}, c.ID)
}

// pathChannel loads the channel named by the {id} path segment, without permission checks.
func (s *Server) pathChannel(r *http.Request) (*channel, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, errf(http.StatusNotFound, "not_found", "no such channel")
	}
	return channelByID(r.Context(), s.db, id)
}

// allChannels returns every channel with its overrides, ordered by (position, id).
func allChannels(ctx context.Context, q querier) ([]*channel, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+channelCols+` FROM channels ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	list := []*channel{}
	byID := map[int64]*channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, c)
		byID[c.ID] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, loadOverrides(ctx, q, byID, 0)
}

// handleListChannels returns the channels the member can see, as a flat list
// ordered by (position, id); clients build the tree from parent_id.
func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	ps, err := s.loadPerms(r.Context(), s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ps.visibleChannels(memberFrom(r).ID))
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return "", errf(http.StatusBadRequest, "invalid_name", "channel name must be 1-100 characters")
	}
	return name, nil
}

func validTopic(topic string) (string, error) {
	topic = strings.TrimSpace(topic)
	if len([]rune(topic)) > 1024 {
		return "", errf(http.StatusBadRequest, "invalid_topic", "topic must be at most 1024 characters")
	}
	return topic, nil
}

// checkParent validates that a channel of type typ may be placed under parent (0: top level).
func (s *Server) checkParent(ctx context.Context, typ string, parent int64) error {
	if parent == 0 {
		return nil
	}
	if typ == chanCategory {
		return errf(http.StatusBadRequest, "invalid_parent", "categories cannot be nested")
	}
	p, err := channelByID(ctx, s.db, parent)
	if httpapi.IsCode(err, "not_found") {
		return errf(http.StatusBadRequest, "invalid_parent", "parent category not found")
	}
	if err != nil {
		return err
	}
	if p.Type != chanCategory {
		return errf(http.StatusBadRequest, "invalid_parent", "parent must be a category")
	}
	return nil
}

func nullParent(parent int64) sql.NullInt64 {
	return sql.NullInt64{Int64: parent, Valid: parent != 0}
}

// broadcastChannel sends a channel-scoped event to the members who can see the channel.
func (s *Server) broadcastChannel(ctx context.Context, t string, channelID int64, d any) {
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		s.logErr("loading permissions for "+t, err)
		return
	}
	s.hub.BroadcastTo(t, d, func(memberID, _ string) bool {
		return ps.inChannel(memberID, channelID)&permViewChannel != 0
	})
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Topic    string `json:"topic"`
		ParentID int64  `json:"parent_id"` // 0: top level
		Position *int64 `json:"position"`  // absent: after its siblings
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	if req.Type == "" {
		req.Type = chanText
	}
	if req.Type != chanText && req.Type != chanVoice && req.Type != chanCategory && req.Type != chanAnnouncement {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_type", "type must be text, announcement, voice or category (threads start from a message)"))
		return
	}
	storedType, announcement := req.Type, false
	if req.Type == chanAnnouncement {
		storedType, announcement = chanText, true
	}
	name, err := validName(req.Name)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	topic, err := validTopic(req.Topic)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.checkParent(ctx, req.Type, req.ParentID); err != nil {
		writeErr(w, r, err)
		return
	}
	var pos int64
	if req.Position != nil {
		pos = *req.Position
	} else if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM channels WHERE parent_id IS ?`,
		nullParent(req.ParentID)).Scan(&pos); err != nil {
		writeErr(w, r, err)
		return
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO channels (type, name, topic, parent_id, position, created_at, announcement) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		storedType, name, topic, nullParent(req.ParentID), pos, s.nowMs(), announcement)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	c, err := channelByID(ctx, s.db, id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.broadcastChannel(ctx, "CHANNEL_CREATE", c.ID, c)
	s.audit(ctx, s.db, memberFrom(r).ID, auditChannelCreate, fmt.Sprint(c.ID), "", map[string]any{"name": c.Name, "type": c.Type})
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     *string `json:"name"`
		Topic    *string `json:"topic"`
		ParentID *int64  `json:"parent_id"` // 0: move to top level
		Position *int64  `json:"position"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	c, err := s.pathChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.requireChannelPerm(r, c, permManageChannels); err != nil {
		writeErr(w, r, err)
		return
	}
	if req.Name != nil {
		if c.Name, err = validName(*req.Name); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if req.Topic != nil {
		if c.Topic, err = validTopic(*req.Topic); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	moved := false
	if req.ParentID != nil && c.Type == chanThread {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_parent", "a thread stays in its channel"))
		return
	}
	if req.ParentID != nil {
		if *req.ParentID == c.ID {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_parent", "a channel cannot be its own parent"))
			return
		}
		if err := s.checkParent(ctx, c.Type, *req.ParentID); err != nil {
			writeErr(w, r, err)
			return
		}
		c.ParentID, moved = nil, true
		if *req.ParentID != 0 {
			c.ParentID = req.ParentID
		}
	}
	if req.Position != nil {
		c.Position = *req.Position
	}
	var parent int64
	if c.ParentID != nil {
		parent = *c.ParentID
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE channels SET name = ?, topic = ?, parent_id = ?, position = ? WHERE id = ?`,
		c.Name, c.Topic, nullParent(parent), c.Position, c.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.broadcastChannel(ctx, "CHANNEL_UPDATE", c.ID, c)
	s.audit(ctx, s.db, memberFrom(r).ID, auditChannelUpdate, fmt.Sprint(c.ID), "", map[string]any{"name": c.Name, "topic": c.Topic, "parent_id": c.ParentID, "position": c.Position})
	if moved {
		s.syncPermissions(ctx) // a new category can change who sees the channel
	}
	writeJSON(w, http.StatusOK, c)
}

// handleDeleteChannel deletes a channel and its messages; a deleted
// category's channels move to the top level.
func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	c, err := s.pathChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.requireChannelPerm(r, c, permManageChannels); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	// A channel's threads go with it (the parent foreign key would only detach them).
	if _, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ? OR (thread = 1 AND parent_id = ?)`, c.ID, c.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.Broadcast("CHANNEL_DELETE", map[string]int64{"id": c.ID})
	s.audit(ctx, s.db, memberFrom(r).ID, auditChannelDelete, fmt.Sprint(c.ID), "", map[string]any{"name": c.Name, "type": c.Type})
	if c.Type == chanCategory {
		s.syncPermissions(ctx) // children moved to the top level, without the category's overrides
	} else {
		s.reconcileVoice(ctx) // disconnects whoever was in a deleted voice channel
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- channel overrides ---

// handleSetOverride creates or replaces the override of a role or member in a
// channel. It needs manage_roles, and one can only allow or deny permissions
// one has in that channel.
func (s *Server) handleSetOverride(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
	}
	if r.Method != http.MethodDelete {
		if err := decode(r, &req); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	ctx := r.Context()
	c, err := s.pathChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ps, err := s.requireChannelPerm(r, c, 0)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	actor := memberFrom(r).ID
	if ps.base(actor)&permManageRoles == 0 {
		writeErr(w, r, missing(permManageRoles))
		return
	}
	allow, err := parsePerms(req.Allow)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	deny, err := parsePerms(req.Deny)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if (allow|deny)&^permChannelScoped != 0 {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_permission", "only channel permissions can be overridden: %s",
			strings.Join(permChannelScoped.names(), ", ")))
		return
	}
	if allow&deny != 0 {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_permission", "a permission cannot be both allowed and denied"))
		return
	}

	typ, target := r.PathValue("type"), r.PathValue("target")
	switch typ {
	case targetRole:
		id, err := strconv.ParseInt(target, 10, 64)
		if err != nil {
			writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such role"))
			return
		}
		rl := ps.roles[id]
		if rl == nil {
			writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such role"))
			return
		}
		if err := checkRoleRank(ps, actor, rl); err != nil {
			writeErr(w, r, err)
			return
		}
	case targetMember:
		m, err := memberBy(ctx, s.db, `id = ? AND left_at IS NULL`, target)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if m == nil {
			writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such member"))
			return
		}
		if m.ID != actor && !ps.outranks(actor, m.ID) {
			writeErr(w, r, errf(http.StatusForbidden, "role_hierarchy", "this member's highest role is not below yours"))
			return
		}
	default:
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "override type must be role or member"))
		return
	}

	var old override
	for _, o := range c.Overrides {
		if o.Type == typ && o.TargetID == target {
			old = o
		}
	}
	changed := (allow ^ old.allow) | (deny ^ old.deny)
	if extra := changed &^ ps.inChannel(actor, c.ID); extra != 0 {
		writeErr(w, r, errf(http.StatusForbidden, "missing_permissions", "you cannot grant or revoke permissions you do not have here: %s",
			strings.Join(extra.names(), ", ")))
		return
	}

	if r.Method == http.MethodDelete || allow|deny == 0 {
		_, err = s.db.ExecContext(ctx, `DELETE FROM channel_overrides WHERE channel_id = ? AND target_type = ? AND target_id = ?`, c.ID, typ, target)
	} else {
		_, err = s.db.ExecContext(ctx, `INSERT INTO channel_overrides (channel_id, target_type, target_id, allow, deny) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (channel_id, target_type, target_id) DO UPDATE SET allow = excluded.allow, deny = excluded.deny`,
			c.ID, typ, target, allow, deny)
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if c, err = channelByID(ctx, s.db, c.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	action := auditOverrideUpdate
	if r.Method == http.MethodDelete || allow|deny == 0 {
		action = auditOverrideDelete
	}
	s.audit(ctx, s.db, memberFrom(r).ID, action, fmt.Sprint(c.ID), "", map[string]any{"target_type": typ, "target_id": target, "allow": allow.names(), "deny": deny.names()})
	s.broadcastChannel(ctx, "CHANNEL_UPDATE", c.ID, c)
	s.syncPermissions(ctx)
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
