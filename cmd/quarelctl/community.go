package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// community is the local state for one joined community server.
type community struct {
	ServerID     string    `json:"server_id"` // pinned at first join; a change means another server answers
	Name         string    `json:"name"`
	SessionToken string    `json:"session_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

type channelInfo struct {
	ID        int64          `json:"id"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Topic     string         `json:"topic"`
	ParentID  *int64         `json:"parent_id"`
	Position  int64          `json:"position"`
	Overrides []overrideInfo `json:"overrides"`
}

type memberInfo struct {
	ID            string     `json:"id"`
	Handle        string     `json:"handle"`
	DisplayName   string     `json:"display_name"`
	Owner         bool       `json:"owner"`
	Roles         []int64    `json:"roles"`
	JoinedAt      time.Time  `json:"joined_at"`
	Bot           bool       `json:"bot"`
	TimeoutUntil  *time.Time `json:"timeout_until"`
	RulesAccepted bool       `json:"rules_accepted"`
	PhoneVerified bool       `json:"phone_verified"`
}

type messageInfo struct {
	ID              int64      `json:"id"`
	ChannelID       int64      `json:"channel_id"`
	AuthorID        string     `json:"author_id"`
	Content         string     `json:"content"`
	Mentions        []string   `json:"mentions"`
	MentionEveryone bool       `json:"mention_everyone"`
	CreatedAt       time.Time  `json:"created_at"`
	EditedAt        *time.Time `json:"edited_at"`
	ReplyTo         *int64     `json:"reply_to"`
	Referenced      *struct {
		AuthorID string `json:"author_id"`
		Content  string `json:"content"`
	} `json:"referenced"`
	Attachments []attachmentInfo `json:"attachments"`
	Embeds      []struct {
		URL, Title, Description string
		SiteName                string `json:"site_name"`
	} `json:"embeds"`
	Reactions []struct {
		Emoji string `json:"emoji"`
		Count int    `json:"count"`
		Me    bool   `json:"me"`
	} `json:"reactions"`
	PinnedAt *time.Time `json:"pinned_at"`
	ThreadID *int64     `json:"thread_id"`
}

type attachmentInfo struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
}

type serverInfo struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Access            string `json:"access"`
	MemberCount       int    `json:"member_count"`
	Rules             string `json:"rules"`
	RequirePhone      bool   `json:"require_phone"`
	PhoneVerification bool   `json:"phone_verification"`
}

func (c *cli) runCommunity(cmd string, args []string) (bool, error) {
	var err error
	switch cmd {
	case "srv-info":
		err = c.srvInfo(args)
	case "join":
		if err = need(args, 1, "<url|lien>"); err == nil {
			invite := ""
			if len(args) > 1 {
				invite = args[1]
			}
			err = c.joinCmd(args[0], invite, "")
		}
	case "claim":
		if err = need(args, 2, "<url> <code>"); err == nil {
			err = c.joinCmd(args[0], "", args[1])
		}
	case "servers":
		err = c.listServers()
	case "use":
		if err = need(args, 1, "<url>"); err == nil {
			base, _, _, _ := parseTarget(args[0])
			if c.st.Communities[base] == nil {
				return true, fmt.Errorf("serveur %s non rejoint (voir quarelctl servers)", base)
			}
			c.st.Current = base
			fmt.Println("Serveur courant :", base)
			err = c.save()
		}
	case "srv-set":
		err = c.srvSet(args)
	case "network":
		err = c.printNetwork()
	case "leave":
		err = c.leave()
	case "channels":
		err = c.printChannels()
	case "channel-create":
		err = c.channelCreate(args)
	case "channel-edit":
		err = c.channelEdit(args)
	case "channel-delete":
		err = c.channelDelete(args)
	case "send":
		err = c.send(args)
	case "history":
		err = c.history(args)
	case "edit":
		err = c.editMessage(args)
	case "delete":
		err = c.deleteMessage(args)
	case "listen":
		err = c.listen()
	case "reply":
		err = c.reply(args)
	case "react", "unreact":
		err = c.react(cmd == "react", args)
	case "pin", "unpin":
		err = c.pin(cmd == "pin", args)
	case "pins":
		err = c.pins(args)
	case "send-file":
		err = c.sendFile(args)
	case "download":
		err = c.downloadCmd(args)
	case "search":
		err = c.search(args)
	case "thread":
		err = c.thread(args)
	case "unread":
		err = c.unread()
	case "read":
		err = c.markRead(args)
	case "typing":
		err = c.typing(args)
	case "notify":
		err = c.notify(args)
	case "members":
		err = c.printMembers()
	case "srv-profile":
		err = c.srvProfile(args)
	case "srv-theme":
		err = c.srvTheme(args)
	case "nick":
		err = c.nick(args)
	case "invite":
		err = c.createInvite(args)
	case "invites":
		err = c.listInvites()
	case "invite-revoke":
		if err = need(args, 1, "<code>"); err == nil {
			if err = c.cdo("DELETE", "/v1/invites/"+args[0], nil, nil); err == nil {
				fmt.Println("Invitation révoquée.")
			}
		}
	default:
		return c.runAccess(cmd, args)
	}
	return true, err
}

// parseTarget accepts a server URL (HTTPS by default) or an invite link
// quarel://host:port/CODE?sid=ID.
func parseTarget(arg string) (base, code, sid string, err error) {
	// Web invite links: https://<web app>/join#host:port/CODE?sid=…
	if _, frag, ok := strings.Cut(arg, "/join#"); ok && strings.HasPrefix(arg, "http") {
		if f, err := url.PathUnescape(frag); err == nil {
			arg = "quarel://" + f
		}
	}
	if strings.HasPrefix(arg, "quarel://") {
		u, err := url.Parse(arg)
		if err != nil {
			return "", "", "", fmt.Errorf("lien d'invitation invalide : %w", err)
		}
		return "https://" + u.Host, strings.Trim(u.Path, "/"), u.Query().Get("sid"), nil
	}
	if !strings.Contains(arg, "://") {
		arg = "https://" + arg
	}
	return strings.TrimRight(arg, "/"), "", "", nil
}

func (c *cli) srvInfo(args []string) error {
	base := c.st.Current
	if len(args) > 0 {
		base, _, _, _ = parseTarget(args[0])
	}
	if base == "" {
		return errors.New("précisez l'adresse du serveur")
	}
	var info serverInfo
	if err := c.request(base, "", "GET", "/v1/server", nil, &info); err != nil {
		return err
	}
	access := map[string]string{"private": "privé (sur invitation)", "public": "public"}[info.Access]
	fmt.Printf("%s\n  adresse : %s\n  id      : %s\n  accès   : %s\n  membres : %d\n", info.Name, base, info.ID, access, info.MemberCount)
	if info.RequirePhone {
		fmt.Println("  exige un numéro de téléphone vérifié")
	}
	if info.Rules != "" {
		fmt.Printf("  règles  :\n%s\n", indent(info.Rules))
	}
	return nil
}

type loginResult struct {
	SessionToken string     `json:"session_token"`
	ExpiresAt    time.Time  `json:"expires_at"`
	Member       memberInfo `json:"member"`
	Server       serverInfo `json:"server"`
	Joined       bool       `json:"joined"`
}

// communityLogin proves our identity to a community server and stores the session.
func (c *cli) communityLogin(base, invite, claim, expectSID string) (*loginResult, error) {
	if c.st.SessionToken == "" {
		return nil, errors.New("connectez-vous d'abord au service Identity : quarelctl login <email|pseudo>")
	}
	var ch struct {
		ServerID string `json:"server_id"`
		Nonce    string `json:"nonce"`
	}
	if err := c.request(base, "", "POST", "/v1/auth/challenge", nil, &ch); err != nil {
		return nil, err
	}
	if known := c.st.Communities[base]; known != nil && known.ServerID != ch.ServerID {
		return nil, fmt.Errorf("ATTENTION : l'identifiant du serveur %s a changé (%s → %s) ; connexion refusée", base, known.ServerID, ch.ServerID)
	}
	if expectSID != "" && expectSID != ch.ServerID {
		return nil, fmt.Errorf("le lien d'invitation désigne le serveur %s mais %s répond avec l'identifiant %s ; connexion refusée", expectSID, base, ch.ServerID)
	}
	// The proof names where we connected and what the certificates proved:
	// a server relaying our login elsewhere cannot use it.
	mode, seen := c.tlsMode(base)
	switch {
	case mode == "":
		return nil, fmt.Errorf("ATTENTION : %s a présenté tantôt un certificat ordinaire, tantôt un certificat lié à son identité ; connexion refusée (possible interception)", base)
	case mode == idtoken.TLSBinding && seen != ch.ServerID:
		// The identity proven by the TLS certificate must be the one the server claims.
		return nil, fmt.Errorf("ATTENTION : le certificat de %s prouve l'identité %s mais le serveur annonce %s ; connexion refusée (possible interception)", base, seen, ch.ServerID)
	}
	u, _ := url.Parse(base)
	tok, err := c.tokenFor(ch.ServerID)
	if err != nil {
		return nil, fmt.Errorf("obtention du jeton d'identité : %w", err)
	}
	var res loginResult
	err = c.request(base, "", "POST", "/v1/auth/login", map[string]string{
		"identity_token": tok,
		"nonce":          ch.Nonce,
		"proof":          idtoken.SignProofV2(c.device(), ch.ServerID, ch.Nonce, u.Hostname(), mode),
		"host":           idtoken.NormalizeHost(u.Hostname()),
		"tls":            mode,
		"invite":         invite,
		"claim":          claim,
	}, &res)
	if err != nil {
		return nil, err
	}
	if c.st.Communities == nil {
		c.st.Communities = map[string]*community{}
	}
	c.st.Communities[base] = &community{ServerID: ch.ServerID, Name: res.Server.Name, SessionToken: res.SessionToken, ExpiresAt: res.ExpiresAt}
	return &res, c.save()
}

// tlsMode tells how the connections to base were checked (see
// tlsbind.Verifier.Mode). Plain HTTP (development, on this machine) counts as
// "authority": the server then checks the host name.
func (c *cli) tlsMode(base string) (mode, sid string) {
	if !strings.HasPrefix(base, "https://") {
		return idtoken.TLSAuthority, ""
	}
	c.httpClient(base)
	return c.verifiers[base].Mode()
}

func (c *cli) joinCmd(target, invite, claim string) error {
	base, code, sid, err := parseTarget(target)
	if err != nil {
		return err
	}
	if invite == "" {
		invite = code
	}
	if sid != "" {
		c.expectSID[base] = sid // checked during the TLS handshake already
	}
	res, err := c.communityLogin(base, invite, claim, sid)
	if err != nil {
		return err
	}
	c.st.Current = base
	switch {
	case claim != "":
		fmt.Printf("Vous êtes maintenant propriétaire de « %s ».\n", res.Server.Name)
	case res.Joined:
		fmt.Printf("Bienvenue sur « %s » (%d membres) !\n", res.Server.Name, res.Server.MemberCount)
	default:
		fmt.Printf("Reconnexion à « %s ».\n", res.Server.Name)
	}
	fmt.Printf("Serveur courant : %s — session valable jusqu'au %s\n", base, res.ExpiresAt.Local().Format("2006-01-02 15:04"))
	if res.Server.Rules != "" && !res.Member.RulesAccepted {
		fmt.Printf("\nRègles du serveur, à accepter avant de participer (lecture seule d'ici là) :\n%s\n→ quarelctl accept-rules\n", indent(res.Server.Rules))
	}
	if res.Server.RequirePhone && !res.Member.PhoneVerified {
		fmt.Println("\nCe serveur exige un numéro de téléphone vérifié : quarelctl phone +33612345678, puis quarelctl phone-verify <numéro> <code>")
	}
	return c.save()
}

func indent(s string) string { return "  " + strings.ReplaceAll(s, "\n", "\n  ") }

func (c *cli) current() (string, *community, error) {
	base := c.community
	if base == "" {
		base = c.st.Current
	} else {
		base, _, _, _ = parseTarget(base)
	}
	com := c.st.Communities[base]
	if base == "" || com == nil {
		return "", nil, errors.New("aucun serveur courant : rejoignez-en un avec quarelctl join <url> [invitation]")
	}
	return base, com, nil
}

// cdo calls the current community server, logging in again once if the session expired.
func (c *cli) cdo(method, path string, body, out any) error {
	base, com, err := c.current()
	if err != nil {
		return err
	}
	err = c.request(base, com.SessionToken, method, path, body, out)
	var ae *apiErr
	if errors.As(err, &ae) && ae.Code == "unauthorized" {
		if _, lerr := c.communityLogin(base, "", "", com.ServerID); lerr != nil {
			return fmt.Errorf("session expirée et reconnexion impossible : %w", lerr)
		}
		return c.request(base, c.st.Communities[base].SessionToken, method, path, body, out)
	}
	return err
}

func (c *cli) listServers() error {
	if len(c.st.Communities) == 0 {
		fmt.Println("Aucun serveur rejoint.")
		return nil
	}
	bases := make([]string, 0, len(c.st.Communities))
	for b := range c.st.Communities {
		bases = append(bases, b)
	}
	sort.Strings(bases)
	for _, b := range bases {
		mark := "  "
		if b == c.st.Current {
			mark = "* "
		}
		com := c.st.Communities[b]
		fmt.Printf("%s%-30s %-24s id %s\n", mark, b, com.Name, com.ServerID)
	}
	return nil
}

func keyValues(args []string) (map[string]string, error) {
	kv := map[string]string{}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			return nil, fmt.Errorf("argument %q : format attendu clé=valeur", a)
		}
		kv[k] = v
	}
	return kv, nil
}

func (c *cli) srvSet(args []string) error {
	kv, err := keyValues(args)
	if err != nil {
		return err
	}
	body := map[string]any{}
	for k, v := range kv {
		switch k {
		case "name", "access":
			body[k] = v
		case "require_phone", "telephone":
			body["require_phone"] = v == "true" || v == "oui" || v == "1"
		default:
			return fmt.Errorf("réglage inconnu %q (name, access, require_phone)", k)
		}
	}
	var info serverInfo
	if err := c.cdo("PATCH", "/v1/server", body, &info); err != nil {
		return err
	}
	phone := ""
	if info.RequirePhone {
		phone = ", téléphone vérifié exigé"
	}
	fmt.Printf("Serveur « %s », accès %s%s.\n", info.Name, info.Access, phone)
	return nil
}

func (c *cli) leave() error {
	base, _, err := c.current()
	if err != nil {
		return err
	}
	if err := c.cdo("DELETE", "/v1/members/@me", nil, nil); err != nil {
		return err
	}
	delete(c.st.Communities, base)
	if c.st.Current == base {
		c.st.Current = ""
	}
	fmt.Println("Vous avez quitté le serveur.")
	return c.save()
}

// --- channels ---

func (c *cli) channels() ([]channelInfo, error) {
	var list []channelInfo
	return list, c.cdo("GET", "/v1/channels", nil, &list)
}

// resolveChannel finds a channel by id, exact name, or unique case-insensitive name.
// wantType filters by type when non-empty.
func resolveChannel(list []channelInfo, arg, wantType string) (*channelInfo, error) {
	arg = strings.TrimPrefix(arg, "#")
	var candidates []channelInfo
	for _, ch := range list {
		if wantType == "" || ch.Type == wantType {
			candidates = append(candidates, ch)
		}
	}
	if id, err := strconv.ParseInt(arg, 10, 64); err == nil {
		for _, ch := range candidates {
			if ch.ID == id {
				return &ch, nil
			}
		}
	}
	for _, ch := range candidates {
		if ch.Name == arg {
			return &ch, nil
		}
	}
	var found []channelInfo
	for _, ch := range candidates {
		if strings.EqualFold(ch.Name, arg) {
			found = append(found, ch)
		}
	}
	switch len(found) {
	case 1:
		return &found[0], nil
	case 0:
		return nil, fmt.Errorf("salon %q introuvable (voir quarelctl channels)", arg)
	}
	return nil, fmt.Errorf("plusieurs salons s'appellent %q : utilisez leur id", arg)
}

var typeIcon = map[string]string{"text": "#", "voice": "🔊", "category": "▾", "announcement": "📢", "thread": "🧵"}

func (c *cli) printChannels() error {
	list, err := c.channels()
	if err != nil {
		return err
	}
	children := map[int64][]channelInfo{}
	var top []channelInfo
	for _, ch := range list {
		if ch.ParentID == nil {
			top = append(top, ch)
		} else {
			children[*ch.ParentID] = append(children[*ch.ParentID], ch)
		}
	}
	line := func(indent string, ch channelInfo) {
		topic := ""
		if ch.Topic != "" {
			topic = "  — " + ch.Topic
		}
		lock := ""
		if len(ch.Overrides) > 0 {
			lock = " 🔒"
		}
		fmt.Printf("%s%s %s%s  (id %d)%s\n", indent, typeIcon[ch.Type], ch.Name, lock, ch.ID, topic)
	}
	var walk func(indent string, chs []channelInfo)
	walk = func(indent string, chs []channelInfo) { // category → channel → thread
		for _, ch := range chs {
			line(indent, ch)
			walk(indent+"    ", children[ch.ID])
		}
	}
	walk("", top)
	return nil
}

func (c *cli) channelCreate(args []string) error {
	if err := need(args, 1, "<nom> [text|voice|category|announcement] [catégorie]"); err != nil {
		return err
	}
	body := map[string]any{"name": args[0], "type": "text"}
	if len(args) > 1 {
		body["type"] = args[1]
	}
	if len(args) > 2 {
		list, err := c.channels()
		if err != nil {
			return err
		}
		cat, err := resolveChannel(list, args[2], "category")
		if err != nil {
			return err
		}
		body["parent_id"] = cat.ID
	}
	var ch channelInfo
	if err := c.cdo("POST", "/v1/channels", body, &ch); err != nil {
		return err
	}
	fmt.Printf("Salon créé : %s %s (id %d)\n", typeIcon[ch.Type], ch.Name, ch.ID)
	return nil
}

func (c *cli) channelEdit(args []string) error {
	if err := need(args, 2, "<salon> clé=valeur…"); err != nil {
		return err
	}
	list, err := c.channels()
	if err != nil {
		return err
	}
	ch, err := resolveChannel(list, args[0], "")
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
		case "name", "topic":
			body[k] = v
		case "position":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("position : nombre attendu")
			}
			body[k] = n
		case "parent":
			if v == "0" || v == "" {
				body["parent_id"] = 0
			} else {
				cat, err := resolveChannel(list, v, "category")
				if err != nil {
					return err
				}
				body["parent_id"] = cat.ID
			}
		case "stage":
			body["stage"] = v == "on" || v == "oui" || v == "true"
		default:
			return fmt.Errorf("champ inconnu %q (name, topic, parent, position, stage)", k)
		}
	}
	var out channelInfo
	if err := c.cdo("PATCH", fmt.Sprint("/v1/channels/", ch.ID), body, &out); err != nil {
		return err
	}
	fmt.Printf("Salon modifié : %s %s (id %d)\n", typeIcon[out.Type], out.Name, out.ID)
	return nil
}

func (c *cli) channelDelete(args []string) error {
	if err := need(args, 1, "<salon>"); err != nil {
		return err
	}
	list, err := c.channels()
	if err != nil {
		return err
	}
	ch, err := resolveChannel(list, args[0], "")
	if err != nil {
		return err
	}
	if err := c.cdo("DELETE", fmt.Sprint("/v1/channels/", ch.ID), nil, nil); err != nil {
		return err
	}
	fmt.Printf("Salon %s supprimé.\n", ch.Name)
	return nil
}

// --- members ---

func (c *cli) members() (map[string]memberInfo, error) {
	var list []memberInfo
	if err := c.cdo("GET", "/v1/members", nil, &list); err != nil {
		return nil, err
	}
	m := map[string]memberInfo{}
	for _, mb := range list {
		m[mb.ID] = mb
	}
	return m, nil
}

func (c *cli) printMembers() error {
	var list []memberInfo
	if err := c.cdo("GET", "/v1/members", nil, &list); err != nil {
		return err
	}
	roles, err := c.roles()
	if err != nil {
		return err
	}
	for _, m := range list {
		extra := ""
		if m.Owner {
			extra = "  👑 propriétaire"
		}
		if names := roleNames(m.Roles, roles); names != "" {
			extra += "  [" + names + "]"
		}
		fmt.Printf("%-20s %-32s id %s%s\n", m.DisplayName, m.Handle, m.ID, extra)
	}
	return nil
}

func (c *cli) nick(args []string) error {
	nick := strings.Join(args, " ")
	var m memberInfo
	if err := c.cdo("PATCH", "/v1/members/@me", map[string]string{"nickname": nick}, &m); err != nil {
		return err
	}
	fmt.Println("Nom affiché :", m.DisplayName)
	return nil
}

// srvProfile: my profile on this server, "bio=…" and/or "theme=<JSON>" ({} removes it).
func (c *cli) srvProfile(args []string) error {
	body := map[string]any{}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		switch {
		case ok && k == "bio":
			body["bio"] = v
		case ok && k == "theme":
			body["theme"] = json.RawMessage(v)
		default:
			return fmt.Errorf("usage : srv-profile bio=… theme='{\"colors\":{\"accent\":\"#ff8800\"}}'")
		}
	}
	var m memberInfo
	if err := c.cdo("PATCH", "/v1/members/@me", body, &m); err != nil {
		return err
	}
	fmt.Println("Profil sur ce serveur enregistré pour", m.DisplayName)
	return nil
}

// srvTheme: the server's theme (manage_server), as JSON; "off" removes it.
func (c *cli) srvTheme(args []string) error {
	raw := strings.Join(args, " ")
	if raw == "" {
		var t map[string]any
		if err := c.cdo("GET", "/v1/server/theme", nil, &t); err != nil {
			return err
		}
		out, _ := json.MarshalIndent(t, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if raw == "off" {
		raw = "{}"
	}
	if err := c.cdo("PUT", "/v1/server/theme", map[string]any{"theme": json.RawMessage(raw)}, nil); err != nil {
		return err
	}
	fmt.Println("Thème du serveur enregistré.")
	return nil
}

// --- messages ---

var (
	atWordRe     = regexp.MustCompile(`(^|\s)@([\p{L}\p{N}_.-]+)`)
	mentionTagRe = regexp.MustCompile(`<@([a-z2-7]{26})>`)
)

// toMentions turns "@pseudo" into "<@member_id>" for known members.
func toMentions(text string, members map[string]memberInfo) string {
	return atWordRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := atWordRe.FindStringSubmatch(m)
		word := sub[2]
		if word == "everyone" {
			return m
		}
		for _, mb := range members {
			pseudo, _, _ := strings.Cut(mb.Handle, "@")
			if strings.EqualFold(word, mb.DisplayName) || strings.EqualFold(word, pseudo) {
				return sub[1] + "<@" + mb.ID + ">"
			}
		}
		return m
	})
}

// fromMentions renders "<@member_id>" as "@name".
func fromMentions(text string, members map[string]memberInfo) string {
	return mentionTagRe.ReplaceAllStringFunc(text, func(m string) string {
		id := mentionTagRe.FindStringSubmatch(m)[1]
		if mb, ok := members[id]; ok {
			return "@" + mb.DisplayName
		}
		return "@inconnu"
	})
}

func authorName(members map[string]memberInfo, id string) string {
	if mb, ok := members[id]; ok {
		return mb.DisplayName
	}
	return "(ancien membre)"
}

func formatMessage(m messageInfo, members map[string]memberInfo) string {
	edited := ""
	if m.EditedAt != nil {
		edited = " (modifié)"
	}
	var b strings.Builder
	if m.Referenced != nil {
		fmt.Fprintf(&b, "                 ↱ %s : %s\n", authorName(members, m.Referenced.AuthorID), excerpt(fromMentions(m.Referenced.Content, members), 60))
	} else if m.ReplyTo != nil {
		b.WriteString("                 ↱ (message supprimé)\n")
	}
	pin := ""
	if m.PinnedAt != nil {
		pin = "📌 "
	}
	fmt.Fprintf(&b, "[%s] %-5d %s%s : %s%s", m.CreatedAt.Local().Format("01-02 15:04"), m.ID,
		pin, authorName(members, m.AuthorID), fromMentions(m.Content, members), edited)
	for _, a := range m.Attachments {
		fmt.Fprintf(&b, "\n        📎 %s (%s, %s) — quarelctl download %s", a.Filename, a.ContentType, humanSize(a.Size), a.ID)
	}
	for _, e := range m.Embeds {
		site := e.SiteName
		if site == "" {
			site = e.URL
		}
		fmt.Fprintf(&b, "\n        🔗 %s — %s", site, e.Title)
		if e.Description != "" {
			fmt.Fprintf(&b, " : %s", excerpt(e.Description, 80))
		}
	}
	if len(m.Reactions) > 0 {
		b.WriteString("\n       ")
		for _, r := range m.Reactions {
			mine := ""
			if r.Me {
				mine = "*"
			}
			fmt.Fprintf(&b, " %s %d%s", r.Emoji, r.Count, mine)
		}
	}
	if m.ThreadID != nil {
		fmt.Fprintf(&b, "\n        🧵 fil de discussion : salon %d", *m.ThreadID)
	}
	return b.String()
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > n {
		return string(rs[:n]) + "…"
	}
	return s
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f Mo", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f ko", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d o", n)
}

func (c *cli) textChannel(arg string) (*channelInfo, error) {
	list, err := c.channels()
	if err != nil {
		return nil, err
	}
	ch, err := resolveChannel(list, arg, "")
	if err == nil && ch.Type != "text" && ch.Type != "announcement" && ch.Type != "thread" {
		err = fmt.Errorf("%s n'est pas un salon textuel", ch.Name)
	}
	return ch, err
}

func (c *cli) send(args []string) error {
	if err := need(args, 2, "<salon> <texte…>"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	members, err := c.members()
	if err != nil {
		return err
	}
	var m messageInfo
	body := map[string]string{"content": toMentions(strings.Join(args[1:], " "), members)}
	if err := c.cdo("POST", fmt.Sprint("/v1/channels/", ch.ID, "/messages"), body, &m); err != nil {
		return err
	}
	fmt.Println(formatMessage(m, members))
	return nil
}

func (c *cli) history(args []string) error {
	if err := need(args, 1, "<salon> [nombre] [avant-id]"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	q := url.Values{}
	if len(args) > 1 {
		q.Set("limit", args[1])
	}
	if len(args) > 2 {
		q.Set("before", args[2])
	}
	members, err := c.members()
	if err != nil {
		return err
	}
	var msgs []messageInfo
	if err := c.cdo("GET", fmt.Sprint("/v1/channels/", ch.ID, "/messages?", q.Encode()), nil, &msgs); err != nil {
		return err
	}
	fmt.Printf("#%s — %d message(s)\n", ch.Name, len(msgs))
	for _, m := range msgs {
		fmt.Println(formatMessage(m, members))
	}
	if len(msgs) > 0 {
		fmt.Printf("(plus anciens : quarelctl history %s %d %d)\n", ch.Name, max(len(msgs), 1), msgs[0].ID)
	}
	return nil
}

func (c *cli) editMessage(args []string) error {
	if err := need(args, 3, "<salon> <id> <texte…>"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	members, err := c.members()
	if err != nil {
		return err
	}
	var m messageInfo
	body := map[string]string{"content": toMentions(strings.Join(args[2:], " "), members)}
	if err := c.cdo("PATCH", fmt.Sprint("/v1/channels/", ch.ID, "/messages/", args[1]), body, &m); err != nil {
		return err
	}
	fmt.Println(formatMessage(m, members))
	return nil
}

func (c *cli) deleteMessage(args []string) error {
	if err := need(args, 2, "<salon> <id>"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	if err := c.cdo("DELETE", fmt.Sprint("/v1/channels/", ch.ID, "/messages/", args[1]), nil, nil); err != nil {
		return err
	}
	fmt.Println("Message supprimé.")
	return nil
}

// --- invites ---

type inviteInfo struct {
	Code      string     `json:"code"`
	ServerID  string     `json:"server_id"`
	CreatorID *string    `json:"creator_id"`
	MaxUses   *int64     `json:"max_uses"`
	Uses      int64      `json:"uses"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func parseDuration(s string) (time.Duration, error) {
	if s == "0" {
		return 0, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		return time.Duration(n) * 24 * time.Hour, err
	}
	return time.ParseDuration(s)
}

func (c *cli) inviteLink(inv inviteInfo) (string, error) {
	base, _, err := c.current()
	if err != nil {
		return "", err
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("quarel://%s/%s?sid=%s", u.Host, inv.Code, inv.ServerID), nil
}

func (c *cli) createInvite(args []string) error {
	body := map[string]any{}
	if len(args) > 0 {
		n, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("nombre d'utilisations : entier attendu (0 = illimité)")
		}
		body["max_uses"] = n
	}
	if len(args) > 1 {
		d, err := parseDuration(args[1])
		if err != nil {
			return fmt.Errorf("durée invalide %q (ex. 30m, 24h, 7d, 0 = illimitée)", args[1])
		}
		body["expires_in"] = int64(d.Seconds())
	}
	var inv inviteInfo
	if err := c.cdo("POST", "/v1/invites", body, &inv); err != nil {
		return err
	}
	link, err := c.inviteLink(inv)
	if err != nil {
		return err
	}
	fmt.Printf("Invitation : %s\n  %s\n", inv.Code, describeInvite(inv))
	fmt.Printf("Lien à partager : %s\nPour rejoindre : quarelctl join '%s'\n", link, link)
	return nil
}

func describeInvite(inv inviteInfo) string {
	uses := "utilisations illimitées"
	if inv.MaxUses != nil {
		uses = fmt.Sprintf("%d/%d utilisation(s)", inv.Uses, *inv.MaxUses)
	} else if inv.Uses > 0 {
		uses = fmt.Sprintf("%d utilisation(s), illimité", inv.Uses)
	}
	exp := "sans expiration"
	if inv.ExpiresAt != nil {
		exp = "expire le " + inv.ExpiresAt.Local().Format("2006-01-02 15:04")
	}
	return uses + ", " + exp
}

func (c *cli) listInvites() error {
	var list []inviteInfo
	if err := c.cdo("GET", "/v1/invites", nil, &list); err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("Aucune invitation.")
	}
	for _, inv := range list {
		fmt.Printf("%s  %s\n", inv.Code, describeInvite(inv))
	}
	return nil
}

// --- live events ---

func (c *cli) listen() error {
	// A cheap authenticated call refreshes the session if it expired.
	if err := c.cdo("GET", "/v1/members/@me", nil, nil); err != nil {
		return err
	}
	base, com, err := c.current()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/v1/gateway", &websocket.DialOptions{HTTPClient: c.httpClient(base)})
	if err != nil {
		return fmt.Errorf("connexion temps réel impossible : %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 << 20)
	if err := wsjson.Write(ctx, conn, map[string]string{"op": "auth", "token": com.SessionToken}); err != nil {
		return err
	}

	channels := map[int64]channelInfo{}
	members := map[string]memberInfo{}
	var me string
	chanName := func(id int64) string {
		if ch, ok := channels[id]; ok {
			return "#" + ch.Name
		}
		return fmt.Sprint("#", id)
	}
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
			if reason := websocket.CloseStatus(err); reason != -1 {
				return fmt.Errorf("connexion fermée par le serveur (%d) : %v", reason, err)
			}
			return err
		}
		now := time.Now().Format("15:04:05")
		switch ev.T {
		case "READY":
			var d struct {
				Member   memberInfo    `json:"member"`
				Server   serverInfo    `json:"server"`
				Channels []channelInfo `json:"channels"`
				Members  []memberInfo  `json:"members"`
			}
			json.Unmarshal(ev.D, &d)
			me = d.Member.ID
			for _, ch := range d.Channels {
				channels[ch.ID] = ch
			}
			for _, m := range d.Members {
				members[m.ID] = m
			}
			fmt.Printf("Connexion à « %s » en tant que %s — %d salons, %d membres. En écoute (Ctrl+C pour quitter)…\n",
				d.Server.Name, d.Member.DisplayName, len(d.Channels), len(d.Members))
		case "MESSAGE_CREATE", "MESSAGE_UPDATE":
			var m messageInfo
			json.Unmarshal(ev.D, &m)
			verb := ""
			if ev.T == "MESSAGE_UPDATE" {
				verb = "✎ "
			}
			ping := ""
			for _, id := range m.Mentions {
				if id == me {
					ping = "   🔔 mention pour vous"
				}
			}
			if m.MentionEveryone && ping == "" {
				ping = "   🔔 @everyone"
			}
			fmt.Printf("%s %s%-12s %d  %s : %s%s\n", now, verb, chanName(m.ChannelID), m.ID, authorName(members, m.AuthorID), fromMentions(m.Content, members), ping)
			for _, a := range m.Attachments {
				fmt.Printf("         📎 %s (%s)\n", a.Filename, humanSize(a.Size))
			}
			for _, e := range m.Embeds {
				fmt.Printf("         🔗 %s\n", e.Title)
			}
		case "MESSAGE_DELETE_BULK":
			var d struct {
				ChannelID int64   `json:"channel_id"`
				IDs       []int64 `json:"ids"`
			}
			json.Unmarshal(ev.D, &d)
			fmt.Printf("%s ✗ %-12s %d message(s) supprimé(s) par la modération\n", now, chanName(d.ChannelID), len(d.IDs))
		case "MESSAGE_DELETE":
			var d struct {
				ID        int64 `json:"id"`
				ChannelID int64 `json:"channel_id"`
			}
			json.Unmarshal(ev.D, &d)
			fmt.Printf("%s ✗ %-12s message %d supprimé\n", now, chanName(d.ChannelID), d.ID)
		case "CHANNEL_CREATE", "CHANNEL_UPDATE":
			var ch channelInfo
			json.Unmarshal(ev.D, &ch)
			channels[ch.ID] = ch
			verb := map[string]string{"CHANNEL_CREATE": "créé", "CHANNEL_UPDATE": "modifié"}[ev.T]
			fmt.Printf("%s ⚙ salon %s %s %s (id %d)\n", now, typeIcon[ch.Type], ch.Name, verb, ch.ID)
		case "CHANNEL_DELETE":
			var d struct {
				ID int64 `json:"id"`
			}
			json.Unmarshal(ev.D, &d)
			fmt.Printf("%s ⚙ salon %s supprimé\n", now, chanName(d.ID))
			delete(channels, d.ID)
		case "MEMBER_JOIN", "MEMBER_UPDATE":
			var m memberInfo
			json.Unmarshal(ev.D, &m)
			old, known := members[m.ID]
			members[m.ID] = m
			if ev.T == "MEMBER_JOIN" {
				fmt.Printf("%s → arrivée de %s (%s)\n", now, m.DisplayName, m.Handle)
			} else if known && old.DisplayName != m.DisplayName {
				fmt.Printf("%s ✎ %s → %s (nouveau nom affiché)\n", now, old.DisplayName, m.DisplayName)
			} else if known && fmt.Sprint(old.Roles) != fmt.Sprint(m.Roles) {
				fmt.Printf("%s ⚙ les rôles de %s ont changé\n", now, m.DisplayName)
			} else if m.TimeoutUntil != nil && m.TimeoutUntil.After(time.Now()) && (old.TimeoutUntil == nil || !old.TimeoutUntil.Equal(*m.TimeoutUntil)) {
				fmt.Printf("%s ⏸ exclusion temporaire de %s jusqu'à %s\n", now, m.DisplayName, m.TimeoutUntil.Local().Format("15:04"))
			} else if known && old.TimeoutUntil != nil && m.TimeoutUntil == nil {
				fmt.Printf("%s ▶ fin de l'exclusion de %s\n", now, m.DisplayName)
			}
		case "MEMBER_LEAVE":
			var d struct {
				ID     string `json:"id"`
				Reason string `json:"reason"`
			}
			json.Unmarshal(ev.D, &d)
			how := map[string]string{"kicked": "expulsion", "banned": "bannissement"}[d.Reason]
			if how == "" {
				how = "départ"
			}
			fmt.Printf("%s ← %s : %s\n", now, authorName(members, d.ID), how)
			delete(members, d.ID)
		case "ROLES_UPDATE":
			var list []roleInfo
			json.Unmarshal(ev.D, &list)
			names := []string{}
			for _, r := range list {
				names = append(names, r.Name)
			}
			fmt.Printf("%s ⚙ rôles : %s\n", now, strings.Join(names, ", "))
		case "ROLE_DELETE":
			fmt.Printf("%s ⚙ un rôle a été supprimé\n", now)
		case "CHANNELS_SYNC":
			var d struct {
				Channels []channelInfo `json:"channels"`
			}
			json.Unmarshal(ev.D, &d)
			before := len(channels)
			channels = map[int64]channelInfo{}
			for _, ch := range d.Channels {
				channels[ch.ID] = ch
			}
			if len(channels) != before {
				fmt.Printf("%s ⚙ vos droits ont changé : vous voyez maintenant %d salon(s)\n", now, len(channels))
			}
		case "VOICE_STATE_UPDATE":
			var v voiceStateInfo
			json.Unmarshal(ev.D, &v)
			if v.ChannelID == nil {
				fmt.Printf("%s 🔊 départ du vocal : %s\n", now, authorName(members, v.MemberID))
			} else {
				fmt.Printf("%s 🔊 %s dans %s%s\n", now, authorName(members, v.MemberID), chanName(*v.ChannelID), v.flags())
			}
		case "TYPING_START":
			var d struct {
				ChannelID int64  `json:"channel_id"`
				MemberID  string `json:"member_id"`
			}
			json.Unmarshal(ev.D, &d)
			if d.MemberID != me {
				fmt.Printf("%s … %s écrit dans %s\n", now, authorName(members, d.MemberID), chanName(d.ChannelID))
			}
		case "REACTION_ADD", "REACTION_REMOVE":
			var d struct {
				ChannelID int64  `json:"channel_id"`
				MessageID int64  `json:"message_id"`
				Emoji     string `json:"emoji"`
				MemberID  string `json:"member_id"`
			}
			json.Unmarshal(ev.D, &d)
			verb := "réagit"
			if ev.T == "REACTION_REMOVE" {
				verb = "retire sa réaction"
			}
			fmt.Printf("%s %s %s %s au message %d de %s\n", now, d.Emoji, authorName(members, d.MemberID), verb, d.MessageID, chanName(d.ChannelID))
		case "READ_STATE_UPDATE":
			var d struct {
				ChannelID int64 `json:"channel_id"`
				Unread    int   `json:"unread"`
			}
			json.Unmarshal(ev.D, &d)
			fmt.Printf("%s ✓ %s lu (sur un de vos appareils), %d non lu(s)\n", now, chanName(d.ChannelID), d.Unread)
		case "NOTIFICATION_SETTINGS_UPDATE":
			fmt.Printf("%s ⚙ vos réglages de notification ont changé\n", now)
		case "VOICE_MOVE":
			var d struct {
				ChannelID int64 `json:"channel_id"`
			}
			json.Unmarshal(ev.D, &d)
			fmt.Printf("%s 🔊 la modération vous déplace vers %s (le client rejoint ce salon)\n", now, chanName(d.ChannelID))
		case "SERVER_UPDATE":
			var s serverInfo
			json.Unmarshal(ev.D, &s)
			fmt.Printf("%s ⚙ serveur : « %s », accès %s\n", now, s.Name, s.Access)
		default:
			fmt.Printf("%s %s %s\n", now, ev.T, ev.D)
		}
	}
}
