import { useEffect, useMemo, useRef, useState } from 'react';
import { AlertCircle, BarChart2, Check, ChevronLeft, ChevronRight, Gauge, Layout, Mail, Users, X } from 'lucide-react';
import { ActivityCalendar } from './ActivityCalendar';
import { Help } from './Help';
import { openQuery } from '../lib/tabs';
import { useQueryStore, queryTerms, compose, type QueryTerm } from '../lib/filters';
import { FOLDER_LABELS, hasTerm, joinScope, splitScope } from '../lib/scope';
import { isMailQuery, openingFolderQuery } from '../lib/threading';
import { onMailChanged } from '../lib/mail';

/** One group of a widget, with the term that is exactly it. */
interface Group {
  label: string;
  value: number;
  /** What a click adds; absent for a group no term can name. */
  term?: string;
  /** For topics: extracted by an annotator, or the first word of subjects. */
  kind?: 'topic' | 'word';
}

/** The folders the dashboard can chart, in the rail's order. */
const FOLDERS = ['inbox', 'starred', 'sent', 'allmail', 'spam', 'trash'];

const WIDGETS = ['volume', 'senders', 'topics', 'importance', 'sentby'] as const;
type Widget = typeof WIDGETS[number];

const SENTBY_LABELS: Record<string, string> = {
  person: 'People', organization: 'Organisations', system: 'Systems', unknown: 'Not classified',
};

/** Levels read best first, not by size: a scale, not a leaderboard. */
const IMPORTANCE_ORDER = ['important', 'normal', 'low', 'ignorable'];
const SENTBY_ORDER = ['person', 'organization', 'system', 'unknown'];

/** A widget's last answer, and the request it answered. */
interface Loaded { key: string; groups: Group[]; error?: string }

/** One widget's groups; a failure is an answer, cleared of numbers. */
const fetchWidget = async (w: Widget, query: string, year?: number): Promise<Omit<Loaded, 'key'>> => {
  const params = new URLSearchParams({ type: w });
  if (query) params.set('q', query);
  if (year) params.set('year', String(year));
  try {
    const res = await fetch(`/api/analytics?${params}`);
    const body = await res.json().catch(() => null);
    // Cleared with the error: old numbers beside a warning read as current.
    if (!res.ok) return { groups: [], error: body?.error ?? `Analytics failed (${res.status}).` };
    return { groups: Array.isArray(body) ? body : [] };
  } catch (e) {
    return { groups: [], error: e instanceof Error ? e.message : String(e) };
  }
};

/**
 * The dashboard: how much mail arrived over a year, who sends it, what it is
 * about, how much matters and who — or what — it comes from.
 *
 * # The same query as the Desk
 *
 * It charts the Desk's query, so narrowing here narrows there and **Open in
 * Desk** is the same mail as a list. The folder stands in front as a chip, as
 * it does on the Desk, and the pills are only what was added: clearing them
 * keeps the folder.
 *
 * # A click shows what the bar counted
 *
 * Every group arrives with the term that is exactly it — `from:=` for a
 * sender, not a substring; `topic:=` for a topic an annotator extracted — so
 * the mail a click opens is the mail the bar measured.
 */
export const Dashboard = () => {
  const query = useQueryStore(s => s.text);
  const setQuery = useQueryStore(s => s.set);
  const [termsFor, setTermsFor] = useState<{ q: string; terms: QueryTerm[] } | null>(null);
  useEffect(() => {
    queryTerms(query).then(terms => setTermsFor({ q: query, terms })).catch(() => setTermsFor({ q: query, terms: [] }));
  }, [query]);
  const terms = useMemo(() => (termsFor?.q === query ? termsFor.terms : []), [termsFor, query]);
  const split = useMemo(() => splitScope(query, terms), [query, terms]);
  const scope = split.scope;
  const pills = terms.filter(t => t.text !== scope.term);
  const mail = isMailQuery(query) && scope.mail;

  const toggleTerm = async (term: string) => {
    setQuery(await compose(query, hasTerm(terms, term) ? { remove: term } : { add: term }));
  };
  const removeTerm = async (term: string) => setQuery(await compose(query, { remove: term }));
  const clearAdded = () => setQuery(joinScope(scope.term, ''));
  const chooseFolder = async (id: string) =>
    setQuery(await compose(query, { field: 'folder', term: `folder:${id}` }));

  // The year the calendar draws. A query on a day of another year opens on
  // that year, or the day it names could never be shown; the arrows choose
  // another until the day changes.
  const thisYear = new Date().getFullYear();
  const onDay = terms.find(t => t.field === 'on' && !t.negated);
  const onYear = onDay ? Number(/(\d{4})/.exec(onDay.text)?.[1]) || 0 : 0;
  const [picked, setPicked] = useState<{ day: string; year: number } | null>(null);
  const dayKey = onDay?.text ?? '';
  const year = picked && picked.day === dayKey ? picked.year : onYear || thisYear;
  const setYear = (f: (y: number) => number) => setPicked({ day: dayKey, year: f(year) });

  // Mail changes — a move, a bulk action, Undo, a sync — refresh the charts.
  // A first sync reports every few seconds; those are gathered for a moment.
  const [reload, setReload] = useState(0);
  const pending = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    const off = onMailChanged(source => {
      if (source === 'local') { setReload(r => r + 1); return; }
      if (pending.current) return;
      pending.current = setTimeout(() => { pending.current = null; setReload(r => r + 1); }, 1500);
    });
    return () => { off(); if (pending.current) clearTimeout(pending.current); };
  }, []);

  // Each widget loads on its own: one slow grouping (importance over All Mail
  // takes seconds) leaves the rest drawn, and stepping the year reloads only
  // the calendar. Until a widget answers, its previous numbers stay, dimmed,
  // as the rail's counts do.
  const keyFor = (w: Widget) => `${query}\n${reload}${w === 'volume' ? `\n${year}` : ''}`;
  const [loaded, setLoaded] = useState<Partial<Record<Widget, Loaded>>>({});
  const volumeKey = keyFor('volume');
  const restKey = keyFor('senders');

  useEffect(() => {
    if (!mail) return;
    let cancelled = false;
    fetchWidget('volume', query, year).then(r => { if (!cancelled) setLoaded(l => ({ ...l, volume: { key: volumeKey, ...r } })); });
    return () => { cancelled = true; };
  }, [volumeKey, mail]); // eslint-disable-line react-hooks/exhaustive-deps -- the key is query, year and reload

  useEffect(() => {
    if (!mail) return;
    let cancelled = false;
    for (const w of WIDGETS.filter(w => w !== 'volume')) {
      fetchWidget(w, query).then(r => { if (!cancelled) setLoaded(l => ({ ...l, [w]: { key: restKey, ...r } })); });
    }
    return () => { cancelled = true; };
  }, [restKey, mail]); // eslint-disable-line react-hooks/exhaustive-deps -- the key is query and reload

  const data = useMemo(() => Object.fromEntries(WIDGETS.map(w => [w, loaded[w]?.groups ?? []])) as Record<Widget, Group[]>, [loaded]);
  const stale = (w: Widget) => loaded[w]?.key !== keyFor(w);
  // Why the failure is rendered rather than logged: a chart that is quietly
  // stale is worse than one that says it could not load.
  const error = WIDGETS.map(w => (!stale(w) && loaded[w]?.error) || null).find(Boolean) ?? null;
  const dim = (w: Widget) => (stale(w) ? 'opacity-50 transition-opacity' : 'transition-opacity');

  const calendar = useMemo(() => data.volume.map(d => ({ day: d.label, value: d.value })), [data.volume]);
  // The applied group of a widget, named in its heading instead of "(Filtered)".
  const appliedIn = (groups: Group[]) => groups.find(g => g.term && hasTerm(terms, g.term));
  const cardClass = (on: boolean) =>
    `p-6 bg-card border shadow-sm flex flex-col transition-colors ${on ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-primary/50'}`;

  if (!mail) {
    return (
      <div className="p-6 h-full bg-background text-foreground">
        <h2 className="flex items-center gap-2 text-2xl font-bold mb-6">Analytics Pulse <Help topic="dashboard" /></h2>
        <div className="max-w-lg border border-border bg-card p-5 space-y-3">
          <p className="text-sm">
            The Desk is on <b>{scope.label}</b>. The dashboard charts mail, so there is nothing here to draw for {scope.label.toLowerCase()}.
          </p>
          <button onClick={() => setQuery(openingFolderQuery('folder:allmail'))}
            className="text-xs font-semibold text-primary-foreground bg-primary px-3 py-1.5 hover:opacity-90">
            Chart All Mail
          </button>
        </div>
      </div>
    );
  }

  const senderOn = appliedIn(data.senders);
  const topicOn = appliedIn(data.topics);
  const importanceOn = appliedIn(data.importance);
  const sentbyOn = appliedIn(data.sentby);
  const words = data.topics.some(t => t.kind === 'word');

  return (
    <div className="p-6 overflow-auto h-full bg-background text-foreground">
      <div className="flex justify-between items-center mb-4">
        <h2 className="flex items-center gap-2 text-2xl font-bold">Analytics Pulse <Help topic="dashboard" /></h2>
      </div>

      {/* The frame: the folder as a chip with a picker, then what was added. */}
      <div className="bg-primary/5 border border-primary/10 px-4 py-2 flex items-center gap-3 mb-6">
        <label className="flex items-center gap-1 text-xs font-semibold">
          <span className="sr-only">Folder</span>
          <select aria-label="Folder" value={FOLDERS.includes(scope.id) ? scope.id : ''}
            onChange={e => void chooseFolder(e.target.value)}
            className="bg-background border border-border px-1.5 py-0.5 text-xs font-semibold">
            {!FOLDERS.includes(scope.id) && <option value="">{scope.label}</option>}
            {FOLDERS.map(id => <option key={id} value={id}>{FOLDER_LABELS[id]}</option>)}
          </select>
          <span className="text-muted-foreground">›</span>
        </label>
        <div className="flex flex-1 gap-2 overflow-auto no-scrollbar">
          {pills.length === 0 && <span className="text-[11px] text-muted-foreground">everything in {scope.label} — click a chart to narrow it</span>}
          {pills.map(t => (
            <span key={t.start} className="px-2 py-0.5 bg-primary text-primary-foreground text-[10px] font-mono flex items-center gap-1 shadow-sm whitespace-nowrap">
              {t.text}
              <button aria-label={`Remove ${t.text}`} onClick={() => removeTerm(t.text)}><X className="w-2.5 h-2.5" /></button>
            </span>
          ))}
        </div>
        {pills.length > 0 && (
          <button onClick={clearAdded} className="flex items-center gap-1 text-[10px] font-semibold text-primary hover:bg-primary/20 bg-primary/10 px-2.5 py-1 whitespace-nowrap">
            <X className="w-2.5 h-2.5" /> Clear
          </button>
        )}
        {/* The filters are a query, so they can go somewhere they can be
            edited and saved rather than only cleared. */}
        <button onClick={() => openQuery(query)}
          className="text-[10px] font-semibold text-primary hover:bg-primary/20 bg-primary/10 px-2.5 py-1 whitespace-nowrap">
          Open in Desk
        </button>
      </div>

      {error && (
        <div className="mb-6 flex items-start gap-2 border border-destructive/20 bg-destructive/10 p-3 text-xs text-destructive">
          <AlertCircle className="mt-px h-3.5 w-3.5 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      <div className="space-y-6">
        <div className={`${cardClass(Boolean(onDay))} min-h-80`}>
          <div className="flex items-center gap-3 mb-4">
            <BarChart2 className="w-5 h-5 text-primary" />
            <span className="font-bold text-lg">Communication Intensity {onDay ? onDay.text.replace(/^on:/, 'on ') : ''}</span>
            <div className="ml-auto flex items-center gap-1 text-sm">
              <button aria-label="Previous year" onClick={() => setYear(y => y - 1)} className="p-1 hover:bg-accent"><ChevronLeft className="w-4 h-4" /></button>
              <span className="tabular-nums font-semibold w-12 text-center" aria-label="Year">{year}</span>
              <button aria-label="Next year" disabled={year >= thisYear} onClick={() => setYear(y => y + 1)}
                className="p-1 hover:bg-accent disabled:opacity-30"><ChevronRight className="w-4 h-4" /></button>
            </div>
          </div>
          <div className={`flex-1 h-64 min-h-64 ${dim('volume')}`}>
            {calendar.length > 0 ? (
              <ActivityCalendar data={calendar} from={`${year}-01-01`} to={`${year}-12-31`} onDay={day => toggleTerm(`on:${day}`)} />
            ) : (
              <div className="h-full flex items-center justify-center text-xs text-muted-foreground italic">
                No mail in {year} for these filters
              </div>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div className={`${cardClass(Boolean(senderOn))} min-h-64`} aria-label="Top Senders" role="region">
            <div className="flex items-center gap-3 mb-4">
              <Mail className="w-5 h-5 text-primary" />
              <span className="font-bold text-lg truncate">Top Senders{senderOn ? ` — ${senderOn.label}` : ''}</span>
            </div>
            <div className={`flex-1 space-y-1.5 ${dim('senders')}`}>
              {data.senders.map(d => {
                const on = Boolean(d.term && hasTerm(terms, d.term));
                return (
                  <button key={d.label} disabled={!d.term} onClick={() => d.term && toggleTerm(d.term)}
                    className={`w-full flex items-center gap-2.5 p-1.5 transition-all ${on ? 'bg-primary/10 ring-1 ring-primary' : 'hover:bg-accent'}`}>
                    <div className="w-7 h-7 bg-primary/10 flex items-center justify-center text-[9px] font-bold text-primary">
                      {d.label[0]?.toUpperCase() || '?'}
                    </div>
                    <div className="flex-1 min-w-0 text-left">
                      <div className="text-xs font-semibold truncate leading-tight">{d.label}</div>
                      <div className="text-[9px] text-muted-foreground uppercase tracking-tighter">{d.value} messages</div>
                    </div>
                    {on && <Check className="w-2.5 h-2.5 text-primary" />}
                  </button>
                );
              })}
              {data.senders.length === 0 && <span className="text-xs text-muted-foreground italic text-center pt-10 block">No senders found for these filters</span>}
            </div>
          </div>

          <div className={`${cardClass(Boolean(topicOn))} min-h-64`} aria-label="Topics" role="region">
            <div className="flex items-center gap-3 mb-1">
              <Layout className="w-5 h-5 text-primary" />
              <span className="font-bold text-lg truncate">Topics{topicOn ? ` — ${topicOn.label}` : ''}</span>
            </div>
            {/* Two kinds share the card, and they open different mail: say which. */}
            {words && (
              <p className="text-[10px] text-muted-foreground mb-3">
                <i>Italic</i> ones are the first word of subjects where no topic was extracted; a click finds subjects containing it.
              </p>
            )}
            <div className={`flex-1 flex flex-wrap gap-1.5 content-start ${dim('topics')}`}>
              {data.topics.map(d => {
                const on = Boolean(d.term && hasTerm(terms, d.term));
                return (
                  <button key={d.label} disabled={!d.term} onClick={() => d.term && toggleTerm(d.term)}
                    title={d.kind === 'word' ? `The first word of ${d.value} subjects — ${d.term}` : `Extracted topic — ${d.term}`}
                    className={`px-2.5 py-1 text-[10px] font-semibold flex items-center gap-1.5 transition-all ${d.kind === 'word' ? 'italic' : ''} ${on ? 'bg-primary text-primary-foreground shadow-sm' : 'bg-muted hover:bg-primary/10 hover:text-primary'}`}>
                    <span className="truncate max-w-[100px]">{d.label}</span>
                    <span className={`text-[9px] ${on ? 'text-primary-foreground/70' : 'opacity-50 font-mono'}`}>{d.value}</span>
                  </button>
                );
              })}
              {data.topics.length === 0 && <span className="text-xs text-muted-foreground italic text-center pt-10 w-full block">No topics found for these filters</span>}
            </div>
          </div>

          <Bars title="Importance" icon={Gauge} groups={data.importance} on={importanceOn} terms={terms} onPick={toggleTerm}
            order={IMPORTANCE_ORDER} names={{ '(not judged)': 'Not judged yet' }} stale={stale('importance')}
            empty="Nothing judged yet — the harness judges importance." />
          <Bars title="Kind of sender" icon={Users} groups={data.sentby} on={sentbyOn} terms={terms} onPick={toggleTerm}
            order={SENTBY_ORDER} names={SENTBY_LABELS} stale={stale('sentby')} empty="No senders classified yet." />
        </div>
      </div>
    </div>
  );
};

/**
 * A card of horizontal bars, each a group a click adds to the query, in the
 * order given. A group no term can name — importance's "not judged" — is a
 * line under the bars rather than one of them: it is usually most of the
 * mailbox, and as a bar it shrank every level to a sliver.
 */
const Bars = ({ title, icon: Icon, groups, on, terms, onPick, order = [], names = {}, stale, empty }: {
  title: string;
  icon: React.ComponentType<{ className?: string }>;
  groups: Group[];
  on?: Group;
  terms: QueryTerm[];
  onPick: (term: string) => void;
  order?: string[];
  names?: Record<string, string>;
  stale?: boolean;
  empty: string;
}) => {
  const rank = (g: Group) => (order.includes(g.label) ? order.indexOf(g.label) : order.length);
  const bars = groups.filter(g => g.term).sort((a, b) => rank(a) - rank(b));
  const rest = groups.filter(g => !g.term);
  const total = groups.reduce((n, g) => n + g.value, 0);
  const max = Math.max(1, ...bars.map(g => g.value));
  const name = (g: Group) => names[g.label] ?? g.label;
  return (
    <div className={`p-6 bg-card border shadow-sm flex flex-col transition-colors min-h-48 ${on ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-primary/50'}`}
      aria-label={title} role="region">
      <div className="flex items-center gap-3 mb-4">
        <Icon className="w-5 h-5 text-primary" />
        <span className="font-bold text-lg truncate">{title}{on ? ` — ${name(on)}` : ''}</span>
      </div>
      <div className={`space-y-1.5 transition-opacity ${stale ? 'opacity-50' : ''}`}>
        {bars.map(g => {
          const active = Boolean(g.term && hasTerm(terms, g.term));
          return (
            <button key={g.label} onClick={() => g.term && onPick(g.term)} title={g.term}
              className={`w-full flex items-center gap-3 px-1.5 py-1 text-xs text-left ${active ? 'bg-primary/10 ring-1 ring-primary' : 'hover:bg-accent'}`}>
              <span className="w-28 truncate">{name(g)}</span>
              <span className="flex-1 h-2 bg-muted"><span className="block h-full bg-primary/60" style={{ width: `${(g.value / max) * 100}%` }} /></span>
              <span className="w-12 text-right tabular-nums">{g.value.toLocaleString()}</span>
            </button>
          );
        })}
        {rest.map(g => (
          <p key={g.label} className="px-1.5 pt-1 text-[11px] text-muted-foreground">
            {name(g)}: <span className="tabular-nums">{g.value.toLocaleString()}</span> of {total.toLocaleString()}
          </p>
        ))}
        {bars.length === 0 && <span className="text-xs text-muted-foreground italic block">{empty}</span>}
      </div>
    </div>
  );
};
