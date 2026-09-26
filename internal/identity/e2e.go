package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/httpapi"
	"github.com/anlekg/quarel/pkg/e2ekeys"
)

// End-to-end encryption support. Devices generate their Olm keys and a user
// master key locally; the server stores public keys, checks signatures
// (clients check them too) and relays opaque ciphertexts through per-device
// mailboxes, deleting each entry once its device acknowledges it.

const (
	maxOneTimeKeys  = 100
	maxToDeviceBody = 8 << 20 // to-device messages may carry a history transfer
	maxInboxPage    = 100
)

type deviceKeysJSON struct {
	DeviceID         string  `json:"device_id"`
	DeviceName       string  `json:"device_name"`
	Curve25519       string  `json:"curve25519"`
	Ed25519          string  `json:"ed25519"`
	Signature        string  `json:"signature"`
	MasterSignature  *string `json:"master_signature"`
	Verified         bool    `json:"verified"` // master signature checked by the server; clients re-check
	VerificationCode string  `json:"verification_code"`
}

func (s *Server) masterKey(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, userID string) (string, error) {
	var key string
	err := q.QueryRowContext(ctx, `SELECT ed25519 FROM master_keys WHERE user_id = ?`, userID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return key, err
}

// userDevices lists the devices of userID that published keys.
func (s *Server) userDevices(ctx context.Context, userID string) ([]deviceKeysJSON, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.session_id, s.device_name, d.curve25519, d.ed25519, d.signature, d.master_signature
		FROM device_keys d JOIN sessions s ON s.id = d.session_id
		WHERE d.user_id = ? ORDER BY d.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []deviceKeysJSON{}
	for rows.Next() {
		var d deviceKeysJSON
		var ms sql.NullString
		if err := rows.Scan(&d.DeviceID, &d.DeviceName, &d.Curve25519, &d.Ed25519, &d.Signature, &ms); err != nil {
			return nil, err
		}
		if ms.Valid {
			d.MasterSignature = &ms.String
		}
		d.Verified = ms.Valid
		d.VerificationCode = e2ekeys.VerificationCode(d.Ed25519)
		list = append(list, d)
	}
	return list, rows.Err()
}

// devicesChanged tells the user's devices and friends that their device list changed
// (senders must then share new message keys with the right devices only).
func (s *Server) devicesChanged(ctx context.Context, userID string) {
	audience, err := s.contacts(ctx, userID)
	if err != nil {
		audience = map[string]bool{}
	}
	audience[userID] = true
	s.hub.BroadcastTo("DEVICES_UPDATE", map[string]string{"user_id": userID}, func(_, group string) bool { return audience[group] })
}

// handleUploadDeviceKeys publishes the calling device's Olm identity keys.
// The first device of an account also publishes the master key and a
// certificate for itself: it is verified from the start. Later devices stay
// unverified until a verified device certifies them.
func (s *Server) handleUploadDeviceKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Curve25519      string `json:"curve25519"`
		Ed25519         string `json:"ed25519"`
		Signature       string `json:"signature"`
		MasterKey       string `json:"master_key"`
		MasterSignature string `json:"master_signature"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	sess := sessionFrom(r)
	badSig := errf(http.StatusBadRequest, "invalid_signature", "a key signature does not verify")
	if _, err := e2ekeys.Decode(req.Curve25519); err != nil || req.Curve25519 == "" {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_key", "invalid curve25519 key"))
		return
	}
	if e2ekeys.Verify(req.Ed25519, e2ekeys.DeviceKeys(sess.UserID, sess.ID, req.Curve25519, req.Ed25519), req.Signature) != nil {
		writeErr(w, r, badSig)
		return
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT ed25519 FROM device_keys WHERE session_id = ?`, sess.ID).Scan(&existing)
	if err == nil {
		if existing != req.Ed25519 {
			writeErr(w, r, errf(http.StatusConflict, "keys_already_set", "this device already published other keys; log in again to use new ones"))
			return
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeErr(w, r, err)
		return
	}
	master, err := s.masterKey(ctx, tx, sess.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	cert := e2ekeys.DeviceCert(sess.UserID, sess.ID, req.Ed25519)
	var masterSig sql.NullString
	switch {
	case master == "" && req.MasterKey == "":
		writeErr(w, r, errf(http.StatusBadRequest, "master_key_required", "the first device must publish the account's master key"))
		return
	case master == "":
		if e2ekeys.Verify(req.MasterKey, cert, req.MasterSignature) != nil {
			writeErr(w, r, badSig)
			return
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO master_keys (user_id, ed25519, created_at) VALUES (?, ?, ?)`,
			sess.UserID, req.MasterKey, s.now().Unix()); err != nil {
			writeErr(w, r, err)
			return
		}
		masterSig = sql.NullString{String: req.MasterSignature, Valid: true}
	case req.MasterKey != "" && req.MasterKey != master:
		writeErr(w, r, errf(http.StatusConflict, "master_key_exists", "this account already has a master key"))
		return
	case req.MasterSignature != "":
		if e2ekeys.Verify(master, cert, req.MasterSignature) != nil {
			writeErr(w, r, badSig)
			return
		}
		masterSig = sql.NullString{String: req.MasterSignature, Valid: true}
	}
	if existing == "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO device_keys (session_id, user_id, curve25519, ed25519, signature, master_signature, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, sess.ID, sess.UserID, req.Curve25519, req.Ed25519, req.Signature, masterSig, s.now().Unix())
	} else if masterSig.Valid {
		_, err = tx.ExecContext(ctx, `UPDATE device_keys SET master_signature = ? WHERE session_id = ?`, masterSig, sess.ID)
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.devicesChanged(ctx, sess.UserID)
	s.writeOwnDevice(w, r)
}

func (s *Server) writeOwnDevice(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	devices, err := s.userDevices(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	for _, d := range devices {
		if d.DeviceID == sess.ID {
			writeJSON(w, http.StatusOK, d)
			return
		}
	}
	writeErr(w, r, errf(http.StatusNotFound, "not_found", "this device has not published keys"))
}

// handleCertifyDevice stores a master-key certificate for another device of
// the same user, made by a device holding the master key (after the user
// compared verification codes).
func (s *Server) handleCertifyDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID        string `json:"device_id"`
		MasterSignature string `json:"master_signature"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	me := sessionFrom(r).UserID
	var ed string
	err := s.db.QueryRowContext(ctx, `SELECT ed25519 FROM device_keys WHERE session_id = ? AND user_id = ?`, req.DeviceID, me).Scan(&ed)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such device"))
		return
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	master, err := s.masterKey(ctx, s.db, me)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if master == "" || e2ekeys.Verify(master, e2ekeys.DeviceCert(me, req.DeviceID, ed), req.MasterSignature) != nil {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_signature", "the certificate is not signed by the account's master key"))
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE device_keys SET master_signature = ? WHERE session_id = ?`, req.MasterSignature, req.DeviceID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.devicesChanged(ctx, me)
	w.WriteHeader(http.StatusNoContent)
}

type oneTimeKey struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Signature string `json:"signature"`
	Fallback  bool   `json:"fallback,omitempty"`
}

func (s *Server) countOneTimeKeys(ctx context.Context, deviceID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM one_time_keys WHERE session_id = ? AND fallback = 0`, deviceID).Scan(&n)
	return n, err
}

// handleUploadOneTimeKeys stores signed one-time keys (consumed by whoever
// opens an Olm session with this device) and an optional fallback key
// (reused when one-time keys run out).
func (s *Server) handleUploadOneTimeKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keys     []oneTimeKey `json:"keys"`
		Fallback *oneTimeKey  `json:"fallback"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	sess := sessionFrom(r)
	var ed string
	if err := s.db.QueryRowContext(ctx, `SELECT ed25519 FROM device_keys WHERE session_id = ?`, sess.ID).Scan(&ed); err != nil {
		writeErr(w, r, errf(http.StatusConflict, "no_device_keys", "publish the device keys first"))
		return
	}
	all := req.Keys
	if req.Fallback != nil {
		all = append(all, *req.Fallback)
	}
	for _, k := range all {
		if k.ID == "" || e2ekeys.Verify(ed, e2ekeys.OneTimeKey(sess.ID, k.ID, k.Key), k.Signature) != nil {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_signature", "one-time key %q is not signed by this device", k.ID))
			return
		}
	}
	n, err := s.countOneTimeKeys(ctx, sess.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if n+len(req.Keys) > maxOneTimeKeys {
		writeErr(w, r, errf(http.StatusBadRequest, "too_many_keys", "at most %d unused one-time keys per device", maxOneTimeKeys))
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	for _, k := range req.Keys {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO one_time_keys (session_id, key_id, key, signature) VALUES (?, ?, ?, ?)`,
			sess.ID, k.ID, k.Key, k.Signature); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if f := req.Fallback; f != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM one_time_keys WHERE session_id = ? AND fallback = 1`, sess.ID); err != nil {
			writeErr(w, r, err)
			return
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO one_time_keys (session_id, key_id, key, signature, fallback) VALUES (?, ?, ?, ?, 1)`,
			sess.ID, f.ID, f.Key, f.Signature); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	n, _ = s.countOneTimeKeys(ctx, sess.ID)
	writeJSON(w, http.StatusOK, map[string]int{"one_time_keys": n})
}

// deviceOwner returns the user owning a device that published keys.
func (s *Server) deviceOwner(ctx context.Context, deviceID string) (string, error) {
	var owner string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM device_keys WHERE session_id = ?`, deviceID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(http.StatusNotFound, "not_found", "no such device")
	}
	return owner, err
}

// handleClaimKeys hands out one one-time key per requested device (or its
// fallback key when none is left) to open Olm sessions with them.
func (s *Server) handleClaimKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceIDs []string `json:"device_ids"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	me := sessionFrom(r).UserID
	out := map[string]oneTimeKey{}
	for _, id := range req.DeviceIDs {
		owner, err := s.deviceOwner(ctx, id)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if err := s.requireContact(ctx, me, owner); err != nil {
			writeErr(w, r, err)
			return
		}
		var k oneTimeKey
		err = s.db.QueryRowContext(ctx, `
			DELETE FROM one_time_keys WHERE rowid = (
				SELECT rowid FROM one_time_keys WHERE session_id = ? AND fallback = 0 ORDER BY rowid LIMIT 1)
			RETURNING key_id, key, signature`, id).Scan(&k.ID, &k.Key, &k.Signature)
		if errors.Is(err, sql.ErrNoRows) {
			k.Fallback = true
			err = s.db.QueryRowContext(ctx, `SELECT key_id, key, signature FROM one_time_keys WHERE session_id = ? AND fallback = 1`, id).
				Scan(&k.ID, &k.Key, &k.Signature)
			if errors.Is(err, sql.ErrNoRows) {
				continue // the device will have to publish keys first
			}
		}
		if err != nil {
			writeErr(w, r, err)
			return
		}
		out[id] = k
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUserKeys returns a user's master key and device keys (friends and self only).
func (s *Server) handleUserKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me, other, err := s.pathOtherUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.requireContact(ctx, me.ID, other.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	master, err := s.masterKey(ctx, s.db, other.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	devices, err := s.userDevices(ctx, other.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var masterJSON *string
	if master != "" {
		masterJSON = &master
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": s.publicUser(other), "master_key": masterJSON, "devices": devices})
}

// --- mailbox ---

type inboxItem struct {
	ID           int64     `json:"id"`
	Kind         string    `json:"kind"` // to_device | dm | receipt
	SenderUser   string    `json:"sender_user"`
	SenderDevice string    `json:"sender_device"`
	DMID         *string   `json:"dm_id,omitempty"`
	EventID      *int64    `json:"event_id,omitempty"`
	Payload      string    `json:"payload"`
	CreatedAt    time.Time `json:"created_at"`
}

// deliver stores an item in a device's mailbox and pushes it if the device is online.
// errInboxFull: the device has too much waiting (see inboxRoom).
var errInboxFull = errf(http.StatusInsufficientStorage, "inbox_full", "this device has too many undelivered messages waiting; it must come online first")

func (s *Server) deliver(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, deviceID string, it inboxItem) (inboxItem, error) {
	if err := s.inboxRoom(ctx, q, deviceID, it.SenderUser, int64(len(it.Payload))); err != nil {
		return it, err
	}
	it.CreatedAt = s.now().UTC().Truncate(time.Second)
	res, err := q.ExecContext(ctx, `INSERT INTO inbox (session_id, kind, sender_user, sender_device, dm_id, event_id, payload, created_at, size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, deviceID, it.Kind, it.SenderUser, it.SenderDevice, it.DMID, it.EventID, it.Payload, it.CreatedAt.Unix(), len(it.Payload))
	if err != nil {
		return it, err
	}
	it.ID, _ = res.LastInsertId()
	return it, nil
}

// inboxRoom bounds what waits for one device: cfg.InboxMaxBytes in all, and
// a quarter of it per sender (the device's own account aside: history
// transfers between one's devices), so no contact can fill it alone.
func (s *Server) inboxRoom(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, deviceID, sender string, size int64) error {
	if s.cfg.InboxMaxBytes <= 0 {
		return nil
	}
	var total, fromSender int64
	var owner string
	err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0), COALESCE(SUM(CASE WHEN sender_user = ? THEN size ELSE 0 END), 0),
		(SELECT user_id FROM sessions WHERE id = ?) FROM inbox WHERE session_id = ?`, sender, deviceID, deviceID).Scan(&total, &fromSender, &owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if total+size > s.cfg.InboxMaxBytes || (sender != owner && fromSender+size > s.cfg.InboxMaxBytes/4) {
		return errInboxFull
	}
	return nil
}

func (s *Server) push(deviceID string, items []inboxItem) {
	if len(items) > 0 {
		s.hub.BroadcastTo("INBOX", items, func(key, _ string) bool { return key == deviceID })
	}
}

// handleSendToDevice relays Olm-encrypted messages to specific devices of
// friends or of the sender (key sharing, device approval, history transfer).
func (s *Server) handleSendToDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			DeviceID string `json:"device_id"`
			Payload  string `json:"payload"`
		} `json:"messages"`
	}
	if err := httpapi.DecodeLimit(r, &req, maxToDeviceBody); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	sess := sessionFrom(r)
	if err := s.limit.sends.Check(sess.UserID); err != nil {
		writeErr(w, r, err)
		return
	}
	// Check every recipient before the transaction: it holds the only connection.
	for _, m := range req.Messages {
		owner, err := s.deviceOwner(ctx, m.DeviceID)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if err := s.requireContact(ctx, sess.UserID, owner); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	pushes := map[string][]inboxItem{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	for _, m := range req.Messages {
		it, err := s.deliver(ctx, tx, m.DeviceID, inboxItem{Kind: "to_device", SenderUser: sess.UserID, SenderDevice: sess.ID, Payload: m.Payload})
		if err != nil {
			writeErr(w, r, err)
			return
		}
		pushes[m.DeviceID] = append(pushes[m.DeviceID], it)
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	for dev, items := range pushes {
		s.push(dev, items)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSendDM fans a Megolm-encrypted message out to every device of every
// member of the conversation (the sender's other devices included). In a
// direct conversation, both must still be friends.
func (s *Server) handleSendDM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Payload string `json:"payload"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	sess := sessionFrom(r)
	if err := s.limit.sends.Check(sess.UserID); err != nil {
		writeErr(w, r, err)
		return
	}
	convID := r.PathValue("id")
	c, err := s.conversation(ctx, convID, sess.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	others := map[string]bool{}
	for _, m := range c.Members {
		if m.ID != sess.UserID {
			others[m.ID] = true
		}
	}
	if c.Kind == convKindDirect {
		if c.User == nil {
			writeErr(w, r, errf(http.StatusGone, "conversation_closed", "the other participant deleted their account"))
			return
		}
		if err := s.requireFriendOrSelf(ctx, sess.UserID, c.User.ID); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if req.Payload == "" {
		writeErr(w, r, errf(http.StatusBadRequest, "empty_payload", "payload is required"))
		return
	}
	members := []any{sess.UserID}
	for id := range others {
		members = append(members, id)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO conv_events (conversation_id, sender_user, sender_device, created_at) VALUES (?, ?, ?, ?)`,
		convID, sess.UserID, sess.ID, s.now().Unix())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	eventID, _ := res.LastInsertId()
	rows, err := tx.QueryContext(ctx, `SELECT session_id, user_id FROM device_keys WHERE user_id IN (?`+strings.Repeat(",?", len(members)-1)+`) AND session_id != ?`,
		append(members, sess.ID)...)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var devices []string
	recipientDevices := 0
	for rows.Next() {
		var id, owner string
		if err := rows.Scan(&id, &owner); err != nil {
			rows.Close()
			writeErr(w, r, err)
			return
		}
		devices = append(devices, id)
		if others[owner] {
			recipientDevices++
		}
	}
	rows.Close()
	if recipientDevices == 0 {
		// Nothing will ever acknowledge it: no delivery receipt to track.
		if _, err := tx.ExecContext(ctx, `DELETE FROM conv_events WHERE id = ?`, eventID); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	pushes := map[string][]inboxItem{}
	for _, dev := range devices {
		it, err := s.deliver(ctx, tx, dev, inboxItem{Kind: "dm", SenderUser: sess.UserID, SenderDevice: sess.ID, DMID: &convID, EventID: &eventID, Payload: req.Payload})
		if errors.Is(err, errInboxFull) { // that device will miss it; the others get it
			slog.Warn("inbox full, message not queued", "device", dev)
			continue
		}
		if err != nil {
			writeErr(w, r, err)
			return
		}
		pushes[dev] = append(pushes[dev], it)
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	for dev, items := range pushes {
		s.push(dev, items)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event_id": eventID, "recipient_devices": recipientDevices})
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	limit := maxInboxPage
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v < maxInboxPage {
		limit = v
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, kind, sender_user, sender_device, dm_id, event_id, payload, created_at
		FROM inbox WHERE session_id = ? ORDER BY id LIMIT ?`, sessionFrom(r).ID, limit)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []inboxItem{}
	for rows.Next() {
		var it inboxItem
		var dm sql.NullString
		var ev sql.NullInt64
		var created int64
		if err := rows.Scan(&it.ID, &it.Kind, &it.SenderUser, &it.SenderDevice, &dm, &ev, &it.Payload, &created); err != nil {
			writeErr(w, r, err)
			return
		}
		if dm.Valid {
			it.DMID = &dm.String
		}
		if ev.Valid {
			it.EventID = &ev.Int64
		}
		it.CreatedAt = time.Unix(created, 0).UTC()
		list = append(list, it)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleAckInbox deletes delivered items from the calling device's mailbox.
func (s *Server) handleAckInbox(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if len(req.IDs) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx := r.Context()
	args := []any{sessionFrom(r).ID}
	for _, id := range req.IDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `DELETE FROM inbox WHERE session_id = ? AND id IN (?`+strings.Repeat(",?", len(req.IDs)-1)+`)
		RETURNING event_id`, args...)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	events := map[int64]bool{}
	for rows.Next() {
		var ev sql.NullInt64
		if rows.Scan(&ev) == nil && ev.Valid {
			events[ev.Int64] = true
		}
	}
	rows.Close()
	for ev := range events {
		s.checkDelivered(ctx, ev)
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkDelivered sends a delivery receipt to the sender's devices once every
// device of the other members has acknowledged a message, then forgets the event.
func (s *Server) checkDelivered(ctx context.Context, eventID int64) {
	var dmID, sender, senderDevice string
	err := s.db.QueryRowContext(ctx, `SELECT conversation_id, sender_user, sender_device FROM conv_events WHERE id = ?`, eventID).Scan(&dmID, &sender, &senderDevice)
	if err != nil {
		return
	}
	var pending int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM inbox i JOIN device_keys d ON d.session_id = i.session_id
		WHERE i.event_id = ? AND i.kind = 'dm' AND d.user_id != ?`, eventID, sender).Scan(&pending); err != nil || pending > 0 {
		return
	}
	payload, _ := json.Marshal(map[string]any{"dm_id": dmID, "event_id": eventID})
	rows, err := s.db.QueryContext(ctx, `SELECT session_id FROM device_keys WHERE user_id = ?`, sender)
	if err != nil {
		return
	}
	var devices []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			devices = append(devices, id)
		}
	}
	rows.Close()
	for _, dev := range devices {
		it, err := s.deliver(ctx, s.db, dev, inboxItem{Kind: "receipt", SenderUser: sender, SenderDevice: senderDevice, DMID: &dmID, EventID: &eventID, Payload: string(payload)})
		if err == nil {
			s.push(dev, []inboxItem{it})
		}
	}
	s.db.ExecContext(ctx, `DELETE FROM conv_events WHERE id = ?`, eventID)
}
