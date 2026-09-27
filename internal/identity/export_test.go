package identity

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// The export holds what the service keeps about the account, never a secret.
func TestExport(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	e.registerVerified("carol@example.com", "carol", "mot-de-passe-carol")
	e.befriend(alice, bob, "bob")
	e.expect(200, "", e.call("POST", "/v1/blocks", alice.SessionToken, map[string]string{"pseudo": "carol"}, nil))
	e.expect(200, "", e.call("POST", "/v1/dms", alice.SessionToken, map[string]string{"user_id": bob.User.ID}, nil))
	e.expect(200, "", e.call("PATCH", "/v1/me/profile", alice.SessionToken, map[string]string{"bio": "Joueuse de tarot"}, nil))

	var doc struct {
		Format        string
		Account       map[string]any
		Sessions      []map[string]any
		Friends       []map[string]any
		Blocks        []map[string]any
		Conversations []map[string]any
	}
	e.expect(200, "", e.call("GET", "/v1/me/export", alice.SessionToken, nil, &doc))
	if doc.Format != "quarel-identity-export-1" || doc.Account["email"] != "alice@example.com" || doc.Account["bio"] != "Joueuse de tarot" {
		t.Fatalf("account = %+v", doc.Account)
	}
	if len(doc.Sessions) != 1 || len(doc.Friends) != 1 || doc.Friends[0]["pseudo"] != "bob" || len(doc.Blocks) != 1 || doc.Blocks[0]["pseudo"] != "carol" {
		t.Fatalf("sessions %v, friends %v, blocks %v", doc.Sessions, doc.Friends, doc.Blocks)
	}
	if len(doc.Conversations) != 1 || !strings.Contains(doc.Conversations[0]["members"].(string), "bob") {
		t.Fatalf("conversations = %v", doc.Conversations)
	}
	req, _ := http.NewRequest("GET", e.http.URL+"/v1/me/export", nil)
	req.Header.Set("Authorization", "Bearer "+alice.SessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("Content-Disposition = %q", resp.Header.Get("Content-Disposition"))
	}
	for _, secret := range []string{"password", "argon2", "totp_secret", "token_hash", "$argon2id"} {
		if strings.Contains(strings.ToLower(string(raw)), secret) {
			t.Fatalf("the export contains %q", secret)
		}
	}
}
