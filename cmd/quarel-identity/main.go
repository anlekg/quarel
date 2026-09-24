// Command quarel-identity runs the Quarel Identity service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anlekg/quarel/internal/identity"
	"github.com/anlekg/quarel/internal/tlsconf"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "admin" {
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

func run() error {
	cfg, err := identity.ConfigFromEnv()
	if err != nil {
		return err
	}
	srv, err := identity.Open(cfg)
	if err != nil {
		return err
	}
	defer srv.Close()
	if cfg.SMTP.Host == "" {
		slog.Warn("QUAREL_SMTP_HOST not set: verification codes will be printed in this log (development mode)")
	}

	if cfg.TLS.Mode == tlsconf.SelfSigned {
		return errors.New("QUAREL_TLS=self-signed is not supported for the Identity service: community servers check its certificate against public authorities (use acme, files, or off behind a reverse proxy)")
	}
	tlsCfg, err := cfg.TLS.Build(cfg.DataDir, nil)
	if err != nil {
		return fmt.Errorf("TLS: %w", err)
	}
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		// No global read/write timeouts: they would cut long-lived WebSocket connections.
		IdleTimeout: 2 * time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go srv.WatchDisabled(ctx, 15*time.Second) // accounts disabled by "quarel-identity admin" lose their live connections
	go func() {                               // expired conversation files
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
	go func() {
		<-ctx.Done()
		srv.DisconnectAll() // Shutdown does not wait for WebSocket (hijacked) connections
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdown)
	}()

	slog.Info("quarel-identity listening", "addr", cfg.Addr, "tls", cfg.TLS.Mode, "issuer", cfg.Issuer, "data", cfg.DataDir)
	if tlsCfg != nil {
		err = httpSrv.ListenAndServeTLS("", "")
	} else {
		err = httpSrv.ListenAndServe()
	}
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
