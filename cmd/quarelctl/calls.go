package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// Peer-to-peer audio calls between friends. Signalling (offer, answer,
// hang-up) travels as Olm-encrypted to-device messages: the Identity service
// relays it without seeing IP addresses or media parameters. Media goes
// directly between devices (DTLS-SRTP), or through the service's TURN relay
// when a direct path is impossible — unless the user turned the relay off.
// This test client sends a 440 Hz tone and measures what it receives.

type callSignal struct {
	CallID     string    `json:"call_id"`
	Action     string    `json:"action"` // invite | answer | reject | hangup
	SDP        string    `json:"sdp,omitempty"`
	FromUser   string    `json:"from_user,omitempty"`
	FromDevice string    `json:"from_device,omitempty"`
	At         time.Time `json:"at,omitempty"`
}

const (
	callRingTimeout    = 30 * time.Second
	callConnectTimeout = 20 * time.Second
	callSignalTTL      = 2 * time.Minute
)

// takeSignals removes and returns the pending signals matching keep.
func (e *e2e) takeSignals(keep func(callSignal) bool) []callSignal {
	var out, rest []callSignal
	for _, s := range e.st.CallSignals {
		switch {
		case time.Since(s.At) > callSignalTTL:
		case keep(s):
			out = append(out, s)
		default:
			rest = append(rest, s)
		}
	}
	e.st.CallSignals = rest
	return out
}

func (c *cli) runCalls(cmd string, args []string) (bool, error) {
	var err error
	switch cmd {
	case "call":
		err = c.placeCall(args)
	case "call-listen":
		err = c.callListen(args)
	case "calls":
		err = c.callSettings(args)
	default:
		return c.runAccount(cmd, args)
	}
	return true, err
}

type callOptions struct {
	seconds   int
	relayOnly bool
	once      bool
}

func parseCallOptions(args []string) (callOptions, []string, error) {
	o := callOptions{seconds: 5}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--relay-only":
			o.relayOnly = true
		case a == "--once":
			o.once = true
		case a == "--seconds" && i+1 < len(args):
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 {
				return o, nil, fmt.Errorf("durée invalide : %s", args[i+1])
			}
			o.seconds = n
			i++
		default:
			rest = append(rest, a)
		}
	}
	return o, rest, nil
}

// callSettings: calls [relay=on|off]. With the relay off, calls only work
// when a direct path exists, but no Quarel server ever carries the media.
func (c *cli) callSettings(args []string) error {
	for _, a := range args {
		switch a {
		case "relay=on", "relay=oui":
			c.st.CallRelayOff = false
		case "relay=off", "relay=non":
			c.st.CallRelayOff = true
		default:
			return fmt.Errorf("réglage inconnu %q (relay=on|off)", a)
		}
	}
	if err := c.save(); err != nil {
		return err
	}
	if c.st.CallRelayOff {
		fmt.Println("Relais d'appel désactivé : appels en direct seulement (ils échouent si aucun chemin direct n'existe).")
	} else {
		fmt.Println("Relais d'appel autorisé : utilisé seulement si aucun chemin direct n'existe (le média reste chiffré).")
	}
	return nil
}

// peer is one side of a call.
type peer struct {
	pc        *webrtc.PeerConnection
	tone      *webrtc.TrackLocalStaticSample
	received  atomic.Int64
	connected chan struct{}
	failed    chan struct{}
}

func (c *cli) newPeer(o callOptions) (*peer, error) {
	var servers struct {
		ICEServers []struct {
			URLs       []string `json:"urls"`
			Username   string   `json:"username"`
			Credential string   `json:"credential"`
		} `json:"ice_servers"`
	}
	if err := c.do("GET", "/v1/calls/ice-servers", nil, &servers); err != nil {
		return nil, err
	}
	cfg := webrtc.Configuration{}
	hasRelay := false
	for _, s := range servers.ICEServers {
		turn := len(s.URLs) > 0 && strings.HasPrefix(s.URLs[0], "turn")
		if turn && c.st.CallRelayOff {
			continue
		}
		hasRelay = hasRelay || turn
		cfg.ICEServers = append(cfg.ICEServers, webrtc.ICEServer{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	if o.relayOnly {
		if !hasRelay {
			return nil, errors.New("--relay-only : aucun relais disponible (désactivé ici ou sur le service)")
		}
		cfg.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	me := &webrtc.MediaEngine{}
	if err := me.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(me)).NewPeerConnection(cfg)
	if err != nil {
		return nil, err
	}
	p := &peer{pc: pc, connected: make(chan struct{}), failed: make(chan struct{})}
	p.tone, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000, Channels: 1}, "audio", "quarel")
	if err != nil {
		pc.Close()
		return nil, err
	}
	sender, err := pc.AddTrack(p.tone)
	if err != nil {
		pc.Close()
		return nil, err
	}
	go func() { // RTCP must be read for the sender to work
		buf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buf); err != nil {
				return
			}
		}
	}()
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			pkt, _, err := t.ReadRTP()
			if err != nil {
				return
			}
			p.received.Add(int64(len(pkt.Payload)))
		}
	})
	var once, failOnce atomic.Bool
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		switch s {
		case webrtc.PeerConnectionStateConnected:
			if once.CompareAndSwap(false, true) {
				close(p.connected)
			}
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			if failOnce.CompareAndSwap(false, true) {
				close(p.failed)
			}
		}
	})
	return p, nil
}

// localSDP completes ICE gathering (no trickle: one message each way).
func (p *peer) localSDP(desc webrtc.SessionDescription) (string, error) {
	done := webrtc.GatheringCompletePromise(p.pc)
	if err := p.pc.SetLocalDescription(desc); err != nil {
		return "", err
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		return "", errors.New("collecte des chemins réseau trop longue")
	}
	return p.pc.LocalDescription().SDP, nil
}

// talk sends the tone for d, then reports the path used and audio received.
func (p *peer) talk(d time.Duration, hungUp func() bool) (string, int64) {
	frame := make([]byte, 160) // 20 ms of 8 kHz µ-law
	stop := time.After(d)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-stop:
			return p.path(), p.received.Load()
		case <-p.failed:
			return p.path(), p.received.Load()
		case <-tick.C:
			for i := range frame {
				t := float64(n*160+i) / 8000
				frame[i] = linearToMulaw(int16(8000 * math.Sin(2*math.Pi*440*t)))
			}
			n++
			p.tone.WriteSample(media.Sample{Data: frame, Duration: 20 * time.Millisecond})
			if n%25 == 0 && hungUp != nil && hungUp() {
				return p.path(), p.received.Load()
			}
		}
	}
}

// path describes the selected route: direct (local network or through NATs) or relay.
func (p *peer) path() string {
	for _, s := range p.pc.GetSenders() {
		if s.Transport() == nil {
			continue
		}
		pair, err := s.Transport().ICETransport().GetSelectedCandidatePair()
		if err != nil || pair == nil {
			continue
		}
		if pair.Local.Typ == webrtc.ICECandidateTypeRelay || pair.Remote.Typ == webrtc.ICECandidateTypeRelay {
			return "par le relais TURN"
		}
		if pair.Local.Typ == webrtc.ICECandidateTypeHost && pair.Remote.Typ == webrtc.ICECandidateTypeHost {
			return "en direct (réseau local)"
		}
		return "en direct (pair à pair)"
	}
	return "inconnu"
}

func linearToMulaw(sample int16) byte {
	const bias, clip = 0x84, 32635
	s, sign := int(sample), byte(0)
	if s < 0 {
		s, sign = -s, 0x80
	}
	s = min(s, clip) + bias
	exponent := 7
	for mask := 0x4000; s&mask == 0 && exponent > 0; mask >>= 1 {
		exponent--
	}
	return ^(sign | byte(exponent<<4) | byte((s>>(exponent+3))&0x0F))
}

func newCallID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// placeCall: call <pseudo> [--seconds N] [--relay-only]
func (c *cli) placeCall(args []string) error {
	o, rest, err := parseCallOptions(args)
	if err != nil {
		return err
	}
	if err := need(rest, 1, "<pseudo> [--seconds N] [--relay-only]"); err != nil {
		return err
	}
	u, rel, err := c.findUser(rest[0])
	if err != nil {
		return err
	}
	if rel != "friends" {
		return fmt.Errorf("les appels sont réservés aux amis")
	}
	callID := newCallID()
	p, err := c.newPeer(o)
	if err != nil {
		return err
	}
	defer p.pc.Close()
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	sdp, err := p.localSDP(offer)
	if err != nil {
		return err
	}
	var devices []deviceInfo
	err = c.withE2E(func(e *e2e) error {
		if err := e.sync(func(string) {}); err != nil {
			return err
		}
		if e.st.MasterSeed == "" {
			return errors.New("cet appareil n'est pas encore validé")
		}
		if devices, err = e.trusted(u.ID); err != nil {
			return err
		}
		if len(devices) == 0 {
			return fmt.Errorf("%s n'a aucun appareil capable de recevoir un appel", u.Pseudo)
		}
		return e.sendSecret(devices, "call", callSignal{CallID: callID, Action: "invite", SDP: sdp})
	})
	if err != nil {
		return err
	}
	fmt.Printf("📞 Appel de %s (%d appareil(s))…\n", u.Pseudo, len(devices))

	var answer *callSignal
	deadline := time.Now().Add(callRingTimeout)
	for answer == nil && time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		var sigs []callSignal
		if err := c.withE2E(func(e *e2e) error {
			if err := e.sync(func(string) {}); err != nil {
				return err
			}
			sigs = e.takeSignals(func(s callSignal) bool { return s.CallID == callID })
			return nil
		}); err != nil {
			return err
		}
		for _, s := range sigs {
			switch {
			case s.Action == "reject" && s.FromUser == u.ID:
				fmt.Printf("%s a refusé l'appel.\n", u.Pseudo)
				return nil
			case s.Action == "answer" && s.FromUser == u.ID && answer == nil:
				answer = &s
			}
		}
	}
	if answer == nil {
		c.hangup(u.ID, callID, "")
		return fmt.Errorf("pas de réponse de %s", u.Pseudo)
	}
	// Other devices of the callee stop ringing.
	c.hangup(u.ID, callID, answer.FromDevice)
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer.SDP}); err != nil {
		return err
	}
	return c.converse(p, o, u.Pseudo, callID, u.ID, answer.FromDevice)
}

// converse waits for the connection, talks, then hangs up.
func (c *cli) converse(p *peer, o callOptions, who, callID, otherUser, otherDevice string) error {
	select {
	case <-p.connected:
	case <-p.failed:
		return errors.New("connexion impossible (aucun chemin réseau entre les deux appareils)")
	case <-time.After(callConnectTimeout):
		return errors.New("connexion impossible dans le temps imparti (aucun chemin réseau ; relais désactivé ?)")
	}
	fmt.Printf("🔊 En communication avec %s.\n", who)
	hungUp := func() bool {
		gone := false
		c.withE2E(func(e *e2e) error {
			if e.sync(func(string) {}) == nil {
				gone = len(e.takeSignals(func(s callSignal) bool { return s.CallID == callID && s.Action == "hangup" })) > 0
			}
			return nil
		})
		return gone
	}
	path, bytes := p.talk(time.Duration(o.seconds)*time.Second, hungUp)
	c.hangup(otherUser, callID, "", otherDevice)
	fmt.Printf("Appel terminé : audio reçu %s (%d octets), chemin %s.\n", map[bool]string{true: "✔", false: "✘"}[bytes > 1000], bytes, path)
	return nil
}

// hangup tells devices of user the call is over: all of them except skip,
// or only the listed ones.
func (c *cli) hangup(user, callID, skip string, only ...string) {
	c.withE2E(func(e *e2e) error {
		devs, err := e.trusted(user)
		if err != nil {
			return err
		}
		var to []deviceInfo
		for _, d := range devs {
			if d.DeviceID == skip || len(only) > 0 && d.DeviceID != only[0] {
				continue
			}
			to = append(to, d)
		}
		return e.sendSecret(to, "call", callSignal{CallID: callID, Action: "hangup"})
	})
}

// callListen answers incoming calls from friends (test client: automatically).
// call-listen [--once] [--seconds N] [--relay-only]
func (c *cli) callListen(args []string) error {
	o, _, err := parseCallOptions(args)
	if err != nil {
		return err
	}
	fmt.Println("En attente d'appels (Ctrl+C pour quitter)…")
	for {
		var invites []callSignal
		if err := c.withE2E(func(e *e2e) error {
			if err := e.sync(func(string) {}); err != nil {
				return err
			}
			invites = e.takeSignals(func(s callSignal) bool { return s.Action == "invite" })
			return nil
		}); err != nil {
			return err
		}
		for _, inv := range invites {
			if err := c.answer(inv, o); err != nil {
				fmt.Println("⚠ " + err.Error())
			}
			if o.once {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (c *cli) answer(inv callSignal, o callOptions) error {
	l, err := c.friendLists()
	if err != nil {
		return err
	}
	var who string
	for _, f := range l.Friends {
		if f.ID == inv.FromUser {
			who = f.Pseudo
		}
	}
	if who == "" {
		return errors.New("appel d'une personne qui n'est pas votre amie : ignoré")
	}
	fmt.Printf("📞 Appel de %s : réponse automatique.\n", who)
	p, err := c.newPeer(o)
	if err != nil {
		return err
	}
	defer p.pc.Close()
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: inv.SDP}); err != nil {
		return err
	}
	ans, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	sdp, err := p.localSDP(ans)
	if err != nil {
		return err
	}
	if err := c.withE2E(func(e *e2e) error {
		devs, err := e.trusted(inv.FromUser)
		if err != nil {
			return err
		}
		for _, d := range devs {
			if d.DeviceID == inv.FromDevice {
				return e.sendSecret([]deviceInfo{d}, "call", callSignal{CallID: inv.CallID, Action: "answer", SDP: sdp})
			}
		}
		return errors.New("l'appareil appelant n'est plus validé")
	}); err != nil {
		return err
	}
	return c.converse(p, o, who, inv.CallID, inv.FromUser, inv.FromDevice)
}
