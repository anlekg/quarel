package community

import "context"

// Stages (P2): voice channels marked stage. Members arrive in the audience
// (listening only); they may raise their hand (PATCH /v1/voice/state
// {hand_raised}); members who may mute others in the channel talk freely and
// invite someone to speak or send them back (PATCH /v1/voice/states/{member}
// {speaker}). A speaker may step down themselves. Speakers are remembered in
// memory until they leave the stage (a restart forgets them).

func (s *Server) isSpeaker(memberID string, chID int64) bool {
	if s.voice == nil {
		return false
	}
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	return s.voice.speakers[memberID] == chID
}

// setSpeaker invites a member to speak on the stage they are on, or sends
// them back to the audience; LiveKit permissions follow.
func (s *Server) setSpeaker(ctx context.Context, memberID string, chID int64, on bool) {
	s.voice.mu.Lock()
	if on {
		s.voice.speakers[memberID] = chID
	} else if s.voice.speakers[memberID] == chID {
		delete(s.voice.speakers, memberID)
	}
	if st := s.voice.states[memberID]; st != nil && st.ChannelID == chID {
		st.Speaker, st.HandRaised, st.changed = on, false, true
	}
	s.voice.mu.Unlock()
	s.reconcileVoice(ctx)
}
