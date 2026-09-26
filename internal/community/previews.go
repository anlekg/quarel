package community

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/anlekg/quarel/internal/netguard"
	"golang.org/x/net/html"
)

// Link previews: after a message is posted, the server fetches the pages it
// links to (at most 3) and reads their title/description (Open Graph). The
// server — not the members — contacts those sites. To stop anyone from using
// it to probe the host's own network (SSRF), connections to private, local
// and internal addresses are refused after DNS resolution, only ports 80/443
// are allowed, and time, size and redirects are capped.

const (
	maxPreviewsPerMessage = 3
	previewTimeout        = 8 * time.Second
	previewMaxBytes       = 1 << 20
	previewMaxRedirects   = 3
)

type linkEmbed struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	SiteName    string `json:"site_name"`
	ImageURL    string `json:"image_url"` // not fetched by the server; clients decide whether to load it
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"]+`)

func extractURLs(content string) []string {
	var out []string
	seen := map[string]bool{}
	for _, u := range urlRe.FindAllString(content, -1) {
		u = strings.TrimRight(u, ".,;:!?)]}'")
		if seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
		if len(out) == maxPreviewsPerMessage {
			break
		}
	}
	return out
}

// previewer fetches pages safely.
type previewer struct {
	client       *http.Client
	allowPrivate bool // tests only
}

var errForbiddenAddress = errors.New("link preview: address not allowed")

func newPreviewer(allowPrivate bool) *previewer {
	p := &previewer{allowPrivate: allowPrivate}
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		// Control sees the resolved address actually dialled: DNS rebinding cannot bypass it.
		Control: func(network, address string, _ syscall.RawConn) error {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || (!p.allowPrivate && (!netguard.Public(ip) || (port != "80" && port != "443"))) {
				return errForbiddenAddress
			}
			return nil
		},
	}
	p.client = &http.Client{
		Timeout: previewTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			Proxy:                 nil,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
			MaxIdleConns:          4,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= previewMaxRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	return p
}

func (p *previewer) fetch(ctx context.Context, raw string) (*linkEmbed, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, errors.New("unsupported URL")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "QuarelBot/1.0 (link preview; +https://quarel.app)")
	req.Header.Set("Accept", "text/html")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return nil, fmt.Errorf("not an HTML page (%d %s)", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	return parsePreview(io.LimitReader(resp.Body, previewMaxBytes), raw)
}

func parsePreview(r io.Reader, pageURL string) (*linkEmbed, error) {
	e := &linkEmbed{URL: pageURL}
	var title string
	z := html.NewTokenizer(r)
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			goto done
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "title":
				inTitle = true
			case "meta":
				var key, content string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch strings.ToLower(string(k)) {
					case "property", "name":
						key = strings.ToLower(string(v))
					case "content":
						content = strings.TrimSpace(string(v))
					}
				}
				switch key {
				case "og:title":
					e.Title = content
				case "og:description":
					e.Description = content
				case "description":
					if e.Description == "" {
						e.Description = content
					}
				case "og:site_name":
					e.SiteName = content
				case "og:image":
					if iu, err := url.Parse(content); err == nil && (iu.Scheme == "https" || iu.Scheme == "http") {
						e.ImageURL = content
					}
				}
			case "body":
				goto done // metadata lives in <head>
			}
		case html.TextToken:
			if inTitle {
				title += string(z.Text())
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "title" {
				inTitle = false
			}
		}
	}
done:
	if e.Title == "" {
		e.Title = strings.TrimSpace(title)
	}
	e.Title, e.Description, e.SiteName = clip(e.Title, 200), clip(e.Description, 400), clip(e.SiteName, 100)
	if e.Title == "" && e.Description == "" {
		return nil, errors.New("no title or description")
	}
	return e, nil
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > n {
		return string(rs[:n]) + "…"
	}
	return s
}

// schedulePreviews fetches previews in the background, then announces the updated message.
func (s *Server) schedulePreviews(msg *message) {
	if s.previews == nil {
		return
	}
	urls := extractURLs(msg.Content)
	if len(urls) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*previewTimeout)
		defer cancel()
		found := 0
		for _, u := range urls {
			e, err := s.previews.fetch(ctx, u)
			if err != nil {
				slog.Debug("link preview", "url", u, "err", err)
				continue
			}
			if _, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO link_previews (message_id, url, title, description, site_name, image_url) VALUES (?, ?, ?, ?, ?, ?)`,
				msg.ID, e.URL, e.Title, e.Description, e.SiteName, e.ImageURL); err != nil {
				return // message deleted meanwhile (foreign key) or database closed
			}
			found++
		}
		if found == 0 {
			return
		}
		if m, err := s.messageByID(ctx, "", msg.ChannelID, msg.ID); err == nil {
			s.broadcastChannel(ctx, "MESSAGE_UPDATE", msg.ChannelID, m)
		}
	}()
}
