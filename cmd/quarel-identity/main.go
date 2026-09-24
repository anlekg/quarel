// Command quarel-identity runs the Quarel Identity service.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/anlekg/quarel/internal/adminui"
	"github.com/anlekg/quarel/internal/backup"
	"github.com/anlekg/quarel/internal/identity"
	"github.com/anlekg/quarel/internal/settings"
	"github.com/anlekg/quarel/internal/tlsconf"
)

func backupSpec(dir string) backup.Spec {
	return backup.Spec{Kind: "quarel-identity", DataDir: dir, Database: "identity.db", Required: []string{"signing.key"},
		Files: []string{"retired-keys.json", "turn.secret", settings.FileName}, Dirs: []string{"dm-files", "acme"}, Keep: []string{"admin.json"}}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] != "admin" {
		dir := settings.DataDir()
		settings.Load(dir)
		tool := backup.Tool{
			Spec:      backupSpec(dir),
			MaxSchema: identity.SchemaVersion(),
			Identity: func() (string, bool) {
				iss := settings.Get("QUAREL_ISSUER")
				return "service " + iss, iss != ""
			},
		}
		handled, err := tool.Run(os.Args[1], os.Args[2:])
		if !handled {
			fmt.Fprintln(os.Stderr, "usage : quarel-identity [admin … | backup <fichier> | restore <fichier> [--force] | version]")
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "erreur :", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		settings.Load(settings.DataDir())
		if err := admin(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "erreur :", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// run starts the administration interface, then runs the service under a
// supervisor that restarts it when its settings change.
func run() error {
	logs := adminui.NewLogs(2000)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logs), nil)))
	dir := settings.DataDir()
	if err := settings.Load(dir); err != nil {
		slog.Error("settings file", "err", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sup := adminui.NewSupervisor()
	state := &live{restart: sup.Restart}
	ui, err := adminui.New(adminui.Options{
		Product:        "Service d'identité Quarel",
		Kind:           "identity",
		Fields:         fields,
		Validate:       func() error { _, err := validConfig(); return err },
		Restart:        sup.Restart,
		Status:         state.status,
		Backup:         backupSpec(dir),
		Identity:       func() string { return "service " + settings.Get("QUAREL_ISSUER") },
		MaxSchema:      identity.SchemaVersion(),
		StopForRestore: sup.Pause,
		API:            state.api(),
		Logs:           logs,
	})
	if err != nil {
		return err
	}
	adminui.Serve(ctx, ui, adminAddr(), os.Stderr)
	sup.Run(ctx, ui, func(ctx context.Context, ready func()) error { return serve(ctx, ready, state) })
	return nil
}

func validConfig() (identity.Config, error) {
	cfg, err := identity.ConfigFromEnv()
	if err == nil && cfg.TLS.Mode == tlsconf.SelfSigned {
		err = errors.New("certificat automatique impossible pour le service d'identité : les serveurs communautaires le vérifient auprès des autorités publiques (choisissez Let's Encrypt, vos fichiers, ou aucun derrière un proxy HTTPS)")
	}
	return cfg, err
}

func adminAddr() string {
	if v := os.Getenv("QUAREL_ADMIN_ADDR"); v != "" {
		return v
	}
	return defaultAdminAddr
}

// serve runs the Identity service until ctx is cancelled.
func serve(ctx context.Context, ready func(), state *live) error {
	cfg, err := validConfig()
	if err != nil {
		return err
	}
	srv, err := identity.Open(cfg)
	if err != nil {
		return err
	}
	defer srv.Close()
	defer state.set(nil, cfg)
	if cfg.SMTP.Host == "" {
		slog.Warn("QUAREL_SMTP_HOST not set: verification codes will be printed in this log (development mode)")
	}

	turnSrv, err := srv.StartTURN()
	if err != nil {
		return err
	}
	if turnSrv != nil {
		defer turnSrv.Close()
	}

	tlsCfg, err := cfg.TLS.Build(cfg.DataDir, nil)
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
		// No global read/write timeouts: they would cut long-lived WebSocket connections.
		IdleTimeout: 2 * time.Minute,
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wg.Add(2)
	go func() { defer wg.Done(); srv.WatchDisabled(ctx, 15*time.Second) }() // disabled accounts lose their live connections
	go func() {                                                             // expired conversation files
		defer wg.Done()
		for {
			if err := srv.CleanupFiles(ctx); err != nil {
				slog.Warn("conversation files cleanup", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
		}
	}()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		srv.DisconnectAll() // Shutdown does not wait for WebSocket (hijacked) connections
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdown)
	}()

	slog.Info("quarel-identity listening", "addr", cfg.Addr, "tls", cfg.TLS.Mode, "issuer", cfg.Issuer, "data", cfg.DataDir)
	state.set(srv, cfg)
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
