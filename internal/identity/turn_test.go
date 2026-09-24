package identity

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pion/turn/v5"
)

// turnClient allocates a relay on the service with credentials from the API.
func turnClient(t *testing.T, e *testEnv, token string) (net.PacketConn, func()) {
	t.Helper()
	var servers struct {
		ICEServers []iceServer `json:"ice_servers"`
		Relay      bool        `json:"relay"`
	}
	e.expect(200, "", e.call("GET", "/v1/calls/ice-servers", token, nil, &servers))
	if !servers.Relay || len(servers.ICEServers) != 2 {
		t.Fatalf("ice servers = %+v", servers)
	}
	tu := servers.ICEServers[1]
	addr := strings.TrimSuffix(strings.TrimPrefix(tu.URLs[0], "turn:"), "?transport=udp")
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c, err := turn.NewClient(&turn.ClientConfig{TURNServerAddr: addr, STUNServerAddr: addr, Conn: conn,
		Username: tu.Username, Password: tu.Credential, Realm: e.srv.cfg.Issuer})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Listen(); err != nil {
		t.Fatal(err)
	}
	relay, err := c.Allocate()
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	return relay, func() { relay.Close(); c.Close(); conn.Close() }
}

func startTURN(t *testing.T, allowPrivate bool) *testEnv {
	e := newEnv(t)
	e.srv.cfg.TURN = TURNConfig{Enabled: true, Listen: "127.0.0.1:0", PublicIP: "127.0.0.1", MinPort: 51000, MaxPort: 51050, AllowPrivatePeers: allowPrivate}
	// Pick a free port for the relay listener.
	l, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	e.srv.cfg.TURN.Listen = l.LocalAddr().String()
	l.Close()
	closer, err := e.srv.StartTURN()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closer.Close() })
	return e
}

func TestTURNRelaysButNotToPrivateNetworks(t *testing.T) {
	// The real configuration: peers on private or local addresses are refused,
	// so the relay cannot be used to reach the host's own network.
	e := startTURN(t, false)
	lr, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	relay, done := turnClient(t, e, lr.SessionToken)
	defer done()
	target, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	defer target.Close()
	if _, err := relay.WriteTo([]byte("sonde"), target.LocalAddr()); err == nil {
		target.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		if _, _, err := target.ReadFrom(make([]byte, 64)); err == nil {
			t.Fatal("the relay forwarded to a local address")
		}
	}
	// Without a session, no credentials.
	e.expect(401, "unauthorized", e.call("GET", "/v1/calls/ice-servers", "", nil, nil))
}

func TestTURNRelays(t *testing.T) {
	e := startTURN(t, true) // tests run on 127.0.0.1
	lr, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	relay, done := turnClient(t, e, lr.SessionToken)
	defer done()
	target, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	defer target.Close()
	if _, err := relay.WriteTo([]byte("bonjour"), target.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	target.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, from, err := target.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "bonjour" || from.String() != relay.LocalAddr().String() {
		t.Fatalf("relayed %q from %v (relay %v): %v", buf[:n], from, relay.LocalAddr(), err)
	}
	// Wrong credentials are refused.
	conn, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	defer conn.Close()
	c, _ := turn.NewClient(&turn.ClientConfig{TURNServerAddr: e.srv.cfg.TURN.Listen, Conn: conn, Username: "9999999999:x", Password: "faux", Realm: e.srv.cfg.Issuer})
	c.Listen()
	defer c.Close()
	if _, err := c.Allocate(); err == nil {
		t.Fatal("allocation with forged credentials")
	}
}

func TestICEServersWithoutRelay(t *testing.T) {
	e := newEnv(t)
	lr, _ := e.registerVerified("carol@example.com", "carol", "mot-de-passe-carol")
	req, _ := http.NewRequest("GET", e.http.URL+"/v1/calls/ice-servers", nil)
	req.Header.Set("Authorization", "Bearer "+lr.SessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		ICEServers []iceServer `json:"ice_servers"`
		Relay      bool        `json:"relay"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Relay || len(out.ICEServers) != 0 {
		t.Fatalf("relay offered while disabled: %+v", out)
	}
}
