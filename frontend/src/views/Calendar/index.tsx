import { useCallback, useEffect, useMemo, useState } from 'react';
import { CalendarDays, ChevronLeft, ChevronRight, Eye, EyeOff, Loader2, Plus, RefreshCw, Settings2 } from 'lucide-react';
import {
  approveChange, createEvent, deleteEvent, listCalendars, listChanges, listOccurrences, rejectChange, syncAccount,
  updateEvent, patchCalendar,
  type CalendarAccount, type CalendarInfo, type Change, type EventDetail, type EventInput, type Occurrence,
} from './api';
import { TimeGrid } from './TimeGrid';
import { Agenda, MonthGrid } from './MonthAgenda';
import { Editor, type EditorSeed } from './Editor';
import { EventPanel } from './EventPanel';
import { addDays, addMonths, monthGrid, sameDay, startOfDay, startOfMonth, startOfWeek } from './dates';
import { useCalendarNav, type CalendarView } from '../../lib/calendarNav';
import { requestSettingsSection } from '../../lib/settingsNav';
import { openTool } from '../../lib/tabs';
import { Help } from '../Help';

/**
 * The Calendar tab.
 *
 * Every calendar in one place — your own, the invitations that came by mail,
 * and Google and Exchange once connected — on the views people expect: day,
 * week, month and agenda. The same events are a query too: `in:events`.
 */

function rangeFor(view: CalendarView, anchor: Date): [Date, Date] {
  switch (view) {
    case 'day':
      return [startOfDay(anchor), addDays(startOfDay(anchor), 1)];
    case 'week':
      return [startOfWeek(anchor), addDays(startOfWeek(anchor), 7)];
    case 'month': {
      const g = monthGrid(anchor);
      return [g[0], addDays(g[41], 1)];
    }
    default:
      return [startOfDay(anchor), addDays(startOfDay(anchor), 28)];
  }
}

function title(view: CalendarView, anchor: Date): string {
  if (view === 'day') return anchor.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
  if (view === 'week') {
    const s = startOfWeek(anchor);
    const e = addDays(s, 6);
    const sameMonth = s.getMonth() === e.getMonth();
    return `${s.toLocaleDateString(undefined, { day: 'numeric', month: sameMonth ? undefined : 'short' })} – ${e.toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' })}`;
  }
  if (view === 'month') return anchor.toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
  return `From ${anchor.toLocaleDateString(undefined, { day: 'numeric', month: 'long' })}`;
}

const MiniMonth = ({ anchor, onPick }: { anchor: Date; onPick: (d: Date) => void }) => {
  const [month, setMonth] = useState(startOfMonth(anchor));
  useEffect(() => setMonth(startOfMonth(anchor)), [anchor]);
  const today = new Date();
  return (
    <div className="px-3 pb-3 text-xs">
      <div className="mb-1 flex items-center justify-between">
        <button onClick={() => setMonth(addMonths(month, -1))} aria-label="Previous month" className="p-0.5 hover:bg-accent"><ChevronLeft className="h-3.5 w-3.5" /></button>
        <span className="font-medium">{month.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}</span>
        <button onClick={() => setMonth(addMonths(month, 1))} aria-label="Next month" className="p-0.5 hover:bg-accent"><ChevronRight className="h-3.5 w-3.5" /></button>
      </div>
      <div className="grid grid-cols-7 text-center">
        {['M', 'T', 'W', 'T', 'F', 'S', 'S'].map((d, i) => <span key={i} className="py-0.5 text-[10px] text-muted-foreground">{d}</span>)}
        {monthGrid(month).map(d => (
          <button
            key={d.toISOString()}
            onClick={() => onPick(d)}
            className={`py-0.5 tabular-nums ${sameDay(d, today) ? 'bg-primary text-primary-foreground' : sameDay(d, anchor) ? 'bg-accent font-semibold' : d.getMonth() === month.getMonth() ? '' : 'text-muted-foreground/50'} hover:bg-accent`}
          >
            {d.getDate()}
          </button>
        ))}
      </div>
    </div>
  );
};

export const Calendar = () => {
  const { view, setView, anchor: anchorISO, setAnchor, selected, select } = useCalendarNav();
  const anchor = useMemo(() => new Date(anchorISO), [anchorISO]);
  const [calendars, setCalendars] = useState<CalendarInfo[]>([]);
  const [accounts, setAccounts] = useState<CalendarAccount[]>([]);
  const [occurrences, setOccurrences] = useState<Occurrence[]>([]);
  const [changes, setChanges] = useState<Change[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [editor, setEditor] = useState<EditorSeed | null>(null);
  const [selectedOcc, setSelectedOcc] = useState<Occurrence | null>(null);
  const [syncing, setSyncing] = useState(false);

  const [from, to] = useMemo(() => rangeFor(view, anchor), [view, anchor]);

  const loadCalendars = useCallback(() => {
    listCalendars().then(r => { setCalendars(r.calendars); setAccounts(r.accounts ?? []); }).catch(e => setError(e.message));
    listChanges().then(r => setChanges(r.changes.filter(c => c.state !== 'pending'))).catch(() => {});
  }, []);

  const loadEvents = useCallback(() => {
    setLoading(true);
    listOccurrences(from, to)
      .then(r => { setOccurrences(r.occurrences); setError(null); })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [from, to]);

  useEffect(loadCalendars, [loadCalendars]);
  useEffect(loadEvents, [loadEvents]);

  // Opened at an event from elsewhere — a message's invitation card.
  //
  // After a reload the open card follows its occurrence: the same one if it
  // is still there, another of the same event if it moved, and the card
  // closes when the event is gone — a moved occurrence becomes an event of
  // its own, and a card still showing the old time would be wrong.
  useEffect(() => {
    if (!selected) { setSelectedOcc(null); return; }
    setSelectedOcc(cur => {
      const same = cur && occurrences.find(o => o.eventId === cur.eventId && o.start === cur.start);
      if (same) return same;
      const hit = occurrences.find(o => o.eventId === selected);
      if (hit) return hit;
      if (cur && !loading) select(null);
      return cur && loading ? cur : null;
    });
  }, [selected, occurrences]); // eslint-disable-line react-hooks/exhaustive-deps

  const refresh = () => { loadCalendars(); loadEvents(); };

  const calMap = useMemo(() => new Map(calendars.map(c => [c.id, { color: c.color, writable: c.writable }])), [calendars]);
  const colourOf = useCallback((o: Occurrence) => o.color || calMap.get(o.calendarId)?.color || '#2563eb', [calMap]);
  const defaultCalendar = calendars.find(c => c.writable && c.primary) ?? calendars.find(c => c.writable);

  const step = (n: number) => {
    if (view === 'day') setAnchor(addDays(anchor, n));
    else if (view === 'week') setAnchor(addDays(anchor, 7 * n));
    else if (view === 'month') setAnchor(addMonths(anchor, n));
    else setAnchor(addDays(anchor, 28 * n));
  };

  const report = (r: { state: string; note?: string }) => {
    setNotice(r.note ?? (r.state === 'queued' ? 'Saved. It goes to the provider on the next sync.' : null));
  };

  const openSelect = (o: Occurrence) => { select(o.eventId); setSelectedOcc(o); };

  const onCreate = (start: Date, end: Date, allDay: boolean) => {
    if (!defaultCalendar) { setError('There is no calendar you can add to.'); return; }
    setEditor({ calendarId: defaultCalendar.id, start, end, allDay });
  };

  // File → New event, from anywhere. The request stays pending until the
  // calendar has somewhere to put the event, then is cleared.
  const newRequest = useCalendarNav(s => s.newRequest);
  useEffect(() => {
    if (!newRequest || !defaultCalendar) return;
    useCalendarNav.setState({ newRequest: 0 });
    const s = new Date(Math.ceil(Date.now() / 3600_000) * 3600_000);
    onCreate(s, new Date(s.getTime() + 3600_000), false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [newRequest, defaultCalendar]);

  const onSave = async (calendarId: string, input: EventInput, scope: string) => {
    if (editor?.event) {
      const ev = editor.event;
      let body = input;
      if (scope === 'all' && editor.occurrence && ((ev.recurrence?.length ?? 0) > 0)) {
        // Edited from one occurrence, applied to the series: move the
        // series by however much this occurrence moved, rather than moving
        // the whole series to this occurrence's date.
        const shiftStart = new Date(input.start).getTime() - new Date(editor.occurrence).getTime();
        const length = new Date(input.end).getTime() - new Date(input.start).getTime();
        const s = new Date(new Date(ev.start).getTime() + shiftStart);
        body = { ...input, start: s.toISOString(), end: new Date(s.getTime() + length).toISOString() };
      }
      report(await updateEvent(ev.id, body, scope, scope === 'all' ? undefined : editor.occurrence));
    } else {
      report(await createEvent(calendarId, input));
    }
    setEditor(null);
    refresh();
  };

  // Dragging a whole series asks the same question the editor does; dragging
  // a single event just moves it.
  const [pendingMove, setPendingMove] = useState<{ o: Occurrence; start: Date; end: Date } | null>(null);
  const applyMove = async (o: Occurrence, start: Date, end: Date, scope: string) => {
    try {
      const d = await import('./api').then(m => m.getEvent(o.eventId));
      const ev = d.event;
      let s = start;
      let e = end;
      if (scope === 'all' && o.recurring) {
        const shift = start.getTime() - new Date(o.start).getTime();
        s = new Date(new Date(ev.start).getTime() + shift);
        e = new Date(s.getTime() + (end.getTime() - start.getTime()));
      }
      report(await updateEvent(ev.id, {
        summary: ev.summary, description: ev.description, location: ev.location, conferenceUrl: ev.conferenceUrl,
        start: s.toISOString(), end: e.toISOString(), allDay: ev.allDay, tz: ev.tz, recurrence: ev.recurrence,
        showAs: ev.showAs, private: ev.private, attendees: ev.attendees, alarms: ev.alarms,
      }, scope, scope === 'all' ? undefined : o.start));
    } catch (err: any) {
      setError(err.message);
    }
    setPendingMove(null);
    refresh();
  };
  const onMove = (o: Occurrence, start: Date, end: Date) => {
    if (o.recurring) setPendingMove({ o, start, end });
    else void applyMove(o, start, end, 'all');
  };

  const onEdit = (d: EventDetail) => {
    const o = selectedOcc;
    setEditor({
      calendarId: d.event.calendarId, event: d.event,
      start: new Date(o?.start ?? d.event.start), end: new Date(o?.end ?? d.event.end),
      allDay: d.event.allDay, occurrence: o?.recurring ? o.start : undefined,
    });
  };

  const onDelete = async (d: EventDetail, scope: string) => {
    try {
      report(await deleteEvent(d.event.id, scope, scope === 'this' ? selectedOcc?.start : undefined));
      select(null);
      refresh();
    } catch (e: any) { setError(e.message); }
  };

  const syncAll = async () => {
    setSyncing(true);
    try {
      for (const a of accounts) await syncAccount(a.id).catch(e => setError(`${a.name}: ${e.message}`));
    } finally { setSyncing(false); refresh(); }
  };

  // Keyboard: t today, ← → previous/next, d w m a for the views, n for new.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (t.closest('input, textarea, select, [contenteditable="true"]') || e.metaKey || e.ctrlKey || e.altKey) return;
      if (!document.querySelector('[data-calendar-root]')?.contains(document.activeElement) && document.activeElement !== document.body) return;
      const map: Record<string, () => void> = {
        t: () => setAnchor(new Date()), ArrowLeft: () => step(-1), ArrowRight: () => step(1),
        d: () => setView('day'), w: () => setView('week'), m: () => setView('month'), a: () => setView('agenda'),
        n: () => { const s = new Date(); s.setMinutes(0, 0, 0); s.setHours(s.getHours() + 1); onCreate(s, new Date(s.getTime() + 3600_000), false); },
        Escape: () => select(null),
      };
      if (map[e.key] && !editor) { e.preventDefault(); map[e.key](); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  const days = view === 'day' ? [startOfDay(anchor)] : Array.from({ length: 7 }, (_, i) => addDays(startOfWeek(anchor), i));
  const attention = changes.filter(c => c.state === 'proposed' || c.state === 'conflict' || c.state === 'failed');

  return (
    <div data-calendar-root tabIndex={-1} className="flex h-full bg-background text-foreground overflow-hidden outline-none">
      <aside className="w-60 shrink-0 overflow-auto border-r border-border bg-card/30">
        <div className="p-3">
          <button onClick={() => onCreate(new Date(Math.ceil(Date.now() / 3600_000) * 3600_000), new Date(Math.ceil(Date.now() / 3600_000) * 3600_000 + 3600_000), false)}
            className="flex w-full items-center justify-center gap-2 bg-primary px-3 py-2 text-sm font-semibold text-primary-foreground hover:opacity-90">
            <Plus className="h-4 w-4" /> New event
          </button>
        </div>
        <MiniMonth anchor={anchor} onPick={d => setAnchor(d)} />

        <div className="border-t border-border px-3 py-2">
          <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Calendars</p>
          <ul className="space-y-0.5 text-xs">
            {calendars.map(c => (
              <li key={c.id} className="group flex items-center gap-2">
                <label className="flex flex-1 cursor-pointer items-center gap-2 py-0.5">
                  <input type="checkbox" className="sr-only" checked={!c.hidden}
                    onChange={() => patchCalendar(c.id, { hidden: !c.hidden }).then(refresh)} />
                  <span className="h-3 w-3 shrink-0 border" style={{ background: c.hidden ? 'transparent' : c.color || '#2563eb', borderColor: c.color || '#2563eb' }} />
                  <span className={`truncate ${c.hidden ? 'text-muted-foreground' : ''}`}>{c.name}</span>
                </label>
                <span className="text-muted-foreground opacity-0 group-hover:opacity-100">
                  {c.hidden ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                </span>
              </li>
            ))}
          </ul>
          <button onClick={() => { requestSettingsSection('calendars'); openTool('settings', 'Settings'); }}
            className="mt-2 flex items-center gap-1.5 text-[11px] text-primary hover:underline">
            <Settings2 className="h-3 w-3" /> {accounts.length ? 'Calendar accounts' : 'Connect Google or Exchange'}
          </button>
          {accounts.some(a => a.status === 'error') && (
            <p className="mt-1 text-[11px] text-destructive">{accounts.filter(a => a.status === 'error').map(a => `${a.name}: ${a.lastError}`).join(' · ')}</p>
          )}
        </div>

        {attention.length > 0 && (
          <div className="border-t border-border px-3 py-2">
            <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-amber-600">Waiting for you</p>
            <ul className="space-y-2 text-xs">
              {attention.map(c => (
                <li key={c.id}>
                  <p className="truncate" title={c.payload.summary}>{c.payload.summary ?? c.op}</p>
                  <p className="text-[10px] text-muted-foreground">
                    {c.state === 'proposed' ? `proposed by ${c.origin}` : c.state === 'conflict' ? 'changed elsewhere first' : `failed: ${c.error ?? ''}`}
                  </p>
                  {c.state === 'proposed' && (
                    <div className="mt-1 flex gap-1">
                      <button onClick={() => approveChange(c.id).then(refresh).catch(e => setError(e.message))} className="bg-primary px-2 py-0.5 text-[11px] font-semibold text-primary-foreground">Approve</button>
                      <button onClick={() => rejectChange(c.id).then(refresh).catch(e => setError(e.message))} className="border border-border px-2 py-0.5 text-[11px] hover:bg-accent">Reject</button>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
          <button onClick={() => setAnchor(new Date())} className="border border-border px-3 py-1 text-xs hover:bg-accent">Today</button>
          <button onClick={() => step(-1)} aria-label="Previous" className="p-1 hover:bg-accent"><ChevronLeft className="h-4 w-4" /></button>
          <button onClick={() => step(1)} aria-label="Next" className="p-1 hover:bg-accent"><ChevronRight className="h-4 w-4" /></button>
          <h2 className="ml-1 flex items-center gap-2 text-base font-semibold">
            <CalendarDays className="h-4 w-4 text-muted-foreground" /> {title(view, anchor)}
          </h2>
          <Help topic="calendar" />
          {loading && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />}
          <div className="ml-auto flex items-center gap-2">
            {accounts.length > 0 && (
              <button onClick={syncAll} disabled={syncing} className="p-1 text-muted-foreground hover:text-foreground" title="Sync calendars now">
                <RefreshCw className={`h-4 w-4 ${syncing ? 'animate-spin' : ''}`} />
              </button>
            )}
            <div className="flex border border-border text-xs" role="tablist" aria-label="View">
              {(['day', 'week', 'month', 'agenda'] as const).map(v => (
                <button key={v} role="tab" aria-selected={view === v} onClick={() => setView(v)}
                  className={`px-3 py-1 capitalize ${view === v ? 'bg-primary/10 font-semibold text-primary' : 'hover:bg-accent'}`}>{v}</button>
              ))}
            </div>
          </div>
        </div>

        {(error || notice) && (
          <div className={`flex items-center gap-2 px-3 py-1.5 text-xs ${error ? 'bg-destructive/10 text-destructive' : 'bg-primary/5'}`}>
            <span className="flex-1">{error ?? notice}</span>
            <button onClick={() => { setError(null); setNotice(null); }} className="text-muted-foreground">Dismiss</button>
          </div>
        )}

        {pendingMove && (
          <div className="flex flex-wrap items-center gap-2 border-b border-border bg-card px-3 py-2 text-xs">
            <span className="font-medium">Move “{pendingMove.o.summary}” — this repeats. Move…</span>
            <button onClick={() => applyMove(pendingMove.o, pendingMove.start, pendingMove.end, 'this')} className="border border-border px-2 py-1 hover:bg-accent">Only this one</button>
            <button onClick={() => applyMove(pendingMove.o, pendingMove.start, pendingMove.end, 'following')} className="border border-border px-2 py-1 hover:bg-accent">This and the ones after</button>
            <button onClick={() => applyMove(pendingMove.o, pendingMove.start, pendingMove.end, 'all')} className="border border-border px-2 py-1 hover:bg-accent">All of them</button>
            <button onClick={() => { setPendingMove(null); loadEvents(); }} className="px-2 py-1 text-muted-foreground">Cancel</button>
          </div>
        )}

        <div className="flex min-h-0 flex-1">
          <div className="min-w-0 flex-1">
            {(view === 'day' || view === 'week') && (
              <TimeGrid days={days} occurrences={occurrences} calendars={calMap} selected={selected}
                onSelect={openSelect} onCreate={onCreate} onMove={onMove} />
            )}
            {view === 'month' && (
              <MonthGrid anchor={anchor} occurrences={occurrences} colourOf={colourOf} selected={selected} onSelect={openSelect}
                onCreate={d => onCreate(new Date(d.getTime() + 9 * 3600_000), new Date(d.getTime() + 10 * 3600_000), false)}
                onOpenDay={d => { setAnchor(d); setView('day'); }} />
            )}
            {view === 'agenda' && <Agenda occurrences={occurrences} colourOf={colourOf} selected={selected} onSelect={openSelect} />}
          </div>
          {selectedOcc && (
            <EventPanel key={selectedOcc.eventId + selectedOcc.start} occurrence={selectedOcc}
              onClose={() => select(null)} onEdit={onEdit} onDelete={onDelete} onChanged={refresh} />
          )}
        </div>
      </div>

      {editor && <Editor seed={editor} calendars={calendars} onSave={onSave} onClose={() => setEditor(null)} />}
    </div>
  );
};

