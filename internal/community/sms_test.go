package community

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookSender(t *testing.T) {
	var got map[string]string
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("s3cret"))
		mac.Write(body)
		if r.Header.Get("X-Quarel-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			w.WriteHeader(401)
			return
		}
		json.Unmarshal(body, &got)
		if fail {
			w.WriteHeader(502)
		}
	}))
	defer srv.Close()
	v := newCodeVerifier(webhookSender{url: srv.URL, secret: "s3cret", client: srv.Client()}.send)
	ctx := context.Background()
	if err := v.Start(ctx, "+33612345678"); err != nil {
		t.Fatal(err)
	}
	if got["phone"] != "+33612345678" || len(got["code"]) != 6 || !strings.Contains(got["text"], got["code"]) {
		t.Fatalf("webhook payload = %v", got)
	}
	if ok, _ := v.Check(ctx, "+33612345678", got["code"]); !ok {
		t.Fatal("code sent by the webhook refused")
	}
	fail = true
	if err := v.Start(ctx, "+33612345678"); err == nil {
		t.Fatal("gateway failure hidden")
	}
	if ok, _ := v.Check(ctx, "+33612345678", got["code"]); ok {
		t.Fatal("a code that could not be sent was accepted")
	}
	bad := webhookSender{url: srv.URL, secret: "wrong", client: srv.Client()}
	if err := bad.send(ctx, "+33612345678", "123456"); err == nil {
		t.Fatal("bad signature accepted by the fake gateway")
	}
}

func TestOVHSender(t *testing.T) {
	const appSecret, consumer = "app-secret", "consumer-key"
	var job struct {
		Message           string   `json:"message"`
		Receivers         []string `json:"receivers"`
		SenderForResponse bool     `json:"senderForResponse"`
		NoStopClause      bool     `json:"noStopClause"`
	}
	serverNow := time.Now().Add(-90 * time.Second) // OVH's clock differs from ours
	invalid := false
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/1.0/auth/time":
			fmt.Fprint(w, serverNow.Unix())
		case "/1.0/sms/sms-ab12345-1/jobs":
			body, _ := io.ReadAll(r.Body)
			stamp := r.Header.Get("X-Ovh-Timestamp")
			sum := sha1.Sum([]byte(strings.Join([]string{appSecret, consumer, "POST", base + r.URL.Path, string(body), stamp}, "+")))
			ts := 0
			fmt.Sscan(stamp, &ts)
			if r.Header.Get("X-Ovh-Signature") != "$1$"+hex.EncodeToString(sum[:]) || r.Header.Get("X-Ovh-Application") != "app-key" ||
				r.Header.Get("X-Ovh-Consumer") != consumer || abs(int64(ts)-serverNow.Unix()) > 5 {
				w.WriteHeader(403)
				w.Write([]byte(`{"message":"Invalid signature"}`))
				return
			}
			json.Unmarshal(body, &job)
			if invalid {
				w.Write([]byte(`{"invalidReceivers":["+33612345678"],"validReceivers":[]}`))
				return
			}
			w.Write([]byte(`{"ids":[1],"validReceivers":["+33612345678"],"invalidReceivers":[]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	base = srv.URL
	o := &ovhSender{endpoint: srv.URL + "/1.0", appKey: "app-key", appSecret: appSecret, consumerKey: consumer,
		service: "sms-ab12345-1", client: srv.Client()}
	if err := o.send(context.Background(), "+33612345678", "654321"); err != nil {
		t.Fatal(err)
	}
	if len(job.Receivers) != 1 || job.Receivers[0] != "+33612345678" || !strings.Contains(job.Message, "654321") || !job.SenderForResponse || !job.NoStopClause {
		t.Fatalf("job = %+v", job)
	}
	invalid = true
	if err := o.send(context.Background(), "+33612345678", "654321"); err == nil {
		t.Fatal("refused number hidden")
	}
	o2 := *o
	o2.appSecret = "wrong"
	if err := o2.send(context.Background(), "+33612345678", "654321"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("bad credentials: %v", err)
	}
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
