import { useEffect, useState } from 'react';
import { CalendarDays, FileText, Loader2, Mail, SquareKanban, Users } from 'lucide-react';
import { openContact, openMessage, openTool, previewAttachment } from '../../lib/tabs';
import { useCalendarNav } from '../../lib/calendarNav';
import { openCardOnBoard } from '../Board/labels';

interface Group {
  id: 'mail' | 'files' | 'cards' | 'people' | 'events';
  label: string;
  query: string;
  total: number;
  items?: any;
  error?: string;
}

const ICONS = { mail: Mail, files: FileText, cards: SquareKanban, people: Users, events: CalendarDays };

const when = (iso?: string) => (iso ? new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' }) : '');

/**
 * All Items: the same words across mail, files, cards, people and events,
 * each kind's answer beside the others with "See all", which opens that kind
 * as a folder. Acting on things happens there, inside one kind, where an
 * action means one thing.
 */
export const AllItems = ({ query, onSeeAll }: { query: string; onSeeAll: (q: string) => void }) => {
  const [groups, setGroups] = useState<Group[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!query.trim()) { setGroups([]); return; }
    let cancelled = false;
    setGroups(null);
    fetch(`/api/search?${new URLSearchParams({ q: query, limit: '5' })}`)
      .then(r => (r.ok ? r.json() : r.json().then(b => Promise.reject(new Error(b.error ?? `${r.status}`)))))
      .then(b => { if (!cancelled) { setGroups(b.groups ?? []); setError(null); } })
      .catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [query]);

  if (!query.trim()) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-2 text-muted-foreground p-8 text-center">
        <p className="text-sm">Search everything at once: mail, files, cards, people and events.</p>
        <p className="text-xs">Words find text anywhere; <code className="font-mono">from:acme</code> finds Acme's mail, the files they sent, the cards their mail raised, and them.</p>
      </div>
    );
  }
  if (error) return <p className="p-4 text-sm text-destructive">{error}</p>;
  if (!groups) {
    return <p className="p-4 text-sm text-muted-foreground flex items-center gap-2"><Loader2 size={14} className="animate-spin" /> Searching…</p>;
  }
  const found = groups.filter(g => g.total > 0);
  return (
    <div className="flex-1 overflow-auto p-4 space-y-5" aria-label="All Items">
      {found.length === 0 && <p className="text-sm text-muted-foreground">Nothing anywhere matches “{query}”.</p>}
      {groups.map(g => {
        const Icon = ICONS[g.id] ?? Mail;
        if (g.error) {
          return (
            <p key={g.id} className="text-xs text-muted-foreground flex items-center gap-1.5">
              <Icon size={12} /> {g.label}: not searched — {g.error}
            </p>
          );
        }
        if (g.total === 0) return null;
        return (
          <section key={g.id} aria-label={g.label}>
            <header className="flex items-center gap-2 mb-1.5">
              <Icon size={14} className="text-muted-foreground" />
              <h3 className="text-sm font-semibold">{g.label}</h3>
              <span className="text-xs text-muted-foreground tabular-nums">{g.total.toLocaleString()}</span>
              <button type="button" onClick={() => onSeeAll(g.query)} className="ml-auto text-xs text-primary hover:underline">
                See all {g.total > 5 ? g.total.toLocaleString() : ''}
              </button>
            </header>
            <ul className="border border-border divide-y divide-border/60">
              {rows(g).map(r => (
                <li key={r.key}>
                  <button type="button" onClick={r.open} className="w-full text-left flex items-center gap-3 px-3 py-1.5 text-sm hover:bg-accent/40">
                    <span className="w-48 shrink-0 truncate text-muted-foreground">{r.first}</span>
                    <span className="flex-1 truncate">{r.second}</span>
                    <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{r.third}</span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        );
      })}
    </div>
  );
};

interface Row { key: string; first: string; second: string; third: string; open: () => void }

function rows(g: Group): Row[] {
  const it = g.items ?? {};
  switch (g.id) {
    case 'mail':
      return (it.messages ?? []).map((m: any) => ({
        key: m.id, first: m.from || '(no sender)', second: m.subject || '(no subject)', third: when(m.date),
        open: () => openMessage(m),
      }));
    case 'files':
      return (it.attachments ?? []).map((f: any) => ({
        key: f.key, first: f.mimeType ?? '', second: f.filename || '(unnamed)', third: f.size ? `${Math.round(f.size / 1024)} KB` : '',
        open: () => previewAttachment(f),
      }));
    case 'cards':
      return (it.tickets ?? []).map((t: any) => ({
        key: t.id, first: t.status, second: t.title, third: t.dueAt ? `due ${when(t.dueAt)}` : '',
        open: () => openCardOnBoard(t.id),
      }));
    case 'people':
      return (it.contacts ?? []).map((c: any) => ({
        key: c.address, first: c.displayName || c.headerName || '', second: c.address, third: c.kind ?? '',
        open: () => openContact(c.address),
      }));
    case 'events':
      return (it.events ?? []).map((e: any) => ({
        key: `${e.eventId}-${e.start}`, first: when(e.start), second: e.summary || '(no title)', third: e.location ?? '',
        open: () => {
          const nav = useCalendarNav.getState();
          nav.setAnchor(new Date(e.start));
          nav.select(e.eventId);
          openTool('calendar', 'Calendar');
        },
      }));
  }
  return [];
}
