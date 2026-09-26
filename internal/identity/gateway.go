package identity

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/anlekg/quarel/internal/realtime"
)

// The Identity gateway (GET /v1/gateway) follows the protocol of package
// realtime; one connection per device (session). Events:
//
//	READY           {user, device_id, inbox_pending, one_time_keys}
//	INBOX           [inbox items] for this device (then acknowledge them)
//	FRIENDS_UPDATE  {user, status: friends|incoming|outgoing|none}
//	DEVICES_UPDATE  {user_id}: that user's device list changed
//	PRESENCE_UPDATE {user_id, status}: a friend's presence (online, idle, dnd, offline)
//	PRESENCE_SETTING {status}: my own presence setting changed on another device
//	USER_UPDATE     profile: my or a friend's pseudo, bio or avatar changed

// sessionForToken resolves a bearer token to its session, or nil.
func (s *Server) sessionForToken(ctx context.Context, token string) (*session, bool, error) {
	var sess session
	var disabled sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.user_id, s.device_key, u.disabled_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.last_seen_at > ?`, sha256Hex(token), s.now().Add(-s.cfg.SessionIdle).Unix()).Scan(&sess.ID, &sess.UserID, &sess.DeviceKey, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &sess, disabled.Valid, nil
}

func (s *Server) handleGateway(w http.ResponseWriter, r *http.Request) {
	s.hub.Serve(w, r,
		func(ctx context.Context, token string) (*realtime.Auth, error) {
			sess, disabled, err := s.sessionForToken(ctx, token)
			if err != nil || sess == nil || disabled {
				return nil, err
			}
			s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, s.now().Unix(), sess.ID)
			return &realtime.Auth{Key: sess.ID, Group: sess.UserID}, nil
		},
		func(ctx context.Context, a *realtime.Auth) (any, error) {
			u, err := s.userBy(ctx, "id", a.Group)
			if err != nil || u == nil {
				return nil, errors.New("session user vanished")
			}
			var pending int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbox WHERE session_id = ?`, a.Key).Scan(&pending); err != nil {
				return nil, err
			}
			otks, err := s.countOneTimeKeys(ctx, a.Key)
			if err != nil {
				return nil, err
			}
			var presence string
			s.db.QueryRowContext(ctx, `SELECT presence FROM users WHERE id = ?`, u.ID).Scan(&presence)
			return map[string]any{"user": s.userResp(u), "device_id": a.Key, "inbox_pending": pending, "one_time_keys": otks,
				"presence": presence}, nil
		})
}

// sessionEnded disconnects a closed session and tells contacts its device is gone.
func (s *Server) sessionEnded(ctx context.Context, sessionID, userID string) {
	s.hub.Disconnect(func(key, _ string) bool { return key == sessionID }, "session closed")
	s.devicesChanged(ctx, userID)
}
