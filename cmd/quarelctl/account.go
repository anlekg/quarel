package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Account management, public profile, blocks and presence (Identity service).

func (c *cli) runAccount(cmd string, args []string) (bool, error) {
	var err error
	switch cmd {
	case "forgot-password":
		if err = need(args, 1, "<email>"); err == nil {
			if err = c.do("POST", "/v1/auth/forgot-password", map[string]string{"email": args[0]}, nil); err == nil {
				fmt.Println("Si un compte correspond à cette adresse, un code vient d'y être envoyé. Puis : quarelctl reset-password <email> <code>")
			}
		}
	case "reset-password":
		if err = need(args, 2, "<email> <code>"); err == nil {
			err = c.resetPassword(args[0], args[1])
		}
	case "passwd":
		err = c.changePassword()
	case "email-change":
		if err = need(args, 1, "<nouvelle adresse>"); err == nil {
			err = c.changeEmail(args[0])
		}
	case "email-confirm":
		if err = need(args, 1, "<code>"); err == nil {
			var me struct{ Email string }
			if err = c.do("POST", "/v1/me/email/confirm", map[string]string{"code": args[0]}, &me); err == nil {
				fmt.Printf("Adresse email changée : %s (l'ancienne adresse a été prévenue).\n", me.Email)
			}
		}
	case "pseudo":
		if err = need(args, 1, "<nouveau pseudo>"); err == nil {
			var me struct{ Handle string }
			if err = c.do("PATCH", "/v1/me", map[string]string{"pseudo": args[0]}, &me); err == nil {
				c.st.Handle = me.Handle
				fmt.Printf("Pseudo changé : %s. Votre identité ne change pas : bans, rôles et amis restent attachés au compte.\n", me.Handle)
				err = c.save()
			}
		}
	case "delete-account":
		err = c.deleteAccount()
	case "profile":
		err = c.showProfile(args)
	case "bio":
		var p profileInfo
		if err = c.do("PATCH", "/v1/me/profile", map[string]string{"bio": strings.Join(args, " ")}, &p); err == nil {
			fmt.Println("Bio enregistrée.")
		}
	case "avatar":
		err = c.setAvatar(args)
	case "block":
		if err = need(args, 1, "<pseudo>"); err == nil {
			var u publicUser
			if err = c.do("POST", "/v1/blocks", map[string]string{"pseudo": strings.TrimPrefix(args[0], "@")}, &u); err == nil {
				fmt.Printf("Blocage de %s : plus d'amitié ni de demandes possibles entre vous.\n", u.Handle)
			}
		}
	case "unblock":
		if err = need(args, 1, "<pseudo>"); err == nil {
			err = c.unblock(args[0])
		}
	case "blocks":
		var list []publicUser
		if err = c.do("GET", "/v1/blocks", nil, &list); err == nil {
			if len(list) == 0 {
				fmt.Println("Personne n'est bloqué.")
			}
			for _, u := range list {
				fmt.Printf("  %-20s %s\n", u.Pseudo, u.Handle)
			}
		}
	case "status":
		if err = need(args, 1, "online|idle|dnd|invisible"); err == nil {
			if err = c.do("PUT", "/v1/me/presence", map[string]string{"status": args[0]}, nil); err == nil {
				fmt.Printf("Statut : %s.\n", presenceText[args[0]])
			}
		}
	default:
		return c.runRecovery(cmd, args)
	}
	return true, err
}

var presenceText = map[string]string{
	"online": "🟢 en ligne", "idle": "🌙 en pause", "dnd": "⛔ ne pas déranger", "invisible": "⚫ invisible (affiché hors ligne)", "offline": "⚫ hors ligne",
}

// newPassword reads a new password (QUAREL_NEW_PASSWORD for scripts).
func (c *cli) newPassword() (string, error) {
	if pw := os.Getenv("QUAREL_NEW_PASSWORD"); pw != "" {
		return pw, nil
	}
	return c.password("Nouveau mot de passe : ", true)
}

func (c *cli) resetPassword(email, code string) error {
	pw, err := c.newPassword()
	if err != nil {
		return err
	}
	body := map[string]string{"email": email, "code": code, "password": pw}
	err = c.do("POST", "/v1/auth/reset-password", body, nil)
	var ae *apiErr
	if errors.As(err, &ae) && ae.Code == "mfa_required" {
		if body["totp_code"], err = c.prompt("Code 2FA (ou code de secours) : "); err != nil {
			return err
		}
		err = c.do("POST", "/v1/auth/reset-password", body, nil)
	}
	if err != nil {
		return err
	}
	c.st.SessionToken = ""
	fmt.Println("Mot de passe réinitialisé ; tous vos appareils ont été déconnectés. Reconnectez-vous avec quarelctl login.")
	return c.save()
}

func (c *cli) changePassword() error {
	current, err := c.password("Mot de passe actuel : ", false)
	if err != nil {
		return err
	}
	pw, err := c.newPassword()
	if err != nil {
		return err
	}
	if err := c.do("POST", "/v1/me/password", map[string]string{"current_password": current, "new_password": pw}, nil); err != nil {
		return err
	}
	fmt.Println("Mot de passe changé ; vos autres appareils ont été déconnectés.")
	return nil
}

func (c *cli) changeEmail(address string) error {
	pw, err := c.password("Mot de passe : ", false)
	if err != nil {
		return err
	}
	if err := c.do("POST", "/v1/me/email", map[string]string{"password": pw, "new_email": address}, nil); err != nil {
		return err
	}
	fmt.Printf("Code envoyé à %s. Puis : quarelctl email-confirm <code>\n", address)
	return nil
}

func (c *cli) deleteAccount() error {
	var me struct {
		Pseudo      string `json:"pseudo"`
		TOTPEnabled bool   `json:"totp_enabled"`
	}
	if err := c.do("GET", "/v1/me", nil, &me); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "⚠ Suppression DÉFINITIVE du compte : amis, conversations, appareils, sauvegarde et profil seront effacés.")
	confirm, err := c.prompt(fmt.Sprintf("Tapez votre pseudo (%s) pour confirmer : ", me.Pseudo))
	if err != nil {
		return err
	}
	if !strings.EqualFold(confirm, me.Pseudo) {
		return errors.New("confirmation incorrecte : rien n'a été supprimé")
	}
	pw, err := c.password("Mot de passe : ", false)
	if err != nil {
		return err
	}
	body := map[string]string{"password": pw}
	if me.TOTPEnabled {
		if body["totp_code"], err = c.prompt("Code 2FA (ou code de secours) : "); err != nil {
			return err
		}
	}
	if err := c.do("DELETE", "/v1/me", body, nil); err != nil {
		return err
	}
	c.st.SessionToken = ""
	fmt.Println("Compte supprimé. Les serveurs communautaires gardent seulement votre ancien identifiant aléatoire.")
	return c.save()
}

type profileInfo struct {
	ID        string  `json:"id"`
	Handle    string  `json:"handle"`
	Pseudo    string  `json:"pseudo"`
	Bio       string  `json:"bio"`
	AvatarURL *string `json:"avatar_url"`
}

// showProfile [pseudo|id]: own profile, or a friend's (by pseudo) or anyone's (by id).
func (c *cli) showProfile(args []string) error {
	var id string
	if len(args) == 0 {
		var me struct{ ID string }
		if err := c.do("GET", "/v1/me", nil, &me); err != nil {
			return err
		}
		id = me.ID
	} else if u, _, err := c.findUser(args[0]); err == nil {
		id = u.ID
	} else {
		id = args[0]
	}
	var p profileInfo
	if err := c.do("GET", "/v1/users/"+id+"/profile", nil, &p); err != nil {
		return err
	}
	fmt.Printf("%s\n  id  : %s\n", p.Handle, p.ID)
	if p.Bio != "" {
		fmt.Printf("  bio : %s\n", p.Bio)
	}
	if p.AvatarURL != nil {
		fmt.Printf("  avatar : %s%s\n", c.st.Server, *p.AvatarURL)
	} else {
		fmt.Println("  avatar : (aucun)")
	}
	return nil
}

func (c *cli) setAvatar(args []string) error {
	if err := need(args, 1, "<image PNG/JPEG/GIF/WebP> | --remove"); err != nil {
		return err
	}
	if args[0] == "--remove" {
		if err := c.do("DELETE", "/v1/me/avatar", nil, nil); err != nil {
			return err
		}
		fmt.Println("Avatar retiré.")
		return nil
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	req, _ := http.NewRequest("PUT", c.st.Server+"/v1/me/avatar", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.st.SessionToken)
	resp, err := c.httpClient(c.st.Server).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var er struct{ Error apiErr }
		json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&er)
		er.Error.Status = resp.StatusCode
		return &er.Error
	}
	fmt.Println("Avatar enregistré.")
	return nil
}

func (c *cli) unblock(pseudo string) error {
	var list []publicUser
	if err := c.do("GET", "/v1/blocks", nil, &list); err != nil {
		return err
	}
	for _, u := range list {
		if strings.EqualFold(u.Pseudo, strings.TrimPrefix(pseudo, "@")) {
			if err := c.do("DELETE", "/v1/blocks/"+u.ID, nil, nil); err != nil {
				return err
			}
			fmt.Printf("Fin du blocage de %s.\n", u.Handle)
			return nil
		}
	}
	return fmt.Errorf("aucun blocage pour %s", pseudo)
}
