// Command quarel-identity runs the Quarel Identity service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anlekg/quarel/internal/identity"
)

func main() {
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

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No global read/write timeouts: they would cut long-lived WebSocket connections.
		IdleTimeout: 2 * time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		srv.DisconnectAll() // Shutdown does not wait for WebSocket (hijacked) connections
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdown)
	}()

	slog.Info("quarel-identity listening", "addr", cfg.Addr, "issuer", cfg.Issuer, "data", cfg.DataDir)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
