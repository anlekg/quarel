package voice

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/secret"
)

// EmbeddedConfig describes the livekit-server process run next to the community server.
type EmbeddedConfig struct {
	Binary     string // path or name of livekit-server
	DataDir    string // where keys and the generated config live
	SignalPort int    // loopback HTTP/WebSocket port, reached through the community server's /lk proxy
	TCPPort    int    // ICE/TCP fallback port (to open on the router)
	UDPPort    int    // single UDP port for media (to open on the router)
	PublicIP   string // "" = local addresses only, "auto" = discover via STUN, else this IP
	WebhookURL string // community server endpoint receiving room events
}

// Keys are the API credentials shared by the community server and livekit-server.
type Keys struct {
	Key    string `json:"key"`
	Secret string `json:"secret"`
}

// LoadOrCreateKeys reads livekit.keys from dir, generating it on first run.
func LoadOrCreateKeys(dir string) (Keys, error) {
	path := filepath.Join(dir, "livekit.keys")
	var k Keys
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &k); err != nil || k.Key == "" || k.Secret == "" {
			return k, fmt.Errorf("%s: invalid keys file", path)
		}
		return k, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return k, err
	}
	k = Keys{Key: "Q" + secret.Code(8), Secret: secret.NewToken()}
	data, _ = json.Marshal(k)
	return k, os.WriteFile(path, data, 0o600)
}

func (c EmbeddedConfig) yaml(k Keys) string {
	var b strings.Builder
	fmt.Fprintf(&b, "port: %d\nbind_addresses: [\"127.0.0.1\"]\n", c.SignalPort)
	fmt.Fprintf(&b, "rtc:\n  tcp_port: %d\n  udp_port: %d\n", c.TCPPort, c.UDPPort)
	switch c.PublicIP {
	case "":
	case "auto":
		b.WriteString("  use_external_ip: true\n")
	default:
		fmt.Fprintf(&b, "  node_ip: %q\n", c.PublicIP)
	}
	fmt.Fprintf(&b, "keys:\n  %s: %s\n", k.Key, k.Secret)
	fmt.Fprintf(&b, "webhook:\n  api_key: %s\n  urls: [%q]\n", k.Key, c.WebhookURL)
	b.WriteString("room:\n  auto_create: true\n  empty_timeout: 60\n")
	b.WriteString("logging:\n  level: warn\n")
	return b.String()
}

// RunEmbedded writes the config and keeps livekit-server running until ctx
// ends, restarting it with backoff if it exits. ready is closed once the
// server answers for the first time.
func RunEmbedded(ctx context.Context, c EmbeddedConfig, k Keys, ready chan<- struct{}) error {
	bin, err := exec.LookPath(c.Binary)
	if err != nil {
		return fmt.Errorf("livekit-server not found (%s): install it or set QUAREL_LIVEKIT_BIN, or QUAREL_VOICE=off", c.Binary)
	}
	cfgPath := filepath.Join(c.DataDir, "livekit.yaml")
	if err := os.WriteFile(cfgPath, []byte(c.yaml(k)), 0o600); err != nil {
		return err
	}
	go waitReady(ctx, fmt.Sprintf("http://127.0.0.1:%d/", c.SignalPort), ready)

	backoff := time.Second
	for {
		start := time.Now()
		cmd := exec.CommandContext(ctx, bin, "--config", cfgPath)
		cmd.WaitDelay = 5 * time.Second
		out, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		if err := cmd.Start(); err != nil {
			return err
		}
		slog.Info("livekit-server started", "pid", cmd.Process.Pid, "signal_port", c.SignalPort, "udp_port", c.UDPPort, "tcp_port", c.TCPPort)
		go relayLogs(out)
		err := cmd.Wait()
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		slog.Error("livekit-server exited, restarting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
	}
}

func relayLogs(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		slog.Warn("livekit: " + sc.Text())
	}
}

func waitReady(ctx context.Context, addr string, ready chan<- struct{}) {
	client := &http.Client{Timeout: time.Second}
	for ctx.Err() == nil {
		if resp, err := client.Get(addr); err == nil {
			resp.Body.Close()
			close(ready)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Proxy forwards requests under prefix (e.g. "/lk") to a loopback
// livekit-server, WebSocket signalling included, so clients only need the
// community server's HTTP port plus LiveKit's media ports.
func Proxy(prefix string, signalPort int) http.Handler {
	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", signalPort)}
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, prefix)
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host
		},
	}
}
