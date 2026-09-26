package docsmcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const instructions = `Documentation of Quarel (https://quarel.app), a self-hostable, open-source Discord alternative: community servers hosted by their owners, identity services for accounts, end-to-end encrypted private messages and calls.
The documentation is in French. Use search_docs to find the relevant sections (French keywords work best, e.g. "sauvegarde", "ports", "bot", "phrase de récupération"), then read_page for a whole page. Always cite the page URLs you used.`

// NewServer builds the MCP server over the loaded docs.
func NewServer(d *Docs, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "quarel-docs", Title: "Documentation de Quarel", Version: version}, &mcp.ServerOptions{Instructions: instructions})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_pages",
		Title:       "Lister les pages",
		Description: "Lists every page of the Quarel documentation with its path, title and summary.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(bool)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		var b strings.Builder
		for _, p := range d.Pages {
			fmt.Fprintf(&b, "- %s — %s\n  %s\n  %s\n", p.Slug, p.Title, p.URL(), p.Description)
		}
		return text(b.String()), nil, nil
	})

	type searchArgs struct {
		Query string `json:"query" jsonschema:"words to look for, preferably in French"`
		Limit int    `json:"limit,omitempty" jsonschema:"maximum number of sections returned (1-10, default 5)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_docs",
		Title:       "Chercher dans la documentation",
		Description: "Full-text search in the Quarel documentation. Returns the best matching sections with their page, heading, URL and text.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(bool)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, a searchArgs) (*mcp.CallToolResult, any, error) {
		limit := a.Limit
		if limit <= 0 {
			limit = 5
		}
		limit = min(limit, 10)
		if len(a.Query) > 200 {
			a.Query = strings.ToValidUTF8(a.Query[:200], "")
		}
		hits := d.Search(a.Query, limit)
		if len(hits) == 0 {
			return text("Aucun résultat pour « " + a.Query + " ». Essayez d'autres mots (en français), ou list_pages."), nil, nil
		}
		var b strings.Builder
		for i, h := range hits {
			heading := h.Section.Heading
			if heading == "" {
				heading = "(introduction)"
			}
			body := h.Section.Text
			if len(body) > 2000 {
				cut := 2000
				for cut > 0 && !utf8.RuneStart(body[cut]) { // never split a character
					cut--
				}
				body = body[:cut] + "…"
			}
			fmt.Fprintf(&b, "## %d. %s › %s\nPage : %s\nURL : %s\n\n%s\n\n", i+1, h.Page.Title, heading, h.Page.Slug, h.URL(), body)
		}
		return text(b.String()), nil, nil
	})

	type readArgs struct {
		Page string `json:"page" jsonschema:"page path from list_pages or search_docs (e.g. wiki/heberger/reseau), or its URL"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_page",
		Title:       "Lire une page",
		Description: "Returns a whole page of the Quarel documentation in Markdown.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(bool)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, a readArgs) (*mcp.CallToolResult, any, error) {
		p := d.Page(a.Page)
		if p == nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Page inconnue : " + a.Page + ". Utilisez list_pages."}}}, nil, nil
		}
		return text("# " + p.Title + "\n\nURL : " + p.URL() + "\n\n" + p.Body), nil, nil
	})

	// Each page is also a resource, for clients that browse resources.
	for _, p := range d.Pages {
		s.AddResource(&mcp.Resource{URI: p.URL(), Name: p.Slug, Title: p.Title, Description: p.Description, MIMEType: "text/markdown"},
			func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: p.URL(), MIMEType: "text/markdown", Text: "# " + p.Title + "\n\n" + p.Body}}}, nil
			})
	}
	return s
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// Handler serves the MCP "streamable HTTP" transport, stateless (no session
// kept on the server). The content is public and read-only: DNS rebinding
// protection (meant for local servers with private data) is off so that it
// works behind a reverse proxy on the same machine.
func Handler(s *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		JSONResponse:               true,
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        64 << 10,
	})
}
