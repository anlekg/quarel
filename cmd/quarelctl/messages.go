package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Replies, reactions, pins, attachments, search, threads, unread state and
// notification settings on community servers.

func (c *cli) reply(args []string) error {
	if err := need(args, 3, "<salon> <id> <texte…>"); err != nil {
		return err
	}
	return c.post(args[0], strings.Join(args[2:], " "), args[1], nil)
}

// post sends a message; replyTo and attachments are optional.
func (c *cli) post(channel, text, replyTo string, attachments []string) error {
	ch, err := c.textChannel(channel)
	if err != nil {
		return err
	}
	members, err := c.members()
	if err != nil {
		return err
	}
	body := map[string]any{"content": toMentions(text, members)}
	if replyTo != "" {
		id, err := strconv.ParseInt(replyTo, 10, 64)
		if err != nil {
			return fmt.Errorf("id de message invalide : %s", replyTo)
		}
		body["reply_to"] = id
	}
	if len(attachments) > 0 {
		body["attachments"] = attachments
	}
	var m messageInfo
	if err := c.cdo("POST", fmt.Sprint("/v1/channels/", ch.ID, "/messages"), body, &m); err != nil {
		return err
	}
	fmt.Println(formatMessage(m, members))
	return nil
}

func (c *cli) react(add bool, args []string) error {
	if err := need(args, 3, "<salon> <id> <emoji>"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	method := "PUT"
	if !add {
		method = "DELETE"
	}
	if err := c.cdo(method, fmt.Sprintf("/v1/channels/%d/messages/%s/reactions/%s", ch.ID, args[1], url.PathEscape(args[2])), nil, nil); err != nil {
		return err
	}
	if add {
		fmt.Printf("Réaction %s ajoutée.\n", args[2])
	} else {
		fmt.Printf("Réaction %s retirée.\n", args[2])
	}
	return nil
}

func (c *cli) pin(add bool, args []string) error {
	if err := need(args, 2, "<salon> <id>"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	method := "PUT"
	if !add {
		method = "DELETE"
	}
	if err := c.cdo(method, fmt.Sprintf("/v1/channels/%d/pins/%s", ch.ID, args[1]), nil, nil); err != nil {
		return err
	}
	if add {
		fmt.Println("Message épinglé.")
	} else {
		fmt.Println("Message désépinglé.")
	}
	return nil
}

func (c *cli) pins(args []string) error {
	if err := need(args, 1, "<salon>"); err != nil {
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
	var msgs []messageInfo
	if err := c.cdo("GET", fmt.Sprint("/v1/channels/", ch.ID, "/pins"), nil, &msgs); err != nil {
		return err
	}
	fmt.Printf("#%s — %d message(s) épinglé(s)\n", ch.Name, len(msgs))
	for _, m := range msgs {
		fmt.Println(formatMessage(m, members))
	}
	return nil
}

// sendFile uploads one file then sends it, with optional text.
func (c *cli) sendFile(args []string) error {
	if err := need(args, 2, "<salon> <fichier> [texte…]"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	f, err := os.Open(args[1])
	if err != nil {
		return err
	}
	defer f.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", filepath.Base(args[1]))
	if _, err := io.Copy(fw, f); err != nil {
		return err
	}
	mw.Close()

	base, _, err := c.current()
	if err != nil {
		return err
	}
	// A cheap authenticated call first refreshes the session if it expired.
	if err := c.cdo("GET", "/v1/members/@me", nil, nil); err != nil {
		return err
	}
	req, _ := http.NewRequest("POST", fmt.Sprint(base, "/v1/channels/", ch.ID, "/attachments"), &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.st.Communities[base].SessionToken)
	cl := *c.httpClient(base)
	cl.Timeout = 10 * time.Minute
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var a attachmentInfo
	if err := decodeResponse(resp, &a); err != nil {
		return err
	}
	fmt.Printf("Fichier envoyé : %s (%s, %s)\n", a.Filename, a.ContentType, humanSize(a.Size))
	return c.post(args[0], strings.Join(args[2:], " "), "", []string{a.ID})
}

func decodeResponse(resp *http.Response, out any) error {
	if resp.StatusCode >= 400 {
		var er struct{ Error apiErr }
		json.NewDecoder(resp.Body).Decode(&er)
		er.Error.Status = resp.StatusCode
		if er.Error.Code == "" {
			er.Error.Code = http.StatusText(resp.StatusCode)
		}
		return &er.Error
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// downloadCmd saves an attachment (by id, as shown under messages).
func (c *cli) downloadCmd(args []string) error {
	if err := need(args, 1, "<id-fichier> [destination]"); err != nil {
		return err
	}
	if err := c.cdo("GET", "/v1/members/@me", nil, nil); err != nil {
		return err
	}
	base, com, err := c.current()
	if err != nil {
		return err
	}
	// The file name in the URL is cosmetic: the server looks files up by id.
	req, _ := http.NewRequest("GET", base+"/v1/attachments/"+url.PathEscape(args[0])+"/f", nil)
	req.Header.Set("Authorization", "Bearer "+com.SessionToken)
	cl := *c.httpClient(base)
	cl.Timeout = 10 * time.Minute
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decodeResponse(resp, nil)
	}
	name := args[0]
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		name = filepath.Base(params["filename"])
	}
	if len(args) > 1 {
		name = args[1]
	}
	out, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, resp.Body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	fmt.Printf("Enregistré : %s (%s)\n", name, humanSize(n))
	return nil
}

// search: words, plus optional filters in:<salon> from:<pseudo>.
func (c *cli) search(args []string) error {
	if err := need(args, 1, "<mots…> [in:salon] [from:pseudo]"); err != nil {
		return err
	}
	q := url.Values{}
	var words []string
	members, err := c.members()
	if err != nil {
		return err
	}
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "in:"):
			ch, err := c.textChannel(strings.TrimPrefix(a, "in:"))
			if err != nil {
				return err
			}
			q.Set("channel_id", fmt.Sprint(ch.ID))
		case strings.HasPrefix(a, "from:"):
			id, err := memberByName(members, strings.TrimPrefix(a, "from:"))
			if err != nil {
				return err
			}
			q.Set("author_id", id)
		default:
			words = append(words, a)
		}
	}
	q.Set("q", strings.Join(words, " "))
	var msgs []messageInfo
	if err := c.cdo("GET", "/v1/search?"+q.Encode(), nil, &msgs); err != nil {
		return err
	}
	list, err := c.channels()
	if err != nil {
		return err
	}
	names := map[int64]string{}
	for _, ch := range list {
		names[ch.ID] = ch.Name
	}
	fmt.Printf("%d résultat(s), du plus récent au plus ancien\n", len(msgs))
	for _, m := range msgs {
		fmt.Printf("#%-12s %s\n", names[m.ChannelID], formatMessage(m, members))
	}
	return nil
}

func memberByName(members map[string]memberInfo, name string) (string, error) {
	name = strings.TrimPrefix(name, "@")
	for _, m := range members {
		pseudo, _, _ := strings.Cut(m.Handle, "@")
		if strings.EqualFold(name, m.DisplayName) || strings.EqualFold(name, pseudo) || name == m.Handle || name == m.ID {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("membre %q introuvable", name)
}

func (c *cli) thread(args []string) error {
	if err := need(args, 2, "<salon> <id> [nom…]"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	var th channelInfo
	if err := c.cdo("POST", fmt.Sprintf("/v1/channels/%d/messages/%s/threads", ch.ID, args[1]),
		map[string]string{"name": strings.Join(args[2:], " ")}, &th); err != nil {
		return err
	}
	fmt.Printf("Fil créé : 🧵 %s (id %d) — écrivez-y avec : quarelctl send %d <texte…>\n", th.Name, th.ID, th.ID)
	return nil
}

type readStateInfo struct {
	ChannelID     int64 `json:"channel_id"`
	LastRead      int64 `json:"last_read"`
	LastMessageID int64 `json:"last_message_id"`
	Unread        int   `json:"unread"`
	Mentions      int   `json:"mentions"`
}

func (c *cli) unread() error {
	var states []readStateInfo
	if err := c.cdo("GET", "/v1/read-states", nil, &states); err != nil {
		return err
	}
	list, err := c.channels()
	if err != nil {
		return err
	}
	byID := map[int64]channelInfo{}
	for _, ch := range list {
		byID[ch.ID] = ch
	}
	n := 0
	for _, s := range states {
		if s.Unread == 0 {
			continue
		}
		n++
		count := fmt.Sprint(s.Unread)
		if s.Unread >= 100 {
			count = "100+"
		}
		ping := ""
		if s.Mentions > 0 {
			ping = fmt.Sprintf("  🔔 %d mention(s)", s.Mentions)
		}
		ch := byID[s.ChannelID]
		fmt.Printf("%s %-20s %s non lu(s)%s\n", typeIcon[ch.Type], ch.Name, count, ping)
	}
	if n == 0 {
		fmt.Println("Tout est lu.")
	}
	return nil
}

func (c *cli) markRead(args []string) error {
	if err := need(args, 1, "<salon>"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	if err := c.cdo("POST", fmt.Sprint("/v1/channels/", ch.ID, "/ack"), map[string]any{}, nil); err != nil {
		return err
	}
	fmt.Printf("#%s marqué comme lu.\n", ch.Name)
	return nil
}

func (c *cli) typing(args []string) error {
	if err := need(args, 1, "<salon>"); err != nil {
		return err
	}
	ch, err := c.textChannel(args[0])
	if err != nil {
		return err
	}
	return c.cdo("POST", fmt.Sprint("/v1/channels/", ch.ID, "/typing"), nil, nil)
}

var notifyLevels = map[string]string{
	"default": "réglage par défaut", "all": "tous les messages", "mentions": "mentions seulement", "none": "rien",
}

// notify <salon|serveur> <all|mentions|none|default> [muet=durée|muet=toujours]
func (c *cli) notify(args []string) error {
	if len(args) == 0 {
		return c.printNotify()
	}
	if err := need(args, 2, "<salon|serveur> <all|mentions|none|default> [muet=1h|muet=toujours]"); err != nil {
		return err
	}
	id := int64(0)
	if args[0] != "serveur" && args[0] != "server" {
		list, err := c.channels()
		if err != nil {
			return err
		}
		ch, err := resolveChannel(list, args[0], "")
		if err != nil {
			return err
		}
		id = ch.ID
	}
	body := map[string]any{"level": args[1]}
	for _, a := range args[2:] {
		v, ok := strings.CutPrefix(a, "muet=")
		if !ok {
			return fmt.Errorf("option inconnue %q (muet=durée)", a)
		}
		if v == "toujours" {
			body["mute_for"] = -1
			continue
		}
		d, err := parseDuration(v)
		if err != nil {
			return err
		}
		body["mute_for"] = int64(d.Seconds())
	}
	if err := c.cdo("PUT", fmt.Sprint("/v1/notification-settings/", id), body, nil); err != nil {
		return err
	}
	return c.printNotify()
}

func (c *cli) printNotify() error {
	var settings []struct {
		ChannelID  int64      `json:"channel_id"`
		Level      string     `json:"level"`
		MutedUntil *time.Time `json:"muted_until"`
	}
	if err := c.cdo("GET", "/v1/notification-settings", nil, &settings); err != nil {
		return err
	}
	list, err := c.channels()
	if err != nil {
		return err
	}
	names := map[int64]string{0: "(tout le serveur)"}
	for _, ch := range list {
		names[ch.ID] = "#" + ch.Name
	}
	if len(settings) == 0 {
		fmt.Println("Notifications : réglages par défaut partout.")
	}
	for _, s := range settings {
		muted := ""
		if s.MutedUntil != nil {
			if s.MutedUntil.Year() > 9000 {
				muted = " — en sourdine"
			} else {
				muted = " — en sourdine jusqu'au " + s.MutedUntil.Local().Format("2006-01-02 15:04")
			}
		}
		fmt.Printf("%-20s %s%s\n", names[s.ChannelID], notifyLevels[s.Level], muted)
	}
	return nil
}
