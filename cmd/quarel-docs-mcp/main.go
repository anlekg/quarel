// Command quarel-docs-mcp serves the quarel.app wiki to AI assistants (Model
// Context Protocol): over HTTP at /mcp (public instance: https://quarel.app/mcp),
// or over stdio for a local assistant.
//
//	quarel-docs-mcp -docs site/src/content/docs -addr 127.0.0.1:8095
//	quarel-docs-mcp -docs site/src/content/docs -stdio
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anlekg/quarel/internal/backup"
	"github.com/anlekg/quarel/internal/docsmcp"
	"github.com/anlekg/quarel/internal/ratelimit"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	dir := flag.String("docs", "site/src/content/docs", "folder of the wiki pages (Markdown)")
	addr := flag.String("addr", "127.0.0.1:8095", "HTTP listen address")
	stdio := flag.Bool("stdio", false, "serve one assistant over stdin/stdout instead of HTTP")
	proxies := flag.String("trusted-proxies", "", "reverse proxies whose X-Forwarded-For is trusted (CIDR, comma-separated)")
	flag.Parse()

	docs, err := docsmcp.Load(os.DirFS(*dir))
	if err != nil {
		slog.Error("loading the docs", "dir", *dir, "err", err)
		os.Exit(1)
	}
	server := docsmcp.NewServer(docs, backup.Version())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *stdio {
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
			slog.Error("stdio", "err", err)
			os.Exit(1)
		}
		return
	}

	trusted, err := ratelimit.ParseProxies(*proxies)
	if err != nil {
		slog.Error("trusted-proxies", "err", err)
		os.Exit(1)
	}
	limiter := ratelimit.New(120, time.Minute)
	mux := http.NewServeMux()
	mux.Handle("/mcp", limiter.Wrap(trusted.ClientIP, docsmcp.Handler(server)))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	slog.Info("quarel-docs-mcp", "addr", *addr, "pages", len(docs.Pages))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}
}
