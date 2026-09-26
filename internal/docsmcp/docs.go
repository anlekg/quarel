// Package docsmcp serves the quarel.app wiki (Markdown pages of site/) to AI
// assistants over the Model Context Protocol: list, search and read pages.
// Read-only and public: the same content as the web site.
package docsmcp

import (
	"bufio"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// SiteURL is where the wiki pages are published.
const SiteURL = "https://quarel.app/"

// Page is one wiki page.
type Page struct {
	Slug        string // "wiki/heberger/reseau"
	Title       string
	Description string
	Body        string // Markdown without the front matter
	Sections    []Section
}

// URL of the page on the site.
func (p *Page) URL() string { return SiteURL + p.Slug + "/" }

// Section is the text under one heading (the introduction has no heading).
type Section struct {
	Heading string
	Anchor  string
	Text    string
}

// Docs is the loaded wiki.
type Docs struct {
	Pages  []*Page
	bySlug map[string]*Page
}

// Load reads every .md/.mdx page under root (site/src/content/docs).
func Load(fsys fs.FS) (*Docs, error) {
	d := &Docs{bySlug: map[string]*Page{}}
	err := fs.WalkDir(fsys, ".", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		ext := path.Ext(p)
		if ext != ".md" && ext != ".mdx" {
			return nil
		}
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		page := parse(strings.TrimSuffix(p, ext), string(raw))
		d.Pages = append(d.Pages, page)
		d.bySlug[page.Slug] = page
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(d.Pages) == 0 {
		return nil, fmt.Errorf("docsmcp: no page found")
	}
	sort.Slice(d.Pages, func(i, j int) bool { return d.Pages[i].Slug < d.Pages[j].Slug })
	return d, nil
}

func parse(file, raw string) *Page {
	slug := strings.ToLower(strings.TrimSuffix(file, "/index"))
	p := &Page{Slug: slug, Title: path.Base(slug)}
	body := raw
	if strings.HasPrefix(raw, "---\n") {
		if end := strings.Index(raw[4:], "\n---\n"); end >= 0 {
			for _, line := range strings.Split(raw[4:4+end], "\n") {
				k, v, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				v = strings.Trim(strings.TrimSpace(v), `"'`)
				switch strings.TrimSpace(k) {
				case "title":
					p.Title = v
				case "description":
					p.Description = v
				}
			}
			body = raw[4+end+5:]
		}
	}
	p.Body = strings.TrimSpace(body)
	p.Sections = sections(p.Body)
	return p
}

// sections splits on ##/### headings (outside code blocks).
func sections(body string) []Section {
	var out []Section
	cur := Section{}
	var b strings.Builder
	inCode := false
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
		}
		if !inCode && (strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ")) {
			cur.Text = strings.TrimSpace(b.String())
			if cur.Text != "" || cur.Heading != "" {
				out = append(out, cur)
			}
			h := strings.TrimSpace(strings.TrimLeft(line, "#"))
			cur = Section{Heading: h, Anchor: anchor(h)}
			b.Reset()
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	cur.Text = strings.TrimSpace(b.String())
	if cur.Text != "" || cur.Heading != "" {
		out = append(out, cur)
	}
	return out
}

// anchor mimics the heading ids of the site (github-slugger): lower case,
// punctuation removed, spaces to dashes, accents kept.
func anchor(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Page finds a page by slug, site path or full URL ("heberger/reseau",
// "/wiki/heberger/reseau/", "https://quarel.app/wiki/heberger/reseau/").
func (d *Docs) Page(ref string) *Page {
	s := strings.TrimPrefix(strings.TrimSpace(ref), SiteURL)
	s, _, _ = strings.Cut(s, "#")
	s = strings.ToLower(strings.Trim(s, "/"))
	if p := d.bySlug[s]; p != nil {
		return p
	}
	return d.bySlug["wiki/"+s]
}

// Hit is one search result.
type Hit struct {
	Page    *Page
	Section Section
	Score   float64
}

// URL of the hit, with the section anchor.
func (h Hit) URL() string {
	if h.Section.Anchor == "" {
		return h.Page.URL()
	}
	return h.Page.URL() + "#" + h.Section.Anchor
}

// Search ranks sections by how many query words they contain (prefix match,
// accents and case ignored); words found in titles and headings count more.
func (d *Docs) Search(query string, limit int) []Hit {
	terms := words(query)
	if len(terms) == 0 {
		return nil
	}
	var hits []Hit
	for _, p := range d.Pages {
		title := words(p.Title + " " + p.Description)
		for _, s := range p.Sections {
			head := words(s.Heading)
			text := words(s.Text)
			score, matched := 0.0, 0
			for _, t := range terms {
				n := count(text, t)
				h := count(head, t) + count(title, t)
				if n+h > 0 {
					matched++
				}
				score += float64(min(n, 5)) + 3*float64(min(h, 2))
			}
			if matched == 0 {
				continue
			}
			score *= float64(matched) / float64(len(terms)) // all words beat some words
			hits = append(hits, Hit{Page: p, Section: s, Score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func count(ws []string, t string) int {
	n := 0
	for _, w := range ws {
		if strings.HasPrefix(w, t) {
			n++
		}
	}
	return n
}

var stop = map[string]bool{"le": true, "la": true, "les": true, "de": true, "des": true, "du": true, "un": true, "une": true, "et": true,
	"en": true, "a": true, "au": true, "aux": true, "pour": true, "par": true, "sur": true, "est": true, "the": true, "to": true, "of": true, "how": true, "comment": true}

// words lower-cases, removes accents, splits on anything but letters and
// digits, and drops very common words.
func words(s string) []string {
	s, _, _ = transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), strings.ToLower(s)) // a transformer is not safe for concurrent use
	var out []string
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) > 1 && !stop[w] {
			out = append(out, w)
		}
	}
	return out
}
