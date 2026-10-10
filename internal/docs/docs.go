// Package docs is the user documentation: one set of Markdown files, read in
// the application's Help menu and published on the project site.
//
// # One source, two readers
//
// The guides are embedded in the binary rather than fetched from the site,
// so Help works offline and always describes the version that is running —
// the site describes the newest release, which after an update you have not
// installed is a different program. cmd/docsgen renders the same files, with
// the same renderer, into the site.
package docs

import (
	"bytes"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"

	inboxql "github.com/user/inboxql"
)

// Guide is one document.
type Guide struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Group   string `json:"group"`
	Summary string `json:"summary"`
	file    string
}

// Guides are in reading order. The order is the table of contents, so it is
// written down here rather than derived from file names.
var Guides = []Guide{
	{Slug: "getting-started", Group: "Start here", file: "docs/getting-started.md",
		Summary: "What InboxQL is, installing it, and your first hour with it."},
	{Slug: "installing", Group: "Start here", file: "docs/installing.md",
		Summary: "Where everything goes, the login service, updating, and removing it."},
	{Slug: "accounts-and-import", Group: "Start here", file: "docs/accounts-and-import.md",
		Summary: "Connecting a mail account, and bringing in mail from Apple Mail or .eml files."},
	{Slug: "desk", Group: "Using it", file: "docs/desk.md",
		Summary: "The Desk: the rail, the query bar, conversations, and reading mail."},
	{Slug: "query-language", Group: "Using it", file: "docs/query-language.md",
		Summary: "Every term, match mode and pipeline stage, with examples."},
	{Slug: "labels", Group: "Using it", file: "docs/labels.md",
		Summary: "Annotators: labels and extractors, the engines, and teaching them with rulings."},
	{Slug: "contacts", Group: "Using it", file: "docs/contacts.md",
		Summary: "People and systems, contact labels and notes, and open loops."},
	{Slug: "files", Group: "Using it", file: "docs/files.md",
		Summary: "Attachments as files: one document, every time it was sent."},
	{Slug: "tickets", Group: "Using it", file: "docs/tickets.md",
		Summary: "Work proposed from your mail, and the board you run it on."},
	{Slug: "settings", Group: "Running it", file: "docs/settings.md",
		Summary: "Every setting, and the System section: where your mail is and how the server runs."},
	{Slug: "privacy", Group: "Running it", file: "docs/privacy.md",
		Summary: "What stays on this machine, when a password is asked for, and backups."},
	{Slug: "troubleshooting", Group: "Running it", file: "docs/troubleshooting.md",
		Summary: "When the page will not load, the wrong mailbox opens, or something is slow."},
	{Slug: "command-line", Group: "Reference", file: "docs/command-line.md",
		Summary: "The iql command: global flags, which mailbox, exit codes, and every command."},
	// AGENTS.md is addressed to agents and keeps its own heading for them;
	// in a table of contents for people it is this.
	{Slug: "agents", Title: "For agents", Group: "Reference", file: "AGENTS.md",
		Summary: "The contract for driving InboxQL from an LLM agent."},
}

// Page is a rendered guide.
type Page struct {
	Guide
	HTML     string    `json:"html"`
	Headings []Heading `json:"headings"`
	Words    int       `json:"words"`
}

// Heading is one entry in a page's own table of contents.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	ID    string `json:"id"`
}

// Section is the unit of search: a heading and the text under it, so a
// result can link to the place that answers, not the top of a long page.
type Section struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Heading string `json:"heading"`
	ID      string `json:"id"`
	Text    string `json:"text"`
}

// LinkStyle says how a link to another guide is written: in the application
// it is a fragment the reader intercepts, on the site a file.
type LinkStyle func(slug, fragment string) string

// AppLinks are what the in-app reader expects.
func AppLinks(slug, fragment string) string {
	if fragment != "" {
		return "#doc/" + slug + "/" + fragment
	}
	return "#doc/" + slug
}

// SiteLinks are what the generated site serves.
func SiteLinks(slug, fragment string) string {
	if fragment != "" {
		return slug + ".html#" + fragment
	}
	return slug + ".html"
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Typographer),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	// The guides are this repository's own files, so raw HTML in them is
	// trusted; nothing a user or a message wrote is ever rendered here.
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// guideLink matches a relative link to another guide: "desk.md",
// "desk.md#rail", and AGENTS.md wherever it is linked from.
// mdLink finds inline links in Markdown source, for the link check.
var mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

var guideLink = regexp.MustCompile(`^(?:\.\./)?([A-Za-z0-9-]+)\.md(?:#(.*))?$`)

func source(g Guide) ([]byte, error) {
	return fs.ReadFile(inboxql.Docs, g.file)
}

// Find returns the guide with a slug.
func Find(slug string) (Guide, bool) {
	for _, g := range Guides {
		if g.Slug == slug {
			return g, true
		}
	}
	return Guide{}, false
}

func slugForFile(name string) string {
	for _, g := range Guides {
		base := g.file[strings.LastIndex(g.file, "/")+1:]
		if strings.EqualFold(strings.TrimSuffix(base, ".md"), name) {
			return g.Slug
		}
	}
	return ""
}

// Render renders one guide.
func Render(slug string, links LinkStyle) (*Page, error) {
	g, ok := Find(slug)
	if !ok {
		return nil, fmt.Errorf("no guide named %q", slug)
	}
	src, err := source(g)
	if err != nil {
		return nil, err
	}
	doc := md.Parser().Parse(text.NewReader(src))

	page := &Page{Guide: g, Words: len(strings.Fields(string(src)))}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			id, _ := n.AttributeString("id")
			idText, _ := id.([]byte)
			t := plain(n, src)
			if n.Level == 1 {
				if page.Title == "" {
					page.Title = t
				}
			} else if n.Level <= 3 {
				page.Headings = append(page.Headings, Heading{Level: n.Level, Text: t, ID: string(idText)})
			}
		case *ast.Link:
			if m := guideLink.FindStringSubmatch(string(n.Destination)); m != nil {
				if target := slugForFile(m[1]); target != "" {
					n.Destination = []byte(links(target, m[2]))
				}
			}
		}
		return ast.WalkContinue, nil
	})
	// The page shows its title in its own header, so the first heading is
	// not rendered twice.
	if first := doc.FirstChild(); first != nil {
		if h, ok := first.(*ast.Heading); ok && h.Level == 1 {
			doc.RemoveChild(doc, first)
		}
	}
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return nil, err
	}
	page.HTML = buf.String()
	if page.Title == "" {
		page.Title = g.Slug
	}
	return page, nil
}

// List is every guide, with the titles read from the files.
func List() []Guide {
	out := make([]Guide, 0, len(Guides))
	for _, g := range Guides {
		if p, err := Render(g.Slug, AppLinks); err == nil {
			g.Title = p.Title
		}
		out = append(out, g)
	}
	return out
}

var (
	indexOnce sync.Once
	index     []Section
)

// Index is every guide cut into sections, for search.
func Index() []Section {
	indexOnce.Do(func() { index = buildIndex() })
	return index
}

func buildIndex() []Section {
	var out []Section
	for _, g := range Guides {
		src, err := source(g)
		if err != nil {
			continue
		}
		doc := md.Parser().Parse(text.NewReader(src))
		title := g.Slug
		cur := Section{Slug: g.Slug}
		var body strings.Builder
		flush := func() {
			cur.Text = strings.Join(strings.Fields(body.String()), " ")
			if cur.Text != "" || cur.Heading != "" {
				cur.Title = title
				out = append(out, cur)
			}
			body.Reset()
		}
		for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
			if h, ok := n.(*ast.Heading); ok {
				if h.Level == 1 {
					title = plain(h, src)
					if g.Title != "" {
						title = g.Title
					}
					continue
				}
				if h.Level <= 3 {
					flush()
					id, _ := h.AttributeString("id")
					idText, _ := id.([]byte)
					cur = Section{Slug: g.Slug, Heading: plain(h, src), ID: string(idText)}
					continue
				}
			}
			body.WriteString(plain(n, src))
			body.WriteString(" ")
		}
		flush()
		// Sections made before the title was read carry the slug.
		for i := range out {
			if out[i].Slug == g.Slug {
				out[i].Title = title
			}
		}
	}
	return out
}

// plain is the text of a node, without markup.
func plain(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			// Blocks end in a space, so a paragraph's last word does not
			// run into the next one's first.
			switch c.Kind() {
			case ast.KindParagraph, ast.KindListItem, ast.KindHeading, east.KindTableCell:
				b.WriteByte(' ')
			}
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(src))
			if c.SoftLineBreak() || c.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(c.Value)
		case *ast.CodeBlock, *ast.FencedCodeBlock:
			lines := c.Lines()
			for i := 0; i < lines.Len(); i++ {
				seg := lines.At(i)
				b.Write(seg.Value(src))
			}
			b.WriteByte(' ')
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}
