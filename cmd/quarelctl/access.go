package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Timeouts, purges, audit log, rules screen, phone verification and bots.

func (c *cli) runAccess(cmd string, args []string) (bool, error) {
	var err error
	switch cmd {
	case "timeout":
		err = c.timeout(args)
	case "untimeout":
		err = c.untimeout(args)
	case "purge":
		err = c.purgeCmd(args)
	case "audit":
		err = c.auditLog(args)
	case "rules":
		err = c.rules(args)
	case "accept-rules":
		err = c.acceptRules()
	case "phone":
		err = c.phoneStart(args)
	case "phone-verify":
		err = c.phoneVerify(args)
	case "bot-create":
		err = c.botCreate(args)
	case "bots":
		err = c.listBots()
	case "bot-token":
		err = c.botToken(args)
	case "bot-delete":
		err = c.botDelete(args)
	default:
		return c.runRoles(cmd, args)
	}
	return true, err
}

func (c *cli) timeout(args []string) error {
	if err := need(args, 2, "<membre> <durée> [raison…]"); err != nil {
		return err
	}
	id, name, err := c.moderate(args)
	if err != nil {
		return err
	}
	d, err := parseDuration(args[1])
	if err != nil || d <= 0 {
		return fmt.Errorf("durée invalide %q (ex. 10m, 2h, 7d)", args[1])
	}
	var m memberInfo
	body := map[string]any{"duration": int64(d.Seconds()), "reason": strings.Join(args[2:], " ")}
	if err := c.cdo("PUT", "/v1/members/"+id+"/timeout", body, &m); err != nil {
		return err
	}
	fmt.Printf("Exclusion temporaire de %s jusqu'au %s : lecture seule, vocal coupé.\n", name, m.TimeoutUntil.Local().Format("2006-01-02 15:04"))
	return nil
}

func (c *cli) untimeout(args []string) error {
	id, name, err := c.moderate(args)
	if err != nil {
		return err
	}
	if err := c.cdo("DELETE", "/v1/members/"+id+"/timeout", nil, nil); err != nil {
		return err
	}
	fmt.Printf("Fin de l'exclusion temporaire de %s.\n", name)
	return nil
}

// purgeCmd <membre> <durée|tout> [salon]
func (c *cli) purgeCmd(args []string) error {
	if err := need(args, 2, "<membre> <durée|tout> [salon]"); err != nil {
		return err
	}
	id, name, err := c.moderate(args)
	if err != nil {
		return err
	}
	body := map[string]any{"window": -1}
	if args[1] != "tout" && args[1] != "all" {
		d, err := parseDuration(args[1])
		if err != nil || d <= 0 {
			return fmt.Errorf("durée invalide %q (ex. 1h, 7d, ou « tout »)", args[1])
		}
		body["window"] = int64(d.Seconds())
	}
	if len(args) > 2 {
		ch, err := c.textChannel(args[2])
		if err != nil {
			return err
		}
		body["channel_id"] = ch.ID
	}
	var out struct {
		Deleted int `json:"deleted"`
	}
	if err := c.cdo("POST", "/v1/members/"+id+"/purge", body, &out); err != nil {
		return err
	}
	fmt.Printf("%d message(s) de %s supprimé(s).\n", out.Deleted, name)
	return nil
}

var auditLabels = map[string]string{
	"member_kick": "expulsion", "member_ban": "bannissement", "member_unban": "levée de bannissement",
	"member_timeout": "exclusion temporaire", "member_timeout_remove": "fin d'exclusion",
	"member_role_add": "rôle donné", "member_role_remove": "rôle retiré", "messages_delete": "messages supprimés",
	"role_create": "rôle créé", "role_update": "rôle modifié", "role_delete": "rôle supprimé",
	"channel_create": "salon créé", "channel_update": "salon modifié", "channel_delete": "salon supprimé",
	"override_update": "droits de salon modifiés", "override_delete": "droits de salon retirés",
	"server_update": "réglages du serveur", "invite_delete": "invitation d'un autre révoquée",
	"bot_create": "bot créé", "bot_delete": "bot supprimé", "bot_token_reset": "jeton de bot renouvelé",
	"voice_mute": "micro coupé (modération)", "voice_deafen": "son coupé (modération)",
	"voice_move": "déplacement vocal", "voice_disconnect": "déconnexion vocale",
}

// auditLog [nombre] [action]
func (c *cli) auditLog(args []string) error {
	q := url.Values{}
	if len(args) > 0 {
		q.Set("limit", args[0])
	}
	if len(args) > 1 {
		q.Set("action", args[1])
	}
	var list []struct {
		ID        int64          `json:"id"`
		ActorID   *string        `json:"actor_id"`
		Action    string         `json:"action"`
		TargetID  *string        `json:"target_id"`
		Reason    string         `json:"reason"`
		Details   map[string]any `json:"details"`
		CreatedAt time.Time      `json:"created_at"`
	}
	if err := c.cdo("GET", "/v1/audit-log?"+q.Encode(), nil, &list); err != nil {
		return err
	}
	members, err := c.members()
	if err != nil {
		return err
	}
	name := func(id *string) string {
		if id == nil {
			return "-"
		}
		if m, ok := members[*id]; ok {
			return m.DisplayName
		}
		return *id
	}
	if len(list) == 0 {
		fmt.Println("Journal vide.")
	}
	for _, e := range list {
		label := auditLabels[e.Action]
		if label == "" {
			label = e.Action
		}
		if e.Details["enabled"] == false {
			label = map[string]string{"voice_mute": "micro rétabli (modération)", "voice_deafen": "son rétabli (modération)"}[e.Action]
		}
		line := fmt.Sprintf("[%s] %s — %s", e.CreatedAt.Local().Format("01-02 15:04"), name(e.ActorID), label)
		if e.TargetID != nil {
			line += " → " + name(e.TargetID)
		}
		if e.Reason != "" {
			line += " « " + e.Reason + " »"
		}
		if n, ok := e.Details["count"]; ok {
			line += fmt.Sprintf(" (%v)", n)
		}
		fmt.Println(line)
	}
	return nil
}

// rules: show; rules <texte…>: set; rules --clear: remove; rules -f fichier: from a file.
func (c *cli) rules(args []string) error {
	if len(args) == 0 {
		var info serverInfo
		if err := c.cdo("GET", "/v1/server", nil, &info); err != nil {
			return err
		}
		if info.Rules == "" {
			fmt.Println("Pas de règles à accepter sur ce serveur.")
			return nil
		}
		fmt.Println(info.Rules)
		return nil
	}
	text := strings.Join(args, " ")
	switch {
	case args[0] == "--clear":
		text = ""
	case args[0] == "-f" && len(args) > 1:
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		text = string(data)
	}
	var info serverInfo
	if err := c.cdo("PATCH", "/v1/server", map[string]string{"rules": text}, &info); err != nil {
		return err
	}
	if info.Rules == "" {
		fmt.Println("Règles retirées.")
	} else {
		fmt.Println("Règles enregistrées : les nouveaux membres devront les accepter (les membres actuels ne sont pas bloqués).")
	}
	return nil
}

func (c *cli) acceptRules() error {
	if err := c.cdo("POST", "/v1/members/@me/accept-rules", nil, nil); err != nil {
		return err
	}
	fmt.Println("Règles acceptées : vous pouvez participer.")
	return nil
}

func (c *cli) phoneStart(args []string) error {
	if err := need(args, 1, "<numéro international, ex. +33612345678>"); err != nil {
		return err
	}
	if err := c.cdo("POST", "/v1/members/@me/phone", map[string]string{"phone": strings.Join(args, "")}, nil); err != nil {
		return err
	}
	fmt.Println("Code envoyé par SMS. Puis : quarelctl phone-verify <numéro> <code>")
	return nil
}

func (c *cli) phoneVerify(args []string) error {
	if err := need(args, 2, "<numéro> <code>"); err != nil {
		return err
	}
	code := args[len(args)-1]
	phone := strings.Join(args[:len(args)-1], "")
	if err := c.cdo("POST", "/v1/members/@me/phone/verify", map[string]string{"phone": phone, "code": code}, nil); err != nil {
		return err
	}
	fmt.Println("Numéro vérifié. Le serveur ne garde qu'une empreinte du numéro, pas le numéro lui-même.")
	return nil
}

type botInfo struct {
	Member memberInfo `json:"member"`
	Token  string     `json:"token"`
}

func (c *cli) printBotToken(b botInfo) error {
	base, com, err := c.current()
	if err != nil {
		return err
	}
	fmt.Printf("\nJeton du bot (affiché une seule fois, à garder secret) :\n  %s\n\n", b.Token)
	fmt.Printf("Pour lancer le bot d'exemple :\n  QUAREL_URL=%s QUAREL_SERVER_ID=%s QUAREL_BOT_TOKEN=%s go run ./examples/pingbot\n", base, com.ServerID, b.Token)
	return nil
}

func (c *cli) botCreate(args []string) error {
	if err := need(args, 1, "<nom>"); err != nil {
		return err
	}
	var b botInfo
	if err := c.cdo("POST", "/v1/bots", map[string]string{"name": strings.Join(args, " ")}, &b); err != nil {
		return err
	}
	fmt.Printf("Bot « %s » créé (id %s). Donnez-lui des droits avec role-add.\n", b.Member.DisplayName, b.Member.ID)
	return c.printBotToken(b)
}

func (c *cli) listBots() error {
	var list []botInfo
	if err := c.cdo("GET", "/v1/bots", nil, &list); err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("Aucun bot.")
	}
	for _, b := range list {
		fmt.Printf("🤖 %-20s id %s\n", b.Member.DisplayName, b.Member.ID)
	}
	return nil
}

func (c *cli) botID(arg string) (string, error) {
	var list []botInfo
	if err := c.cdo("GET", "/v1/bots", nil, &list); err != nil {
		return "", err
	}
	for _, b := range list {
		if b.Member.ID == arg || strings.EqualFold(b.Member.DisplayName, arg) {
			return b.Member.ID, nil
		}
	}
	return "", fmt.Errorf("bot %q introuvable (voir quarelctl bots)", arg)
}

func (c *cli) botToken(args []string) error {
	if err := need(args, 1, "<bot>"); err != nil {
		return err
	}
	id, err := c.botID(args[0])
	if err != nil {
		return err
	}
	var b botInfo
	if err := c.cdo("POST", "/v1/bots/"+id+"/token", nil, &b); err != nil {
		return err
	}
	fmt.Println("Nouveau jeton créé : l'ancien ne fonctionne plus.")
	return c.printBotToken(b)
}

func (c *cli) botDelete(args []string) error {
	if err := need(args, 1, "<bot>"); err != nil {
		return err
	}
	id, err := c.botID(args[0])
	if err != nil {
		return err
	}
	if err := c.cdo("DELETE", "/v1/bots/"+id, nil, nil); err != nil {
		return err
	}
	fmt.Println("Bot supprimé.")
	return nil
}
