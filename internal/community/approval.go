package community

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/anlekg/quarel/internal/httpapi"
	"github.com/anlekg/quarel/pkg/idtoken"
)

// Identity services that only let approved servers use their accounts seal
// tokens to this server's X25519 key, derived from its identity key (no
// extra file: a restored backup keeps it).
func deriveEncKey(key ed25519.PrivateKey) *ecdh.PrivateKey {
	b, err := hkdf.Key(sha256.New, key.Seed(), nil, "quarel-server-token-encryption-v1", 32)
	if err != nil {
		panic(err)
	}
	k, err := ecdh.X25519().NewPrivateKey(b)
	if err != nil {
		panic(err)
	}
	return k
}

// IssuerStatus is what the administration page shows about a trusted Identity service.
type IssuerStatus struct {
	Issuer       string `json:"issuer"`
	Reachable    bool   `json:"reachable"`
	ServerPolicy string `json:"server_policy,omitempty"` // open | approved
	Status       string `json:"status,omitempty"`        // none | pending | approved | rejected
	Blocked      bool   `json:"blocked"`
}

var issuerClient = &http.Client{Timeout: 8 * time.Second}

func getJSON(ctx context.Context, url string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	res, err := issuerClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// IdentityServices reports, for each trusted Identity service, whether it
// only accepts approved servers and where this server stands.
func (s *Server) IdentityServices(ctx context.Context) []IssuerStatus {
	out := make([]IssuerStatus, len(s.cfg.TrustedIssuers))
	for i, iss := range s.cfg.TrustedIssuers {
		st := IssuerStatus{Issuer: iss}
		var pol struct {
			ServerPolicy string `json:"server_policy"`
		}
		var req struct {
			Status  string `json:"status"`
			Blocked bool   `json:"blocked"`
		}
		base := issuerBaseURL(iss)
		if getJSON(ctx, base+"/v1/policy", &pol) == nil && getJSON(ctx, base+"/v1/servers/requests/"+s.id, &req) == nil {
			st.Reachable, st.ServerPolicy, st.Status, st.Blocked = true, pol.ServerPolicy, req.Status, req.Blocked
		}
		out[i] = st
	}
	return out
}

// RequestApproval asks an Identity service to approve this server (signed
// with the server key); url is the address its members use, contact how
// the operator can reach the host.
func (s *Server) RequestApproval(ctx context.Context, issuer, url, contact string) (string, error) {
	if !s.cfg.trusts(issuer) {
		return "", fmt.Errorf("%s ne fait pas partie des services d'identité acceptés", issuer)
	}
	info, err := s.info(ctx)
	if err != nil {
		return "", err
	}
	req := idtoken.SignApproval(s.key, issuer, idtoken.ApprovalRequest{
		EncKey:  base64.RawURLEncoding.EncodeToString(s.enc.PublicKey().Bytes()),
		Name:    info.Name,
		URL:     url,
		Contact: contact,
	})
	body, _ := json.Marshal(req)
	hr, _ := http.NewRequestWithContext(ctx, "POST", issuerBaseURL(issuer)+"/v1/servers/requests", bytes.NewReader(body))
	hr.Header.Set("Content-Type", "application/json")
	res, err := issuerClient.Do(hr)
	if err != nil {
		return "", fmt.Errorf("%s injoignable : %w", issuer, err)
	}
	defer res.Body.Close()
	var out struct {
		Status string         `json:"status"`
		Error  *httpapi.Error `json:"error"`
	}
	json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != http.StatusOK {
		if out.Error != nil {
			return "", fmt.Errorf("%s a refusé la demande : %s", issuer, out.Error.Message)
		}
		return "", fmt.Errorf("%s a refusé la demande (HTTP %d)", issuer, res.StatusCode)
	}
	return out.Status, nil
}
