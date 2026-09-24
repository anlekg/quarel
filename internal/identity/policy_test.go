package identity

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
	"github.com/anlekg/quarel/pkg/tlsbind"
)

func (e *testEnv) register(email, pseudo, invite string) result {
	e.t.Helper()
	return e.call("POST", "/v1/auth/register", "", map[string]string{"email": email, "pseudo": pseudo, "password": "motdepasse-solide", "invite": invite}, nil)
}

func TestRegistrationRules(t *testing.T) {
	e := newEnv(t)
	admin := e.srv.Admin()

	e.srv.cfg.Registration = "closed"
	e.expect(403, "registration_closed", e.register("a@x.org", "alice", ""))

	e.srv.cfg.Registration = "open"
	e.srv.cfg.RegistrationDomains = []string{"asso.fr"}
	e.expect(403, "email_domain_not_allowed", e.register("a@x.org", "alice", ""))
	e.expect(201, "", e.register("alice@asso.fr", "alice", ""))
	e.srv.cfg.RegistrationDomains = nil

	e.srv.cfg.MaxAccounts = 1
	e.expect(403, "account_limit_reached", e.register("bob@x.org", "bob", ""))
	e.srv.cfg.MaxAccounts = 0

	// By invitation: operator invitation with two uses, then expiry.
	e.srv.cfg.Registration = "invite"
	var pol map[string]any
	e.expect(200, "", e.call("GET", "/v1/policy", "", nil, &pol))
	if pol["registration"] != "invite" || pol["server_policy"] != "open" {
		t.Fatalf("policy: %v", pol)
	}
	e.expect(403, "invite_required", e.register("bob@x.org", "bob", ""))
	e.expect(403, "invalid_invite", e.register("bob@x.org", "bob", "nimportequoi"))
	inv, err := admin.CreateInvite(t.Context(), "famille", 2, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	e.expect(201, "", e.register("bob@x.org", "bob", inv.Code))
	e.expect(201, "", e.register("carol@x.org", "carol", " "+inv.Code+" "))
	e.expect(403, "invalid_invite", e.register("dave@x.org", "dave", inv.Code))
	var by string
	e.srv.db.QueryRow(`SELECT invited_by FROM users WHERE pseudo = 'bob'`).Scan(&by)
	if by != "operator" {
		t.Fatalf("invited_by = %q", by)
	}
	inv2, _ := admin.CreateInvite(t.Context(), "", 0, time.Hour)
	e.clock = e.clock.Add(2 * time.Hour)
	e.expect(403, "invalid_invite", e.register("dave@x.org", "dave", inv2.Code))

	// Users' own invitations, within their quota.
	e.srv.cfg.Registration = "open"
	lr, _ := e.registerVerified("erin@x.org", "erin", "motdepasse-solide")
	e.srv.cfg.Registration = "invite"
	e.expect(403, "invite_quota_reached", e.call("POST", "/v1/me/invites", lr.SessionToken, nil, nil))
	e.srv.cfg.UserInvites = 1
	var mine Invite
	e.expect(201, "", e.call("POST", "/v1/me/invites", lr.SessionToken, nil, &mine))
	e.expect(403, "invite_quota_reached", e.call("POST", "/v1/me/invites", lr.SessionToken, nil, nil))
	e.expect(201, "", e.register("fred@x.org", "fred", mine.Code))
	e.expect(403, "invalid_invite", e.register("gina@x.org", "gina", mine.Code)) // single use
	e.srv.db.QueryRow(`SELECT invited_by FROM users WHERE pseudo = 'fred'`).Scan(&by)
	if by != lr.User.ID {
		t.Fatalf("invited_by = %q, want the inviting user", by)
	}
	list, _ := admin.Invites(t.Context())
	if len(list) != 3 || list[0].CreatedBy != "erin" {
		t.Fatalf("operator list: %+v", list)
	}
}

type testServer struct {
	key ed25519.PrivateKey
	enc *ecdh.PrivateKey
	id  string
}

func newTestServer() testServer {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	enc, _ := ecdh.X25519().GenerateKey(rand.Reader)
	return testServer{key: key, enc: enc, id: tlsbind.ServerID(key.Public().(ed25519.PublicKey))}
}

func (s testServer) request(issuer, name string) idtoken.ApprovalRequest {
	return idtoken.SignApproval(s.key, issuer, idtoken.ApprovalRequest{
		EncKey: base64.RawURLEncoding.EncodeToString(s.enc.PublicKey().Bytes()), Name: name, URL: "https://club.example:8090", Contact: "admin@club.example"})
}

func TestCommunityServerPolicy(t *testing.T) {
	e := newEnv(t)
	admin := e.srv.Admin()
	lr, _ := e.registerVerified("alice@x.org", "alice", "motdepasse-solide")
	good, bad := newTestServer(), newTestServer()
	type tokenResp struct {
		Token  string `json:"token"`
		Sealed bool   `json:"sealed"`
	}

	// Open policy: plain tokens; a blocked server gets none and is listed publicly.
	var tr tokenResp
	e.expect(200, "", e.call("POST", "/v1/identity/token", lr.SessionToken, nil, &tr))
	if tr.Sealed || idtoken.IsSealed(tr.Token) {
		t.Fatal("open policy: plain token expected")
	}
	if err := admin.BlockServer(t.Context(), bad.id, "spam", "test"); err != nil {
		t.Fatal(err)
	}
	e.expect(403, "server_blocked", e.call("POST", "/v1/identity/token", lr.SessionToken, map[string]string{"audience": bad.id}, nil))
	var blocked struct {
		Servers []struct{ ID, Reason string }
	}
	e.expect(200, "", e.call("GET", "/v1/servers/blocked", "", nil, &blocked))
	if len(blocked.Servers) != 1 || blocked.Servers[0].ID != bad.id || blocked.Servers[0].Reason != "spam" {
		t.Fatalf("blocked list: %+v", blocked)
	}

	// Approved servers only.
	e.srv.cfg.ServerPolicy = "approved"
	e.expect(400, "audience_required", e.call("POST", "/v1/identity/token", lr.SessionToken, nil, nil))
	e.expect(403, "server_not_approved", e.call("POST", "/v1/identity/token", lr.SessionToken, map[string]string{"audience": good.id}, nil))

	forged := good.request("id.test", "Club")
	forged.Name = "Autre nom"
	e.expect(400, "invalid_signature", e.call("POST", "/v1/servers/requests", "", forged, nil))
	e.expect(400, "invalid_signature", e.call("POST", "/v1/servers/requests", "", good.request("other.issuer", "Club"), nil))
	var st map[string]any
	e.expect(200, "", e.call("POST", "/v1/servers/requests", "", good.request("id.test", "Club"), &st))
	if st["server_id"] != good.id || st["status"] != "pending" {
		t.Fatalf("request: %v", st)
	}
	e.expect(403, "server_not_approved", e.call("POST", "/v1/identity/token", lr.SessionToken, map[string]string{"audience": good.id}, nil))
	if err := admin.DecideServer(t.Context(), good.id, true, "test"); err != nil {
		t.Fatal(err)
	}
	e.expect(200, "", e.call("GET", "/v1/servers/requests/"+good.id, "", nil, &st))
	if st["status"] != "approved" {
		t.Fatalf("status: %v", st)
	}
	e.expect(200, "", e.call("POST", "/v1/identity/token", lr.SessionToken, map[string]string{"audience": good.id}, &tr))
	if !tr.Sealed {
		t.Fatal("approved policy: sealed token expected")
	}
	if _, err := idtoken.Open(tr.Token, bad.enc, good.id); err == nil {
		t.Fatal("only the approved server can open its token")
	}
	plain, err := idtoken.Open(tr.Token, good.enc, good.id)
	if err != nil {
		t.Fatal(err)
	}
	c, err := idtoken.Verify(plain, e.srv.signer.KeySet(), e.clock)
	if err != nil || !c.ForAudience(good.id) || c.ForAudience(bad.id) {
		t.Fatalf("claims: %v %v", c, err)
	}

	// A new encryption key needs a new decision; blocking wins over approval.
	e.expect(200, "", e.call("POST", "/v1/servers/requests", "", testServer{key: good.key, enc: bad.enc, id: good.id}.request("id.test", "Club"), &st))
	if st["status"] != "pending" {
		t.Fatalf("changed key: %v", st)
	}
	admin.DecideServer(t.Context(), good.id, true, "test")
	admin.BlockServer(t.Context(), good.id, "abus", "test")
	e.expect(403, "server_blocked", e.call("POST", "/v1/identity/token", lr.SessionToken, map[string]string{"audience": good.id}, nil))
	list, _ := admin.Servers(t.Context())
	if len(list) != 2 {
		t.Fatalf("servers: %+v", list)
	}
}
