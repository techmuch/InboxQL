import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { usePlaceStore, goTo, HOME_PLACE } from '../lib/places';
import { useQueryStore } from '../lib/filters';
import { queryCount } from '../lib/counts';
import { NAV, isCurrent, type NavItem, type NavSection } from './nav';
import { EVIDENCE_SLOT_ID, useEvidenceCount } from './evidenceSlot';
import './shell.css';

/**
 * The frame every screen sits in: an app bar, the places on the left, the
 * screen in the middle and the evidence on the right.
 *
 * It replaced nexus-shell's docking workspace (see lib/places.ts for why).
 * The frame knows nothing about any one screen: the screen is a child, its
 * evidence arrives through `<Evidence>`, and the left-hand pane is data.
 */
export interface AppShellProps {
  /** The places that exist; a pane item for anything else is left out. */
  available: Set<string>;
  /** Extra sections for the pane, after Needs me and Mail (Subjects). */
  subjects?: NavSection | null;
  /** The screen. */
  children: ReactNode;
  /** What the evidence panel says when the screen has nothing to put there. */
  evidenceDefault?: ReactNode;
  /** Right of the query bar: Go to…, Write. */
  actions?: ReactNode;
  /** The status line: sync, window. */
  status?: ReactNode;
  /** The person, and Sign out. */
  person?: ReactNode;
  /** Running the query bar. */
  onQuery: (q: string) => void;
  /** Asking the words in the query bar, with ⌘↵. */
  onAsk?: (q: string) => void;
  /** Labels of places, for the title and the screen-reader announcement. */
  labelOf: (place: string) => string;
  /** The mail list has its own query editor; the bar's would be a second one. */
  hideQuery?: boolean;
}

const WIDTH_KEY = 'iql.evidenceWidth';
const HIDDEN_KEY = 'iql.evidenceHidden';
const DEFAULT_W = 352;
const MIN_W = 256;

const load = (k: string) => { try { return localStorage.getItem(k); } catch { return null; } };
const save = (k: string, v: string) => { try { localStorage.setItem(k, v); } catch { /* not remembered: fine */ } };

export const AppShell = ({ available, subjects, children, evidenceDefault, actions, status, person, onQuery, onAsk, labelOf, hideQuery }: AppShellProps) => {
  const place = usePlaceStore(s => s.id);
  const query = useQueryStore(s => s.text);
  const frame = useRef<HTMLDivElement>(null);
  const mainRef = useRef<HTMLElement>(null);
  const [width, setWidth] = useState(() => Number(load(WIDTH_KEY)) || DEFAULT_W);
  const [hidden, setHidden] = useState(() => load(HIDDEN_KEY) === '1' || window.innerWidth <= 1100);
  const filled = useEvidenceCount(s => s.n > 0);
  const setSlot = useEvidenceCount(s => s.setSlot);
  // The frame's width, for the widest the panel may be; read on resize, not in render.
  const [frameW, setFrameW] = useState(1440);
  useEffect(() => {
    const el = frame.current;
    if (!el) return;
    const read = () => setFrameW(el.clientWidth || 1440);
    read();
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(read) : null;
    ro?.observe(el);
    return () => ro?.disconnect();
  }, []);

  // The title says where you are, so a screen reader and the window list do.
  useEffect(() => { document.title = `${labelOf(place)} · InboxQL`; }, [place, labelOf]);
  // A new place starts at its top.
  useEffect(() => { mainRef.current?.scrollTo?.({ top: 0 }); }, [place]);

  const maxW = useCallback(() => Math.max(MIN_W, Math.min(720, Math.round(frameW * 0.5))), [frameW]);
  const clamp = useCallback((w: number) => Math.max(MIN_W, Math.min(maxW(), Math.round(w))), [maxW]);
  const commit = (w: number, h: boolean) => { save(WIDTH_KEY, String(w)); save(HIDDEN_KEY, h ? '1' : '0'); };

  const toggle = useCallback(() => {
    setHidden(h => { commit(width, !h); return !h; });
  }, [width]);

  // "." shows or hides the evidence, anywhere outside a field.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '.' || e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return;
      const t = e.target as HTMLElement | null;
      if (t?.closest?.('input, textarea, select, [contenteditable="true"], [role="textbox"]')) return;
      e.preventDefault();
      toggle();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [toggle]);

  const onSepKey = (e: React.KeyboardEvent) => {
    const step = e.shiftKey ? 64 : 16;
    let w = width; let h = hidden;
    if (e.key === 'ArrowLeft') { h = false; w = width + step; }
    else if (e.key === 'ArrowRight') { if (!hidden) w = width - step; }
    else if (e.key === 'Home') { h = false; w = MIN_W; }
    else if (e.key === 'End') { h = false; w = maxW(); }
    else if (e.key === 'Enter') { e.preventDefault(); toggle(); return; }
    else return;
    e.preventDefault();
    w = clamp(w);
    setWidth(w); setHidden(h); commit(w, h);
  };

  const drag = useRef<{ x: number; w: number; moved: boolean } | null>(null);
  const onDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    drag.current = { x: e.clientX, w: hidden ? 0 : width, moved: false };
    e.currentTarget.setPointerCapture(e.pointerId);
    e.currentTarget.classList.add('dragging');
    document.body.classList.add('iql-resizing');
  };
  const onMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d) return;
    if (Math.abs(d.x - e.clientX) > 3) d.moved = true;
    if (!d.moved) return;
    const w = d.w + (d.x - e.clientX);
    // Dragging well past the narrowest hides it, as most editors do.
    if (w < MIN_W - 96) setHidden(true);
    else { setHidden(false); setWidth(clamp(w)); }
  };
  const onUp = (e: React.PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    drag.current = null;
    e.currentTarget.classList.remove('dragging');
    document.body.classList.remove('iql-resizing');
    if (!d) return;
    let h = hidden;
    if (!d.moved && hidden) { h = false; setHidden(false); }
    commit(width, h);
  };

  const sections = [NAV[0], NAV[1], ...(subjects ? [subjects] : []), ...NAV.slice(2)]
    .map(s => ({ ...s, items: s.items.filter(i => available.has(i.place)) }))
    .filter(s => s.items.length > 0);

  return (
    <div ref={frame} className={`iql-frame${hidden ? ' ev-hidden' : ''}`} style={{ '--ev-w': `${width}px` } as React.CSSProperties}>
      <a className="iql-skip" href="#iql-main" onClick={e => { e.preventDefault(); mainRef.current?.focus(); }}>Skip to the screen</a>
      <header className="iql-bar">
        <a className="iql-brand" href={`#/${HOME_PLACE}`} onClick={e => { e.preventDefault(); goTo(HOME_PLACE); }}>InboxQL</a>
        {hideQuery ? <span style={{ flex: 1 }} /> : <QueryBar onRun={onQuery} onAsk={onAsk} />}
        {actions}
        <div className="iql-status" role="status" aria-live="off">{status}</div>
      </header>

      <nav className="iql-nav" aria-label="Places">
        {sections.map(s => (
          <div key={s.key}>
            <h2 id={`nav-${s.key}`}>{s.label}</h2>
            <ul aria-labelledby={`nav-${s.key}`}>
              {s.items.map(item => <NavLink key={item.key} item={item} current={isCurrent(item, place, query)} />)}
            </ul>
          </div>
        ))}
        {person && <div className="who">{person}</div>}
      </nav>

      <main id="iql-main" ref={mainRef} className="iql-main" tabIndex={-1} aria-label={labelOf(place)}>
        {children}
      </main>

      <aside className="iql-ev" aria-label="Evidence" id="iql-evidence">
        <div
          className="iql-splitter"
          role="separator"
          tabIndex={0}
          aria-orientation="vertical"
          aria-controls="iql-evidence"
          aria-label="Resize the evidence panel"
          aria-valuemin={0}
          aria-valuemax={maxW()}
          aria-valuenow={hidden ? 0 : width}
          aria-valuetext={hidden ? 'Evidence panel hidden' : `Evidence panel ${width} pixels wide`}
          title="Drag to resize · Enter or . to hide · double-click to reset"
          onKeyDown={onSepKey}
          onPointerDown={onDown}
          onPointerMove={onMove}
          onPointerUp={onUp}
          onPointerCancel={onUp}
          onDoubleClick={() => { setHidden(false); setWidth(DEFAULT_W); commit(DEFAULT_W, false); }}
        ><span className="grip" aria-hidden="true" /></div>
        <div className="iql-ev-scroll">
          <div id={EVIDENCE_SLOT_ID} ref={setSlot} />
          {!filled && <div className="iql-ev-default">{evidenceDefault}</div>}
        </div>
      </aside>
    </div>
  );
};

const NavLink = ({ item, current }: { item: NavItem; current: boolean }) => {
  const [n, setN] = useState<number | null>(null);
  useEffect(() => {
    if (!item.count) return;
    const ctl = new AbortController();
    const qs = Array.isArray(item.count) ? item.count : [item.count];
    const read = () => Promise.all(qs.map(q => queryCount(q, ctl.signal))).then(vs => {
      if (!ctl.signal.aborted) setN(vs.some(v => v == null) ? null : vs.reduce<number>((a, v) => a + (v ?? 0), 0));
    });
    read();
    // Counts follow the mailbox: a sync, an archive, a send all say so.
    const onChange = () => read();
    window.addEventListener('iql:mail-changed', onChange);
    const t = window.setInterval(read, 60_000);
    return () => { ctl.abort(); window.removeEventListener('iql:mail-changed', onChange); window.clearInterval(t); };
  }, [item.count]);
  return (
    <li>
      <a
        href={`#/${item.place}`}
        aria-current={current ? 'page' : undefined}
        onClick={e => {
          e.preventDefault();
          if (item.query) useQueryStore.getState().set(item.query);
          goTo(item.place);
        }}
      >
        {item.label}
        {n != null && n > 0 && <span className="count">{n}<span className="sr-only"> {item.countNoun ?? ''}</span></span>}
      </a>
    </li>
  );
};

/**
 * The query bar: what the mail list is asking, editable from any screen.
 *
 * Enter runs it in the mail list. `/` focuses it from anywhere.
 */
const QueryBar = ({ onRun, onAsk }: { onRun: (q: string) => void; onAsk?: (q: string) => void }) => {
  const query = useQueryStore(s => s.text);
  const [text, setText] = useState(query);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => { setText(query); }, [query]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t?.closest?.('input, textarea, select, [contenteditable="true"], [role="textbox"]')) return;
      e.preventDefault();
      input.current?.focus();
      input.current?.select();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
  return (
    <form className="iql-querybar" role="search" aria-label="Query" onSubmit={e => { e.preventDefault(); onRun(text.trim()); }}>
      <label htmlFor="iql-q" className="sr-only">Query — type one and press Enter to see what it finds</label>
      <span aria-hidden="true" style={{ opacity: .6 }}>⌕</span>
      <input id="iql-q" ref={input} value={text} onChange={e => setText(e.target.value)} spellCheck={false} autoComplete="off"
        placeholder={onAsk ? 'A query, or a question in words — ⌘↵ asks' : 'from:… · is:unread · words'}
        aria-describedby={onAsk ? 'iql-q-help' : undefined}
        onKeyDown={e => { if (onAsk && e.key === 'Enter' && (e.metaKey || e.ctrlKey) && text.trim()) { e.preventDefault(); onAsk(text.trim()); } }} />
      {onAsk && <span id="iql-q-help" className="sr-only">Enter runs a query. A question in words, or Command Enter, is asked of Claude Code.</span>}
      <kbd className="iql-kbd" aria-hidden="true">/</kbd>
    </form>
  );
};
