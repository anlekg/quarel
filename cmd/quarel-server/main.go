// Command quarel-server runs a self-hosted Quarel community server.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anlekg/quarel/internal/adminui"
	"github.com/anlekg/quarel/internal/backup"
	"github.com/anlekg/quarel/internal/community"
	"github.com/anlekg/quarel/internal/netdiag"
	"github.com/anlekg/quarel/internal/settings"
	"github.com/anlekg/quarel/internal/voice"
)

const upnpLease = time.Hour

// backupSpec: what a backup of the data directory contains.
func backupSpec(dir string) backup.Spec {
	return backup.Spec{Kind: "quarel-server", DataDir: dir, Database: "server.db", Required: []string{"server.key"},
		Files: []string{"livekit.keys", settings.FileName}, Dirs: []string{"attachments", "acme"}, Keep: []string{"admin.json"}}
}

func serverIdentity(dir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "server.key"))
	if err != nil {
		return "", false
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return "", false
	}
	return "serveur " + community.ServerID(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)), true
}

// opsTool: backup, restore and version, on the data directory.
func opsTool() backup.Tool {
	dir := settings.DataDir()
	return backup.Tool{
		Spec:      backupSpec(dir),
		MaxSchema: community.SchemaVersion(),
		Identity:  func() (string, bool) { return serverIdentity(dir) },
	}
}

func main() {
	if len(os.Args) > 1 {
		if handled, err := opsTool().Run(os.Args[1], os.Args[2:]); handled {
			if err != nil {
				fmt.Fprintln(os.Stderr, "erreur :", err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprintln(os.Stderr, "usage : quarel-server [backup <fichier> | restore <fichier> [--force] | version]")
		os.Exit(2)
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// currentUI is the administration interface once started (tray status).
var currentUI atomic.Pointer[adminui.UI]

// runService starts the administration interface, then runs the service under
// a supervisor that restarts it when its settings change, until ctx ends.
// Logs go to console (a file on Windows).
func runService(ctx context.Context, console io.Writer) error {
	logs := adminui.NewLogs(2000)
	out := io.MultiWriter(console, logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(out, nil)))

	dir := settings.DataDir()
	if err := settings.Load(dir); err != nil {
		slog.Error("settings file", "err", err)
	}

	sup := adminui.NewSupervisor()
	state := &live{}
	ui, err := adminui.New(adminui.Options{
		Product:        "Serveur communautaire Quarel",
		Kind:           "community",
		Fields:         fields,
		Validate:       func() error { _, err := community.ConfigFromEnv(); return err },
		Restart:        sup.Restart,
		Status:         state.status,
		Backup:         backupSpec(dir),
		Identity:       func() string { id, _ := serverIdentity(dir); return id },
		MaxSchema:      community.SchemaVersion(),
		StopForRestore: sup.Pause,
		API:            state.api(),
		Logs:           logs,
	})
	if err != nil {
		return err
	}
	currentUI.Store(ui)
	adminui.Serve(ctx, ui, adminAddr(), out)
	sup.Run(ctx, ui, func(ctx context.Context, ready func()) error { return serve(ctx, ready, state, out) })
	time.Sleep(200 * time.Millisecond) // let UPnP mappings be removed
	return nil
}

// adminAddr: QUAREL_ADMIN_ADDR (environment only), "off" to disable.
func adminAddr() string {
	if v := os.Getenv("QUAREL_ADMIN_ADDR"); v != "" {
		return v
	}
	return defaultAdminAddr
}

// serve runs the community service until ctx is cancelled, then releases
// everything (ports, voice server, router mappings, database).
func serve(ctx context.Context, ready func(), state *live, out io.Writer) error {
	cfg, err := community.ConfigFromEnv()
	if err != nil {
		return err
	}
	_, portStr, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return fmt.Errorf("QUAREL_ADDR: %w", err)
	}
	port, _ := strconv.Atoi(portStr)
	srv, err := community.Open(cfg)
	if err != nil {
		return err
	}
	defer srv.Close()
	defer state.set(nil, cfg, "", "", nil, nil)

	// Everything started below stops with ctx; wg waits for it before returning.
	var wg sync.WaitGroup
	defer wg.Wait()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Ports that must be reachable from the Internet.
	public := cfg.PublicPort
	if public == 0 {
		public = port
	}
	ports := []netdiag.Mapping{{Protocol: "TCP", External: public, Internal: port, Purpose: "API"}}
	voiceOn := cfg.Voice == "embedded"
	if voiceOn {
		if _, err := exec.LookPath(cfg.LiveKitBin); err != nil {
			voiceOn = false
		}
	}
	if voiceOn {
		ports = append(ports,
			netdiag.Mapping{Protocol: "UDP", External: cfg.VoiceUDPPort, Internal: cfg.VoiceUDPPort, Purpose: "voice"},
			netdiag.Mapping{Protocol: "TCP", External: cfg.VoiceTCPPort, Internal: cfg.VoiceTCPPort, Purpose: "voice (fallback)"})
	}

	var mapper *netdiag.PortMapper
	if cfg.UPnP {
		mapper = openPorts(ctx, ports, &wg)
	} else {
		slog.Info("UPnP disabled (QUAREL_UPNP=off): open these ports on the router yourself if needed", "ports", describe(ports))
	}
	srv.SetNetworkDiagnosis(func(ctx context.Context) netdiag.Diagnosis {
		var st *netdiag.Status
		if mapper != nil {
			s := mapper.Status()
			st = &s
		}
		ip, _ := netdiag.PublicIP(ctx, netdiag.DefaultSTUNServers)
		return netdiag.Diagnose(st, ip, ports)
	})

	wg.Add(2)
	go func() { defer wg.Done(); srv.WatchDisabled(ctx, cfg.DisabledPoll) }() // accounts disabled by their identity service
	go func() {                                                               // unsent uploads, orphaned files, old audit entries, expired sessions
		defer wg.Done()
		for {
			if err := srv.Housekeeping(ctx); err != nil {
				slog.Warn("housekeeping", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
		}
	}()

	// Loopback-only listener for LiveKit webhooks (plain HTTP: LiveKit cannot
	// verify our self-signed certificate).
	internal, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	internalSrv := &http.Server{Handler: srv.InternalHandler(), ReadHeaderTimeout: 10 * time.Second}
	go internalSrv.Serve(internal)
	defer internalSrv.Close()

	voiceText, err := setupVoice(ctx, cfg, srv, voiceOn, mapper, "http://"+internal.Addr().String()+"/internal/livekit/webhook", &wg)
	if err != nil {
		return err
	}

	claim, err := srv.PrepareClaim(ctx)
	if err != nil {
		return err
	}
	if claim != "" {
		banner := strings.Repeat("=", 64)
		fmt.Fprintf(out, "\n%s\n  Ce serveur n'a pas encore de propriétaire.\n  Code de revendication (usage unique) : %s\n  Un nouveau code est généré à chaque démarrage tant qu'il n'est pas utilisé.\n%s\n\n",
			banner, claim, banner)
	}

	tlsCfg, err := srv.TLSConfig()
	if err != nil {
		return fmt.Errorf("certificat : %w", err)
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("impossible d'écouter sur %s (port déjà utilisé par un autre programme ?) : %w", cfg.Addr, err)
	}
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No global read/write timeouts: they would cut long-lived WebSocket connections.
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		srv.DisconnectAll() // Shutdown does not wait for WebSocket (hijacked) connections
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdown)
	}()

	scheme := "http"
	if tlsCfg != nil {
		scheme = "https"
	}
	slog.Info("quarel-server listening", "url", scheme+"://"+hostFor(cfg.Addr), "tls", cfg.TLS.Mode, "server_id", srv.ID(),
		"trusted_issuers", cfg.TrustedIssuers, "voice", voiceOn, "data", cfg.DataDir)
	state.set(srv, cfg, claim, voiceText, mapper, ports)
	ready()
	if tlsCfg != nil {
		err = httpSrv.ServeTLS(ln, "", "")
	} else {
		err = httpSrv.Serve(ln)
	}
	cancel()
	<-stopped
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func hostFor(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}

func describe(ports []netdiag.Mapping) string {
	var parts []string
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d/%s (%s)", p.External, strings.ToLower(p.Protocol), p.Purpose))
	}
	return strings.Join(parts, ", ")
}

// openPorts maps ports on the router through UPnP and keeps them alive.
func openPorts(ctx context.Context, ports []netdiag.Mapping, wg *sync.WaitGroup) *netdiag.PortMapper {
	gw, err := netdiag.Discover(ctx)
	if err != nil {
		slog.Warn("UPnP: "+err.Error()+"; open these ports on the router yourself", "ports", describe(ports))
		return nil
	}
	pm := netdiag.NewPortMapper(gw, ports, upnpLease)
	st := pm.Refresh(ctx)
	slog.Info("UPnP: ports opened on the router", "router", st.Router, "local_ip", st.LocalIP, "external_ip", st.ExternalIP, "mapped", describe(st.Mapped))
	if len(st.Errors) > 0 {
		slog.Warn("UPnP: some ports could not be opened", "errors", st.Errors)
	}
	if netdiag.IsPrivate(st.ExternalIP) {
		slog.Warn("UPnP: the router's own address is private: this connection is behind another NAT (often carrier-grade NAT); the server is probably NOT reachable from the Internet", "external_ip", st.ExternalIP)
	}
	wg.Add(1)
	go func() { defer wg.Done(); pm.Run(ctx) }() // removes the mappings when ctx ends
	return pm
}

// setupVoice enables voice channels according to QUAREL_VOICE and says, in
// French, what the administrator should know about it.
func setupVoice(ctx context.Context, cfg community.Config, srv *community.Server, embeddedOK bool, mapper *netdiag.PortMapper, webhookURL string, wg *sync.WaitGroup) (string, error) {
	switch cfg.Voice {
	case "off":
		slog.Info("voice disabled (QUAREL_VOICE=off)")
		return "désactivé", nil

	case "external":
		if cfg.LiveKitURL == "" || cfg.LiveKitAPIURL == "" || cfg.LiveKitKey == "" || cfg.LiveKitSecret == "" {
			return "", errors.New("QUAREL_VOICE=external needs QUAREL_LIVEKIT_URL, QUAREL_LIVEKIT_API_URL, QUAREL_LIVEKIT_KEY and QUAREL_LIVEKIT_SECRET")
		}
		srv.EnableVoice(community.VoiceOptions{
			Backend:   voice.NewLiveKit(cfg.LiveKitAPIURL, cfg.LiveKitKey, cfg.LiveKitSecret),
			PublicURL: cfg.LiveKitURL,
		})
		go syncVoice(ctx, srv)
		return "serveur LiveKit externe", nil

	case "embedded":
		if !embeddedOK {
			slog.Warn("voice disabled: livekit-server not found (set QUAREL_LIVEKIT_BIN, or QUAREL_VOICE=off to silence this)", "bin", cfg.LiveKitBin)
			return "indisponible : programme livekit-server introuvable", nil
		}
		keys, err := voice.LoadOrCreateKeys(cfg.DataDir)
		if err != nil {
			return "", err
		}
		// Address announced to voice clients: the router's public address when
		// UPnP knows it, else STUN discovery by LiveKit, or local addresses only.
		publicIP := cfg.VoicePublicIP
		switch publicIP {
		case "local":
			publicIP = ""
		case "auto":
			if mapper != nil {
				if ip := mapper.Status().ExternalIP; ip != "" && !netdiag.IsPrivate(ip) {
					publicIP = ip
				}
			}
		}
		ec := voice.EmbeddedConfig{
			Binary:     cfg.LiveKitBin,
			DataDir:    cfg.DataDir,
			SignalPort: cfg.VoiceSignalPort,
			TCPPort:    cfg.VoiceTCPPort,
			UDPPort:    cfg.VoiceUDPPort,
			PublicIP:   publicIP,
			WebhookURL: webhookURL,
		}
		srv.EnableVoice(community.VoiceOptions{
			Backend: voice.NewLiveKit(fmt.Sprintf("http://127.0.0.1:%d", cfg.VoiceSignalPort), keys.Key, keys.Secret),
			Proxy:   voice.Proxy("/lk", cfg.VoiceSignalPort),
		})
		ready := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := voice.RunEmbedded(ctx, ec, keys, ready); err != nil && ctx.Err() == nil {
				slog.Error("voice server stopped", "err", err)
			}
		}()
		go func() {
			select {
			case <-ready:
				slog.Info("voice ready", "announced_ip", publicIP, "ports", fmt.Sprintf("%d/udp, %d/tcp", cfg.VoiceUDPPort, cfg.VoiceTCPPort))
				syncVoice(ctx, srv)
			case <-ctx.Done():
			}
		}()
		return fmt.Sprintf("intégré (ports %d/udp et %d/tcp)", cfg.VoiceUDPPort, cfg.VoiceTCPPort), nil
	}
	return "", fmt.Errorf("QUAREL_VOICE=%q: expected embedded, external or off", cfg.Voice)
}

func syncVoice(ctx context.Context, srv *community.Server) {
	if err := srv.SyncVoice(ctx); err != nil {
		slog.Warn("could not sync voice participants", "err", err)
	}
}
