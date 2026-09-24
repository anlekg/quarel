package adminui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Serve starts the administration interface on addr ("off" disables it) and
// announces its address (and the setup code on first run) on out. If the
// port is busy, the service runs without it.
func Serve(ctx context.Context, ui *UI, addr string, out io.Writer) {
	if addr == "off" {
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("administration interface unavailable", "addr", addr, "err", err)
		return
	}
	hs := &http.Server{Handler: ui.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go hs.Serve(ln)
	go func() {
		<-ctx.Done()
		hs.Close()
	}()
	urls := DisplayURLs(ln.Addr().String())
	banner := strings.Repeat("=", 64)
	if code := ui.SetupCode(); code != "" {
		slog.Info("administration interface: choose its password", "url", urls[0], "setup_code", code)
		fmt.Fprintf(out, "\n%s\n  Administration : %s\n  Premier lancement : choisissez le mot de passe administrateur.\n  Depuis une autre machine, code d'installation : %s\n%s\n\n",
			banner, strings.Join(urls, "  ou  "), code, banner)
	} else {
		slog.Info("administration interface", "url", strings.Join(urls, " "))
	}
}

// DisplayURLs turns a listening address into the addresses to type in a
// browser: for "all interfaces", this machine's local network addresses.
func DisplayURLs(listen string) []string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return []string{"http://" + listen}
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsUnspecified() {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}
	var out []string
	for _, a := range LocalAddresses() {
		out = append(out, "http://"+net.JoinHostPort(a, port))
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
