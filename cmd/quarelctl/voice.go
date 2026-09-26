package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
)

type voiceStateInfo struct {
	MemberID   string    `json:"member_id"`
	ChannelID  *int64    `json:"channel_id"`
	SelfMute   bool      `json:"self_mute"`
	SelfDeaf   bool      `json:"self_deaf"`
	ServerMute bool      `json:"server_mute"`
	ServerDeaf bool      `json:"server_deaf"`
	CanSpeak   bool      `json:"can_speak"`
	Video      bool      `json:"video"`
	Screen     bool      `json:"screen"`
	JoinedAt   time.Time `json:"joined_at"`
}

func (s voiceStateInfo) flags() string {
	f := ""
	switch {
	case s.ServerDeaf:
		f += " 🔇 son coupé par la modération"
	case s.SelfDeaf:
		f += " 🔇 sourdine"
	}
	switch {
	case s.ServerMute:
		f += " 🎙️ micro coupé par la modération"
	case s.SelfMute && !s.SelfDeaf:
		f += " 🎙️ micro coupé"
	case !s.CanSpeak:
		f += " (écoute seule)"
	}
	if s.Video {
		f += " 📷 caméra"
	}
	if s.Screen {
		f += " 🖥️ partage d'écran"
	}
	return f
}

func (c *cli) runVoice(cmd string, args []string) (bool, error) {
	switch cmd {
	case "voice":
		return true, c.printVoice()
	case "voice-test":
		return true, c.voiceTest()
	case "voice-mute", "voice-deafen":
		return true, c.voiceModerate(cmd, args)
	case "voice-move":
		return true, c.voiceMove(args)
	case "voice-kick":
		return true, c.voiceKick(args)
	}
	return c.runSocial(cmd, args)
}

func (c *cli) printVoice() error {
	var states []voiceStateInfo
	if err := c.cdo("GET", "/v1/voice/states", nil, &states); err != nil {
		return err
	}
	list, err := c.channels()
	if err != nil {
		return err
	}
	members, err := c.members()
	if err != nil {
		return err
	}
	for _, ch := range list {
		if ch.Type != "voice" {
			continue
		}
		fmt.Printf("🔊 %s (id %d)\n", ch.Name, ch.ID)
		n := 0
		for _, s := range states {
			if s.ChannelID != nil && *s.ChannelID == ch.ID {
				fmt.Printf("    %s%s — depuis %s\n", authorName(members, s.MemberID), s.flags(), s.JoinedAt.Local().Format("15:04"))
				n++
			}
		}
		if n == 0 {
			fmt.Println("    (personne)")
		}
	}
	return nil
}

// voiceTest prints the address of the server's voice test page, with the
// session token in the URL fragment (never sent to the server by browsers).
func (c *cli) voiceTest() error {
	// A cheap authenticated call refreshes the session if it expired.
	if err := c.cdo("GET", "/v1/members/@me", nil, nil); err != nil {
		return err
	}
	base, com, err := c.current()
	if err != nil {
		return err
	}
	fmt.Printf("Ouvrez cette adresse dans un navigateur (Chrome ou Firefox) :\n\n  %s/voice-test/#token=%s\n\n", base, com.SessionToken)
	fmt.Println("⚠ Elle contient votre session : ne la partagez pas. Elle expire avec la session (12 h max).")
	if mode, sid := c.tlsMode(base); mode == idtoken.TLSBinding {
		fmt.Println("Le serveur utilise un certificat auto-signé : le navigateur affichera un avertissement à accepter.")
		fmt.Printf("quarelctl a vérifié que ce certificat appartient bien au serveur %s.\n", sid)
	}
	return nil
}

// voiceModerate: voice-mute|voice-deafen <membre> [off] [raison…]
func (c *cli) voiceModerate(cmd string, args []string) error {
	id, name, err := c.moderate(args)
	if err != nil {
		return err
	}
	on, rest := true, args[1:]
	if len(rest) > 0 && (rest[0] == "off" || rest[0] == "non") {
		on, rest = false, rest[1:]
	}
	field, what := "mute", "Micro"
	if cmd == "voice-deafen" {
		field, what = "deaf", "Son"
	}
	if err := c.cdo("PATCH", "/v1/voice/states/"+id, map[string]any{field: on, "reason": strings.Join(rest, " ")}, nil); err != nil {
		return err
	}
	if on {
		fmt.Printf("%s de %s coupé par la modération (jusqu'à levée, même après reconnexion).\n", what, name)
	} else {
		fmt.Printf("%s de %s rétabli.\n", what, name)
	}
	return nil
}

func (c *cli) voiceMove(args []string) error {
	if err := need(args, 2, "<membre> <salon vocal>"); err != nil {
		return err
	}
	id, name, err := c.moderate(args)
	if err != nil {
		return err
	}
	list, err := c.channels()
	if err != nil {
		return err
	}
	ch, err := resolveChannel(list, args[1], "voice")
	if err != nil {
		return err
	}
	if err := c.cdo("PATCH", "/v1/voice/states/"+id, map[string]any{"channel_id": ch.ID}, nil); err != nil {
		return err
	}
	fmt.Printf("Déplacement de %s vers 🔊 %s.\n", name, ch.Name)
	return nil
}

func (c *cli) voiceKick(args []string) error {
	id, name, err := c.moderate(args)
	if err != nil {
		return err
	}
	if err := c.cdo("DELETE", "/v1/voice/states/"+id+"?reason="+url.QueryEscape(strings.Join(args[1:], " ")), nil, nil); err != nil {
		return err
	}
	fmt.Printf("Déconnexion de %s du vocal.\n", name)
	return nil
}
