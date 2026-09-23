package main

// End-to-end encryption for direct messages (test client).
//
// Olm (one session per pair of devices) carries secrets between devices:
// Megolm conversation keys, device approvals, history transfers. Megolm (one
// outbound session per conversation and sending device) encrypts the
// messages themselves. A conversation key is only shared with verified
// devices: devices certified by their account's master key, which is pinned
// on first contact (a change blocks sending). The server only ever relays
// ciphertexts.
//
// Local state lives in <profile>.e2e.json (0600). This test client keeps it
// unencrypted on disk; the real client will use the OS keychain.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/anlekg/quarel/pkg/e2ekeys"
	"maunium.net/go/mautrix/crypto/goolm"
	"maunium.net/go/mautrix/crypto/olm"
	"maunium.net/go/mautrix/id"
)

func init() { goolm.Register() }

const (
	otkTarget         = 20
	megolmMaxMessages = 100
	megolmMaxAge      = 7 * 24 * time.Hour
)

type histMsg struct {
	EventID   int64     `json:"event_id"`
	From      string    `json:"from"` // user id
	Text      string    `json:"text"`
	At        time.Time `json:"at"`
	Delivered bool      `json:"delivered,omitempty"`
}

type outboundState struct {
	Pickle     string          `json:"pickle"`
	SharedWith map[string]bool `json:"shared_with"` // device ids holding the key
	Created    time.Time       `json:"created"`
	Sent       int             `json:"sent"`
}

type inboundState struct {
	Pickle       string `json:"pickle"`
	SenderUser   string `json:"sender_user"`
	SenderDevice string `json:"sender_device"`
	DMID         string `json:"dm_id"`
}

type inboxItem struct {
	ID           int64     `json:"id"`
	Kind         string    `json:"kind"`
	SenderUser   string    `json:"sender_user"`
	SenderDevice string    `json:"sender_device"`
	DMID         *string   `json:"dm_id,omitempty"`
	EventID      *int64    `json:"event_id,omitempty"`
	Payload      string    `json:"payload"`
	CreatedAt    time.Time `json:"created_at"`
}

type e2eStore struct {
	UserID      string                    `json:"user_id"`
	DeviceID    string                    `json:"device_id"`
	PickleKey   string                    `json:"pickle_key"`
	Account     string                    `json:"account,omitempty"`
	MasterSeed  string                    `json:"master_seed,omitempty"` // present on verified devices
	OlmSessions map[string][]string       `json:"olm_sessions"`          // peer curve25519 → pickles, newest first
	Outbound    map[string]*outboundState `json:"outbound"`              // by dm id
	Inbound     map[string]*inboundState  `json:"inbound"`               // by megolm session id
	Seen        map[string]bool           `json:"seen"`                  // "session:index", replay protection
	Pinned      map[string]string         `json:"pinned"`                // user id → master key (trust on first use)
	History     map[string][]histMsg      `json:"history"`               // by dm id
	Undecrypted []inboxItem               `json:"undecrypted"`           // waiting for their key
	Names       map[string]string         `json:"names"`                 // user id → pseudo

	// Encrypted backup (recovery.go).
	BackupKey     string    `json:"backup_key,omitempty"` // derived from the recovery phrase
	BackupVersion int64     `json:"backup_version,omitempty"`
	BackupDigest  string    `json:"backup_digest,omitempty"` // what the last upload contained
	BackupAt      time.Time `json:"backup_at,omitempty"`
}

type e2e struct {
	c    *cli
	st   *e2eStore
	path string
	acc  olm.Account
}

var b64 = base64.RawStdEncoding

type deviceInfo struct {
	DeviceID         string  `json:"device_id"`
	DeviceName       string  `json:"device_name"`
	Curve25519       string  `json:"curve25519"`
	Ed25519          string  `json:"ed25519"`
	Signature        string  `json:"signature"`
	MasterSignature  *string `json:"master_signature"`
	Verified         bool    `json:"verified"`
	VerificationCode string  `json:"verification_code"`
}

type userKeys struct {
	User struct {
		ID     string `json:"id"`
		Handle string `json:"handle"`
		Pseudo string `json:"pseudo"`
	} `json:"user"`
	MasterKey *string      `json:"master_key"`
	Devices   []deviceInfo `json:"devices"`
}

// withE2E runs fn with this profile's encryption state, holding an
// exclusive lock so that several quarelctl processes on the same profile
// (e.g. dm-listen next to dm) never overwrite each other's keys.
func (c *cli) withE2E(fn func(e *e2e) error) error {
	path := strings.TrimSuffix(c.path, ".json") + ".e2e.json"
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	e, err := c.loadE2E(path)
	if err != nil {
		return err
	}
	if err := fn(e); err != nil {
		e.save()
		return err
	}
	return e.save()
}

// lockFile creates path exclusively, waiting for other holders; a lock older
// than two minutes is considered abandoned (e.g. a killed process).
func lockFile(path string) (func(), error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprint(f, os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > 2*time.Minute {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("profil occupé par un autre quarelctl (verrou %s)", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// loadE2E loads the encryption state of this profile, creating and publishing
// the device keys on first use.
func (c *cli) loadE2E(path string) (*e2e, error) {
	if c.st.SessionToken == "" {
		return nil, errors.New("connectez-vous d'abord : quarelctl login <email|pseudo>")
	}
	e := &e2e{c: c, path: path}
	st := &e2eStore{}
	if data, err := os.ReadFile(e.path); err == nil {
		if err := json.Unmarshal(data, st); err != nil {
			return nil, fmt.Errorf("%s illisible : %w", e.path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	e.st = st
	if st.Seen == nil {
		st.Seen = map[string]bool{}
	}
	if st.OlmSessions == nil {
		st.OlmSessions = map[string][]string{}
	}
	if st.Outbound == nil {
		st.Outbound = map[string]*outboundState{}
	}
	if st.Inbound == nil {
		st.Inbound = map[string]*inboundState{}
	}
	if st.Pinned == nil {
		st.Pinned = map[string]string{}
	}
	if st.History == nil {
		st.History = map[string][]histMsg{}
	}
	if st.Names == nil {
		st.Names = map[string]string{}
	}
	if st.PickleKey == "" {
		k := make([]byte, 32)
		rand.Read(k)
		st.PickleKey = b64.EncodeToString(k)
	}
	if st.UserID == "" {
		var me struct{ ID, Pseudo string }
		if err := c.do("GET", "/v1/me", nil, &me); err != nil {
			return nil, err
		}
		st.UserID = me.ID
		st.Names[me.ID] = me.Pseudo
	}
	// A new login is a new device: new Olm account and sessions; history,
	// received keys, pinned contacts and (if present) the master key stay.
	if st.DeviceID != c.st.SessionID {
		st.DeviceID, st.Account = c.st.SessionID, ""
		st.OlmSessions, st.Outbound = map[string][]string{}, map[string]*outboundState{}
	}
	if st.Account == "" {
		acc, err := olm.NewAccount()
		if err != nil {
			return nil, err
		}
		e.acc = acc
		if err := e.publish(); err != nil {
			return nil, err
		}
	} else {
		acc, err := olm.AccountFromPickled([]byte(st.Account), e.pk())
		if err != nil {
			return nil, err
		}
		e.acc = acc
	}
	return e, nil
}

func (e *e2e) pk() []byte {
	k, _ := b64.DecodeString(e.st.PickleKey)
	return k
}

func (e *e2e) save() error {
	if e.acc != nil {
		p, err := e.acc.Pickle(e.pk())
		if err != nil {
			return err
		}
		e.st.Account = string(p)
	}
	data, _ := json.MarshalIndent(e.st, "", " ")
	return os.WriteFile(e.path, data, 0o600)
}

func (e *e2e) identity() (ed, curve string) {
	edK, curveK, _ := e.acc.IdentityKeys()
	return string(edK), string(curveK)
}

func (e *e2e) master() (olm.PKSigning, error) {
	seed, err := b64.DecodeString(e.st.MasterSeed)
	if err != nil {
		return nil, err
	}
	return olm.NewPKSigningFromSeed(seed)
}

func (e *e2e) sign(msg []byte) string {
	s, _ := e.acc.Sign(msg)
	return string(s)
}

// publish uploads this device's identity keys. The account's first device
// creates the master key and certifies itself; a device that already holds
// the master key (same profile, new login) certifies itself too.
func (e *e2e) publish() error {
	uid, did := e.st.UserID, e.st.DeviceID
	ed, curve := e.identity()
	body := map[string]string{"curve25519": curve, "ed25519": ed, "signature": e.sign(e2ekeys.DeviceKeys(uid, did, curve, ed))}
	keys, err := e.keysOf(uid)
	if err != nil {
		return err
	}
	cert := e2ekeys.DeviceCert(uid, did, ed)
	switch {
	case keys.MasterKey == nil:
		m, err := olm.NewPKSigning()
		if err != nil {
			return err
		}
		sig, _ := m.Sign(cert)
		e.st.MasterSeed = b64.EncodeToString(m.Seed())
		body["master_key"], body["master_signature"] = string(m.PublicKey()), string(sig)
		e.st.Pinned[uid] = string(m.PublicKey())
	default:
		if err := e.pin(uid, *keys.MasterKey); err != nil {
			return err
		}
		if e.st.MasterSeed != "" {
			m, err := e.master()
			if err == nil && string(m.PublicKey()) == *keys.MasterKey {
				sig, _ := m.Sign(cert)
				body["master_signature"] = string(sig)
			} else {
				e.st.MasterSeed = ""
			}
		}
	}
	if err := e.c.do("POST", "/v1/keys/device", body, nil); err != nil {
		return err
	}
	if err := e.save(); err != nil {
		return err
	}
	return e.replenish()
}

// replenish keeps enough signed one-time keys on the server for others to open Olm sessions.
func (e *e2e) replenish() error {
	var count struct {
		N int `json:"one_time_keys"`
	}
	if err := e.c.do("POST", "/v1/keys/one-time", map[string]any{"keys": []any{}}, &count); err != nil {
		return err
	}
	if count.N >= otkTarget/2 {
		return nil
	}
	if err := e.acc.GenOneTimeKeys(uint(otkTarget - count.N)); err != nil {
		return err
	}
	otks, err := e.acc.OneTimeKeys()
	if err != nil {
		return err
	}
	keys := []map[string]string{}
	for kid, k := range otks {
		keys = append(keys, map[string]string{"id": kid, "key": string(k), "signature": e.sign(e2ekeys.OneTimeKey(e.st.DeviceID, kid, string(k)))})
	}
	if err := e.c.do("POST", "/v1/keys/one-time", map[string]any{"keys": keys}, nil); err != nil {
		return err
	}
	e.acc.MarkKeysAsPublished()
	return e.save()
}

// --- directory and trust ---

func (e *e2e) keysOf(userID string) (*userKeys, error) {
	var k userKeys
	if err := e.c.do("GET", "/v1/users/"+userID+"/keys", nil, &k); err != nil {
		return nil, err
	}
	if k.User.Pseudo != "" {
		e.st.Names[userID] = k.User.Pseudo
	}
	return &k, nil
}

// pin remembers a user's master key the first time it is seen, and refuses a different one later.
func (e *e2e) pin(userID, master string) error {
	switch pinned := e.st.Pinned[userID]; pinned {
	case "":
		e.st.Pinned[userID] = master
		return nil
	case master:
		return nil
	}
	return fmt.Errorf("⚠ la clé maîtresse de %s a changé depuis le premier contact : possible usurpation, opération refusée", e.name(userID))
}

// trusted lists userID's devices certified by their pinned master key.
func (e *e2e) trusted(userID string) ([]deviceInfo, error) {
	k, err := e.keysOf(userID)
	if err != nil {
		return nil, err
	}
	if k.MasterKey == nil {
		return nil, nil
	}
	if err := e.pin(userID, *k.MasterKey); err != nil {
		return nil, err
	}
	var out []deviceInfo
	for _, d := range k.Devices {
		if e.deviceTrusted(userID, d, *k.MasterKey) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (e *e2e) deviceTrusted(userID string, d deviceInfo, master string) bool {
	return d.MasterSignature != nil &&
		e2ekeys.Verify(d.Ed25519, e2ekeys.DeviceKeys(userID, d.DeviceID, d.Curve25519, d.Ed25519), d.Signature) == nil &&
		e2ekeys.Verify(master, e2ekeys.DeviceCert(userID, d.DeviceID, d.Ed25519), *d.MasterSignature) == nil
}

func (e *e2e) name(userID string) string {
	if n := e.st.Names[userID]; n != "" {
		return n
	}
	return userID
}

// --- Olm (device to device) ---

type olmEnvelope struct {
	Algorithm string `json:"algorithm"` // olm.v1
	SenderKey string `json:"sender_key"`
	Type      int    `json:"type"`
	Body      string `json:"body"`
}

// olmPlain binds a secret to its sender and recipient so it cannot be replayed elsewhere.
type olmPlain struct {
	Type             string          `json:"type"` // room_key | device_approval | history
	SenderUser       string          `json:"sender_user"`
	SenderDevice     string          `json:"sender_device"`
	SenderEd25519    string          `json:"sender_ed25519"`
	RecipientDevice  string          `json:"recipient_device"`
	RecipientEd25519 string          `json:"recipient_ed25519"`
	Content          json.RawMessage `json:"content"`
}

func (e *e2e) storeOlm(curve string, s olm.Session) error {
	p, err := s.Pickle(e.pk())
	if err != nil {
		return err
	}
	list := e.st.OlmSessions[curve]
	for i, old := range list {
		if prev, err := olm.SessionFromPickled([]byte(old), e.pk()); err == nil && prev.ID() == s.ID() {
			list[i] = string(p)
			return nil
		}
	}
	e.st.OlmSessions[curve] = append([]string{string(p)}, list...)
	return nil
}

// olmSession returns a session to device d, opening one with a claimed one-time key if needed.
func (e *e2e) olmSession(d deviceInfo) (olm.Session, error) {
	if list := e.st.OlmSessions[d.Curve25519]; len(list) > 0 {
		return olm.SessionFromPickled([]byte(list[0]), e.pk())
	}
	var claimed map[string]struct {
		ID, Key, Signature string
	}
	if err := e.c.do("POST", "/v1/keys/claim", map[string]any{"device_ids": []string{d.DeviceID}}, &claimed); err != nil {
		return nil, err
	}
	k, ok := claimed[d.DeviceID]
	if !ok {
		return nil, fmt.Errorf("l'appareil %s n'a plus de clé disponible", d.DeviceName)
	}
	if e2ekeys.Verify(d.Ed25519, e2ekeys.OneTimeKey(d.DeviceID, k.ID, k.Key), k.Signature) != nil {
		return nil, fmt.Errorf("clé à usage unique de %s mal signée : refusée", d.DeviceName)
	}
	s, err := e.acc.NewOutboundSession(id.Curve25519(d.Curve25519), id.Curve25519(k.Key))
	if err != nil {
		return nil, err
	}
	return s, e.storeOlm(d.Curve25519, s)
}

// sendSecret Olm-encrypts content for each device and relays it through the server.
func (e *e2e) sendSecret(devs []deviceInfo, typ string, content any) error {
	if len(devs) == 0 {
		return nil
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return err
	}
	ed, curve := e.identity()
	msgs := []map[string]string{}
	for _, d := range devs {
		s, err := e.olmSession(d)
		if err != nil {
			return err
		}
		plain, _ := json.Marshal(olmPlain{Type: typ, SenderUser: e.st.UserID, SenderDevice: e.st.DeviceID, SenderEd25519: ed,
			RecipientDevice: d.DeviceID, RecipientEd25519: d.Ed25519, Content: raw})
		mt, ct, err := s.Encrypt(plain)
		if err != nil {
			return err
		}
		if err := e.storeOlm(d.Curve25519, s); err != nil {
			return err
		}
		env, _ := json.Marshal(olmEnvelope{Algorithm: "olm.v1", SenderKey: curve, Type: int(mt), Body: string(ct)})
		msgs = append(msgs, map[string]string{"device_id": d.DeviceID, "payload": string(env)})
	}
	if err := e.c.do("POST", "/v1/to-device", map[string]any{"messages": msgs}, nil); err != nil {
		return err
	}
	return e.save()
}

// openSecret decrypts an Olm message and checks who sent it and for whom.
// It returns the plaintext and the sender device, whose trust the caller checks.
func (e *e2e) openSecret(it inboxItem) (*olmPlain, *deviceInfo, string, error) {
	var env olmEnvelope
	if err := json.Unmarshal([]byte(it.Payload), &env); err != nil || env.Algorithm != "olm.v1" {
		return nil, nil, "", errors.New("format inconnu")
	}
	keys, err := e.keysOf(it.SenderUser)
	if err != nil {
		return nil, nil, "", err
	}
	var sender *deviceInfo
	for _, d := range keys.Devices {
		if d.DeviceID == it.SenderDevice {
			sender = &d
		}
	}
	if sender == nil || sender.Curve25519 != env.SenderKey {
		return nil, nil, "", errors.New("appareil expéditeur révoqué, inconnu ou de clé différente : ignoré par précaution")
	}
	var pt []byte
	for _, p := range e.st.OlmSessions[env.SenderKey] {
		s, err := olm.SessionFromPickled([]byte(p), e.pk())
		if err != nil {
			continue
		}
		if out, err := s.Decrypt(env.Body, id.OlmMsgType(env.Type)); err == nil {
			pt = out
			e.storeOlm(env.SenderKey, s)
			break
		}
	}
	if pt == nil {
		if env.Type != int(id.OlmMsgTypePreKey) {
			return nil, nil, "", errors.New("aucune session Olm ne déchiffre ce message")
		}
		curve := id.Curve25519(env.SenderKey)
		s, err := e.acc.NewInboundSessionFrom(&curve, env.Body)
		if err != nil {
			return nil, nil, "", err
		}
		if pt, err = s.Decrypt(env.Body, id.OlmMsgTypePreKey); err != nil {
			return nil, nil, "", err
		}
		e.acc.RemoveOneTimeKeys(s)
		e.storeOlm(env.SenderKey, s)
	}
	var plain olmPlain
	if err := json.Unmarshal(pt, &plain); err != nil {
		return nil, nil, "", err
	}
	myEd, _ := e.identity()
	if plain.SenderUser != it.SenderUser || plain.SenderDevice != it.SenderDevice || plain.SenderEd25519 != sender.Ed25519 ||
		plain.RecipientDevice != e.st.DeviceID || plain.RecipientEd25519 != myEd {
		return nil, nil, "", errors.New("expéditeur ou destinataire ne correspondent pas : message rejeté")
	}
	master := ""
	if keys.MasterKey != nil {
		master = *keys.MasterKey
	}
	return &plain, sender, master, nil
}

// --- Megolm (conversation messages) ---

type megolmEnvelope struct {
	Algorithm string `json:"algorithm"` // megolm.v1
	SenderKey string `json:"sender_key"`
	SessionID string `json:"session_id"`
	Body      string `json:"body"`
}

type megolmPlain struct {
	DMID         string    `json:"dm_id"`
	Text         string    `json:"text"`
	SenderUser   string    `json:"sender_user"`
	SenderDevice string    `json:"sender_device"`
	SentAt       time.Time `json:"sent_at"`
}

type roomKey struct {
	DMID       string `json:"dm_id"`
	SessionID  string `json:"session_id"`
	SessionKey string `json:"session_key"`
}

// sendDM encrypts text for a conversation and sends it. The conversation key
// goes to every verified device of both participants that lacks it; it is
// renewed after megolmMaxMessages, megolmMaxAge, or when a device leaves.
func (e *e2e) sendDM(dmID, otherUser, text string) (*histMsg, int, error) {
	if e.st.MasterSeed == "" {
		return nil, 0, errors.New("cet appareil n'est pas encore validé : approuvez-le depuis un appareil déjà validé (quarelctl devices)")
	}
	var targets []deviceInfo
	for _, u := range []string{otherUser, e.st.UserID} {
		devs, err := e.trusted(u)
		if err != nil {
			return nil, 0, err
		}
		for _, d := range devs {
			if d.DeviceID != e.st.DeviceID {
				targets = append(targets, d)
			}
		}
	}
	current := map[string]bool{}
	for _, d := range targets {
		current[d.DeviceID] = true
	}
	ob := e.st.Outbound[dmID]
	rotate := ob == nil || ob.Sent >= megolmMaxMessages || time.Since(ob.Created) > megolmMaxAge
	if ob != nil {
		for dev := range ob.SharedWith {
			rotate = rotate || !current[dev]
		}
	}
	var og olm.OutboundGroupSession
	var err error
	if rotate {
		og, err = olm.NewOutboundGroupSession()
		ob = &outboundState{SharedWith: map[string]bool{}, Created: time.Now()}
	} else {
		og, err = olm.OutboundGroupSessionFromPickled([]byte(ob.Pickle), e.pk())
	}
	if err != nil {
		return nil, 0, err
	}
	var missing []deviceInfo
	for _, d := range targets {
		if !ob.SharedWith[d.DeviceID] {
			missing = append(missing, d)
		}
	}
	if err := e.sendSecret(missing, "room_key", roomKey{DMID: dmID, SessionID: string(og.ID()), SessionKey: og.Key()}); err != nil {
		return nil, 0, err
	}
	for _, d := range missing {
		ob.SharedWith[d.DeviceID] = true
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	plain, _ := json.Marshal(megolmPlain{DMID: dmID, Text: text, SenderUser: e.st.UserID, SenderDevice: e.st.DeviceID, SentAt: now})
	ct, err := og.Encrypt(plain)
	if err != nil {
		return nil, 0, err
	}
	ob.Sent++
	p, err := og.Pickle(e.pk())
	if err != nil {
		return nil, 0, err
	}
	ob.Pickle = string(p)
	e.st.Outbound[dmID] = ob
	if err := e.save(); err != nil { // never reuse a message index, even if sending fails
		return nil, 0, err
	}
	_, curve := e.identity()
	env, _ := json.Marshal(megolmEnvelope{Algorithm: "megolm.v1", SenderKey: curve, SessionID: string(og.ID()), Body: string(ct)})
	var res struct {
		EventID          int64 `json:"event_id"`
		RecipientDevices int   `json:"recipient_devices"`
	}
	if err := e.c.do("POST", "/v1/dms/"+dmID+"/messages", map[string]string{"payload": string(env)}, &res); err != nil {
		return nil, 0, err
	}
	m := histMsg{EventID: res.EventID, From: e.st.UserID, Text: text, At: now}
	e.addHistory(dmID, m)
	if err := e.backup(false); err != nil {
		fmt.Println("⚠ sauvegarde non mise à jour : " + err.Error())
	}
	return &m, res.RecipientDevices, e.save()
}

var errNoKey = errors.New("clé de conversation pas encore reçue")

func (e *e2e) openDM(it inboxItem) (*histMsg, error) {
	var env megolmEnvelope
	if err := json.Unmarshal([]byte(it.Payload), &env); err != nil || env.Algorithm != "megolm.v1" || it.DMID == nil {
		return nil, errors.New("format inconnu")
	}
	is := e.st.Inbound[env.SessionID]
	if is == nil {
		return nil, errNoKey
	}
	if is.SenderUser != it.SenderUser || is.SenderDevice != it.SenderDevice || is.DMID != *it.DMID {
		return nil, errors.New("clé de conversation utilisée par un autre expéditeur ou ailleurs : rejeté")
	}
	ig, err := olm.InboundGroupSessionFromPickled([]byte(is.Pickle), e.pk())
	if err != nil {
		return nil, err
	}
	pt, idx, err := ig.Decrypt([]byte(env.Body))
	if err != nil {
		return nil, err
	}
	seen := fmt.Sprintf("%s:%d", env.SessionID, idx)
	if e.st.Seen[seen] {
		return nil, errors.New("message rejoué : ignoré")
	}
	var plain megolmPlain
	if err := json.Unmarshal(pt, &plain); err != nil {
		return nil, err
	}
	if plain.DMID != *it.DMID || plain.SenderUser != it.SenderUser || plain.SenderDevice != it.SenderDevice {
		return nil, errors.New("contenu incohérent avec l'enveloppe : rejeté")
	}
	e.st.Seen[seen] = true
	return &histMsg{EventID: *it.EventID, From: plain.SenderUser, Text: plain.Text, At: plain.SentAt}, nil
}

func (e *e2e) addHistory(dmID string, m histMsg) bool {
	for _, h := range e.st.History[dmID] {
		if h.EventID == m.EventID {
			return false
		}
	}
	list := append(e.st.History[dmID], m)
	sort.SliceStable(list, func(i, j int) bool { return list[i].At.Before(list[j].At) })
	e.st.History[dmID] = list
	return true
}

// --- receiving ---

type historyTransfer struct {
	History map[string][]histMsg     `json:"history"`
	Inbound map[string]*inboundState `json:"inbound"` // exported sessions (pickles re-encrypted by the receiver)
	Keys    map[string]string        `json:"keys"`    // session id → exported key
}

// sync fetches, decrypts and acknowledges everything waiting for this device.
// out receives one human-readable line per notable event.
func (e *e2e) sync(out func(string)) error {
	for {
		var items []inboxItem
		if err := e.c.do("GET", "/v1/inbox", nil, &items); err != nil {
			return err
		}
		if len(items) == 0 {
			break
		}
		ids := []int64{}
		for _, it := range items {
			e.process(it, out)
			ids = append(ids, it.ID)
		}
		if err := e.save(); err != nil { // keep what was decrypted before acknowledging
			return err
		}
		if err := e.c.do("POST", "/v1/inbox/ack", map[string]any{"ids": ids}, nil); err != nil {
			return err
		}
	}
	e.retryUndecrypted(out)
	if err := e.replenish(); err != nil {
		return err
	}
	if err := e.backup(false); err != nil {
		out("⚠ sauvegarde non mise à jour : " + err.Error())
	}
	return e.save()
}

func (e *e2e) process(it inboxItem, out func(string)) {
	switch it.Kind {
	case "to_device":
		plain, sender, master, err := e.openSecret(it)
		if err != nil {
			out(fmt.Sprintf("⚠ secret illisible de %s : %v", e.name(it.SenderUser), err))
			return
		}
		e.handleSecret(plain, sender, master, out)
	case "dm":
		m, err := e.openDM(it)
		if errors.Is(err, errNoKey) {
			e.st.Undecrypted = append(e.st.Undecrypted, it)
			return
		}
		if err != nil {
			out(fmt.Sprintf("⚠ message de %s rejeté : %v", e.name(it.SenderUser), err))
			return
		}
		if e.addHistory(*it.DMID, *m) {
			out(e.format(*m))
		}
	case "receipt":
		var r struct {
			DMID    string `json:"dm_id"`
			EventID int64  `json:"event_id"`
		}
		json.Unmarshal([]byte(it.Payload), &r)
		for i, h := range e.st.History[r.DMID] {
			if h.EventID == r.EventID {
				e.st.History[r.DMID][i].Delivered = true
				out(fmt.Sprintf("✓ message %d distribué à tous les appareils du destinataire", r.EventID))
			}
		}
	}
}

func (e *e2e) handleSecret(plain *olmPlain, sender *deviceInfo, master string, out func(string)) {
	trusted := master != "" && e.pin(plain.SenderUser, master) == nil && e.deviceTrusted(plain.SenderUser, *sender, master)
	switch plain.Type {
	case "room_key":
		var k roomKey
		if json.Unmarshal(plain.Content, &k) != nil || !trusted {
			out(fmt.Sprintf("⚠ clé de conversation refusée : l'appareil %s de %s n'est pas validé", sender.DeviceName, e.name(plain.SenderUser)))
			return
		}
		ig, err := olm.NewInboundGroupSession([]byte(k.SessionKey))
		if err != nil || string(ig.ID()) != k.SessionID {
			out("⚠ clé de conversation invalide")
			return
		}
		p, _ := ig.Pickle(e.pk())
		e.st.Inbound[k.SessionID] = &inboundState{Pickle: string(p), SenderUser: plain.SenderUser, SenderDevice: plain.SenderDevice, DMID: k.DMID}
	case "device_approval":
		var a struct {
			MasterSeed string `json:"master_seed"`
			BackupKey  string `json:"backup_key"`
		}
		if json.Unmarshal(plain.Content, &a) != nil || plain.SenderUser != e.st.UserID || !trusted {
			out("⚠ approbation refusée : elle ne vient pas d'un de vos appareils validés")
			return
		}
		seed, err := b64.DecodeString(a.MasterSeed)
		m, err2 := olm.NewPKSigningFromSeed(seed)
		if err != nil || err2 != nil || string(m.PublicKey()) != master {
			out("⚠ approbation refusée : clé maîtresse incohérente")
			return
		}
		e.st.MasterSeed = a.MasterSeed
		if a.BackupKey != "" && e.st.BackupKey == "" {
			e.st.BackupKey = a.BackupKey // version learnt on the first upload (conflict → merge)
		}
		out(fmt.Sprintf("✔ cet appareil a été validé par « %s » : il peut maintenant envoyer et recevoir des messages privés", sender.DeviceName))
	case "history":
		var h historyTransfer
		if json.Unmarshal(plain.Content, &h) != nil || plain.SenderUser != e.st.UserID || !trusted {
			out("⚠ transfert d'historique refusé : il ne vient pas d'un de vos appareils validés")
			return
		}
		n := 0
		for dm, msgs := range h.History {
			for _, m := range msgs {
				if e.addHistory(dm, m) {
					n++
				}
			}
		}
		for sid, info := range h.Inbound {
			if _, have := e.st.Inbound[sid]; have || h.Keys[sid] == "" {
				continue
			}
			ig, err := olm.InboundGroupSessionImport([]byte(h.Keys[sid]))
			if err != nil || string(ig.ID()) != sid {
				continue
			}
			p, _ := ig.Pickle(e.pk())
			e.st.Inbound[sid] = &inboundState{Pickle: string(p), SenderUser: info.SenderUser, SenderDevice: info.SenderDevice, DMID: info.DMID}
		}
		out(fmt.Sprintf("✔ historique reçu de « %s » : %d message(s)", sender.DeviceName, n))
	}
}

func (e *e2e) retryUndecrypted(out func(string)) {
	var still []inboxItem
	for _, it := range e.st.Undecrypted {
		m, err := e.openDM(it)
		switch {
		case errors.Is(err, errNoKey):
			still = append(still, it)
		case err != nil:
			out(fmt.Sprintf("⚠ message de %s rejeté : %v", e.name(it.SenderUser), err))
		default:
			if e.addHistory(*it.DMID, *m) {
				out(e.format(*m))
			}
		}
	}
	e.st.Undecrypted = still
}

func (e *e2e) format(m histMsg) string {
	mark := ""
	if m.From == e.st.UserID && m.Delivered {
		mark = "  ✓"
	}
	return fmt.Sprintf("[%s] %s : %s%s", m.At.Local().Format("01-02 15:04"), e.name(m.From), m.Text, mark)
}

// --- device approval ---

// approve certifies another device of the user after the user compared its
// verification code, then gives it the master key and the history.
func (e *e2e) approve(target, code string) (*deviceInfo, int, error) {
	if e.st.MasterSeed == "" {
		return nil, 0, errors.New("cet appareil n'est pas validé lui-même : utilisez un appareil déjà validé")
	}
	// Catch up first so the transferred history is complete.
	if err := e.sync(func(string) {}); err != nil {
		return nil, 0, err
	}
	keys, err := e.keysOf(e.st.UserID)
	if err != nil {
		return nil, 0, err
	}
	var d *deviceInfo
	for _, x := range keys.Devices {
		if x.DeviceID == target || strings.EqualFold(x.DeviceName, target) {
			d = &x
		}
	}
	if d == nil {
		return nil, 0, fmt.Errorf("appareil %q introuvable (voir quarelctl devices)", target)
	}
	if e2ekeys.NormalizeCode(code) != e2ekeys.NormalizeCode(e2ekeys.VerificationCode(d.Ed25519)) {
		return nil, 0, errors.New("⚠ le code ne correspond pas à celui de l'appareil : NE PAS l'approuver (un tiers tente peut-être de s'insérer)")
	}
	if e2ekeys.Verify(d.Ed25519, e2ekeys.DeviceKeys(e.st.UserID, d.DeviceID, d.Curve25519, d.Ed25519), d.Signature) != nil {
		return nil, 0, errors.New("les clés de cet appareil sont mal signées : refusé")
	}
	m, err := e.master()
	if err != nil {
		return nil, 0, err
	}
	sig, _ := m.Sign(e2ekeys.DeviceCert(e.st.UserID, d.DeviceID, d.Ed25519))
	if err := e.c.do("POST", "/v1/keys/certify", map[string]string{"device_id": d.DeviceID, "master_signature": string(sig)}, nil); err != nil {
		return nil, 0, err
	}
	d.MasterSignature = new(string)
	*d.MasterSignature = string(sig)
	if err := e.sendSecret([]deviceInfo{*d}, "device_approval", map[string]string{"master_seed": e.st.MasterSeed, "backup_key": e.st.BackupKey}); err != nil {
		return nil, 0, err
	}
	h := historyTransfer{History: e.st.History, Inbound: map[string]*inboundState{}, Keys: map[string]string{}}
	n := 0
	for _, msgs := range e.st.History {
		n += len(msgs)
	}
	for sid, info := range e.st.Inbound {
		ig, err := olm.InboundGroupSessionFromPickled([]byte(info.Pickle), e.pk())
		if err != nil {
			continue
		}
		exp, err := ig.Export(ig.FirstKnownIndex())
		if err != nil {
			continue
		}
		h.Inbound[sid] = &inboundState{SenderUser: info.SenderUser, SenderDevice: info.SenderDevice, DMID: info.DMID}
		h.Keys[sid] = string(exp)
	}
	return d, n, e.sendSecret([]deviceInfo{*d}, "history", h)
}

// isVerifiedHere reports whether d is certified by the pinned master key of the user.
func (e *e2e) isVerifiedHere(d deviceInfo) bool {
	master := e.st.Pinned[e.st.UserID]
	return master != "" && e.deviceTrusted(e.st.UserID, d, master)
}
