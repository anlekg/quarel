package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

// Conversations (direct or group), edits and deletions, encrypted files,
// typing indicators and read receipts.

type convInfo struct {
	ID      string       `json:"id"`
	Kind    string       `json:"kind"`
	Name    string       `json:"name"`
	OwnerID *string      `json:"owner_id"`
	Members []publicUser `json:"members"`
	User    *publicUser  `json:"user"`
}

func (ci *convInfo) others(me string) []string {
	var out []string
	for _, m := range ci.Members {
		if m.ID != me {
			out = append(out, m.ID)
		}
	}
	return out
}

func (ci *convInfo) label() string {
	if ci.Kind == "group" {
		return "« " + ci.Name + " »"
	}
	if ci.User != nil {
		return "avec " + ci.User.Pseudo
	}
	return ci.ID
}

func (c *cli) conversations() ([]convInfo, error) {
	var list []convInfo
	return list, c.do("GET", "/v1/dms", nil, &list)
}

// resolveConv finds a group by name (or id), else opens the direct
// conversation with a friend.
func (c *cli) resolveConv(e *e2e, target string) (*convInfo, error) {
	list, err := c.conversations()
	if err != nil {
		return nil, err
	}
	remember := func(ci *convInfo) *convInfo {
		for _, m := range ci.Members {
			e.st.Names[m.ID] = m.Pseudo
		}
		return ci
	}
	for i := range list {
		if list[i].ID == target || list[i].Kind == "group" && strings.EqualFold(list[i].Name, target) {
			return remember(&list[i]), nil
		}
	}
	u, rel, err := c.findUser(target)
	if err != nil {
		return nil, fmt.Errorf("%q n'est ni un groupe ni un ami (voir quarelctl dms et friends)", target)
	}
	if rel != "friends" {
		return nil, fmt.Errorf("%s n'est pas (encore) votre ami : les conversations directes sont réservées aux amis", u.Pseudo)
	}
	var ci convInfo
	if err := c.do("POST", "/v1/dms", map[string]string{"user_id": u.ID}, &ci); err != nil {
		return nil, err
	}
	return remember(&ci), nil
}

func (c *cli) runDMs(cmd string, args []string) (bool, error) {
	var err error
	switch cmd {
	case "dms":
		err = c.listConversations()
	case "dm-group":
		if err = need(args, 2, "<nom> <pseudo> [pseudo…]"); err == nil {
			err = c.createGroup(args[0], args[1:])
		}
	case "dm-add", "dm-kick":
		if err = need(args, 2, "<groupe> <pseudo>"); err == nil {
			err = c.groupMember(cmd == "dm-add", args[0], args[1])
		}
	case "dm-leave":
		if err = need(args, 1, "<groupe>"); err == nil {
			err = c.withE2E(func(e *e2e) error {
				conv, err := c.resolveConv(e, args[0])
				if err != nil {
					return err
				}
				if err := c.do("DELETE", "/v1/dms/"+conv.ID+"/members/@me", nil, nil); err != nil {
					return err
				}
				fmt.Printf("Vous avez quitté %s.\n", conv.label())
				return nil
			})
		}
	case "dm-rename":
		if err = need(args, 2, "<groupe> <nouveau nom…>"); err == nil {
			err = c.withE2E(func(e *e2e) error {
				conv, err := c.resolveConv(e, args[0])
				if err != nil {
					return err
				}
				var ci convInfo
				if err := c.do("PATCH", "/v1/dms/"+conv.ID, map[string]string{"name": strings.Join(args[1:], " ")}, &ci); err != nil {
					return err
				}
				fmt.Printf("Groupe renommé : %s.\n", ci.label())
				return nil
			})
		}
	case "dm-edit":
		if err = need(args, 3, "<cible> <n° de message> <nouveau texte…>"); err == nil {
			err = c.editOrDelete(args[0], args[1], strings.Join(args[2:], " "), "edit")
		}
	case "dm-delete":
		if err = need(args, 2, "<cible> <n° de message>"); err == nil {
			err = c.editOrDelete(args[0], args[1], "", "delete")
		}
	case "dm-file":
		if err = need(args, 2, "<cible> <fichier> [texte…]"); err == nil {
			err = c.dmSendFile(args[0], args[1], strings.Join(args[2:], " "))
		}
	case "dm-download":
		if err = need(args, 2, "<cible> <n° de message> [destination]"); err == nil {
			dest := ""
			if len(args) > 2 {
				dest = args[2]
			}
			err = c.dmDownloadFile(args[0], args[1], dest)
		}
	case "dm-typing":
		if err = need(args, 1, "<cible>"); err == nil {
			err = c.withE2E(func(e *e2e) error {
				conv, err := c.resolveConv(e, args[0])
				if err == nil {
					err = c.do("POST", "/v1/dms/"+conv.ID+"/typing", nil, nil)
				}
				return err
			})
		}
	case "dm-read":
		if err = need(args, 1, "<cible>"); err == nil {
			err = c.markDMRead(args[0])
		}
	case "privacy":
		err = c.privacySettings(args)
	default:
		return c.runCalls(cmd, args)
	}
	return true, err
}

func (c *cli) listConversations() error {
	list, err := c.conversations()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("Aucune conversation.")
	}
	for _, ci := range list {
		if ci.Kind == "group" {
			names := []string{}
			for _, m := range ci.Members {
				n := m.Pseudo
				if ci.OwnerID != nil && *ci.OwnerID == m.ID {
					n += " (créateur)"
				}
				names = append(names, n)
			}
			fmt.Printf("👥 %-24s %s\n", ci.Name, strings.Join(names, ", "))
		} else if ci.User != nil {
			fmt.Printf("💬 %-24s conversation directe\n", ci.User.Pseudo)
		}
	}
	return nil
}

func (c *cli) createGroup(name string, pseudos []string) error {
	var ids []string
	for _, p := range pseudos {
		u, rel, err := c.findUser(p)
		if err != nil {
			return err
		}
		if rel != "friends" {
			return fmt.Errorf("%s n'est pas votre ami : un groupe se crée entre amis", u.Pseudo)
		}
		ids = append(ids, u.ID)
	}
	var ci convInfo
	if err := c.do("POST", "/v1/dms", map[string]any{"name": name, "user_ids": ids}, &ci); err != nil {
		return err
	}
	fmt.Printf("Groupe %s créé avec %d membres. Écrivez-y avec : quarelctl dm \"%s\" <texte…>\n", ci.label(), len(ci.Members), ci.Name)
	return nil
}

func (c *cli) groupMember(add bool, group, pseudo string) error {
	return c.withE2E(func(e *e2e) error {
		conv, err := c.resolveConv(e, group)
		if err != nil {
			return err
		}
		var id string
		if u, _, err := c.findUser(pseudo); err == nil {
			id = u.ID
		}
		for _, m := range conv.Members {
			if strings.EqualFold(m.Pseudo, pseudo) {
				id = m.ID
			}
		}
		if id == "" {
			return fmt.Errorf("%q introuvable parmi vos amis et les membres du groupe", pseudo)
		}
		if add {
			if err := c.do("PUT", "/v1/dms/"+conv.ID+"/members/"+id, nil, nil); err != nil {
				return err
			}
			fmt.Printf("Ajout de %s à %s : les messages envoyés dorénavant lui seront lisibles (pas les anciens).\n", pseudo, conv.label())
			return nil
		}
		if err := c.do("DELETE", "/v1/dms/"+conv.ID+"/members/"+id, nil, nil); err != nil {
			return err
		}
		fmt.Printf("Retrait de %s de %s : les prochains messages utiliseront une nouvelle clé.\n", pseudo, conv.label())
		return nil
	})
}

func (c *cli) editOrDelete(target, number, text, kind string) error {
	id, err := strconv.ParseInt(strings.TrimPrefix(number, "#"), 10, 64)
	if err != nil {
		return fmt.Errorf("numéro de message invalide : %s (voir dm-history)", number)
	}
	return c.withE2E(func(e *e2e) error {
		if err := e.sync(printLine); err != nil {
			return err
		}
		conv, err := c.resolveConv(e, target)
		if err != nil {
			return err
		}
		mine := false
		for _, h := range e.st.History[conv.ID] {
			if h.EventID == id && h.From == e.st.UserID {
				mine = true
			}
		}
		if !mine {
			return fmt.Errorf("le message %d n'est pas un de vos messages dans cette conversation", id)
		}
		return c.sendEvent(e, conv, megolmPlain{Type: kind, Target: id, Text: text})
	})
}

// dmSendFile encrypts a file locally (XChaCha20-Poly1305, fresh key), uploads
// the ciphertext and sends the key inside an encrypted event.
func (c *cli) dmSendFile(target, path, text string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return c.withE2E(func(e *e2e) error {
		if err := e.sync(printLine); err != nil {
			return err
		}
		conv, err := c.resolveConv(e, target)
		if err != nil {
			return err
		}
		key := make([]byte, chacha20poly1305.KeySize)
		nonce := make([]byte, chacha20poly1305.NonceSizeX)
		rand.Read(key)
		rand.Read(nonce)
		aead, _ := chacha20poly1305.NewX(key)
		ct := aead.Seal(nil, nonce, data, []byte(conv.ID))
		req, _ := http.NewRequest("POST", c.st.Server+"/v1/dms/"+conv.ID+"/files", bytes.NewReader(ct))
		req.Header.Set("Authorization", "Bearer "+c.st.SessionToken)
		req.Header.Set("Content-Type", "application/octet-stream")
		cl := *c.httpClient(c.st.Server)
		cl.Timeout = 10 * time.Minute
		resp, err := cl.Do(req)
		if err != nil {
			return err
		}
		var up struct {
			ID string `json:"id"`
		}
		err = decodeResponse(resp, &up)
		resp.Body.Close()
		if err != nil {
			return err
		}
		mt := mime.TypeByExtension(filepath.Ext(path))
		if mt == "" {
			mt = http.DetectContentType(data)
		}
		ref := &fileRef{ID: up.ID, Name: filepath.Base(path), Mime: mt, Size: int64(len(data)),
			Key: base64.StdEncoding.EncodeToString(key), Nonce: base64.StdEncoding.EncodeToString(nonce)}
		return c.sendEvent(e, conv, megolmPlain{Type: "file", File: ref, Text: text})
	})
}

func (c *cli) dmDownloadFile(target, number, dest string) error {
	id, err := strconv.ParseInt(strings.TrimPrefix(number, "#"), 10, 64)
	if err != nil {
		return fmt.Errorf("numéro de message invalide : %s", number)
	}
	var ref *fileRef
	var convID string
	err = c.withE2E(func(e *e2e) error {
		if err := e.sync(func(string) {}); err != nil {
			return err
		}
		conv, err := c.resolveConv(e, target)
		if err != nil {
			return err
		}
		convID = conv.ID
		for _, h := range e.st.History[conv.ID] {
			if h.EventID == id && h.File != nil {
				ref = h.File
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if ref == nil {
		return fmt.Errorf("le message %d ne contient pas de fichier", id)
	}
	req, _ := http.NewRequest("GET", c.st.Server+"/v1/dms/"+convID+"/files/"+ref.ID, nil)
	req.Header.Set("Authorization", "Bearer "+c.st.SessionToken)
	cl := *c.httpClient(c.st.Server)
	cl.Timeout = 10 * time.Minute
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decodeResponse(resp, nil)
	}
	ct, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	key, _ := base64.StdEncoding.DecodeString(ref.Key)
	nonce, _ := base64.StdEncoding.DecodeString(ref.Nonce)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return err
	}
	data, err := aead.Open(nil, nonce, ct, []byte(convID))
	if err != nil {
		return fmt.Errorf("fichier altéré ou mauvaise clé : refusé")
	}
	if dest == "" {
		dest = filepath.Base(ref.Name)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("Déchiffré et enregistré : %s (%s)\n", dest, humanSize(int64(len(data))))
	return nil
}

func (c *cli) markDMRead(target string) error {
	return c.withE2E(func(e *e2e) error {
		if err := e.sync(func(string) {}); err != nil {
			return err
		}
		conv, err := c.resolveConv(e, target)
		if err != nil {
			return err
		}
		msgs := e.st.History[conv.ID]
		if len(msgs) == 0 {
			return nil
		}
		last := msgs[len(msgs)-1].EventID
		if err := c.do("POST", "/v1/dms/"+conv.ID+"/read", map[string]int64{"event_id": last}, nil); err != nil {
			return err
		}
		fmt.Printf("Conversation %s lue jusqu'au message %d.\n", conv.label(), last)
		return nil
	})
}

// privacySettings [typing=on|off] [receipts=on|off]
func (c *cli) privacySettings(args []string) error {
	body := map[string]bool{}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		on := v == "on" || v == "oui" || v == "true"
		if !ok || (!on && v != "off" && v != "non" && v != "false") {
			return fmt.Errorf("réglage invalide %q (typing=on|off, receipts=on|off)", a)
		}
		switch k {
		case "typing":
			body["typing"] = on
		case "receipts", "read_receipts":
			body["read_receipts"] = on
		default:
			return fmt.Errorf("réglage inconnu %q (typing, receipts)", k)
		}
	}
	method := "GET"
	var in any
	if len(body) > 0 {
		method, in = "PATCH", body
	}
	var p map[string]bool
	if err := c.do(method, "/v1/me/privacy", in, &p); err != nil {
		return err
	}
	onOff := map[bool]string{true: "partagé", false: "non partagé"}
	fmt.Printf("« En train d'écrire » : %s\nAccusés de lecture : %s\n", onOff[p["typing"]], onOff[p["read_receipts"]])
	return nil
}

// nameOf returns a pseudo for a user id (friends and conversation members).
func (c *cli) nameOf(id string) string {
	if l, err := c.conversations(); err == nil {
		for _, ci := range l {
			for _, m := range ci.Members {
				if m.ID == id {
					return m.Pseudo
				}
			}
		}
	}
	return id
}
