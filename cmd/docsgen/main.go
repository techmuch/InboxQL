// Command docsgen renders the user guides into the project site.
//
//	go run ./cmd/docsgen -out site/docs
//
// The same Markdown, through the same renderer, as Help → Documentation in the
// application — so the two cannot drift. What differs is only the frame: the
// site has its own page around each guide, links between guides point at
// .html files, and search runs over a JSON copy of the same index.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"github.com/user/inboxql/internal/docs"
)

func main() {
	out := flag.String("out", "site/docs", "where to write the site's guides")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "docsgen:", err)
		os.Exit(1)
	}
}

type group struct {
	Name   string
	Guides []docs.Guide
}

type view struct {
	Page   *docs.Page
	Groups []group
	Prev   *docs.Guide
	Next   *docs.Guide
	Index  bool
}

func run(out string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	guides := docs.List()
	var groups []group
	for _, g := range guides {
		if n := len(groups); n > 0 && groups[n-1].Name == g.Group {
			groups[n-1].Guides = append(groups[n-1].Guides, g)
		} else {
			groups = append(groups, group{Name: g.Group, Guides: []docs.Guide{g}})
		}
	}

	tmpl, err := template.New("page").Funcs(template.FuncMap{
		"html":    func(s string) template.HTML { return template.HTML(s) },
		"minutes": func(words int) int { return max(1, (words+110)/220) },
	}).Parse(pageTemplate)
	if err != nil {
		return err
	}

	for i, g := range guides {
		page, err := docs.Render(g.Slug, docs.SiteLinks)
		if err != nil {
			return err
		}
		v := view{Page: page, Groups: groups}
		if i > 0 {
			v.Prev = &guides[i-1]
		}
		if i < len(guides)-1 {
			v.Next = &guides[i+1]
		}
		if err := write(tmpl, filepath.Join(out, g.Slug+".html"), v); err != nil {
			return err
		}
	}

	// The landing page is the table of contents itself.
	if err := write(tmpl, filepath.Join(out, "index.html"), view{Groups: groups, Index: true,
		Page: &docs.Page{Guide: docs.Guide{Title: "Documentation",
			Summary: "Everything in Help → Documentation, for the newest release."}}}); err != nil {
		return err
	}

	b, err := json.Marshal(map[string]any{"sections": docs.Index()})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "search.json"), b, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %d guides and a search index of %d sections to %s\n", len(guides), len(docs.Index()), out)
	return nil
}

func write(t *template.Template, path string, v view) error {
	var buf bytes.Buffer
	if err := t.Execute(&buf, v); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Page.Title}} — InboxQL</title>
<meta name="description" content="{{.Page.Summary}}">
<style>
  :root {
    --bg: #fafaf9; --fg: #1c1917; --muted: #57534e; --faint: #e7e5e4;
    --card: #ffffff; --code: #f5f5f4; --accent: #2563eb; --mark: #dbeafe;
    --mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  }
  @media (prefers-color-scheme: dark) {
    :root { --bg: #0c0a09; --fg: #f5f5f4; --muted: #a8a29e; --faint: #292524;
            --card: #1c1917; --code: #171412; --accent: #60a5fa; --mark: #1e3a5f; }
  }
  * { box-sizing: border-box; }
  body { margin: 0; background: var(--bg); color: var(--fg);
         font: 16px/1.7 -apple-system, BlinkMacSystemFont, "Segoe UI", Inter, system-ui, sans-serif; }
  a { color: var(--accent); }
  .top { display: flex; align-items: center; gap: 16px; padding: 12px 16px; border-bottom: 1px solid var(--faint); }
  .top a.home { color: var(--fg); text-decoration: none; font-weight: 700; }
  .top .crumb { color: var(--muted); }
  .search { position: relative; margin-left: auto; width: min(360px, 55vw); }
  .search input { width: 100%; font: inherit; font-size: 14px; padding: 6px 10px; border: 1px solid var(--faint);
                  background: var(--card); color: var(--fg); }
  .results { position: absolute; right: 0; top: 100%; width: min(480px, 92vw); max-height: 70vh; overflow: auto;
             background: var(--card); border: 1px solid var(--faint); box-shadow: 0 10px 30px rgba(0,0,0,.15);
             z-index: 10; display: none; }
  .results.open { display: block; }
  .results a { display: block; padding: 10px 14px; border-bottom: 1px solid var(--faint); text-decoration: none; color: var(--fg); }
  .results a.active, .results a:hover { background: var(--code); }
  .results small { display: block; color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: .05em; }
  .results b { display: block; font-size: 14px; }
  .results span { display: block; font-size: 13px; color: var(--muted); line-height: 1.45; }
  .results mark { background: var(--mark); color: inherit; }
  .results p { margin: 0; padding: 12px 14px; color: var(--muted); font-size: 13px; }
  .layout { display: grid; grid-template-columns: 240px minmax(0, 1fr); max-width: 1180px; margin: 0 auto; }
  nav.toc { padding: 28px 16px; border-right: 1px solid var(--faint); font-size: 14px; }
  nav.toc h4 { margin: 18px 0 6px; font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); }
  nav.toc h4:first-child { margin-top: 0; }
  nav.toc a { display: block; padding: 3px 0; color: var(--fg); text-decoration: none; }
  nav.toc a[aria-current] { color: var(--accent); font-weight: 600; }
  main { padding: 40px 16px 96px 48px; max-width: 800px; }
  .group { color: var(--muted); font-size: 12px; text-transform: uppercase; letter-spacing: .06em; margin: 0; }
  h1 { font-size: 2.1rem; margin: 4px 0 6px; letter-spacing: -0.02em; line-height: 1.2; }
  .lede { color: var(--muted); margin: 0 0 32px; }
  article h2 { font-size: 1.4rem; margin: 2.2em 0 .5em; letter-spacing: -0.01em; }
  article h3 { font-size: 1.08rem; margin: 1.7em 0 .4em; }
  article code { font-family: var(--mono); font-size: .88em; background: var(--code); padding: 1px 5px; }
  article pre { background: var(--card); border: 1px solid var(--faint); padding: 14px 16px; overflow-x: auto;
                font-size: 13.5px; line-height: 1.55; }
  article pre code { background: none; padding: 0; }
  article table { width: 100%; border-collapse: collapse; margin: 0 0 1.4em; font-size: 14.5px; display: block; overflow-x: auto; }
  article th, article td { text-align: left; vertical-align: top; padding: 7px 14px 7px 0; border-bottom: 1px solid var(--faint); }
  article th { color: var(--muted); font-weight: 500; }
  :target { scroll-margin-top: 16px; background: var(--mark); }
  .cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 12px; margin: 0 0 28px; }
  .cards a { display: block; padding: 14px 16px; border: 1px solid var(--faint); background: var(--card); color: var(--fg); text-decoration: none; }
  .cards a:hover { border-color: var(--accent); }
  .cards b { display: block; margin-bottom: 2px; }
  .cards span { color: var(--muted); font-size: 14px; line-height: 1.5; }
  .pager { display: flex; justify-content: space-between; gap: 16px; margin-top: 56px; padding-top: 20px; border-top: 1px solid var(--faint); }
  .pager a { text-decoration: none; }
  .pager small { display: block; color: var(--muted); font-size: 12px; }
  footer { color: var(--muted); font-size: 14px; margin-top: 40px; }
  @media (max-width: 760px) {
    .layout { display: block; }
    nav.toc { border-right: 0; border-bottom: 1px solid var(--faint); padding: 16px; columns: 2; }
    nav.toc h4 { break-after: avoid; }
    main { padding: 28px 16px 72px; }
    .top .crumb { display: none; }
  }
</style>
</head>
<body>
<div class="top">
  <a class="home" href="../">InboxQL</a>
  <a class="crumb" href="index.html">Documentation</a>
  <div class="search">
    <input id="q" type="search" placeholder="Search the guides  /" aria-label="Search the documentation" autocomplete="off">
    <div id="results" class="results" role="listbox"></div>
  </div>
</div>
<div class="layout">
  <nav class="toc" aria-label="Guides">
    {{- range .Groups}}
    <h4>{{.Name}}</h4>
    {{- range .Guides}}
    <a href="{{.Slug}}.html"{{if and (not $.Index) (eq .Slug $.Page.Slug)}} aria-current="page"{{end}}>{{.Title}}</a>
    {{- end}}
    {{- end}}
  </nav>
  <main>
  {{- if .Index}}
    <h1>Documentation</h1>
    <p class="lede">The same guides as Help → Documentation inside InboxQL. These describe the newest release;
      the copy in the application describes the version you are running.</p>
    {{- range .Groups}}
    <h2 class="group" style="margin:28px 0 10px">{{.Name}}</h2>
    <div class="cards">
      {{- range .Guides}}
      <a href="{{.Slug}}.html"><b>{{.Title}}</b><span>{{.Summary}}</span></a>
      {{- end}}
    </div>
    {{- end}}
  {{- else}}
    <p class="group">{{.Page.Group}}</p>
    <h1>{{.Page.Title}}</h1>
    <p class="lede">{{.Page.Summary}} · {{minutes .Page.Words}} min read</p>
    <article>{{html .Page.HTML}}</article>
    <div class="pager">
      {{if .Prev}}<a href="{{.Prev.Slug}}.html"><small>← Previous</small>{{.Prev.Title}}</a>{{else}}<span></span>{{end}}
      {{if .Next}}<a href="{{.Next.Slug}}.html" style="text-align:right"><small>Next →</small>{{.Next.Title}}</a>{{end}}
    </div>
  {{- end}}
    <footer><a href="../">Install</a> · <a href="https://github.com/techmuch/InboxQL">Source</a> ·
      <a href="https://github.com/techmuch/InboxQL/releases">Releases</a></footer>
  </main>
</div>
<script>
// The same search as the application's: every word must start a word in the
// section; title beats heading beats text; a phrase beats scattered words.
(function () {
  var input = document.getElementById('q'), box = document.getElementById('results');
  var index = null, hits = [], active = 0;
  function esc(s) { return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); }
  function html(s) { return s.replace(/[&<>"]/g, function (c) { return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]; }); }
  function words(q) { return q.toLowerCase().split(/[^\p{L}\p{N}_.:-]+/u).map(function (t) { return t.replace(/^[.:-]+|[.:-]+$/g, ''); }).filter(Boolean); }
  function starts(w) { return new RegExp('(^|[^\\p{L}\\p{N}])' + esc(w), 'iu'); }
  function load() {
    if (index) return Promise.resolve(index);
    return fetch('search.json').then(function (r) { return r.json(); }).then(function (d) { index = d.sections || []; return index; });
  }
  function search(q) {
    var ws = words(q); if (!ws.length) return [];
    var tests = ws.map(starts), phrase = q.trim().toLowerCase(), out = [];
    index.forEach(function (s) {
      var all = s.title + ' ' + s.heading + ' ' + s.text;
      if (!tests.every(function (t) { return t.test(all); })) return;
      var score = 0;
      tests.forEach(function (t) {
        if (t.test(s.title)) score += 8;
        if (t.test(s.heading)) score += 5;
        score += Math.min((s.text.match(new RegExp(t.source, 'giu')) || []).length, 5);
      });
      if (ws.length > 1 && all.toLowerCase().indexOf(phrase) >= 0) score += 10;
      if (s.heading.toLowerCase().indexOf(phrase) >= 0) score += 6;
      out.push({ s: s, score: score, ws: ws });
    });
    out.sort(function (a, b) { return b.score - a.score; });
    return out.slice(0, 20);
  }
  function snippet(text, ws) {
    var lower = text.toLowerCase(), first = -1;
    ws.forEach(function (w) { var i = lower.search(starts(w)); if (i >= 0 && (first < 0 || i < first)) first = i; });
    var start = Math.max(0, first < 0 ? 0 : first - 50), piece = text.slice(start, start + 180);
    if (start > 0) piece = '… ' + piece;
    if (start + 180 < text.length) piece += ' …';
    return html(piece).replace(new RegExp('(' + ws.map(function (w) { return esc(html(w)); }).join('|') + ')', 'giu'), '<mark>$1</mark>');
  }
  function render() {
    if (!input.value.trim()) { box.classList.remove('open'); return; }
    box.innerHTML = hits.length ? hits.map(function (h, i) {
      var href = h.s.slug + '.html' + (h.s.id ? '#' + h.s.id : '');
      return '<a role="option" href="' + href + '"' + (i === active ? ' class="active"' : '') + '><small>' + html(h.s.title) +
        '</small><b>' + html(h.s.heading || h.s.title) + '</b><span>' + snippet(h.s.text, h.ws) + '</span></a>';
    }).join('') : '<p>Nothing matches every word. Search is by words, not meaning — try another way of saying it.</p>';
    box.classList.add('open');
  }
  input.addEventListener('input', function () { load().then(function () { hits = search(input.value); active = 0; render(); }); });
  input.addEventListener('focus', function () { load(); if (input.value.trim()) render(); });
  input.addEventListener('keydown', function (e) {
    if (e.key === 'ArrowDown') { e.preventDefault(); active = Math.min(active + 1, hits.length - 1); render(); }
    if (e.key === 'ArrowUp') { e.preventDefault(); active = Math.max(active - 1, 0); render(); }
    if (e.key === 'Enter' && hits[active]) { location.href = box.querySelectorAll('a')[active].getAttribute('href'); }
    if (e.key === 'Escape') { input.value = ''; render(); input.blur(); }
  });
  document.addEventListener('click', function (e) { if (!e.target.closest('.search')) box.classList.remove('open'); });
  document.addEventListener('keydown', function (e) {
    if (e.key === '/' && document.activeElement !== input && !e.metaKey && !e.ctrlKey) { e.preventDefault(); input.focus(); }
  });
})();
</script>
</body>
</html>
`
