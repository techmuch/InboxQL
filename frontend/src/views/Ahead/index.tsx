import { useEffect, useMemo, useState } from 'react';
import { runQuery } from '../../lib/counts';
import { openQuery, openTool } from '../../lib/tabs';
import { dayKey, useImportantDates, type ImportantDate } from '../../lib/importantDates';
import { onMailChanged } from '../../lib/mail';
import { Evidence } from '../../shell/Evidence';
import { Countdowns, MarkDateDialog } from './Countdowns';
import { addDays, gather, mondayOf, type Item, type Occ, type Ticket } from './gather';

/**
 * Ahead: what is coming, and is any of it a problem.
 *
 * The calendar, card due dates and the dates marked important, on one page in
 * date order. Two ranges: the rest of this week and next week, day by day; or
 * this month and next as grids, with what matters listed in words beneath
 * each, so the grid is never the only way to read it.
 */

export const Ahead = () => {
  const [range, setRange] = useState<'weeks' | 'months'>('weeks');
  const [events, setEvents] = useState<Occ[] | null>(null);
  const [tickets, setTickets] = useState<Ticket[] | null>(null);
  const { dates, load } = useImportantDates();
  const [marking, setMarking] = useState<Partial<ImportantDate> | null>(null);

  const today = useMemo(() => { const n = new Date(); return new Date(n.getFullYear(), n.getMonth(), n.getDate()); }, []);
  const end = useMemo(() => range === 'weeks'
    ? addDays(mondayOf(today), 13)
    : new Date(today.getFullYear(), today.getMonth() + 2, 0), [range, today]);
  const span = Math.round((end.getTime() - today.getTime()) / 86_400_000) + 1;

  useEffect(() => {
    const ctl = new AbortController();
    const fetchAll = () => {
      setEvents(null); setTickets(null);
      runQuery<{ events?: Occ[] }>(`in:events next:${span}d`, 500, ctl.signal).then(r => setEvents(r?.events ?? []));
      runQuery<{ tickets?: Ticket[] }>(`in:tickets due:${span}d -state:closed -state:rejected`, 200, ctl.signal).then(r => setTickets(r?.tickets ?? []));
      void load();
    };
    fetchAll();
    const off = onMailChanged(fetchAll);
    return () => { ctl.abort(); off(); };
  }, [span, load]);

  // w and m switch the range, i marks a date.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (e.metaKey || e.ctrlKey || e.altKey || t?.closest?.('input, textarea, select, [role="dialog"]')) return;
      if (e.key === 'w') setRange('weeks');
      else if (e.key === 'm') setRange('months');
      else if (e.key === 'i') { e.preventDefault(); setMarking({}); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const byDay = useMemo(() => events && tickets && dates ? gather(events, tickets, dates, today, end) : null, [events, tickets, dates, today, end]);
  const ready = byDay != null;
  const nextMon = addDays(mondayOf(today), 7);
  const nextWeekHours = ready ? range === 'weeks' ? sumHours(byDay!, nextMon, addDays(nextMon, 6)) : 0 : 0;
  const overdue = (tickets ?? []).filter(t => t.dueAt && new Date(t.dueAt) < today);

  const markItem = (it: Item) => setMarking({ title: it.title.replace(/^Start: /, ''), date: it.day, refKind: it.ref?.kind ?? '', refId: it.ref?.id ?? '' });

  return (
    <div className="mx-auto max-w-5xl px-6 py-5">
      <p className="text-sm text-muted-foreground"><button type="button" className="underline" onClick={() => openTool('today', 'Today')}>Today</button> · {today.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })}</p>
      <h1 className="text-2xl font-semibold">Ahead</h1>
      <p className="mt-1 text-[15px] text-muted-foreground" data-testid="ahead-summary">
        {!ready ? 'Gathering the calendar, cards and important dates…' : summary(byDay!, today, end, range, nextWeekHours, overdue.length)}
      </p>

      <nav aria-label="How far ahead" className="mt-4 flex flex-wrap items-center gap-1 border-b border-border">
        {(['weeks', 'months'] as const).map(r => (
          <button key={r} type="button" aria-current={range === r ? 'page' : undefined} onClick={() => setRange(r)}
            className={`-mb-px border-b-2 px-3 py-2 text-sm ${range === r ? 'border-primary font-semibold' : 'border-transparent text-muted-foreground'}`}>
            {r === 'weeks' ? 'Two weeks' : 'Two months'} <kbd className="iql-kbd">{r[0]}</kbd>
          </button>
        ))}
        <span className="ml-auto text-xs text-muted-foreground">Weeks start Monday</span>
      </nav>

      <section className="mt-6" aria-labelledby="a-imp">
        <header className="mb-2 flex items-baseline gap-3">
          <h2 id="a-imp" className="flex-1 text-lg font-semibold">Important dates</h2>
          <button type="button" className="iql-btn" onClick={() => setMarking({})}>+ Mark a date <kbd>i</kbd></button>
        </header>
        <Countdowns compact={range === 'months'} onMark={() => setMarking({})} />
      </section>

      {overdue.length > 0 && (
        <section className="mt-6" aria-labelledby="a-over">
          <h2 id="a-over" className="mb-2 text-lg font-semibold">Overdue <span className="font-normal text-muted-foreground">· {overdue.length}</span></h2>
          <ul className="divide-y divide-border border border-amber-600 bg-card dark:border-amber-400">
            {overdue.slice(0, 8).map(t => (
              <li key={t.id} className="flex flex-wrap gap-3 px-4 py-2 text-sm">
                <span className="w-24 shrink-0 tabular-nums text-muted-foreground">{new Date(t.dueAt!).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}</span>
                <span className="min-w-0 flex-1">{t.title}</span>
                <span className="text-muted-foreground">{t.status === 'proposed' ? 'proposed card' : 'card'}</span>
              </li>
            ))}
          </ul>
          {overdue.length > 8 && <button type="button" className="mt-1 text-sm underline" onClick={() => openQuery('in:tickets due:0d -state:closed -state:rejected')}>All {overdue.length} overdue cards</button>}
        </section>
      )}

      {!ready ? <p className="mt-6 text-sm text-muted-foreground">Loading…</p> : range === 'weeks' ? (
        <>
          <WeekList title="This week" sub={daysLeftThisWeek(today) === 1 ? 'the last day' : `${daysLeftThisWeek(today)} days left`} from={today} to={addDays(mondayOf(today), 6)} byDay={byDay!} today={today} onMark={markItem} />
          <WeekList title="Next week" sub={`${nextMon.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} – ${addDays(nextMon, 6).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}`}
            from={nextMon} to={addDays(nextMon, 6)} byDay={byDay!} today={today} onMark={markItem} glance />
        </>
      ) : (
        <>
          <Month year={today.getFullYear()} month={today.getMonth()} byDay={byDay!} today={today} />
          <Month year={end.getFullYear()} month={end.getMonth()} byDay={byDay!} today={today} />
        </>
      )}

      {marking && <MarkDateDialog initial={marking} onClose={() => setMarking(null)} />}

      <Evidence>
        <h2 className="mb-1 text-base font-semibold">How this was made</h2>
        <p className="mb-3 text-sm text-muted-foreground">Three kinds of thing on one page. Each line is a query you can open.</p>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1.5 text-sm">
          {([['Events', `in:events next:${span}d`], ['Due', `in:tickets due:${span}d -state:closed`]] as const).map(([k, q]) => (
            <div key={k} className="contents"><dt className="text-muted-foreground">{k}</dt>
              <dd><button type="button" className="text-left font-mono text-xs underline underline-offset-2" onClick={() => openQuery(q)}>{q}</button></dd></div>
          ))}
          <div className="contents"><dt className="text-muted-foreground">Important</dt><dd>dates you marked ({dates?.length ?? 0})</dd></div>
        </dl>
        <h3 className="mb-1 mt-5 text-sm font-semibold">Not counted</h3>
        <ul className="list-disc space-y-1 pl-5 text-sm">
          <li>Proposed events show, marked, but are not counted in hours booked until you approve them.</li>
          <li>Declined events are struck through and not counted.</li>
          <li>Hidden calendars are left out, as in the Calendar.</li>
        </ul>
        <h3 className="mb-1 mt-5 text-sm font-semibold">Reminders</h3>
        <p className="text-sm">Important dates remind you at 8 in the morning when a lead time starts, a week before, the day before and that morning — in every open window, or as a system notification when none is open.</p>
      </Evidence>
    </div>
  );
};

function daysLeftThisWeek(today: Date) { return 7 - ((today.getDay() + 6) % 7); }

function sumHours(byDay: Map<string, Item[]>, from: Date, to: Date) {
  let h = 0;
  for (let d = from; d <= to; d = addDays(d, 1)) for (const it of byDay.get(dayKey(d)) ?? []) h += it.hours ?? 0;
  return Math.round(h * 10) / 10;
}

function summary(byDay: Map<string, Item[]>, today: Date, end: Date, range: 'weeks' | 'months', nextWeekHours: number, overdue: number) {
  let events = 0; let declined = 0; let due = 0; let important = 0; let hours = 0;
  for (let d = today; d <= end; d = addDays(d, 1)) {
    for (const it of byDay.get(dayKey(d)) ?? []) {
      if (it.kind === 'Due') due++; else if (it.kind === 'Important') important++; else if (it.declined) declined++; else events++;
      hours += it.hours ?? 0;
    }
  }
  const parts = [
    `${events} event${events === 1 ? '' : 's'} (${Math.round(hours)} h booked${declined ? `; ${declined} declined` : ''})`,
    `${due} card${due === 1 ? '' : 's'} due`,
    `${important} important date${important === 1 ? '' : 's'}`,
  ];
  const lead = range === 'weeks' ? `Between now and the end of next week: ${parts.join(', ')}.` : `Between now and the end of next month: ${parts.join(', ')}.`;
  return `${lead}${range === 'weeks' ? ` Next week has ${nextWeekHours} hours booked.` : ''}${overdue ? ` ${overdue} card${overdue === 1 ? ' is' : 's are'} overdue.` : ''}`;
}

const WeekList = ({ title, sub, from, to, byDay, today, onMark, glance }: {
  title: string; sub: string; from: Date; to: Date; byDay: Map<string, Item[]>; today: Date; onMark: (it: Item) => void; glance?: boolean;
}) => {
  const days: Date[] = [];
  for (let d = from; d <= to; d = addDays(d, 1)) days.push(d);
  const id = `w-${dayKey(from)}`;
  return (
    <section className="mt-7" aria-labelledby={id}>
      <h2 id={id} className="mb-2 text-lg font-semibold">{title} <span className="font-normal text-muted-foreground">· {sub}</span></h2>
      {glance && (
        <ul className="mb-3 grid grid-cols-7 gap-1" aria-label={`${title} at a glance: hours booked each day`}>
          {days.map(d => {
            const h = sumHours(byDay, d, d);
            return (
              <li key={dayKey(d)} className="border border-border bg-card px-1.5 py-1 text-xs">
                <span className="block font-semibold">{d.toLocaleDateString(undefined, { weekday: 'short' })} {d.getDate()}</span>
                <span aria-hidden="true" className="relative my-1 block h-8 overflow-hidden bg-muted">
                  <i className="absolute inset-x-0 bottom-0 bg-foreground/50" style={{ height: `${Math.min(100, h / 8 * 100)}%` }} />
                </span>
                {h} h
              </li>
            );
          })}
        </ul>
      )}
      <ol className="divide-y divide-border border border-border bg-card">
        {days.map(d => {
          const items = byDay.get(dayKey(d)) ?? [];
          const isToday = dayKey(d) === dayKey(today);
          return (
            <li key={dayKey(d)} className={`grid grid-cols-[5.5rem_minmax(0,1fr)] gap-3 px-4 py-2.5 ${isToday ? 'bg-selected' : ''}`}>
              <h3 className="text-sm font-semibold">
                <span className="sr-only">{d.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })}{isToday ? ', today' : ''}</span>
                <span aria-hidden="true">{d.toLocaleDateString(undefined, { weekday: 'short' })}{isToday && ' · today'}<span className="block text-xl tabular-nums">{d.getDate()}</span></span>
              </h3>
              {items.length === 0 ? <p className={`text-sm ${isToday ? '' : 'text-muted-foreground'}`}>Nothing scheduled</p> : (
                <ul className="space-y-1">
                  {items.map(it => (
                    <li key={it.key} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 text-sm">
                      <span className="w-20 shrink-0 text-xs uppercase tracking-wide text-muted-foreground">{it.kind}</span>
                      <span className={`min-w-0 flex-1 ${it.declined ? 'text-muted-foreground line-through' : ''} ${it.important ? 'font-semibold' : ''}`}>
                        {it.important && <span aria-label="important">★ </span>}{it.title}
                        {it.detail && <span className="text-muted-foreground"> · {it.detail}</span>}
                      </span>
                      {!it.important && it.ref && (
                        <button type="button" className="text-xs underline text-muted-foreground" onClick={() => onMark(it)} aria-label={`Mark important: ${it.title}`}>Mark important</button>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </li>
          );
        })}
      </ol>
    </section>
  );
};

const WEEKDAYS = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

const Month = ({ year, month, byDay, today }: { year: number; month: number; byDay: Map<string, Item[]>; today: Date }) => {
  const first = new Date(year, month, 1);
  const start = mondayOf(first);
  const last = new Date(year, month + 1, 0);
  const weeks: Date[][] = [];
  for (let d = start; d <= last || weeks.length === 0 || d.getDay() !== 1; d = addDays(d, 1)) {
    if (d.getDay() === 1) weeks.push([]);
    weeks[weeks.length - 1].push(d);
    if (weeks.length > 6) break;
  }
  const name = first.toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
  const matters: { d: Date; it: Item }[] = [];
  for (let d = first; d <= last; d = addDays(d, 1)) {
    if (d < today) continue;
    for (const it of byDay.get(dayKey(d)) ?? []) if (it.kind !== 'Event' && it.kind !== 'Proposed') matters.push({ d, it });
  }
  const id = `m-${year}-${month}`;
  return (
    <section className="mt-7" aria-labelledby={id}>
      <h2 id={id} className="mb-2 text-lg font-semibold">{name}</h2>
      <div className="overflow-x-auto" tabIndex={0} role="region" aria-label={`${name} grid`}>
        <table className="w-full table-fixed border-collapse text-xs">
          <caption className="sr-only">{name}</caption>
          <thead><tr>{WEEKDAYS.map(w => <th key={w} scope="col" className="px-1 py-1 text-left font-semibold text-muted-foreground"><abbr title={w}>{w.slice(0, 3)}</abbr></th>)}</tr></thead>
          <tbody>
            {weeks.map(w => (
              <tr key={dayKey(w[0])}>
                {w.map(d => {
                  const out = d.getMonth() !== month;
                  const items = out ? [] : (byDay.get(dayKey(d)) ?? []);
                  const imp = items.some(i => i.important);
                  const isToday = dayKey(d) === dayKey(today);
                  return (
                    <td key={dayKey(d)} className={`h-20 overflow-hidden border border-border p-1 align-top ${out || d < today ? 'bg-muted text-muted-foreground' : 'bg-card'} ${imp ? 'bg-selected' : ''} ${isToday ? 'outline outline-2 -outline-offset-2 outline-primary' : ''}`}>
                      <span className="font-semibold tabular-nums"><span aria-hidden="true">{d.getDate()}</span><span className="sr-only">{d.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })}</span></span>
                      {items.slice(0, 2).map(i => (
                        <span key={i.key} className={`block truncate leading-snug ${i.important ? 'font-semibold' : ''} ${i.declined ? 'line-through' : ''}`} title={i.title}>{i.important ? '★ ' : ''}{i.title}</span>
                      ))}
                      {items.length > 2 && <span className="block text-muted-foreground">+{items.length - 2} more</span>}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <h3 className="mb-1 mt-3 text-sm font-semibold">What matters in {first.toLocaleDateString(undefined, { month: 'long' })}</h3>
      {matters.length === 0 ? <p className="text-sm text-muted-foreground">No due dates, important dates or all-day events from today on.</p> : (
        <ul className="space-y-1 border border-border bg-card px-4 py-2 text-sm">
          {matters.map(({ d, it }) => (
            <li key={it.key} className="flex flex-wrap gap-x-3">
              <span className="w-20 shrink-0 tabular-nums text-muted-foreground">{d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric' })}</span>
              <span className="w-20 shrink-0 text-xs uppercase tracking-wide text-muted-foreground">{it.kind}</span>
              <span className={`min-w-0 flex-1 ${it.important ? 'font-semibold' : ''}`}>{it.important ? '★ ' : ''}{it.title}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
};
