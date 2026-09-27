package community

import (
	"context"
	"net/http"
	"time"
)

// Threads and forum posts are channels: without bounds, a member could make
// every READY, CHANNELS_SYNC and channel list grow without end.
//   - Creating them is limited per member (Limits.Threads per 10 minutes);
//     whoever may manage the channel is not limited.
//   - A thread without a message for threadArchiveAfter is archived
//     (archived_at, Housekeeping): it leaves READY, CHANNELS_SYNC and the
//     channel lists, stays readable (GET /v1/channels/{id}, its messages,
//     search, forum posts), and a new message reopens it (CHANNEL_CREATE).

const threadArchiveAfter = 7 * 24 * time.Hour

// allowThread counts the creation of a thread or forum post in c by member.
func (s *Server) allowThread(ps *permSnapshot, member string, c *channel) error {
	if ps.inChannel(member, c.ID)&permManageChannels != 0 {
		return nil
	}
	return s.limit.threads.Check(member)
}

// ArchiveThreads archives the threads without a message for threadArchiveAfter.
func (s *Server) ArchiveThreads(ctx context.Context) error {
	now := s.now()
	rows, err := s.db.QueryContext(ctx, `UPDATE channels SET archived_at = ?
		WHERE thread = 1 AND archived_at IS NULL
		  AND COALESCE((SELECT MAX(created_at) FROM messages WHERE channel_id = channels.id), created_at) < ?
		RETURNING id`, now.UnixMilli(), now.Add(-threadArchiveAfter).UnixMilli())
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if c, err := channelByID(ctx, s.db, id); err == nil {
			s.broadcastChannel(ctx, "CHANNEL_UPDATE", c.ID, c) // clients drop it from their lists
		}
	}
	return rows.Err()
}

// revive reopens an archived thread that gets a message.
func (s *Server) revive(ctx context.Context, c *channel) {
	if c.ArchivedAt == nil {
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE channels SET archived_at = NULL WHERE id = ?`, c.ID); err != nil {
		s.logErr("reopening a thread", err)
		return
	}
	c.ArchivedAt = nil
	s.broadcastChannel(ctx, "CHANNEL_CREATE", c.ID, c)
}

// handleGetChannel: GET /v1/channels/{id}, for a channel left out of the
// lists (an archived thread the member opens from a message or a forum).
func (s *Server) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	c, err := s.pathChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.requireChannelPerm(r, c, 0); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
