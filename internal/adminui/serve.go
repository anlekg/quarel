package adminui

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/settings"
)

// Serve starts the administration interface on addr ("off" disables it) and
// announces its address (and the setup code on first run) on out. If the
// port is busy, the service runs without it.
func Serve(ctx context.Context, ui *UI, addr string, out io.Writer) {
	if addr == "off" {
		return
	}
	cert, fingerprint, err := loadCertificate(settings.DataDir())
	if err != nil {
		slog.Warn("administration interface unavailable: certificate", "err", err)
		return
	}
	raw, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("administration interface unavailable", "addr", addr, "err", err)
		return
	}
	ln := newSniffListener(raw, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})
	hs := &http.Server{Handler: ui.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
		ErrorLog: log.New(io.Discard, "", 0)} // TLS warnings of browsers meeting the self-signed certificate
	go hs.Serve(ln)
	go func() {
		<-ctx.Done()
		hs.Close()
	}()
	urls := DisplayURLs(raw.Addr().String())
	banner := strings.Repeat("=", 64)
	if code := ui.SetupCode(); code != "" {
		slog.Info("administration interface: choose its password", "url", urls[0], "setup_code", code, "certificate_sha256", fingerprint)
		fmt.Fprintf(out, "\n%s\n  Administration : %s\n  Premier lancement : choisissez le mot de passe administrateur.\n  Depuis une autre machine, code d'installation : %s\n  Empreinte du certificat (à comparer à celle du navigateur) :\n  %s\n%s\n\n",
			banner, strings.Join(urls, "  ou  "), code, fingerprint, banner)
	} else {
		slog.Info("administration interface", "url", strings.Join(urls, " "), "certificate_sha256", fingerprint)
	}
}

// DisplayURLs turns a listening address into the addresses to type in a
// browser: for "all interfaces", this machine's local network addresses
// (HTTPS: plain HTTP only works from this machine itself).
func DisplayURLs(listen string) []string {
	scheme := "https://"
	if plainHTTPAllowed() {
		scheme = "http://"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return []string{"http://" + listen}
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsUnspecified() {
		if ip != nil && ip.IsLoopback() {
			scheme = "http://"
		}
		return []string{scheme + net.JoinHostPort(host, port)}
	}
	var out []string
	for _, a := range LocalAddresses() {
		out = append(out, scheme+net.JoinHostPort(a, port))
	}
	return append(out, "http://localhost:"+port)
}

// LocalAddresses lists this machine's private IPv4 addresses on physical-looking
// interfaces (container bridges skipped).
func LocalAddresses() []string {
	ifaces, _ := net.Interfaces()
	var out []string
	for _, ifc := range ifaces {
		n := ifc.Name
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 ||
			strings.HasPrefix(n, "docker") || strings.HasPrefix(n, "br-") || strings.HasPrefix(n, "veth") || strings.HasPrefix(n, "virbr") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && ipn.IP.IsPrivate() {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}

// Port returns the port of a listening address, or 0.
func Port(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return n
}
