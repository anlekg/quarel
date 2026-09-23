package community

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/voice"
)

// Voice channels are LiveKit rooms named "channel-<id>". The community server
// never carries media: it hands out join tokens according to permissions
// (connect to join, speak to publish), learns who is connected from LiveKit
// webhooks, and removes or mutes participants when their rights change.

// VoiceBackend is the SFU. *voice.LiveKit implements it.
type VoiceBackend interface {
	JoinToken(room, identity, name string, canPublish bool, ttl time.Duration) (string, error)
	RemoveParticipant(ctx context.Context, room, identity string) error
	SetCanPublish(ctx context.Context, room, identity string, canPublish bool) error
	Participants(ctx context.Context) (map[string][]string, error)
	ReceiveWebhook(r *http.Request) (voice.Event, error)
}

// VoiceOptions enables voice on a Server.
type VoiceOptions struct {
	Backend VoiceBackend
	// Proxy forwards /lk/ to an embedded livekit-server (nil for an external one).
	Proxy http.Handler
	// PublicURL is the LiveKit URL given to clients; empty means this server's /lk.
	PublicURL string
}

const voiceTokenTTL = time.Hour

type voiceState struct {
	MemberID  string    `json:"member_id"`
	ChannelID int64     `json:"channel_id"`
	SelfMute  bool      `json:"self_mute"`
	SelfDeaf  bool      `json:"self_deaf"`
	CanSpeak  bool      `json:"can_speak"`
	JoinedAt  time.Time `json:"joined_at"`
}

// voiceRegistry is the in-memory list of who is in which voice channel.
// LiveKit is the source of truth; the registry is rebuilt from it at startup.
type voiceRegistry struct {
	mu     sync.Mutex
	states map[string]*voiceState // by member ID
}

func roomName(channelID int64) string { return fmt.Sprint("channel-", channelID) }

func roomChannel(room string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimPrefix(room, "channel-"), 10, 64)
	return id, err == nil && strings.HasPrefix(room, "channel-")
}

// EnableVoice turns voice channels on.
func (s *Server) EnableVoice(o VoiceOptions) {
	s.voiceOpts = &o
	s.voice = &voiceRegistry{states: map[string]*voiceState{}}
}

func (s *Server) requireVoice() error {
	if s.voiceOpts == nil {
		return errf(http.StatusServiceUnavailable, "voice_disabled", "voice is disabled on this server")
	}
	return nil
}

// voiceStates returns a copy of the states in channels the member can see.
func (s *Server) voiceStates(ps *permSnapshot, memberID string) []voiceState {
	out := []voiceState{}
	if s.voice == nil {
		return out
	}
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	for _, st := range s.voice.states {
		if ps.inChannel(memberID, st.ChannelID)&permViewChannel != 0 {
			out = append(out, *st)
		}
	}
	return out
}

// broadcastVoice announces st to the members who can see its channel, and a
// departure (channel_id null) to those who could only see previous.
func (s *Server) broadcastVoice(ctx context.Context, memberID string, st *voiceState, previous int64) {
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		s.logErr("loading permissions for VOICE_STATE_UPDATE", err)
		return
	}
	left := map[string]any{"member_id": memberID, "channel_id": nil}
	s.hub.sendEachIf("VOICE_STATE_UPDATE", func(viewer string) (any, bool) {
		if st != nil && ps.inChannel(viewer, st.ChannelID)&permViewChannel != 0 {
			return st, true
		}
		if previous != 0 && ps.inChannel(viewer, previous)&permViewChannel != 0 {
			return left, true
		}
		return nil, false
	})
}

func (s *Server) voiceURL(r *http.Request) string {
	if s.voiceOpts.PublicURL != "" {
		return s.voiceOpts.PublicURL
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/lk"
}

// handleVoiceJoin issues a LiveKit token for a voice channel. The member
// appears in the channel once their client actually connects to LiveKit.
func (s *Server) handleVoiceJoin(w http.ResponseWriter, r *http.Request) {
	if err := s.requireVoice(); err != nil {
		writeErr(w, r, err)
		return
	}
	c, err := s.pathChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ps, err := s.requireChannelPerm(r, c, permConnect)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if c.Type != chanVoice {
		writeErr(w, r, errf(http.StatusBadRequest, "not_voice_channel", "only voice channels can be joined"))
		return
	}
	m := memberFrom(r)
	canSpeak := ps.inChannel(m.ID, c.ID)&permSpeak != 0
	token, err := s.voiceOpts.Backend.JoinToken(roomName(c.ID), m.ID, m.json().DisplayName, canSpeak, voiceTokenTTL)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": s.voiceURL(r), "token": token, "room": roomName(c.ID), "can_speak": canSpeak})
}

func (s *Server) handleVoiceStates(w http.ResponseWriter, r *http.Request) {
	ps, err := s.loadPerms(r.Context(), s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.voiceStates(ps, memberFrom(r).ID))
}

// handleVoiceSelfState updates the member's own mute/deafen flags (informative:
// the client mutes itself; deafened implies muted).
func (s *Server) handleVoiceSelfState(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SelfMute *bool `json:"self_mute"`
		SelfDeaf *bool `json:"self_deaf"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.requireVoice(); err != nil {
		writeErr(w, r, err)
		return
	}
	id := memberFrom(r).ID
	s.voice.mu.Lock()
	st := s.voice.states[id]
	if st != nil {
		if req.SelfMute != nil {
			st.SelfMute = *req.SelfMute
		}
		if req.SelfDeaf != nil {
			st.SelfDeaf = *req.SelfDeaf
		}
		if st.SelfDeaf {
			st.SelfMute = true
		}
	}
	var snapshot voiceState
	if st != nil {
		snapshot = *st
	}
	s.voice.mu.Unlock()
	if st == nil {
		writeErr(w, r, errf(http.StatusConflict, "not_in_voice", "you are not in a voice channel"))
		return
	}
	s.broadcastVoice(r.Context(), id, &snapshot, 0)
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleVoiceLeave(w http.ResponseWriter, r *http.Request) {
	if err := s.requireVoice(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.disconnectVoice(r.Context(), memberFrom(r).ID)
	w.WriteHeader(http.StatusNoContent)
}

// disconnectVoice removes a member from voice, in LiveKit and locally.
func (s *Server) disconnectVoice(ctx context.Context, memberID string) {
	if s.voice == nil {
		return
	}
	s.voice.mu.Lock()
	st := s.voice.states[memberID]
	delete(s.voice.states, memberID)
	s.voice.mu.Unlock()
	if st == nil {
		return
	}
	if err := s.voiceOpts.Backend.RemoveParticipant(ctx, roomName(st.ChannelID), memberID); err != nil && err != voice.ErrNotFound {
		s.logErr("removing voice participant", err)
	}
	s.broadcastVoice(ctx, memberID, nil, st.ChannelID)
}

// handleLiveKitWebhook receives room events from LiveKit (signed with the API secret).
func (s *Server) handleLiveKitWebhook(w http.ResponseWriter, r *http.Request) {
	ev, err := s.voiceOpts.Backend.ReceiveWebhook(r)
	if err != nil {
		writeErr(w, r, errf(http.StatusUnauthorized, "invalid_webhook", "%v", err))
		return
	}
	s.handleVoiceEvent(r.Context(), ev)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleVoiceEvent(ctx context.Context, ev voice.Event) {
	chID, ok := roomChannel(ev.Room)
	if !ok {
		return
	}
	switch ev.Type {
	case "participant_joined":
		s.voiceJoined(ctx, chID, ev.Identity)
	case "participant_left":
		s.voice.mu.Lock()
		st := s.voice.states[ev.Identity]
		if st != nil && st.ChannelID == chID {
			delete(s.voice.states, ev.Identity)
		}
		s.voice.mu.Unlock()
		if st != nil && st.ChannelID == chID {
			s.broadcastVoice(ctx, ev.Identity, nil, chID)
		}
	case "room_finished":
		for _, id := range s.voiceMembersIn(chID) {
			s.handleVoiceEvent(ctx, voice.Event{Type: "participant_left", Room: ev.Room, Identity: id})
		}
	}
}

func (s *Server) voiceMembersIn(chID int64) []string {
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	ids := []string{}
	for id, st := range s.voice.states {
		if st.ChannelID == chID {
			ids = append(ids, id)
		}
	}
	return ids
}

// voiceJoined records a participant LiveKit reports as connected, after
// re-checking their rights (a token may outlive a permission change).
// Joining another channel moves the member: one voice channel at a time.
func (s *Server) voiceJoined(ctx context.Context, chID int64, memberID string) {
	backend := s.voiceOpts.Backend
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		s.logErr("loading permissions for voice join", err)
		return
	}
	m, err := memberBy(ctx, s.db, `id = ? AND left_at IS NULL`, memberID)
	p := ps.inChannel(memberID, chID)
	if err != nil || m == nil || p&permConnect == 0 {
		backend.RemoveParticipant(ctx, roomName(chID), memberID)
		return
	}
	st := &voiceState{MemberID: memberID, ChannelID: chID, CanSpeak: p&permSpeak != 0, JoinedAt: s.now().UTC().Truncate(time.Millisecond)}
	s.voice.mu.Lock()
	prev := s.voice.states[memberID]
	s.voice.states[memberID] = st
	snapshot := *st
	s.voice.mu.Unlock()

	var previous int64
	if prev != nil && prev.ChannelID != chID {
		previous = prev.ChannelID
		if err := backend.RemoveParticipant(ctx, roomName(prev.ChannelID), memberID); err != nil && err != voice.ErrNotFound {
			s.logErr("moving voice participant", err)
		}
	}
	s.broadcastVoice(ctx, memberID, &snapshot, previous)
}

// reconcileVoice applies permission changes to connected participants:
// removes those who lost connect (or membership, or the channel), and
// grants or revokes speaking according to speak.
func (s *Server) reconcileVoice(ctx context.Context) {
	if s.voice == nil {
		return
	}
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		s.logErr("loading permissions for voice", err)
		return
	}
	active, err := s.activeMemberIDs(ctx)
	if err != nil {
		s.logErr("listing members for voice", err)
		return
	}
	type change struct {
		st     voiceState
		remove bool
	}
	var changes []change
	s.voice.mu.Lock()
	for id, st := range s.voice.states {
		p := ps.inChannel(id, st.ChannelID)
		switch {
		case !active[id] || p&permConnect == 0:
			delete(s.voice.states, id)
			changes = append(changes, change{*st, true})
		case (p&permSpeak != 0) != st.CanSpeak:
			st.CanSpeak = !st.CanSpeak
			changes = append(changes, change{*st, false})
		}
	}
	s.voice.mu.Unlock()

	for _, ch := range changes {
		room := roomName(ch.st.ChannelID)
		if ch.remove {
			if err := s.voiceOpts.Backend.RemoveParticipant(ctx, room, ch.st.MemberID); err != nil && err != voice.ErrNotFound {
				s.logErr("removing voice participant", err)
			}
			s.broadcastVoice(ctx, ch.st.MemberID, nil, ch.st.ChannelID)
			continue
		}
		if err := s.voiceOpts.Backend.SetCanPublish(ctx, room, ch.st.MemberID, ch.st.CanSpeak); err != nil {
			s.logErr("updating voice permission", err)
		}
		s.broadcastVoice(ctx, ch.st.MemberID, &ch.st, 0)
	}
}

func (s *Server) activeMemberIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM members WHERE left_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SyncVoice rebuilds the voice registry from LiveKit (after a restart of the
// community server while LiveKit kept running) and applies current permissions.
func (s *Server) SyncVoice(ctx context.Context) error {
	if s.voice == nil {
		return nil
	}
	rooms, err := s.voiceOpts.Backend.Participants(ctx)
	if err != nil {
		return err
	}
	for room, ids := range rooms {
		chID, ok := roomChannel(room)
		if !ok {
			continue
		}
		for _, id := range ids {
			s.voiceJoined(ctx, chID, id)
		}
	}
	return nil
}
