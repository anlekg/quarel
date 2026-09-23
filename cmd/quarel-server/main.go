// Command quarel-server runs a self-hosted Quarel community server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/anlekg/quarel/internal/community"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := community.ConfigFromEnv()
	srv, err := community.Open(cfg)
	if err != nil {
		return err
	}
	defer srv.Close()

	claim, err := srv.PrepareClaim(context.Background())
	if err != nil {
		return err
	}
	if claim != "" {
		banner := strings.Repeat("=", 64)
		fmt.Fprintf(os.Stderr, "\n%s\n  Ce serveur n'a pas encore de propriétaire.\n  Code de revendication (usage unique) : %s\n  Un nouveau code est généré à chaque démarrage tant qu'il n'est pas utilisé.\n%s\n\n",
			banner, claim, banner)
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No global read/write timeouts: they would cut long-lived WebSocket connections.
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

	slog.Info("quarel-server listening", "addr", cfg.Addr, "server_id", srv.ID(), "trusted_issuers", cfg.TrustedIssuers, "data", cfg.DataDir)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
