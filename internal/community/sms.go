package community

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SMS senders for phone verification (the codes themselves are handled by
// codeVerifier). Recommended: a webhook to the host's own gateway, or
// OVHcloud SMS (European provider).

// webhookSender POSTs {phone, code, text} as JSON to a URL chosen by the
// host, who plugs in any SMS gateway (their own phone, a GSM modem, a
// provider). With a secret, the body is signed: header
// X-Quarel-Signature: sha256=<hex HMAC-SHA256(secret, body)>.
type webhookSender struct {
	url, secret string
	client      *http.Client
}

func (w webhookSender) send(ctx context.Context, phone, code string) error {
	body, _ := json.Marshal(map[string]string{"phone": phone, "code": code, "text": smsText(code)})
	req, err := http.NewRequestWithContext(ctx, "POST", w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Quarel")
	if w.secret != "" {
		mac := hmac.New(sha256.New, []byte(w.secret))
		mac.Write(body)
		req.Header.Set("X-Quarel-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("phone webhook: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("phone webhook: HTTP %d", resp.StatusCode)
	}
	return nil
}

// ovhSender uses the OVHcloud SMS API (POST /sms/{service}/jobs), signed as
// the OVH API requires: X-Ovh-Signature = "$1$" + SHA1(appSecret + "+" +
// consumerKey + "+" + method + "+" + url + "+" + body + "+" + timestamp).
type ovhSender struct {
	endpoint                       string // https://eu.api.ovh.com/1.0
	appKey, appSecret, consumerKey string
	service                        string // sms-xx12345-1
	sender                         string // registered sender name; empty: a short number is used
	client                         *http.Client
	timeDelta                      time.Duration // OVH clock minus ours
	synced                         bool
}

func (o *ovhSender) serverTime(ctx context.Context) (int64, error) {
	if !o.synced {
		req, err := http.NewRequestWithContext(ctx, "GET", o.endpoint+"/auth/time", nil)
		if err != nil {
			return 0, err
		}
		resp, err := o.client.Do(req)
		if err != nil {
			return 0, fmt.Errorf("OVH: %w", err)
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		t, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("OVH: invalid /auth/time answer")
		}
		o.timeDelta, o.synced = time.Until(time.Unix(t, 0)), true
	}
	return time.Now().Add(o.timeDelta).Unix(), nil
}

func (o *ovhSender) send(ctx context.Context, phone, code string) error {
	job := map[string]any{
		"message":      smsText(code),
		"receivers":    []string{phone},
		"charset":      "UTF-8",
		"priority":     "high",
		"noStopClause": true, // transactional message, not marketing
	}
	if o.sender != "" {
		job["sender"] = o.sender
	} else {
		job["senderForResponse"] = true
	}
	body, _ := json.Marshal(job)
	url := o.endpoint + "/sms/" + o.service + "/jobs"
	ts, err := o.serverTime(ctx)
	if err != nil {
		return err
	}
	stamp := strconv.FormatInt(ts, 10)
	sum := sha1.Sum([]byte(strings.Join([]string{o.appSecret, o.consumerKey, "POST", url, string(body), stamp}, "+")))
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ovh-Application", o.appKey)
	req.Header.Set("X-Ovh-Consumer", o.consumerKey)
	req.Header.Set("X-Ovh-Timestamp", stamp)
	req.Header.Set("X-Ovh-Signature", "$1$"+hex.EncodeToString(sum[:]))
	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("OVH: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		var e struct{ Message string }
		json.Unmarshal(data, &e)
		return fmt.Errorf("OVH: HTTP %d %s", resp.StatusCode, e.Message)
	}
	var res struct {
		InvalidReceivers []string `json:"invalidReceivers"`
	}
	json.Unmarshal(data, &res)
	if len(res.InvalidReceivers) > 0 {
		return fmt.Errorf("OVH: number refused")
	}
	return nil
}
