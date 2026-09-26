package identity

import (
	"context"
	"log/slog"
	"time"
)

// Housekeeping runs the periodic cleanups every hour until ctx ends.
func (s *Server) Housekeeping(ctx context.Context) {
	for {
		s.cleanup(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour):
		}
	}
}

func (s *Server) cleanup(ctx context.Context) {
	if err := s.CleanupFiles(ctx); err != nil {
		slog.Warn("conversation files cleanup", "err", err)
	}
	if err := s.endIdleSessions(ctx); err != nil {
		slog.Warn("idle sessions cleanup", "err", err)
	}
	now := s.now()
	// Ended sessions are published while the tokens they got may be valid
	// (revoked_at comes from SQLite's clock: see the sessions_ended trigger).
	s.db.ExecContext(ctx, `DELETE FROM revoked_devices WHERE revoked_at <= CAST(strftime('%s', 'now') AS INTEGER) - ?`, int64((s.cfg.TokenTTL + time.Hour).Seconds()))
	s.db.ExecContext(ctx, `DELETE FROM known_devices WHERE last_login <= ?`, now.Add(-knownDeviceTTL).Unix())
}

// endIdleSessions ends the sessions unused for cfg.SessionIdle (a device
// lost or forgotten long ago): the device must sign in again, and its keys
// and pending messages go.
func (s *Server) endIdleSessions(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id FROM sessions WHERE last_seen_at <= ?`, s.now().Add(-s.cfg.SessionIdle).Unix())
	if err != nil {
		return err
	}
	type ended struct{ id, user string }
	var list []ended
	for rows.Next() {
		var e ended
		if rows.Scan(&e.id, &e.user) == nil {
			list = append(list, e)
		}
	}
	rows.Close()
	for _, e := range list {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, e.id); err != nil {
			return err
		}
		s.sessionEnded(ctx, e.id, e.user)
	}
	if len(list) > 0 {
		slog.Info("idle sessions ended", "count", len(list))
	}
	return nil
}
