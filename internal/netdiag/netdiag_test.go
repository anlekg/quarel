package netdiag

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeSTUN answers Binding Requests with XOR-MAPPED-ADDRESS = mapped.
func fakeSTUN(t *testing.T, mapped string) string {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 20 {
				continue
			}
			resp := make([]byte, 20+12)
			binary.BigEndian.PutUint16(resp[0:], 0x0101)
			binary.BigEndian.PutUint16(resp[2:], 12)
			binary.BigEndian.PutUint32(resp[4:], stunMagic)
			copy(resp[8:20], buf[8:20])
			attr := resp[20:]
			binary.BigEndian.PutUint16(attr[0:], 0x0020)
			binary.BigEndian.PutUint16(attr[2:], 8)
			attr[5] = 0x01
			ip := net.ParseIP(mapped).To4()
			var magic [4]byte
			binary.BigEndian.PutUint32(magic[:], stunMagic)
			for i := range 4 {
				attr[8+i] = ip[i] ^ magic[i]
			}
			pc.WriteTo(resp, addr)
		}
	}()
	return pc.LocalAddr().String()
}

func TestPublicIP(t *testing.T) {
	ctx := context.Background()
	ip, err := PublicIP(ctx, []string{"127.0.0.1:9", fakeSTUN(t, "203.0.113.7")}) // first server is silent
	if err != nil || ip != "203.0.113.7" {
		t.Fatalf("PublicIP = %q, %v", ip, err)
	}
}

type fakeGateway struct {
	mu       sync.Mutex
	external string
	fail     map[int]bool
	mapped   map[int]string
}

func (g *fakeGateway) Name() string                               { return "Box de test" }
func (g *fakeGateway) LocalIP() string                            { return "192.168.1.20" }
func (g *fakeGateway) ExternalIP(context.Context) (string, error) { return g.external, nil }
func (g *fakeGateway) Add(_ context.Context, m Mapping, ip string, _ time.Duration) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fail[m.External] {
		return errors.New("ConflictInMappingEntry")
	}
	g.mapped[m.External] = ip
	return nil
}
func (g *fakeGateway) Delete(_ context.Context, m Mapping) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.mapped, m.External)
	return nil
}

var ports = []Mapping{{"TCP", 8090, 8090, "API"}, {"UDP", 7882, 7882, "voix"}, {"TCP", 7881, 7881, "voix (secours)"}}

func TestPortMapperAndDiagnosis(t *testing.T) {
	g := &fakeGateway{external: "203.0.113.7", fail: map[int]bool{}, mapped: map[int]string{}}
	pm := NewPortMapper(g, ports, time.Hour)
	st := pm.Refresh(context.Background())
	if len(st.Mapped) != 3 || g.mapped[7882] != "192.168.1.20" {
		t.Fatalf("status = %+v", st)
	}
	if d := Diagnose(&st, "203.0.113.7", ports); d.Verdict != VerdictOK {
		t.Fatalf("verdict = %s", d.Verdict)
	}

	// Mappings are removed when the server stops.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { pm.Run(ctx); close(done) }()
	cancel()
	<-done
	if len(g.mapped) != 0 {
		t.Fatalf("mappings left: %v", g.mapped)
	}

	g.fail[7881] = true
	st = pm.Refresh(context.Background())
	if d := Diagnose(&st, "203.0.113.7", ports); d.Verdict != VerdictPartial {
		t.Fatalf("partial verdict = %s", d.Verdict)
	}

	// The router's "external" address is itself private: carrier-grade NAT.
	g.fail = map[int]bool{}
	g.external = "100.72.1.9"
	st = pm.Refresh(context.Background())
	if d := Diagnose(&st, "203.0.113.7", ports); d.Verdict != VerdictDoubleNAT {
		t.Fatalf("CGNAT verdict = %s", d.Verdict)
	}
	if d := Diagnose(nil, "203.0.113.7", ports); d.Verdict != VerdictManual {
		t.Fatalf("no-UPnP verdict = %s", d.Verdict)
	}
}
