// Package netdiag makes a home-hosted server reachable from the Internet and
// explains when it cannot be: UPnP port mapping on the router, public IP
// discovery through STUN, and a reachability diagnosis.
package netdiag

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
)

// Mapping is a port to open on the router.
type Mapping struct {
	Protocol string `json:"protocol"` // TCP | UDP
	External int    `json:"external"`
	Internal int    `json:"internal"`
	Purpose  string `json:"purpose"`
}

// Gateway is a router able to map ports (UPnP IGD).
type Gateway interface {
	Name() string
	LocalIP() string // this machine's address as seen by the router
	ExternalIP(ctx context.Context) (string, error)
	Add(ctx context.Context, m Mapping, internalIP string, lease time.Duration) error
	Delete(ctx context.Context, m Mapping) error
}

// igdClient is what the UPnP WANIPConnection/WANPPPConnection clients share.
type igdClient interface {
	AddPortMappingCtx(ctx context.Context, remoteHost string, extPort uint16, proto string, intPort uint16, intClient string, enabled bool, desc string, lease uint32) error
	DeletePortMappingCtx(ctx context.Context, remoteHost string, extPort uint16, proto string) error
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
	LocalAddr() net.IP
}

type upnpGateway struct {
	c    igdClient
	name string
}

func (g *upnpGateway) Name() string    { return g.name }
func (g *upnpGateway) LocalIP() string { return g.c.LocalAddr().String() }
func (g *upnpGateway) ExternalIP(ctx context.Context) (string, error) {
	return g.c.GetExternalIPAddressCtx(ctx)
}
func (g *upnpGateway) Add(ctx context.Context, m Mapping, ip string, lease time.Duration) error {
	return g.c.AddPortMappingCtx(ctx, "", uint16(m.External), m.Protocol, uint16(m.Internal), ip, true, "Quarel "+m.Purpose, uint32(lease.Seconds()))
}
func (g *upnpGateway) Delete(ctx context.Context, m Mapping) error {
	return g.c.DeletePortMappingCtx(ctx, "", uint16(m.External), m.Protocol)
}

// Discover looks for a UPnP Internet gateway on the local network.
func Discover(ctx context.Context) (Gateway, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	wrap := func(c igdClient, friendly string) Gateway { return &upnpGateway{c: c, name: friendly} }
	if cs, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx); err == nil && len(cs) > 0 {
		return wrap(cs[0], cs[0].RootDevice.Device.FriendlyName), nil
	}
	if cs, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx); err == nil && len(cs) > 0 {
		return wrap(cs[0], cs[0].RootDevice.Device.FriendlyName), nil
	}
	if cs, _, err := internetgateway2.NewWANPPPConnection1ClientsCtx(ctx); err == nil && len(cs) > 0 {
		return wrap(cs[0], cs[0].RootDevice.Device.FriendlyName), nil
	}
	return nil, errors.New("no UPnP gateway found (UPnP disabled on the router, or not on a home network)")
}

// PortMapper keeps port mappings alive on a gateway and removes them on stop.
type PortMapper struct {
	gw       Gateway
	mappings []Mapping
	lease    time.Duration

	mu     sync.Mutex
	status Status
}

// Status is the last known state of the port mappings.
type Status struct {
	Router     string    `json:"router"`
	LocalIP    string    `json:"local_ip"`
	ExternalIP string    `json:"external_ip"`
	Mapped     []Mapping `json:"mapped"`
	Errors     []string  `json:"errors"`
	CheckedAt  time.Time `json:"checked_at"`
}

// NewPortMapper prepares mappings on gw, renewed before each lease ends.
func NewPortMapper(gw Gateway, mappings []Mapping, lease time.Duration) *PortMapper {
	return &PortMapper{gw: gw, mappings: mappings, lease: lease}
}

// Refresh (re)creates every mapping once and records the outcome.
func (p *PortMapper) Refresh(ctx context.Context) Status {
	st := Status{Router: p.gw.Name(), LocalIP: p.gw.LocalIP(), Mapped: []Mapping{}, Errors: []string{}, CheckedAt: time.Now().UTC()}
	for _, m := range p.mappings {
		if err := p.gw.Add(ctx, m, st.LocalIP, p.lease); err != nil {
			st.Errors = append(st.Errors, fmt.Sprintf("%s %d (%s): %v", m.Protocol, m.External, m.Purpose, err))
			continue
		}
		st.Mapped = append(st.Mapped, m)
	}
	if ip, err := p.gw.ExternalIP(ctx); err == nil {
		st.ExternalIP = ip
	} else {
		st.Errors = append(st.Errors, "external IP: "+err.Error())
	}
	p.mu.Lock()
	p.status = st
	p.mu.Unlock()
	return st
}

// Status returns the last Refresh result.
func (p *PortMapper) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// Run keeps the mappings alive until ctx ends, then deletes them.
func (p *PortMapper) Run(ctx context.Context) {
	tick := time.NewTicker(p.lease / 2)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if st := p.Refresh(ctx); len(st.Errors) > 0 {
				slog.Warn("UPnP renewal", "errors", st.Errors)
			}
		case <-ctx.Done():
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			for _, m := range p.Status().Mapped {
				if err := p.gw.Delete(cleanup, m); err != nil {
					slog.Warn("UPnP: could not remove mapping", "port", m.External, "protocol", m.Protocol, "err", err)
				}
			}
			cancel()
			return
		}
	}
}

// --- STUN (RFC 5389): public address as seen from the Internet ---

// DefaultSTUNServers are public STUN servers asked for our public IP.
var DefaultSTUNServers = []string{"stun.l.google.com:19302", "stun.cloudflare.com:3478"}

const stunMagic = 0x2112A442

// PublicIP asks STUN servers for this machine's public address.
func PublicIP(ctx context.Context, servers []string) (string, error) {
	var lastErr error
	for _, s := range servers {
		ip, err := stunQuery(ctx, s)
		if err == nil {
			return ip, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("STUN: %w", lastErr)
}

func stunQuery(ctx context.Context, server string) (string, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "udp4", server) // IPv4: compared with the router's IPv4 address
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)

	req := make([]byte, 20) // Binding Request, no attributes
	binary.BigEndian.PutUint16(req[0:], 0x0001)
	binary.BigEndian.PutUint32(req[4:], stunMagic)
	txID := req[8:20]
	rand.Read(txID)
	if _, err := conn.Write(req); err != nil {
		return "", err
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return parseSTUN(buf[:n], txID)
}

func parseSTUN(msg, txID []byte) (string, error) {
	if len(msg) < 20 || binary.BigEndian.Uint16(msg[0:]) != 0x0101 || binary.BigEndian.Uint32(msg[4:]) != stunMagic || string(msg[8:20]) != string(txID) {
		return "", errors.New("unexpected STUN response")
	}
	attrs := msg[20:]
	for len(attrs) >= 4 {
		typ, size := binary.BigEndian.Uint16(attrs[0:]), int(binary.BigEndian.Uint16(attrs[2:]))
		if len(attrs) < 4+size {
			break
		}
		val := attrs[4 : 4+size]
		if (typ == 0x0020 || typ == 0x0001) && size >= 8 && val[1] == 0x01 { // (XOR-)MAPPED-ADDRESS, IPv4
			ip := make(net.IP, 4)
			copy(ip, val[4:8])
			if typ == 0x0020 {
				var magic [4]byte
				binary.BigEndian.PutUint32(magic[:], stunMagic)
				for i := range ip {
					ip[i] ^= magic[i]
				}
			}
			return ip.String(), nil
		}
		attrs = attrs[4+(size+3)/4*4:]
	}
	return "", errors.New("no mapped address in STUN response")
}

// --- diagnosis ---

// Verdicts of Diagnose.
const (
	VerdictOK         = "ok"           // UPnP mapped every port and the router's address is the public one
	VerdictManual     = "manual_ports" // no UPnP: open the ports by hand on the router
	VerdictDoubleNAT  = "double_nat"   // the router is itself behind another NAT (often carrier-grade NAT)
	VerdictPartial    = "partial"      // some ports could not be mapped
	VerdictNoPublicIP = "unknown"      // public IP could not be determined
)

// Diagnosis explains whether the server is likely reachable from the Internet.
type Diagnosis struct {
	Verdict  string    `json:"verdict"`
	PublicIP string    `json:"public_ip"` // from STUN
	UPnP     *Status   `json:"upnp"`      // nil when UPnP is off or no gateway was found
	Ports    []Mapping `json:"ports"`     // what must be reachable
}

// Diagnose combines the port mapping status and the STUN-observed address.
func Diagnose(upnp *Status, stunIP string, ports []Mapping) Diagnosis {
	d := Diagnosis{PublicIP: stunIP, UPnP: upnp, Ports: ports}
	switch {
	case upnp == nil:
		d.Verdict = VerdictManual
	case upnp.ExternalIP != "" && (IsPrivate(upnp.ExternalIP) || stunIP != "" && upnp.ExternalIP != stunIP):
		d.Verdict = VerdictDoubleNAT
	case len(upnp.Errors) > 0 || len(upnp.Mapped) < len(ports):
		d.Verdict = VerdictPartial
	case stunIP == "":
		d.Verdict = VerdictNoPublicIP
	default:
		d.Verdict = VerdictOK
	}
	return d
}

var privateNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "169.254.0.0/16", "127.0.0.0/8"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

// IsPrivate reports whether ip is a private, carrier-grade NAT or local address.
func IsPrivate(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	for _, n := range privateNets {
		if n.Contains(p) {
			return true
		}
	}
	return false
}
