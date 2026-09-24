package identity

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/settings"

	"github.com/pion/turn/v5"
)

// Calls between friends are peer-to-peer: media never goes through Quarel.
// When two devices cannot reach each other directly (strict NATs), a TURN
// relay run by this service forwards their encrypted media (it cannot read
// it: WebRTC encrypts end to end with DTLS-SRTP). Users can refuse the relay.
// Credentials are short-lived and per user; the relay never forwards to
// private or local addresses, so it cannot be used to reach the host's network.

const turnCredentialTTL = 12 * time.Hour

// TURNConfig configures the embedded TURN relay.
type TURNConfig struct {
	Enabled           bool
	Listen            string // UDP address, e.g. ":3478"
	PublicIP          string // address given to clients, or "auto" (found by the program: UPnP or STUN, SetTURNPublicIP)
	MinPort, MaxPort  int    // relay port range, to open on the router/firewall
	AllowPrivatePeers bool   // development and tests only
}

func turnConfigFromEnv() (TURNConfig, error) {
	c := TURNConfig{Enabled: env("QUAREL_TURN", "off") == "on", Listen: env("QUAREL_TURN_LISTEN", ":3478"),
		PublicIP: env("QUAREL_TURN_PUBLIC_IP", "auto"), AllowPrivatePeers: settings.Get("QUAREL_TURN_ALLOW_PRIVATE") == "1"}
	lo, hi, ok := strings.Cut(env("QUAREL_TURN_PORTS", "49160-49200"), "-")
	var err1, err2 error
	c.MinPort, err1 = strconv.Atoi(lo)
	c.MaxPort, err2 = strconv.Atoi(hi)
	if !ok || err1 != nil || err2 != nil || c.MinPort < 1024 || c.MaxPort > 65535 || c.MinPort > c.MaxPort {
		return c, errors.New("relais d'appels : plage de ports invalide, par exemple 49160-49200 (QUAREL_TURN_PORTS)")
	}
	if c.Enabled && c.PublicIP != "auto" && net.ParseIP(c.PublicIP).To4() == nil {
		return c, errors.New("relais d'appels : adresse IP publique invalide (une adresse IPv4, ou « auto » pour la trouver automatiquement)")
	}
	return c, nil
}

func publicPeer(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || (ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xC0 == 64))
}

func (s *Server) turnSecret() (string, error) {
	path := filepath.Join(s.cfg.DataDir, "turn.secret")
	if data, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(data)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	secret := newSecret()
	return secret, os.WriteFile(path, []byte(secret+"\n"), 0o600)
}

// SetTURNPublicIP sets the address announced for the relay (found by the
// program when QUAREL_TURN_PUBLIC_IP=auto); it may change while running.
func (s *Server) SetTURNPublicIP(ip string) {
	s.turnIP.Store(ip)
}

// TURNPublicIP is the address announced for the relay ("" if unknown).
func (s *Server) TURNPublicIP() string {
	ip, _ := s.turnIP.Load().(string)
	return ip
}

// relayAddresses allocates relay ports and announces the current public IP.
type relayAddresses struct {
	*turn.RelayAddressGeneratorPortRange
	ip func() string
}

func (r relayAddresses) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	conn, addr, err := r.RelayAddressGeneratorPortRange.AllocatePacketConn(conf)
	if u, ok := addr.(*net.UDPAddr); ok && err == nil {
		u.IP = net.ParseIP(r.ip())
	}
	return conn, addr, err
}

// StartTURN runs the relay until the returned closer is closed (nil if disabled).
func (s *Server) StartTURN() (io.Closer, error) {
	c := s.cfg.TURN
	if !c.Enabled {
		return nil, nil
	}
	if c.PublicIP != "auto" {
		s.SetTURNPublicIP(c.PublicIP)
	}
	if net.ParseIP(s.TURNPublicIP()) == nil {
		return nil, errors.New("relais d'appels : adresse IP publique de cette machine introuvable (ni par l'UPnP de la box, ni par STUN) : indiquez-la (QUAREL_TURN_PUBLIC_IP)")
	}
	secret, err := s.turnSecret()
	if err != nil {
		return nil, err
	}
	s.turnKey = secret
	conn, err := net.ListenPacket("udp4", c.Listen)
	if err != nil {
		return nil, fmt.Errorf("TURN: %w", err)
	}
	srv, err := turn.NewServer(turn.ServerConfig{
		Realm:       s.cfg.Issuer,
		AuthHandler: turn.LongTermTURNRESTAuthHandler(secret, nil),
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: conn,
			RelayAddressGenerator: relayAddresses{&turn.RelayAddressGeneratorPortRange{
				RelayAddress: net.ParseIP(s.TURNPublicIP()), Address: "0.0.0.0",
				MinPort: uint16(c.MinPort), MaxPort: uint16(c.MaxPort),
			}, s.TURNPublicIP},
			PermissionHandler: func(_ net.Addr, peer net.IP) bool { return c.AllowPrivatePeers || publicPeer(peer) },
		}},
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("TURN: %w", err)
	}
	slog.Info("TURN relay ready", "listen", c.Listen, "public_ip", s.TURNPublicIP(), "relay_ports", fmt.Sprintf("%d-%d", c.MinPort, c.MaxPort))
	return srv, nil
}

type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// handleCallServers: GET /v1/calls/ice-servers → the STUN/TURN servers a
// device may use for a call, with its own short-lived relay credentials.
func (s *Server) handleCallServers(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.TURN.Enabled || s.turnKey == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ice_servers": []iceServer{}, "relay": false})
		return
	}
	me := sessionFrom(r).UserID
	if err := s.limit.turn.Check(me); err != nil {
		writeErr(w, r, err)
		return
	}
	user, pass, err := turn.GenerateLongTermTURNRESTCredentials(s.turnKey, me, turnCredentialTTL)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	_, port, _ := net.SplitHostPort(s.cfg.TURN.Listen)
	host := net.JoinHostPort(s.TURNPublicIP(), port)
	writeJSON(w, http.StatusOK, map[string]any{
		"ice_servers": []iceServer{
			{URLs: []string{"stun:" + host}},
			{URLs: []string{"turn:" + host + "?transport=udp"}, Username: user, Credential: pass},
		},
		"relay":      true,
		"expires_at": s.now().Add(turnCredentialTTL).UTC().Truncate(time.Second),
	})
}
