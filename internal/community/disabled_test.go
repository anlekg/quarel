package community

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledAccounts(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	ctx := context.Background()
	bob := c.users["bob"]

	if err := c.srv.ApplyDisabled(ctx, testIssuer, []string{bob.sub}); err != nil {
		t.Fatal(err)
	}
	c.expect(401, "unauthorized", c.call("GET", "/v1/members/@me", c.tok("bob"), nil, nil))
	c.expect(200, "", c.call("GET", "/v1/members/@me", c.tok("carol"), nil, nil))
	var lr loginResp
	c.expect(403, "account_disabled", c.login(bob, loginOpts{}, &lr))

	// Re-enabled: bob comes back as the same member.
	if err := c.srv.ApplyDisabled(ctx, testIssuer, nil); err != nil {
		t.Fatal(err)
	}
	back := c.mustLogin(bob, loginOpts{})
	if back.Member.ID != c.id("bob") {
		t.Fatal("re-enabled account came back as another member")
	}
}

func TestFetchDisabled(t *testing.T) {
	issuer := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/disabled-accounts" {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "accounts": []map[string]string{{"sub": "abc", "since": "2026-09-24T10:00:00Z"}}})
	}))
	defer srv.Close()
	issuer = strings.TrimPrefix(srv.URL, "http://") // 127.0.0.1:port → plain HTTP, like a local issuer
	subs, err := fetchDisabled(context.Background(), srv.Client(), issuer)
	if err != nil || len(subs) != 1 || subs[0] != "abc" {
		t.Fatalf("subs = %v, %v", subs, err)
	}
	if _, err := fetchDisabled(context.Background(), srv.Client(), "localhost"+issuer[strings.Index(issuer, ":"):]); err == nil {
		t.Fatal("a list claiming another issuer was accepted")
	}
}
