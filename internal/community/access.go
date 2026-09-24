package community

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Access requirements a server may add on top of invites: a rules screen to
// accept, and a verified phone number. Until they are met, a member can only
// read (see permRestricted). Bots are exempt; the owner and administrators too.

const maxRulesLength = 4000

// loadRestrictions fills ps.restricted. It reads through q: it may run inside a transaction.
func (s *Server) loadRestrictions(ctx context.Context, q querier, ps *permSnapshot) error {
	var rules, phone string
	if err := q.QueryRowContext(ctx, `SELECT COALESCE((SELECT value FROM settings WHERE key = 'rules'), ''),
		COALESCE((SELECT value FROM settings WHERE key = 'require_phone'), '')`).Scan(&rules, &phone); err != nil {
		return err
	}
	now := s.nowMs()
	rows, err := q.QueryContext(ctx, `
		SELECT id, COALESCE(timeout_until > ?, 0), rules_accepted_at IS NULL
		FROM members
		WHERE left_at IS NULL AND is_owner = 0
		  AND (timeout_until > ? OR (bot = 0 AND ((? AND rules_accepted_at IS NULL) OR (? AND phone_hash IS NULL))))`,
		now, now, rules != "", phone == "1")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var timedOut, noRules bool
		if err := rows.Scan(&id, &timedOut, &noRules); err != nil {
			return err
		}
		switch {
		case timedOut:
			ps.restricted[id] = restrictTimeout
		case rules != "" && noRules:
			ps.restricted[id] = restrictRules
		default:
			ps.restricted[id] = restrictPhone
		}
	}
	return rows.Err()
}

// handleAcceptRules: POST /v1/members/@me/accept-rules.
func (s *Server) handleAcceptRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rules, err := s.setting(ctx, "rules")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if rules == "" {
		writeErr(w, r, errf(http.StatusBadRequest, "no_rules", "this server has no rules to accept"))
		return
	}
	m := memberFrom(r)
	if _, err := s.db.ExecContext(ctx, `UPDATE members SET rules_accepted_at = COALESCE(rules_accepted_at, ?) WHERE id = ?`, s.nowMs(), m.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.memberChanged(ctx, w, r, m.ID)
}

// memberChanged announces a member whose state changed, resynchronises
// permissions, and answers with the member.
func (s *Server) memberChanged(ctx context.Context, w http.ResponseWriter, r *http.Request, id string) {
	m, err := memberBy(ctx, s.db, `id = ?`, id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := s.memberView(ctx, m)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.Broadcast("MEMBER_UPDATE", view)
	s.syncPermissions(ctx)
	writeJSON(w, http.StatusOK, view)
}

// --- phone verification ---

// PhoneVerifier sends a code to a phone number and checks it. The server
// host chooses (and pays for) the provider.
type PhoneVerifier interface {
	Start(ctx context.Context, phone string) error
	Check(ctx context.Context, phone, code string) (bool, error)
}

var phoneRe = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// normalizePhone accepts international numbers (E.164: +33612345678), with
// spaces, dots, dashes or parentheses.
func normalizePhone(raw string) (string, error) {
	p := strings.Map(func(r rune) rune {
		if strings.ContainsRune(" .-() ", r) {
			return -1
		}
		return r
	}, raw)
	if strings.HasPrefix(p, "00") {
		p = "+" + p[2:]
	}
	if !phoneRe.MatchString(p) {
		return "", errf(http.StatusBadRequest, "invalid_phone", "give the number in international format, e.g. +33 6 12 34 56 78")
	}
	return p, nil
}

// phoneHash is a keyed hash of the number: the server never stores numbers.
func (s *Server) phoneHash(phone string) string {
	key := sha256.Sum256(append([]byte("quarel phone hash\x00"), s.key.Seed()...))
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(phone))
	return hex.EncodeToString(mac.Sum(nil))
}

// handlePhoneStart: POST /v1/members/@me/phone {phone} sends a code.
func (s *Server) handlePhoneStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if s.phone == nil {
		writeErr(w, r, errf(http.StatusServiceUnavailable, "phone_verification_unavailable", "this server has no phone verification provider"))
		return
	}
	phone, err := normalizePhone(req.Phone)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.limit.phone.Check(memberFrom(r).ID); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.phone.Start(r.Context(), phone); err != nil {
		slog.Warn("phone verification", "err", err)
		writeErr(w, r, errf(http.StatusBadGateway, "phone_provider_error", "the verification code could not be sent"))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "code_sent"})
}

// handlePhoneVerify: POST /v1/members/@me/phone/verify {phone, code}. A number
// can verify one member at a time, and never again once its member is banned.
func (s *Server) handlePhoneVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if s.phone == nil {
		writeErr(w, r, errf(http.StatusServiceUnavailable, "phone_verification_unavailable", "this server has no phone verification provider"))
		return
	}
	phone, err := normalizePhone(req.Phone)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	ok, err := s.phone.Check(ctx, phone, strings.TrimSpace(req.Code))
	if err != nil {
		slog.Warn("phone verification", "err", err)
		writeErr(w, r, errf(http.StatusBadGateway, "phone_provider_error", "the code could not be checked"))
		return
	}
	if !ok {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_code", "wrong or expired code"))
		return
	}
	me := memberFrom(r).ID
	hash := s.phoneHash(phone)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	var other string
	var active, banned bool
	err = tx.QueryRowContext(ctx, `SELECT id, left_at IS NULL, EXISTS (SELECT 1 FROM bans WHERE member_id = members.id)
		FROM members WHERE phone_hash = ?`, hash).Scan(&other, &active, &banned)
	switch {
	case err == nil && other == me:
	case err == nil && banned:
		writeErr(w, r, errf(http.StatusForbidden, "phone_banned", "this number belongs to a member banned from this server"))
		return
	case err == nil && active:
		writeErr(w, r, errf(http.StatusConflict, "phone_in_use", "this number already verifies another member of this server"))
		return
	case err == nil: // a former member: the number moves to this account
		if _, err := tx.ExecContext(ctx, `UPDATE members SET phone_hash = NULL WHERE id = ?`, other); err != nil {
			writeErr(w, r, err)
			return
		}
	case !errors.Is(err, sql.ErrNoRows):
		writeErr(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE members SET phone_hash = ? WHERE id = ?`, hash, me); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.memberChanged(ctx, w, r, me)
}

// codeVerifier generates and checks the codes itself (6 digits, 10 minutes,
// 5 attempts) and only delegates sending them: to the log (development), a
// webhook, or OVHcloud SMS. Twilio Verify, instead, handles the codes itself.
type codeVerifier struct {
	mu    sync.Mutex
	codes map[string]*pendingCode
	now   func() time.Time
	send  func(ctx context.Context, phone, code string) error
}

type pendingCode struct {
	code     string
	expires  time.Time
	attempts int
}

const phoneCodeTTL = 10 * time.Minute

func newCodeVerifier(send func(ctx context.Context, phone, code string) error) *codeVerifier {
	return &codeVerifier{codes: map[string]*pendingCode{}, now: time.Now, send: send}
}

// newLogVerifier is the development provider: codes are written to the log.
func newLogVerifier() *codeVerifier {
	return newCodeVerifier(func(_ context.Context, phone, code string) error {
		slog.Warn("phone verification code (QUAREL_PHONE_VERIFY=log, development only)", "phone", phone, "code", code)
		return nil
	})
}

// smsText is the message sent to the member.
func smsText(code string) string {
	return fmt.Sprintf("Quarel : votre code de vérification est %s (valable %d min). Ne le communiquez à personne.", code, int(phoneCodeTTL.Minutes()))
}

func (v *codeVerifier) Start(ctx context.Context, phone string) error {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	if err := v.send(ctx, phone, code); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for p, c := range v.codes { // forget expired codes
		if v.now().After(c.expires) {
			delete(v.codes, p)
		}
	}
	v.codes[phone] = &pendingCode{code: code, expires: v.now().Add(phoneCodeTTL)}
	return nil
}

func (v *codeVerifier) Check(_ context.Context, phone, code string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	p := v.codes[phone]
	if p == nil || v.now().After(p.expires) || p.attempts >= 5 {
		return false, nil
	}
	p.attempts++
	if subtle.ConstantTimeCompare([]byte(p.code), []byte(code)) != 1 {
		return false, nil
	}
	delete(v.codes, phone)
	return true, nil
}

// twilioVerifier uses Twilio Verify (https://www.twilio.com/docs/verify/api):
// Twilio generates, sends and checks the codes.
type twilioVerifier struct {
	accountSID, authToken, serviceSID string
	baseURL                           string // https://verify.twilio.com (tests: a fake)
	client                            *http.Client
}

func (t *twilioVerifier) post(ctx context.Context, path string, form url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/v2/Services/"+url.PathEscape(t.serviceSID)+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(t.accountSID, t.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}

func (t *twilioVerifier) Start(ctx context.Context, phone string) error {
	status, err := t.post(ctx, "/Verifications", url.Values{"To": {phone}, "Channel": {"sms"}}, nil)
	if err == nil && status >= 300 {
		err = fmt.Errorf("twilio: HTTP %d", status)
	}
	return err
}

func (t *twilioVerifier) Check(ctx context.Context, phone, code string) (bool, error) {
	var out struct {
		Status string `json:"status"`
	}
	status, err := t.post(ctx, "/VerificationCheck", url.Values{"To": {phone}, "Code": {code}}, &out)
	switch {
	case err != nil:
		return false, err
	case status == http.StatusNotFound: // expired, already approved or too many attempts
		return false, nil
	case status >= 300:
		return false, fmt.Errorf("twilio: HTTP %d", status)
	}
	return out.Status == "approved", nil
}

// newPhoneVerifier builds the provider chosen in the configuration (nil: none).
func newPhoneVerifier(cfg Config) PhoneVerifier {
	client := &http.Client{Timeout: 15 * time.Second}
	switch cfg.PhoneVerify {
	case "log":
		return newLogVerifier()
	case "webhook":
		return newCodeVerifier(webhookSender{url: cfg.PhoneWebhookURL, secret: cfg.PhoneWebhookSecret, client: client}.send)
	case "ovh":
		return newCodeVerifier((&ovhSender{endpoint: cfg.OVHEndpoint, appKey: cfg.OVHAppKey, appSecret: cfg.OVHAppSecret,
			consumerKey: cfg.OVHConsumerKey, service: cfg.OVHSMSService, sender: cfg.OVHSMSSender, client: client}).send)
	case "twilio":
		return &twilioVerifier{accountSID: cfg.TwilioAccountSID, authToken: cfg.TwilioAuthToken, serviceSID: cfg.TwilioVerifySID,
			baseURL: "https://verify.twilio.com", client: client}
	}
	return nil
}
