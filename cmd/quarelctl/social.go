package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/anlekg/quarel/pkg/e2ekeys"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type publicUser struct {
	ID       string `json:"id"`
	Handle   string `json:"handle"`
	Pseudo   string `json:"pseudo"`
	Presence string `json:"presence"`
}

func (c *cli) runSocial(cmd string, args []string) (bool, error) {
	var err error
	switch cmd {
	case "friends":
		err = c.printFriends()
	case "friend-add":
		if err = need(args, 1, "<pseudo>"); err == nil {
			err = c.friendAdd(args[0])
		}
	case "friend-accept", "friend-remove":
		if err = need(args, 1, "<pseudo>"); err == nil {
			err = c.friendAction(cmd, args[0])
		}
	case "e2e":
		err = c.e2eStatus()
	case "devices":
		err = c.printDevices()
	case "device-approve":
		if err = need(args, 2, "<appareil> <code>"); err == nil {
			err = c.deviceApprove(args[0], strings.Join(args[1:], ""))
		}
	case "dm":
		if err = need(args, 2, "<pseudo|groupe> <texte…>"); err == nil {
			err = c.dmSend(args[0], strings.Join(args[1:], " "))
		}
	case "dm-history":
		if err = need(args, 1, "<pseudo|groupe>"); err == nil {
			err = c.dmHistory(args[0])
		}
	case "dm-sync":
		err = c.dmSync()
	case "dm-listen":
		err = c.dmListen()
	default:
		return c.runDMs(cmd, args)
	}
	return true, err
}

type friendLists struct {
	Friends  []publicUser `json:"friends"`
	Incoming []publicUser `json:"incoming"`
	Outgoing []publicUser `json:"outgoing"`
}

func (c *cli) friendLists() (*friendLists, error) {
	var l friendLists
	return &l, c.do("GET", "/v1/friends", nil, &l)
}

func (c *cli) printFriends() error {
	l, err := c.friendLists()
	if err != nil {
		return err
	}
	section := func(title string, users []publicUser, hint string) {
		fmt.Printf("%s (%d)\n", title, len(users))
		for _, u := range users {
			fmt.Printf("  %-20s %-32s %s\n", u.Pseudo, u.Handle, presenceText[u.Presence])
		}
		if len(users) > 0 && hint != "" {
			fmt.Println("  " + hint)
		}
	}
	section("Amis", l.Friends, "")
	section("Demandes reçues", l.Incoming, "→ quarelctl friend-accept <pseudo>  ou  friend-remove <pseudo> pour refuser")
	section("Demandes envoyées", l.Outgoing, "→ quarelctl friend-remove <pseudo> pour annuler")
	return nil
}

var relationText = map[string]string{
	"friends":  "vous êtes maintenant amis",
	"outgoing": "demande envoyée",
	"incoming": "demande reçue",
	"none":     "aucune relation",
}

func (c *cli) friendAdd(pseudo string) error {
	var res struct {
		User   publicUser `json:"user"`
		Status string     `json:"status"`
	}
	if err := c.do("POST", "/v1/friends", map[string]string{"pseudo": strings.TrimPrefix(pseudo, "@")}, &res); err != nil {
		return err
	}
	fmt.Printf("%s : %s.\n", res.User.Handle, relationText[res.Status])
	return nil
}

// findUser looks a pseudo up among friends and requests.
func (c *cli) findUser(pseudo string) (*publicUser, string, error) {
	l, err := c.friendLists()
	if err != nil {
		return nil, "", err
	}
	pseudo = strings.TrimPrefix(pseudo, "@")
	for rel, users := range map[string][]publicUser{"friends": l.Friends, "incoming": l.Incoming, "outgoing": l.Outgoing} {
		for _, u := range users {
			if strings.EqualFold(u.Pseudo, pseudo) || u.ID == pseudo {
				return &u, rel, nil
			}
		}
	}
	return nil, "", fmt.Errorf("%q n'est ni un ami ni une demande en cours (voir quarelctl friends)", pseudo)
}

func (c *cli) friendAction(cmd, pseudo string) error {
	u, rel, err := c.findUser(pseudo)
	if err != nil {
		return err
	}
	if cmd == "friend-accept" {
		if rel != "incoming" {
			return fmt.Errorf("aucune demande reçue de %s", u.Pseudo)
		}
		if err := c.do("POST", "/v1/friends/"+u.ID+"/accept", nil, nil); err != nil {
			return err
		}
		fmt.Printf("Vous êtes maintenant amis avec %s.\n", u.Pseudo)
		return nil
	}
	if err := c.do("DELETE", "/v1/friends/"+u.ID, nil, nil); err != nil {
		return err
	}
	fmt.Printf("%s : %s.\n", u.Pseudo, map[string]string{"friends": "retiré des amis", "incoming": "demande refusée", "outgoing": "demande annulée"}[rel])
	return nil
}

// --- devices ---

func (c *cli) e2eStatus() error { return c.withE2E(c.e2eStatusWith) }

func (c *cli) e2eStatusWith(e *e2e) error {
	ed, _ := e.identity()
	fmt.Printf("Appareil : %s (%s)\n", e.st.DeviceID, c.st.Handle)
	fmt.Printf("Code de vérification de cet appareil : %s\n", e2ekeys.VerificationCode(ed))
	if e.st.MasterSeed != "" {
		fmt.Println("Statut : ✔ validé — cet appareil peut envoyer et recevoir des messages privés.")
	} else {
		fmt.Println("Statut : ⏳ en attente de validation.")
		fmt.Println("Sur un appareil déjà validé, lancez :  quarelctl devices   puis   quarelctl device-approve <appareil> <code ci-dessus>")
		fmt.Println("Comparez le code de vos propres yeux : ne le recopiez jamais depuis un message reçu.")
	}
	return nil
}

func (c *cli) printDevices() error {
	return c.withE2E(func(e *e2e) error { return c.printDevicesWith(e) })
}

func (c *cli) printDevicesWith(e *e2e) error {
	keys, err := e.keysOf(e.st.UserID)
	if err != nil {
		return err
	}
	for _, d := range keys.Devices {
		status := "⏳ en attente de validation"
		if e.isVerifiedHere(d) {
			status = "✔ validé"
		}
		here := ""
		if d.DeviceID == e.st.DeviceID {
			here = "  (cet appareil)"
		}
		fmt.Printf("%-24s %s  code %s  id %s%s\n", d.DeviceName, status, d.VerificationCode, d.DeviceID, here)
	}
	return nil
}

func (c *cli) deviceApprove(target, code string) error {
	return c.withE2E(func(e *e2e) error {
		d, n, err := e.approve(target, code)
		if err != nil {
			return err
		}
		fmt.Printf("✔ « %s » validé : il a reçu la clé du compte et l'historique (%d message(s)).\n", d.DeviceName, n)
		return nil
	})
}

// --- direct messages ---

func printLine(s string) { fmt.Println(s) }

func (c *cli) dmSend(target, text string) error {
	return c.withE2E(func(e *e2e) error { return c.dmSendWith(e, target, text) })
}

func (c *cli) dmSendWith(e *e2e, target, text string) error {
	if err := e.sync(printLine); err != nil {
		return err
	}
	conv, err := c.resolveConv(e, target)
	if err != nil {
		return err
	}
	return c.sendEvent(e, conv, megolmPlain{Text: text})
}

// sendEvent sends an encrypted event and reports where it went.
func (c *cli) sendEvent(e *e2e, conv *convInfo, content megolmPlain) error {
	m, devices, err := e.sendDM(conv.ID, conv.others(e.st.UserID), content)
	if err != nil {
		return err
	}
	switch content.Type {
	case "edit":
		fmt.Printf("Message %d modifié.\n", content.Target)
	case "delete":
		fmt.Printf("Message %d supprimé chez tout le monde.\n", content.Target)
	default:
		fmt.Println(e.format(*m))
	}
	if devices == 0 {
		fmt.Printf("(⚠ personne dans %s n'a encore d'appareil capable de recevoir des messages chiffrés)\n", conv.label())
	} else {
		fmt.Printf("(chiffré de bout en bout, envoyé à %d appareil(s) dans %s)\n", devices, conv.label())
	}
	return nil
}

func (c *cli) dmHistory(target string) error {
	return c.withE2E(func(e *e2e) error { return c.dmHistoryWith(e, target) })
}

func (c *cli) dmHistoryWith(e *e2e, target string) error {
	if err := e.sync(func(string) {}); err != nil {
		return err
	}
	conv, err := c.resolveConv(e, target)
	if err != nil {
		return err
	}
	msgs := e.st.History[conv.ID]
	fmt.Printf("Conversation %s — %d message(s), chiffrée de bout en bout\n", conv.label(), len(msgs))
	for _, m := range msgs {
		fmt.Println(e.format(m))
	}
	pending := 0
	for _, it := range e.st.Undecrypted {
		if it.DMID != nil && *it.DMID == conv.ID {
			pending++
		}
	}
	if pending > 0 {
		fmt.Printf("(%d message(s) pas encore déchiffrable(s) : clé non reçue — l'appareil n'était peut-être pas validé quand ils ont été envoyés)\n", pending)
	}
	return nil
}

func (c *cli) dmSync() error {
	return c.withE2E(func(e *e2e) error {
		n := 0
		err := e.sync(func(s string) { n++; fmt.Println(s) })
		if err == nil && n == 0 {
			fmt.Println("Rien de nouveau.")
		}
		return err
	})
}

// dmListen shows direct messages and friend events live, through the Identity gateway.
func (c *cli) dmListen() error {
	var me string
	if err := c.withE2E(func(e *e2e) error { me = e.st.UserID; return nil }); err != nil {
		return err
	}
	// The state is loaded under lock for each sync, never kept in memory,
	// so other commands can run on the same profile meanwhile.
	sync := func(out func(string)) error { return c.withE2E(func(e *e2e) error { return e.sync(out) }) }
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.st.Server, "http")+"/v1/gateway", nil)
	if err != nil {
		return fmt.Errorf("connexion temps réel impossible : %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 << 20)
	if err := wsjson.Write(ctx, conn, map[string]string{"op": "auth", "token": c.st.SessionToken}); err != nil {
		return err
	}
	out := func(s string) { fmt.Printf("%s %s\n", time.Now().Format("15:04:05"), s) }
	for {
		var ev struct {
			T string          `json:"t"`
			D json.RawMessage `json:"d"`
		}
		if err := wsjson.Read(ctx, conn, &ev); err != nil {
			if ctx.Err() != nil {
				fmt.Println("\nArrêt de l'écoute.")
				return nil
			}
			return fmt.Errorf("connexion fermée : %w", err)
		}
		switch ev.T {
		case "READY":
			fmt.Println("Messages privés en direct (Ctrl+C pour quitter)…")
			if err := sync(out); err != nil {
				return err
			}
			c.handleFileSignals(out)
		case "INBOX":
			if err := sync(out); err != nil {
				return err
			}
			c.handleFileSignals(out)
		case "FRIENDS_UPDATE":
			var d struct {
				User   publicUser `json:"user"`
				Status string     `json:"status"`
			}
			json.Unmarshal(ev.D, &d)
			out(fmt.Sprintf("👥 %s : %s", d.User.Pseudo, relationText[d.Status]))
		case "PRESENCE_UPDATE":
			var d struct {
				UserID string `json:"user_id"`
				Status string `json:"status"`
			}
			json.Unmarshal(ev.D, &d)
			name := d.UserID
			if l, err := c.friendLists(); err == nil {
				for _, f := range l.Friends {
					if f.ID == d.UserID {
						name = f.Pseudo
					}
				}
			}
			out(fmt.Sprintf("%s : %s", name, presenceText[d.Status]))
		case "USER_UPDATE":
			var p profileInfo
			json.Unmarshal(ev.D, &p)
			out(fmt.Sprintf("✎ profil mis à jour : %s", p.Handle))
		case "DM_TYPING":
			var d struct {
				UserID string `json:"user_id"`
			}
			json.Unmarshal(ev.D, &d)
			out(fmt.Sprintf("… %s écrit", c.nameOf(d.UserID)))
		case "DM_READ":
			var d struct {
				UserID  string `json:"user_id"`
				EventID int64  `json:"event_id"`
			}
			json.Unmarshal(ev.D, &d)
			if d.UserID != me {
				out(fmt.Sprintf("👁 vu par %s (jusqu'au message %d)", c.nameOf(d.UserID), d.EventID))
			}
		case "DM_UPDATE":
			var ci convInfo
			json.Unmarshal(ev.D, &ci)
			if ci.Kind == "group" {
				names := []string{}
				for _, m := range ci.Members {
					names = append(names, m.Pseudo)
				}
				out(fmt.Sprintf("👥 groupe %s : %s", ci.label(), strings.Join(names, ", ")))
			}
		case "DM_REMOVED":
			out("👥 vous ne faites plus partie d'un groupe")
		case "DEVICES_UPDATE":
			var d struct {
				UserID string `json:"user_id"`
			}
			json.Unmarshal(ev.D, &d)
			if d.UserID == me {
				out("🔑 la liste de vos appareils a changé (quarelctl devices)")
			}
		}
	}
}

// handleFileSignals answers file transfers while dm-listen runs: fetches the
// files just offered by other devices, and serves the ones this device holds.
func (c *cli) handleFileSignals(out func(string)) {
	var sigs []fileSignal
	c.withE2E(func(e *e2e) error {
		sigs = e.takeFileSignals(func(s fileSignal) bool { return s.Action == "offer" || s.Action == "fetch" })
		return nil
	})
	for _, s := range sigs {
		if s.Action == "offer" {
			go c.autoFetch(s, out)
		} else {
			go func(s fileSignal) {
				if c.serveFetch(s) {
					out(fmt.Sprintf("📎 fichier envoyé en direct à un appareil de %s", c.nameOf(s.FromUser)))
				}
			}(s)
		}
	}
}
