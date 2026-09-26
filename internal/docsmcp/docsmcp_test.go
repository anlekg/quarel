package docsmcp

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The real wiki of the site.
func loadWiki(t *testing.T) *Docs {
	t.Helper()
	d, err := Load(os.DirFS("../../site/src/content/docs"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLoadAndFind(t *testing.T) {
	d := loadWiki(t)
	p := d.Page("https://quarel.app/wiki/heberger/reseau/#https")
	if p == nil || p.Title != "Réseau, ports et HTTPS" || p.Description == "" || strings.HasPrefix(p.Body, "---") {
		t.Fatalf("page: %+v", p)
	}
	if d.Page("heberger/reseau") != p || d.Page("/wiki/heberger/reseau/") != p || d.Page("nope") != nil {
		t.Fatal("lookup by path")
	}
	for _, s := range p.Sections {
		if s.Heading == "Ports d’un serveur communautaire" && s.Anchor != "ports-dun-serveur-communautaire" {
			t.Fatalf("anchor %q", s.Anchor)
		}
	}
}

func TestSearch(t *testing.T) {
	d := loadWiki(t)
	cases := map[string]string{
		"sauvegarder serveur communautaire": "wiki/heberger/serveur-communautaire",
		"PHRASE de recuperation":            "wiki/utiliser/messages-prives", // accents and case ignored
		"ports UPnP box":                    "wiki/heberger/reseau",
		"jeton bot":                         "wiki/developper/",
	}
	for q, want := range cases {
		hits := d.Search(q, 3)
		if len(hits) == 0 || !strings.HasPrefix(hits[0].Page.Slug, want) {
			t.Errorf("%q: first hit %v, want %s", q, hits, want)
		}
	}
	if len(d.Search("le la les", 5)) != 0 {
		t.Error("stop words alone should find nothing")
	}
}

func TestMCP(t *testing.T) {
	srv := httptest.NewServer(Handler(NewServer(loadWiki(t), "test")))
	defer srv.Close()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	sess, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	tools, err := sess.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 3 {
		t.Fatalf("tools: %v %v", tools, err)
	}
	call := func(name string, args map[string]any) (string, bool) {
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(name, err)
		}
		return res.Content[0].(*mcp.TextContent).Text, res.IsError
	}
	if out, _ := call("list_pages", map[string]any{}); !strings.Contains(out, "https://quarel.app/wiki/developper/api/") {
		t.Fatalf("list_pages: %s", out)
	}
	if out, _ := call("search_docs", map[string]any{"query": "restaurer une sauvegarde", "limit": 2}); !strings.Contains(out, "URL : https://quarel.app/wiki/heberger/") {
		t.Fatalf("search_docs: %s", out)
	}
	if out, isErr := call("read_page", map[string]any{"page": "wiki/decouvrir/securite"}); isErr || !strings.Contains(out, "argon2id") {
		t.Fatalf("read_page: %s", out)
	}
	if _, isErr := call("read_page", map[string]any{"page": "../../etc/passwd"}); !isErr {
		t.Fatal("unknown page should be an error")
	}
	res, err := sess.ReadResource(ctx, &mcp.ReadResourceParams{URI: "https://quarel.app/wiki/heberger/reseau/"})
	if err != nil || !strings.Contains(res.Contents[0].Text, "7882/udp") {
		t.Fatalf("resource: %v", err)
	}
}
