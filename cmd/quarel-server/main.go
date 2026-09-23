// Command quarel-server runs a self-hosted Quarel community server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/anlekg/quarel/internal/community"
	"github.com/anlekg/quarel/internal/voice"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := setupVoice(ctx, cfg, srv); err != nil {
		return err
	}

	claim, err := srv.PrepareClaim(ctx)
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
	go func() {
		<-ctx.Done()
		srv.DisconnectAll() // Shutdown does not wait for WebSocket (hijacked) connections
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdown)
	}()

	slog.Info("quarel-server listening", "addr", cfg.Addr, "server_id", srv.ID(), "trusted_issuers", cfg.TrustedIssuers, "voice", cfg.Voice, "data", cfg.DataDir)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// setupVoice enables voice channels according to QUAREL_VOICE.
func setupVoice(ctx context.Context, cfg community.Config, srv *community.Server) error {
	switch cfg.Voice {
	case "off":
		slog.Info("voice disabled (QUAREL_VOICE=off)")
		return nil

	case "external":
		if cfg.LiveKitURL == "" || cfg.LiveKitAPIURL == "" || cfg.LiveKitKey == "" || cfg.LiveKitSecret == "" {
			return errors.New("QUAREL_VOICE=external needs QUAREL_LIVEKIT_URL, QUAREL_LIVEKIT_API_URL, QUAREL_LIVEKIT_KEY and QUAREL_LIVEKIT_SECRET")
		}
		srv.EnableVoice(community.VoiceOptions{
			Backend:   voice.NewLiveKit(cfg.LiveKitAPIURL, cfg.LiveKitKey, cfg.LiveKitSecret),
			PublicURL: cfg.LiveKitURL,
		})
		go syncVoice(ctx, srv)
		return nil

	case "embedded":
		if _, err := exec.LookPath(cfg.LiveKitBin); err != nil {
			slog.Warn("voice disabled: livekit-server not found (set QUAREL_LIVEKIT_BIN, or QUAREL_VOICE=off to silence this)", "bin", cfg.LiveKitBin)
			return nil
		}
		keys, err := voice.LoadOrCreateKeys(cfg.DataDir)
		if err != nil {
			return err
		}
		_, port, err := net.SplitHostPort(cfg.Addr)
		if err != nil {
			return fmt.Errorf("QUAREL_ADDR: %w", err)
		}
		ec := voice.EmbeddedConfig{
			Binary:     cfg.LiveKitBin,
			DataDir:    cfg.DataDir,
			SignalPort: cfg.VoiceSignalPort,
			TCPPort:    cfg.VoiceTCPPort,
			UDPPort:    cfg.VoiceUDPPort,
			PublicIP:   cfg.VoicePublicIP,
			WebhookURL: "http://127.0.0.1:" + port + "/internal/livekit/webhook",
		}
		srv.EnableVoice(community.VoiceOptions{
			Backend: voice.NewLiveKit(fmt.Sprintf("http://127.0.0.1:%d", cfg.VoiceSignalPort), keys.Key, keys.Secret),
			Proxy:   voice.Proxy("/lk", cfg.VoiceSignalPort),
		})
		ready := make(chan struct{})
		go func() {
			if err := voice.RunEmbedded(ctx, ec, keys, ready); err != nil {
				slog.Error("voice server stopped", "err", err)
			}
		}()
		go func() {
			select {
			case <-ready:
				slog.Info("voice ready", "open_ports", fmt.Sprintf("%d/udp, %d/tcp", cfg.VoiceUDPPort, cfg.VoiceTCPPort))
				syncVoice(ctx, srv)
			case <-ctx.Done():
			}
		}()
		return nil
	}
	return fmt.Errorf("QUAREL_VOICE=%q: expected embedded, external or off", cfg.Voice)
}

func syncVoice(ctx context.Context, srv *community.Server) {
	if err := srv.SyncVoice(ctx); err != nil {
		slog.Warn("could not sync voice participants", "err", err)
	}
}
