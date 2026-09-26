package community

import (
	"context"
	"net/http"

	"github.com/anlekg/quarel/internal/realtime"
	"github.com/anlekg/quarel/internal/secret"
)

// The gateway (GET /v1/gateway) follows the protocol of package realtime.
// READY carries {member, server, roles, members, channels, voice_states, permissions,
// read_states, notification_settings}.

func (s *Server) handleGateway(w http.ResponseWriter, r *http.Request) {
	s.hub.Serve(w, r,
		func(ctx context.Context, token string) (*realtime.Auth, error) {
			m, expires, err := s.memberForToken(ctx, token)
			if err != nil || m == nil {
				return nil, err
			}
			// Group: the session, so that one ended session (its device logged
			// out on its identity service) can be disconnected alone.
			return &realtime.Auth{Key: m.ID, Group: secret.SHA256Hex(token), Expires: expires}, nil
		},
		func(ctx context.Context, a *realtime.Auth) (any, error) {
			m, err := memberBy(ctx, s.db, `id = ?`, a.Key)
			if err != nil {
				return nil, err
			}
			return s.readyPayload(ctx, m)
		})
}

// memberState is what a member can see and do: visible channels, who is in
// their voice channels, and permissions.
func (s *Server) memberState(ps *permSnapshot, memberID string) map[string]any {
	return map[string]any{
		"channels":     ps.visibleChannels(memberID),
		"voice_states": s.voiceStates(ps, memberID),
		"permissions":  map[string]any{"server": ps.base(memberID).names(), "channels": ps.channelPerms(memberID)},
		"restriction":  ps.restricted[memberID], // "", timed_out, rules_not_accepted or phone_not_verified
	}
}

// syncPermissions sends every connected member their visible channels and
// permissions (CHANNELS_SYNC) after a change that may affect them: role
// permissions, member roles, channel overrides, channel moves.
func (s *Server) syncPermissions(ctx context.Context) {
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		s.logErr("loading permissions for CHANNELS_SYNC", err)
		return
	}
	s.hub.SendEachIf("CHANNELS_SYNC", func(memberID, _ string) (any, bool) { return s.memberState(ps, memberID), true })
	s.reconcileVoice(ctx)
}

func (s *Server) readyPayload(ctx context.Context, m *member) (map[string]any, error) {
	info, err := s.info(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		return nil, err
	}
	me, err := s.memberView(ctx, m)
	if err != nil {
		return nil, err
	}
	members, err := s.activeMembers(ctx)
	if err != nil {
		return nil, err
	}
	roles, err := allRoles(ctx, s.db)
	if err != nil {
		return nil, err
	}
	readStates, err := s.readStates(ctx, m, messagingChannels(ps, m.ID))
	if err != nil {
		return nil, err
	}
	notifications, err := s.notificationSettings(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	ready := s.memberState(ps, m.ID)
	ready["member"], ready["server"], ready["members"], ready["roles"] = me, info, members, roles
	ready["read_states"], ready["notification_settings"] = readStates, notifications
	return ready, nil
}
