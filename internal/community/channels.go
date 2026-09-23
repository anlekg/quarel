package community

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/anlekg/quarel/internal/httpapi"
)

const (
	chanText     = "text"
	chanVoice    = "voice"
	chanCategory = "category"
)

type channel struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Topic    string `json:"topic"`
	ParentID *int64 `json:"parent_id"`
	Position int64  `json:"position"`
}

func scanChannel(sc interface{ Scan(...any) error }) (*channel, error) {
	var c channel
	var parent sql.NullInt64
	if err := sc.Scan(&c.ID, &c.Type, &c.Name, &c.Topic, &parent, &c.Position); err != nil {
		return nil, err
	}
	if parent.Valid {
		c.ParentID = &parent.Int64
	}
	return &c, nil
}

const channelCols = `id, type, name, topic, parent_id, position`

func (s *Server) channelByID(ctx context.Context, q querier, id int64) (*channel, error) {
	c, err := scanChannel(q.QueryRowContext(ctx, `SELECT `+channelCols+` FROM channels WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(http.StatusNotFound, "not_found", "no such channel")
	}
	return c, err
}

// pathChannel loads the channel named by the {id} path segment.
func (s *Server) pathChannel(r *http.Request) (*channel, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, errf(http.StatusNotFound, "not_found", "no such channel")
	}
	return s.channelByID(r.Context(), s.db, id)
}

func (s *Server) allChannels(ctx context.Context) ([]*channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelCols+` FROM channels ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// handleListChannels returns a flat list ordered by (position, id); clients
// build the tree from parent_id.
func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	list, err := s.allChannels(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
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
	p, err := s.channelByID(ctx, s.db, parent)
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
	if req.Type != chanText && req.Type != chanVoice && req.Type != chanCategory {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_type", "type must be text, voice or category"))
		return
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
	res, err := s.db.ExecContext(ctx, `INSERT INTO channels (type, name, topic, parent_id, position, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		req.Type, name, topic, nullParent(req.ParentID), pos, s.nowMs())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	c, err := s.channelByID(ctx, s.db, id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.broadcast("CHANNEL_CREATE", c)
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
	if req.ParentID != nil {
		if *req.ParentID == c.ID {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_parent", "a channel cannot be its own parent"))
			return
		}
		if err := s.checkParent(ctx, c.Type, *req.ParentID); err != nil {
			writeErr(w, r, err)
			return
		}
		c.ParentID = nil
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
	s.hub.broadcast("CHANNEL_UPDATE", c)
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
	ctx := r.Context()
	var children []*channel
	if c.Type == chanCategory {
		all, err := s.allChannels(ctx)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		for _, ch := range all {
			if ch.ParentID != nil && *ch.ParentID == c.ID {
				ch.ParentID = nil // the foreign key moves them to the top level
				children = append(children, ch)
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, c.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.broadcast("CHANNEL_DELETE", map[string]int64{"id": c.ID})
	for _, ch := range children {
		s.hub.broadcast("CHANNEL_UPDATE", ch)
	}
	w.WriteHeader(http.StatusNoContent)
}
