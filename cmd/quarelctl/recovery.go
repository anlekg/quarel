package main

// Recovery phrase and encrypted backup: the account master key, message
// history and conversation keys, encrypted with a key derived from a 12-word
// phrase (pkg/recovery) and stored opaque on the Identity service. With the
// phrase, a new device restores everything even if all devices are lost.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anlekg/quarel/pkg/e2ekeys"
	"github.com/anlekg/quarel/pkg/recovery"
	"maunium.net/go/mautrix/crypto/olm"
)

type backupPayload struct {
	Format     int                      `json:"format"`
	MasterSeed string                   `json:"master_seed"`
	History    map[string][]histMsg     `json:"history"`
	Inbound    map[string]*inboundState `json:"inbound"` // exported: Pickle holds the exported key
	Pinned     map[string]string        `json:"pinned"`
	Names      map[string]string        `json:"names"`
	CreatedAt  time.Time                `json:"created_at"`
}

type serverBackup struct {
	Version   int64     `json:"version"`
	Data      string    `json:"data"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (c *cli) runRecovery(cmd string, args []string) (bool, error) {
	switch cmd {
	case "recovery-setup":
		replace := len(args) > 0 && args[0] == "--replace"
		return true, c.withE2E(func(e *e2e) error { return e.recoverySetup(replace) })
	case "recovery-status":
		return true, c.withE2E(func(e *e2e) error { return e.recoveryStatus() })
	case "recovery-restore":
		if err := need(args, 12, "<les 12 mots de la phrase de récupération>"); err != nil {
			return true, err
		}
		return true, c.withE2E(func(e *e2e) error { return e.recoveryRestore(strings.Join(args, " ")) })
	}
	return false, nil
}

func (e *e2e) backupKey() []byte {
	k, _ := base64.RawStdEncoding.DecodeString(e.st.BackupKey)
	return k
}

func (e *e2e) payload() backupPayload {
	p := backupPayload{Format: 1, MasterSeed: e.st.MasterSeed, History: e.st.History, Inbound: map[string]*inboundState{},
		Pinned: e.st.Pinned, Names: e.st.Names, CreatedAt: time.Now().UTC()}
	for sid, in := range e.st.Inbound {
		ig, err := olm.InboundGroupSessionFromPickled([]byte(in.Pickle), e.pk())
		if err != nil {
			continue
		}
		exp, err := ig.Export(ig.FirstKnownIndex())
		if err != nil {
			continue
		}
		p.Inbound[sid] = &inboundState{Pickle: string(exp), SenderUser: in.SenderUser, SenderDevice: in.SenderDevice, DMID: in.DMID}
	}
	return p
}

// digest summarises what a backup would contain, to skip useless uploads.
func (e *e2e) digest() string {
	sids := make([]string, 0, len(e.st.Inbound))
	for sid := range e.st.Inbound {
		sids = append(sids, sid)
	}
	sort.Strings(sids)
	data, _ := json.Marshal([]any{e.st.History, sids, e.st.Pinned, e.st.MasterSeed != ""})
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// merge imports a backup's content into the local state.
func (e *e2e) merge(p backupPayload) (messages int) {
	for dm, msgs := range p.History {
		for _, m := range msgs {
			if e.addHistory(dm, m) {
				messages++
			}
		}
	}
	for sid, in := range p.Inbound {
		if _, have := e.st.Inbound[sid]; have {
			continue
		}
		ig, err := olm.InboundGroupSessionImport([]byte(in.Pickle))
		if err != nil || string(ig.ID()) != sid {
			continue
		}
		pk, _ := ig.Pickle(e.pk())
		e.st.Inbound[sid] = &inboundState{Pickle: string(pk), SenderUser: in.SenderUser, SenderDevice: in.SenderDevice, DMID: in.DMID}
	}
	for uid, key := range p.Pinned {
		if e.st.Pinned[uid] == "" {
			e.st.Pinned[uid] = key
		}
	}
	for uid, n := range p.Names {
		if e.st.Names[uid] == "" {
			e.st.Names[uid] = n
		}
	}
	return messages
}

func (e *e2e) fetchBackup() (*serverBackup, error) {
	var b serverBackup
	err := e.c.do("GET", "/v1/backup", nil, &b)
	var ae *apiErr
	if errors.As(err, &ae) && ae.Code == "no_backup" {
		return nil, nil
	}
	return &b, err
}

func (e *e2e) openBackup(b *serverBackup, key []byte) (*backupPayload, error) {
	ct, err := base64.StdEncoding.DecodeString(b.Data)
	if err != nil {
		return nil, err
	}
	pt, err := recovery.Open(key, e.st.UserID, ct)
	if err != nil {
		return nil, err
	}
	var p backupPayload
	return &p, json.Unmarshal(pt, &p)
}

// backup uploads the encrypted state if backups are enabled and something
// changed. If another device saved in between, its backup is merged first.
func (e *e2e) backup(force bool) error {
	if e.st.BackupKey == "" || e.st.MasterSeed == "" {
		return nil
	}
	if !force && e.digest() == e.st.BackupDigest {
		return nil
	}
	for attempt := 0; attempt < 3; attempt++ {
		plain, _ := json.Marshal(e.payload())
		ct, err := recovery.Seal(e.backupKey(), e.st.UserID, plain)
		if err != nil {
			return err
		}
		var res struct {
			Version int64 `json:"version"`
		}
		err = e.c.do("PUT", "/v1/backup", map[string]any{"version": e.st.BackupVersion, "data": base64.StdEncoding.EncodeToString(ct)}, &res)
		var ae *apiErr
		if errors.As(err, &ae) && ae.Code == "version_conflict" {
			b, err := e.fetchBackup()
			if err != nil {
				return err
			}
			if b == nil {
				e.st.BackupVersion = 0
				continue
			}
			p, err := e.openBackup(b, e.backupKey())
			if err != nil {
				return fmt.Errorf("la sauvegarde du serveur a été créée avec une autre phrase de récupération (recovery-setup --replace pour la remplacer) : %w", err)
			}
			e.merge(*p)
			e.st.BackupVersion = b.Version
			continue
		}
		if err != nil {
			return err
		}
		e.st.BackupVersion, e.st.BackupDigest, e.st.BackupAt = res.Version, e.digest(), time.Now().UTC()
		return nil
	}
	return errors.New("sauvegarde : trop de modifications concurrentes, réessayez")
}

func (e *e2e) recoverySetup(replace bool) error {
	if e.st.MasterSeed == "" {
		return errors.New("cet appareil n'est pas validé : utilisez un appareil validé pour créer la phrase de récupération")
	}
	existing, err := e.fetchBackup()
	if err != nil {
		return err
	}
	if existing != nil && !replace {
		return errors.New("une sauvegarde existe déjà pour ce compte. « recovery-setup --replace » crée une nouvelle phrase : l'ancienne ne servira plus")
	}
	phrase, secret, err := recovery.Generate()
	if err != nil {
		return err
	}
	e.st.BackupKey = base64.RawStdEncoding.EncodeToString(recovery.Key(secret, e.st.UserID))
	e.st.BackupVersion = 0
	if existing != nil {
		e.st.BackupVersion = existing.Version
	}
	if err := e.backup(true); err != nil {
		return err
	}
	words := strings.Fields(phrase)
	fmt.Println("Votre phrase de récupération (à écrire sur papier, dans l'ordre) :")
	fmt.Println()
	for i := 0; i < 12; i += 3 {
		fmt.Printf("   %2d. %-14s %2d. %-14s %2d. %s\n", i+1, words[i], i+2, words[i+1], i+3, words[i+2])
	}
	fmt.Println()
	fmt.Println("• Elle permet de tout restaurer (clé du compte et historique des messages privés) si vous perdez tous vos appareils :")
	fmt.Println("    quarelctl recovery-restore <les 12 mots>")
	fmt.Println("• Elle ne sera plus jamais affichée. Personne, pas même le serveur, ne peut la retrouver.")
	fmt.Println("• Quiconque la possède peut lire vos messages privés : ne la stockez pas en ligne, ne la communiquez à personne.")
	fmt.Printf("Sauvegarde chiffrée envoyée (version %d). Elle sera mise à jour automatiquement.\n", e.st.BackupVersion)
	return nil
}

func (e *e2e) recoveryStatus() error {
	b, err := e.fetchBackup()
	if err != nil {
		return err
	}
	switch {
	case b == nil:
		fmt.Println("Aucune sauvegarde : si vous perdez tous vos appareils, l'historique sera perdu.")
		fmt.Println("Créez une phrase de récupération depuis un appareil validé : quarelctl recovery-setup")
	case e.st.BackupKey == "":
		fmt.Printf("Une sauvegarde existe (version %d, %s), mais cet appareil n'en a pas la clé.\n", b.Version, b.UpdatedAt.Local().Format("2006-01-02 15:04"))
		fmt.Println("Elle vous est transmise lors de la validation de l'appareil, ou avec quarelctl recovery-restore <12 mots>.")
	default:
		fmt.Printf("✔ Sauvegarde active : version %d, mise à jour le %s.\n", b.Version, b.UpdatedAt.Local().Format("2006-01-02 15:04"))
	}
	return nil
}

func (e *e2e) recoveryRestore(phrase string) error {
	secret, err := recovery.Parse(phrase)
	if err != nil {
		return fmt.Errorf("phrase invalide : %w", err)
	}
	b, err := e.fetchBackup()
	if err != nil {
		return err
	}
	if b == nil {
		return errors.New("aucune sauvegarde n'existe pour ce compte")
	}
	key := recovery.Key(secret, e.st.UserID)
	p, err := e.openBackup(b, key)
	if errors.Is(err, recovery.ErrWrongKey) {
		return errors.New("cette phrase n'ouvre pas la sauvegarde de ce compte (phrase d'un autre compte, ou remplacée depuis)")
	}
	if err != nil {
		return err
	}
	// The backup must hold the account's real master key.
	keys, err := e.keysOf(e.st.UserID)
	if err != nil {
		return err
	}
	seed, err := base64.RawStdEncoding.DecodeString(p.MasterSeed)
	if err != nil {
		return errors.New("sauvegarde incomplète : clé du compte absente")
	}
	m, err := olm.NewPKSigningFromSeed(seed)
	if err != nil || keys.MasterKey == nil || string(m.PublicKey()) != *keys.MasterKey {
		return errors.New("la clé du compte contenue dans la sauvegarde ne correspond pas à celle publiée : restauration refusée")
	}
	if err := e.pin(e.st.UserID, *keys.MasterKey); err != nil {
		return err
	}
	e.st.MasterSeed = p.MasterSeed
	ed, _ := e.identity()
	sig, _ := m.Sign(e2ekeys.DeviceCert(e.st.UserID, e.st.DeviceID, ed))
	if err := e.c.do("POST", "/v1/keys/certify", map[string]string{"device_id": e.st.DeviceID, "master_signature": string(sig)}, nil); err != nil {
		return err
	}
	n := e.merge(*p)
	e.st.BackupKey = base64.RawStdEncoding.EncodeToString(key)
	e.st.BackupVersion, e.st.BackupDigest = b.Version, e.digest()
	fmt.Printf("✔ Compte restauré depuis la sauvegarde du %s : cet appareil est validé, %d message(s) retrouvé(s).\n",
		b.UpdatedAt.Local().Format("2006-01-02 15:04"), n)
	fmt.Println("Pensez à révoquer les appareils perdus : quarelctl sessions, puis revoke-session <id>.")
	return nil
}
