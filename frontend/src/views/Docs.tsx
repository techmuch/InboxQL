import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { BookOpen, ChevronLeft, ChevronRight, ExternalLink, Loader2, Search, X } from 'lucide-react';
import { searchDocs, type DocHit, type DocSection } from '../lib/docsSearch';
import { DOC_EVENT, takeDocRequest, type DocRequest } from '../lib/docsNav';

/**
 * Help → Documentation: the guides, searchable, in a tab.
 *
 * # Why they are embedded rather than loaded from the site
 *
 * The site describes the newest release. After an update you have not
 * installed, that is a different program, and help that describes buttons you
 * do not have is worse than none. So the guides are compiled into the binary
 * and this reads them from the server that is running — offline too — and
 * links to the site for the newest.
 *
 * # Why the links are fragments
 *
 * A guide links to another as `#doc/slug/heading`. The reader intercepts those
 * and moves itself, so following a link never navigates the window away from
 * the application.
 */

interface Guide {
  slug: string;
  title: string;
  group: string;
  summary: string;
}

interface Page extends Guide {
  html: string;
  headings: { level: number; text: string; id: string }[];
  words: number;
}

export const SITE_DOCS = 'https://techmuch.github.io/InboxQL/docs/';

export const Docs = () => {
  const [guides, setGuides] = useState<Guide[]>([]);
  const [version, setVersion] = useState('');
  const [index, setIndex] = useState<DocSection[] | null>(null);
  const [current, setCurrent] = useState<DocRequest>(() => takeDocRequest() ?? { slug: 'getting-started' });
  const [page, setPage] = useState<Page | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fetch('/api/docs').then(r => r.json()).then(d => { setGuides(d.guides ?? []); setVersion(d.version ?? ''); })
      .catch(() => setError('could not load the guides'));
    fetch('/api/docs/index').then(r => r.json()).then(d => setIndex(d.sections ?? [])).catch(() => setIndex([]));
  }, []);

  // Another part of the application asking for a guide — Settings links to
  // the one that explains it — while this tab is already open.
  useEffect(() => {
    const go = (e: Event) => {
      const req = (e as CustomEvent<DocRequest>).detail;
      if (req) { takeDocRequest(); setCurrent(req); setQuery(''); }
    };
    window.addEventListener(DOC_EVENT, go);
    return () => window.removeEventListener(DOC_EVENT, go);
  }, []);

  useEffect(() => {
    let cancelled = false;
    setError(null);
    fetch(`/api/docs/${encodeURIComponent(current.slug)}`)
      .then(r => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((p: Page) => { if (!cancelled) setPage(p); })
      .catch(() => { if (!cancelled) setError(`there is no guide called “${current.slug}”`); });
    return () => { cancelled = true; };
  }, [current.slug]);

  // Scroll to the heading asked for once the page it is on has rendered, or
  // to the top for a new page.
  useEffect(() => {
    if (!page || page.slug !== current.slug) return;
    const body = bodyRef.current;
    if (!body) return;
    if (current.anchor) {
      const el = body.querySelector(`[id="${CSS.escape(current.anchor)}"]`);
      if (el) {
        el.scrollIntoView({ block: 'start' });
        el.classList.add('iql-doc-target');
        setTimeout(() => el.classList.remove('iql-doc-target'), 1600);
        return;
      }
    }
    body.scrollTop = 0;
  }, [page, current]);

  const open = useCallback((slug: string, anchor?: string) => {
    setCurrent({ slug, anchor, at: Date.now() });
  }, []);

  // Links inside a guide: other guides move the reader; the web opens in a
  // new tab; everything else is left alone.
  const onBodyClick = (e: React.MouseEvent) => {
    const a = (e.target as HTMLElement).closest('a');
    if (!a) return;
    const href = a.getAttribute('href') ?? '';
    const m = href.match(/^#doc\/([^/]+)(?:\/(.+))?$/);
    if (m) {
      e.preventDefault();
      open(m[1], m[2] ? decodeURIComponent(m[2]) : undefined);
    } else if (href.startsWith('#')) {
      e.preventDefault();
      open(current.slug, href.slice(1));
    } else if (/^https?:/.test(href)) {
      a.setAttribute('target', '_blank');
      a.setAttribute('rel', 'noreferrer');
    }
  };

  const hits: DocHit[] = useMemo(() => (index ? searchDocs(index, query) : []), [index, query]);
  useEffect(() => setActive(0), [query]);

  // "/" to search, as on most documentation sites, unless typing elsewhere.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '/' || e.metaKey || e.ctrlKey) return;
      const t = e.target as HTMLElement;
      if (t.closest('input, textarea, [contenteditable="true"]')) return;
      if (!bodyRef.current?.isConnected) return;
      e.preventDefault();
      searchRef.current?.focus();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const choose = (h: DocHit) => {
    open(h.section.slug, h.section.id || undefined);
    setQuery('');
  };

  const groups = useMemo(() => {
    const out: { group: string; guides: Guide[] }[] = [];
    for (const g of guides) {
      const last = out[out.length - 1];
      if (last && last.group === g.group) last.guides.push(g);
      else out.push({ group: g.group, guides: [g] });
    }
    return out;
  }, [guides]);

  const position = guides.findIndex(g => g.slug === current.slug);
  const prev = position > 0 ? guides[position - 1] : null;
  const next = position >= 0 && position < guides.length - 1 ? guides[position + 1] : null;

  return (
    <div className="flex h-full bg-background text-foreground overflow-hidden">
      <aside className="w-64 shrink-0 border-r border-border bg-card/30 flex flex-col">
        <div className="p-3 border-b border-border">
          <div className="flex items-center bg-background border border-border focus-within:ring-1 focus-within:ring-primary">
            <Search className="ml-2.5 h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <input
              ref={searchRef}
              type="search"
              aria-label="Search the documentation"
              placeholder="Search the guides  /"
              className="w-full bg-transparent px-2 py-1.5 text-sm focus:outline-none"
              value={query}
              onChange={e => setQuery(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'ArrowDown') { e.preventDefault(); setActive(a => Math.min(a + 1, hits.length - 1)); }
                if (e.key === 'ArrowUp') { e.preventDefault(); setActive(a => Math.max(a - 1, 0)); }
                if (e.key === 'Enter' && hits[active]) choose(hits[active]);
                if (e.key === 'Escape') setQuery('');
              }}
            />
            {query && (
              <button aria-label="Clear search" className="px-2 text-muted-foreground hover:text-foreground" onClick={() => setQuery('')}>
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        </div>

        <nav className="flex-1 overflow-auto py-2 text-sm" aria-label="Guides">
          {query.trim() ? (
            <SearchResults hits={hits} active={active} loading={index === null} onChoose={choose} />
          ) : (
            groups.map(g => (
              <div key={g.group} className="mb-3">
                <p className="px-4 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{g.group}</p>
                {g.guides.map(guide => (
                  <button
                    key={guide.slug}
                    onClick={() => open(guide.slug)}
                    aria-current={guide.slug === current.slug ? 'page' : undefined}
                    className={`w-full text-left px-4 py-1.5 transition-colors ${guide.slug === current.slug ? 'bg-primary/10 text-primary font-semibold' : 'hover:bg-accent text-foreground/80'}`}
                  >
                    {guide.title}
                  </button>
                ))}
              </div>
            ))
          )}
        </nav>

        <div className="p-3 border-t border-border text-[11px] text-muted-foreground leading-snug">
          These guides describe <span className="font-mono">v{version}</span>, the version running.{' '}
          <a className="text-primary hover:underline inline-flex items-center gap-0.5" href={SITE_DOCS} target="_blank" rel="noreferrer">
            Newest on the web <ExternalLink className="h-2.5 w-2.5" />
          </a>
        </div>
      </aside>

      <div ref={bodyRef} className="flex-1 overflow-auto" onClick={onBodyClick}>
        {error && <p className="p-8 text-sm text-destructive">{error}</p>}
        {!page && !error && <p className="p-8"><Loader2 className="h-4 w-4 animate-spin text-muted-foreground" /></p>}
        {page && (
          <div className="mx-auto flex max-w-5xl gap-10 px-8 py-10">
            <article className="min-w-0 flex-1 max-w-[46rem]">
              <p className="mb-1 text-[11px] uppercase tracking-wider text-muted-foreground">{page.group}</p>
              <h1 className="mb-2 text-3xl font-bold tracking-tight">{page.title}</h1>
              <p className="mb-8 text-sm text-muted-foreground">
                {page.summary} <span className="whitespace-nowrap">· {Math.max(1, Math.round(page.words / 220))} min read</span>
              </p>
              <div className="iql-doc" dangerouslySetInnerHTML={{ __html: page.html }} />
              <div className="mt-12 flex justify-between gap-4 border-t border-border pt-6 text-sm">
                {prev ? (
                  <button onClick={() => open(prev.slug)} className="text-left hover:text-primary">
                    <span className="flex items-center gap-1 text-[11px] text-muted-foreground"><ChevronLeft className="h-3 w-3" /> Previous</span>
                    {prev.title}
                  </button>
                ) : <span />}
                {next && (
                  <button onClick={() => open(next.slug)} className="text-right hover:text-primary">
                    <span className="flex items-center justify-end gap-1 text-[11px] text-muted-foreground">Next <ChevronRight className="h-3 w-3" /></span>
                    {next.title}
                  </button>
                )}
              </div>
            </article>
            {page.headings.length > 2 && (
              <nav className="hidden xl:block w-52 shrink-0" aria-label="On this page">
                <div className="sticky top-0">
                  <p className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">On this page</p>
                  <ul className="space-y-1 text-xs">
                    {page.headings.map(h => (
                      <li key={h.id} className={h.level === 3 ? 'pl-3' : ''}>
                        <button onClick={() => open(page.slug, h.id)} className="text-left text-muted-foreground hover:text-foreground">
                          {h.text}
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              </nav>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

const SearchResults = ({ hits, active, loading, onChoose }: {
  hits: DocHit[]; active: number; loading: boolean; onChoose: (h: DocHit) => void;
}) => {
  if (loading) return <p className="px-4 py-2 text-xs text-muted-foreground"><Loader2 className="inline h-3 w-3 animate-spin" /></p>;
  if (hits.length === 0) {
    return (
      <p className="px-4 py-2 text-xs text-muted-foreground">
        Nothing matches every word. Search is by words, not meaning — try another way of saying it.
      </p>
    );
  }
  return (
    <ul role="listbox" aria-label="Search results">
      {hits.map((h, i) => (
        <li key={`${h.section.slug}#${h.section.id}`} role="option" aria-selected={i === active}>
          <button
            onClick={() => onChoose(h)}
            className={`w-full text-left px-4 py-2 border-b border-border/40 ${i === active ? 'bg-primary/10' : 'hover:bg-accent'}`}
          >
            <span className="block text-[10px] uppercase tracking-wider text-muted-foreground">{h.section.title}</span>
            <span className="block text-xs font-semibold">{h.section.heading || h.section.title}</span>
            <span className="block text-[11px] text-muted-foreground leading-snug mt-0.5">
              {h.snippet.map((p, j) => p.mark
                ? <mark key={j} className="bg-primary/20 text-foreground">{p.text}</mark>
                : <span key={j}>{p.text}</span>)}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
};

/** An icon for the Help menu and the tab. */
export const DocsIcon = BookOpen;
