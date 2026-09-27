package main

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type roleInfo struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Color       int64    `json:"color"`
	Position    int64    `json:"position"`
	Permissions []string `json:"permissions"`
	Mentionable bool     `json:"mentionable"`
	Hoist       bool     `json:"hoist"`
}

type overrideInfo struct {
	Type  string   `json:"type"`
	ID    string   `json:"id"`
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// permissionHelp documents the permission names accepted by the server.
const permissionHelp = `Permissions (noms à utiliser dans les commandes) :
  view_channel       voir un salon et lire son historique        (salon)
  send_messages      écrire dans un salon texte                  (salon)
  manage_messages    supprimer les messages des autres           (salon)
  mention_everyone   @everyone et mentionner tous les rôles      (salon)
  manage_channels    créer, modifier, supprimer des salons       (salon)
  manage_webhooks    créer, supprimer les webhooks d'un salon    (salon)
  connect, speak     rejoindre un salon vocal / y parler         (salon, jalon 4)
  create_invite      créer des invitations
  manage_roles       gérer les rôles inférieurs, les rôles des membres, les droits par salon
  kick_members       expulser un membre de rang inférieur
  ban_members        bannir un membre de rang inférieur
  manage_server      réglages du serveur, toutes les invitations
  administrator      toutes les permissions, ignore les droits par salon
« (salon) » : permission réglable salon par salon avec la commande override.
`

func (c *cli) runRoles(cmd string, args []string) (bool, error) {
	var err error
	switch cmd {
	case "permissions":
		fmt.Print(permissionHelp)
	case "my-perms":
		err = c.myPerms()
	case "roles":
		err = c.printRoles()
	case "role-create":
		err = c.roleCreate(args)
	case "role-edit":
		err = c.roleEdit(args)
	case "role-delete":
		err = c.roleDelete(args)
	case "role-add", "role-remove":
		err = c.memberRole(cmd == "role-add", args)
	case "override":
		err = c.setOverride(args, false)
	case "override-clear":
		err = c.setOverride(args, true)
	case "kick":
		err = c.kick(args)
	case "transfer-owner":
		err = c.transferOwner(args)
	case "ban":
		err = c.ban(args)
	case "unban":
		err = c.unban(args)
	case "bans":
		err = c.printBans()
	default:
		return c.runVoice(cmd, args)
	}
	return true, err
}

func (c *cli) roles() ([]roleInfo, error) {
	var list []roleInfo
	return list, c.cdo("GET", "/v1/roles", nil, &list)
}

// resolveRole finds a role by id or name (case-insensitive); "everyone" means @everyone.
func resolveRole(list []roleInfo, arg string) (*roleInfo, error) {
	arg = strings.TrimPrefix(strings.TrimPrefix(arg, "@&"), "@")
	if id, err := strconv.ParseInt(arg, 10, 64); err == nil {
		for _, r := range list {
			if r.ID == id {
				return &r, nil
			}
		}
	}
	for _, r := range list {
		if strings.EqualFold(strings.TrimPrefix(r.Name, "@"), arg) {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("rôle %q introuvable (voir quarelctl roles)", arg)
}

// resolveMember finds an active member by id, display name or pseudo; a raw
// 26-character ID is accepted as is (e.g. a member who left, to ban them).
func resolveMember(members map[string]memberInfo, arg string) (string, error) {
	arg = strings.TrimPrefix(arg, "@")
	if _, ok := members[arg]; ok {
		return arg, nil
	}
	for id, m := range members {
		pseudo, _, _ := strings.Cut(m.Handle, "@")
		if strings.EqualFold(m.DisplayName, arg) || strings.EqualFold(pseudo, arg) || strings.EqualFold(m.Handle, arg) {
			return id, nil
		}
	}
	if len(arg) == 26 {
		return arg, nil
	}
	return "", fmt.Errorf("membre %q introuvable (voir quarelctl members)", arg)
}

func roleNames(ids []int64, roles []roleInfo) string {
	names := []string{}
	for _, id := range ids {
		for _, r := range roles {
			if r.ID == id {
				names = append(names, r.Name)
			}
		}
	}
	return strings.Join(names, ", ")
}

func (c *cli) myPerms() error {
	var out struct {
		Server   []string            `json:"server"`
		Channels map[string][]string `json:"channels"`
	}
	if err := c.cdo("GET", "/v1/members/@me/permissions", nil, &out); err != nil {
		return err
	}
	list, err := c.channels()
	if err != nil {
		return err
	}
	fmt.Printf("Sur le serveur : %s\n", strings.Join(out.Server, ", "))
	for _, ch := range list {
		fmt.Printf("  %s %-20s %s\n", typeIcon[ch.Type], ch.Name, strings.Join(out.Channels[fmt.Sprint(ch.ID)], ", "))
	}
	return nil
}

func (c *cli) printRoles() error {
	list, err := c.roles()
	if err != nil {
		return err
	}
	for _, r := range list {
		extra := ""
		if r.Color != 0 {
			extra += fmt.Sprintf(" couleur #%06x", r.Color)
		}
		if r.Mentionable {
			extra += " mentionnable"
		}
		perms := strings.Join(r.Permissions, ", ")
		if perms == "" {
			perms = "(aucune)"
		}
		fmt.Printf("%2d. %-20s (id %d)%s\n      %s\n", r.Position, r.Name, r.ID, extra, perms)
	}
	return nil
}

func splitList(v string) []string {
	out := []string{}
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseColor(v string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(v, "#"), 16, 64)
}

func (c *cli) roleCreate(args []string) error {
	if err := need(args, 1, "<nom> [permission…]"); err != nil {
		return err
	}
	var r roleInfo
	body := map[string]any{"name": args[0], "permissions": args[1:]}
	if err := c.cdo("POST", "/v1/roles", body, &r); err != nil {
		return err
	}
	fmt.Printf("Rôle créé : %s (id %d, position %d) — %s\n", r.Name, r.ID, r.Position, strings.Join(r.Permissions, ", "))
	return nil
}

func (c *cli) roleEdit(args []string) error {
	if err := need(args, 2, "<rôle> clé=valeur…"); err != nil {
		return err
	}
	list, err := c.roles()
	if err != nil {
		return err
	}
	rl, err := resolveRole(list, args[0])
	if err != nil {
		return err
	}
	kv, err := keyValues(args[1:])
	if err != nil {
		return err
	}
	body := map[string]any{}
	for k, v := range kv {
		switch k {
		case "name":
			body["name"] = v
		case "color":
			col, err := parseColor(v)
			if err != nil {
				return fmt.Errorf("couleur : format #RRGGBB attendu")
			}
			body["color"] = col
		case "perms":
			body["permissions"] = splitList(v)
		case "mentionable":
			body["mentionable"] = v == "true" || v == "oui" || v == "1"
		case "hoist", "separe":
			body["hoist"] = v == "true" || v == "oui" || v == "1"
		case "position":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("position : nombre attendu")
			}
			body["position"] = n
		default:
			return fmt.Errorf("champ inconnu %q (name, color, perms, mentionable, hoist, position)", k)
		}
	}
	var r roleInfo
	if err := c.cdo("PATCH", fmt.Sprint("/v1/roles/", rl.ID), body, &r); err != nil {
		return err
	}
	fmt.Printf("Rôle modifié : %s (position %d) — %s\n", r.Name, r.Position, strings.Join(r.Permissions, ", "))
	return nil
}

func (c *cli) roleDelete(args []string) error {
	if err := need(args, 1, "<rôle>"); err != nil {
		return err
	}
	list, err := c.roles()
	if err != nil {
		return err
	}
	rl, err := resolveRole(list, args[0])
	if err != nil {
		return err
	}
	if err := c.cdo("DELETE", fmt.Sprint("/v1/roles/", rl.ID), nil, nil); err != nil {
		return err
	}
	fmt.Printf("Rôle %s supprimé.\n", rl.Name)
	return nil
}

func (c *cli) memberRole(add bool, args []string) error {
	if err := need(args, 2, "<membre> <rôle>"); err != nil {
		return err
	}
	members, err := c.members()
	if err != nil {
		return err
	}
	mid, err := resolveMember(members, args[0])
	if err != nil {
		return err
	}
	list, err := c.roles()
	if err != nil {
		return err
	}
	rl, err := resolveRole(list, args[1])
	if err != nil {
		return err
	}
	method, verb := "PUT", "ajouté à"
	if !add {
		method, verb = "DELETE", "retiré de"
	}
	var m memberInfo
	if err := c.cdo(method, fmt.Sprintf("/v1/members/%s/roles/%d", mid, rl.ID), nil, &m); err != nil {
		return err
	}
	fmt.Printf("Rôle %s %s %s. Rôles : %s\n", rl.Name, verb, m.DisplayName, roleNames(m.Roles, list))
	return nil
}

// setOverride: override <salon> <role:NOM|member:PSEUDO> allow=a,b deny=c
func (c *cli) setOverride(args []string, clear bool) error {
	usage := "<salon> <role:NOM|member:PSEUDO> allow=perm,… deny=perm,…"
	if clear {
		usage = "<salon> <role:NOM|member:PSEUDO>"
	}
	if err := need(args, 2, usage); err != nil {
		return err
	}
	chans, err := c.channels()
	if err != nil {
		return err
	}
	ch, err := resolveChannel(chans, args[0], "")
	if err != nil {
		return err
	}
	typ, name, ok := strings.Cut(args[1], ":")
	if !ok || (typ != "role" && typ != "member") {
		return fmt.Errorf("cible attendue : role:NOM ou member:PSEUDO (ex. role:everyone)")
	}
	var target string
	if typ == "role" {
		list, err := c.roles()
		if err != nil {
			return err
		}
		rl, err := resolveRole(list, name)
		if err != nil {
			return err
		}
		target = fmt.Sprint(rl.ID)
	} else {
		members, err := c.members()
		if err != nil {
			return err
		}
		if target, err = resolveMember(members, name); err != nil {
			return err
		}
	}
	path := fmt.Sprintf("/v1/channels/%d/overrides/%s/%s", ch.ID, typ, url.PathEscape(target))
	if clear {
		if err := c.cdo("DELETE", path, nil, nil); err != nil {
			return err
		}
		fmt.Printf("Droits particuliers de %s retirés sur %s.\n", args[1], ch.Name)
		return nil
	}
	kv, err := keyValues(args[2:])
	if err != nil {
		return err
	}
	body := map[string][]string{"allow": {}, "deny": {}}
	for k, v := range kv {
		if k != "allow" && k != "deny" {
			return fmt.Errorf("champ inconnu %q (allow, deny)", k)
		}
		body[k] = splitList(v)
	}
	if err := c.cdo("PUT", path, body, nil); err != nil {
		return err
	}
	fmt.Printf("Sur %s pour %s : autorisé [%s], refusé [%s]\n", ch.Name, args[1], strings.Join(body["allow"], ", "), strings.Join(body["deny"], ", "))
	return nil
}

func (c *cli) moderate(args []string) (string, string, error) {
	if err := need(args, 1, "<membre> [raison…]"); err != nil {
		return "", "", err
	}
	members, err := c.members()
	if err != nil {
		return "", "", err
	}
	id, err := resolveMember(members, args[0])
	if err != nil {
		return "", "", err
	}
	name := args[0]
	if m, ok := members[id]; ok {
		name = m.DisplayName
	}
	return id, name, nil
}

// transferOwner hands the server over to another member (owner only).
func (c *cli) transferOwner(args []string) error {
	id, name, err := c.moderate(args)
	if err != nil {
		return err
	}
	if err := c.cdo("POST", "/v1/members/"+id+"/transfer-ownership", nil, nil); err != nil {
		return err
	}
	fmt.Printf("%s est maintenant propriétaire du serveur ; vous restez membre.\n", name)
	return nil
}

func (c *cli) kick(args []string) error {
	id, name, err := c.moderate(args)
	if err != nil {
		return err
	}
	if err := c.cdo("POST", "/v1/members/"+id+"/kick", map[string]string{"reason": strings.Join(args[1:], " ")}, nil); err != nil {
		return err
	}
	fmt.Printf("Expulsion de %s effectuée (retour possible avec une invitation).\n", name)
	return nil
}

// ban <membre> [--purge=durée|tout] [raison…]
func (c *cli) ban(args []string) error {
	id, name, err := c.moderate(args)
	if err != nil {
		return err
	}
	body := map[string]any{}
	reason := args[1:]
	if len(reason) > 0 && strings.HasPrefix(reason[0], "--purge=") {
		v := strings.TrimPrefix(reason[0], "--purge=")
		body["delete_messages"] = -1
		if v != "tout" && v != "all" {
			d, err := parseDuration(v)
			if err != nil || d <= 0 {
				return fmt.Errorf("durée invalide %q (ex. 1h, 7d, ou « tout »)", v)
			}
			body["delete_messages"] = int64(d.Seconds())
		}
		reason = reason[1:]
	}
	body["reason"] = strings.Join(reason, " ")
	if err := c.cdo("PUT", "/v1/bans/"+id, body, nil); err != nil {
		return err
	}
	fmt.Printf("Bannissement de %s effectué : cette identité ne pourra plus rejoindre le serveur.\n", name)
	return nil
}

type banInfo struct {
	Member    memberInfo `json:"member"`
	Reason    string     `json:"reason"`
	BannedBy  *string    `json:"banned_by"`
	CreatedAt time.Time  `json:"created_at"`
}

func (c *cli) bans() ([]banInfo, error) {
	var list []banInfo
	return list, c.cdo("GET", "/v1/bans", nil, &list)
}

func (c *cli) unban(args []string) error {
	if err := need(args, 1, "<membre>"); err != nil {
		return err
	}
	list, err := c.bans()
	if err != nil {
		return err
	}
	arg := strings.TrimPrefix(args[0], "@")
	for _, b := range list {
		pseudo, _, _ := strings.Cut(b.Member.Handle, "@")
		if b.Member.ID == arg || strings.EqualFold(pseudo, arg) || strings.EqualFold(b.Member.DisplayName, arg) {
			if err := c.cdo("DELETE", "/v1/bans/"+b.Member.ID, nil, nil); err != nil {
				return err
			}
			fmt.Printf("Bannissement levé pour %s (une invitation reste nécessaire si le serveur est privé).\n", b.Member.Handle)
			return nil
		}
	}
	return fmt.Errorf("aucun bannissement pour %q (voir quarelctl bans)", arg)
}

func (c *cli) printBans() error {
	list, err := c.bans()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("Aucun bannissement.")
		return nil
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	for _, b := range list {
		reason := b.Reason
		if reason == "" {
			reason = "(sans raison)"
		}
		fmt.Printf("%-32s bannissement du %s — %s  (id %s)\n", b.Member.Handle, b.CreatedAt.Local().Format("2006-01-02 15:04"), reason, b.Member.ID)
	}
	return nil
}
