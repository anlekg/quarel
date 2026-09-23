package main

import (
	"fmt"
	"strings"
	"time"
)

type voiceStateInfo struct {
	MemberID  string    `json:"member_id"`
	ChannelID *int64    `json:"channel_id"`
	SelfMute  bool      `json:"self_mute"`
	SelfDeaf  bool      `json:"self_deaf"`
	CanSpeak  bool      `json:"can_speak"`
	JoinedAt  time.Time `json:"joined_at"`
}

func (s voiceStateInfo) flags() string {
	f := ""
	switch {
	case s.SelfDeaf:
		f += " 🔇 sourdine"
	case s.SelfMute:
		f += " 🎙️ micro coupé"
	}
	if !s.CanSpeak {
		f += " (écoute seule)"
	}
	return f
}

func (c *cli) runVoice(cmd string, args []string) (bool, error) {
	switch cmd {
	case "voice":
		return true, c.printVoice()
	case "voice-test":
		return true, c.voiceTest()
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
	if strings.HasPrefix(base, "https://") && c.tlsSeen[base] != "" {
		fmt.Println("Le serveur utilise un certificat auto-signé : le navigateur affichera un avertissement à accepter.")
		fmt.Printf("quarelctl a vérifié que ce certificat appartient bien au serveur %s.\n", c.tlsSeen[base])
	}
	return nil
}
