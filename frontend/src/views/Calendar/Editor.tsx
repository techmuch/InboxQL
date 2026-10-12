import { useMemo, useState } from 'react';
import { Loader2, X } from 'lucide-react';
import type { CalendarInfo, EventInput, StoredEvent } from './api';
import { toDateInput, toLocalInput } from './dates';

/**
 * Creating and editing an event.
 *
 * A change to a recurring event asks what it is for — this occurrence, this
 * and the ones after it, or all of them — before anything is saved, because
 * the three are different edits and guessing wrong rewrites someone's series.
 */

const REPEATS: { label: string; rule: (start: Date) => string }[] = [
  { label: 'Does not repeat', rule: () => '' },
  { label: 'Every day', rule: () => 'FREQ=DAILY' },
  { label: 'Every weekday', rule: () => 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR' },
  { label: 'Every week on this day', rule: s => `FREQ=WEEKLY;BYDAY=${['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA'][s.getDay()]}` },
  { label: 'Every two weeks', rule: s => `FREQ=WEEKLY;INTERVAL=2;BYDAY=${['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA'][s.getDay()]}` },
  { label: 'Every month on this date', rule: s => `FREQ=MONTHLY;BYMONTHDAY=${s.getDate()}` },
  { label: 'Every year', rule: () => 'FREQ=YEARLY' },
];

const REMINDERS = [0, 5, 10, 15, 30, 60, 120, 1440];

export interface EditorSeed {
  calendarId: string;
  start: Date;
  end: Date;
  allDay: boolean;
  event?: StoredEvent;
  /** The occurrence being edited, for a recurring event. */
  occurrence?: string;
}

export const Editor = ({ seed, calendars, onSave, onClose }: {
  seed: EditorSeed;
  calendars: CalendarInfo[];
  onSave: (calendarId: string, input: EventInput, scope: string) => Promise<void>;
  onClose: () => void;
}) => {
  const ev = seed.event;
  const recurring = !!ev && ((ev.recurrence?.length ?? 0) > 0 || !!ev.recurrenceId);
  const existingRule = (ev?.recurrence ?? []).find(l => l.toUpperCase().startsWith('RRULE:'))?.slice(6) ?? '';
  const [calendarId, setCalendarId] = useState(seed.calendarId);
  const [summary, setSummary] = useState(ev?.summary ?? '');
  const [allDay, setAllDay] = useState(seed.allDay);
  const [start, setStart] = useState(seed.allDay ? toDateInput(seed.start) : toLocalInput(seed.start));
  const [end, setEnd] = useState(seed.allDay ? toDateInput(new Date(seed.end.getTime() - 1)) : toLocalInput(seed.end));
  const [location, setLocation] = useState(ev?.location ?? '');
  const [conference, setConference] = useState(ev?.conferenceUrl ?? '');
  const [description, setDescription] = useState(ev?.description ?? '');
  const [attendees, setAttendees] = useState((ev?.attendees ?? []).map(a => a.address).join(', '));
  const [rule, setRule] = useState(existingRule);
  const [showAs, setShowAs] = useState(ev?.showAs ?? 'busy');
  const [priv, setPriv] = useState(ev?.private ?? false);
  const [alarm, setAlarm] = useState<number | ''>(ev ? (ev.alarms[0] ?? '') : 10);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const writable = useMemo(() => calendars.filter(c => c.writable), [calendars]);
  const startDate = new Date(allDay ? `${start}T00:00` : start);
  const presetLabel = REPEATS.find(r => r.rule(startDate) === rule)?.label ?? (rule ? 'Custom' : 'Does not repeat');

  const input = (): EventInput => {
    const s = allDay ? new Date(`${start}T00:00`) : new Date(start);
    let e = allDay ? new Date(`${end}T00:00`) : new Date(end);
    if (allDay) e = new Date(e.getFullYear(), e.getMonth(), e.getDate() + 1);
    const known = new Map((ev?.attendees ?? []).map(a => [a.address, a]));
    return {
      summary: summary.trim(),
      description, location, conferenceUrl: conference.trim(),
      // All-day events are dates; send them as UTC midnights of those dates.
      start: allDay ? new Date(Date.UTC(s.getFullYear(), s.getMonth(), s.getDate())).toISOString() : s.toISOString(),
      end: allDay ? new Date(Date.UTC(e.getFullYear(), e.getMonth(), e.getDate())).toISOString() : e.toISOString(),
      allDay,
      tz: allDay ? '' : Intl.DateTimeFormat().resolvedOptions().timeZone,
      recurrence: rule ? [`RRULE:${rule}`, ...(ev?.recurrence ?? []).filter(l => !l.toUpperCase().startsWith('RRULE:'))] : [],
      showAs, private: priv,
      attendees: attendees.split(/[,\s;]+/).map(a => a.trim().toLowerCase()).filter(a => a.includes('@'))
        .map(a => known.get(a) ?? { address: a, role: 'required', status: 'needs-action' }),
      alarms: alarm === '' ? [] : [alarm],
    };
  };

  const save = async (scope: string) => {
    setError(null);
    const i = input();
    if (!i.summary) { setError('An event needs a title.'); return; }
    if (new Date(i.end) < new Date(i.start)) { setError('It ends before it starts.'); return; }
    setBusy(true);
    try {
      await onSave(calendarId, i, scope);
    } catch (e: any) {
      setError(e?.message ?? 'Could not save.');
    } finally {
      setBusy(false);
      setAsking(false);
    }
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (recurring) setAsking(true);
    else void save('all');
  };

  const field = 'w-full bg-background border border-border px-2 py-1.5 text-sm';
  const label = 'block text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-1';

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/30 pt-[8vh]" onMouseDown={onClose}>
      <form
        onSubmit={submit}
        onMouseDown={e => e.stopPropagation()}
        className="w-[34rem] max-w-[95vw] max-h-[84vh] overflow-auto border border-border bg-card p-5 shadow-xl"
        aria-label={ev ? 'Edit event' : 'New event'}
      >
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{ev ? 'Edit event' : 'New event'}</h2>
          <button type="button" onClick={onClose} aria-label="Close" className="p-1 text-muted-foreground hover:text-foreground"><X className="h-4 w-4" /></button>
        </div>

        <input autoFocus className={`${field} text-base font-medium mb-3`} placeholder="Title" value={summary} onChange={e => setSummary(e.target.value)} />

        <div className="mb-3 grid grid-cols-2 gap-3">
          <div>
            <label className={label} htmlFor="ev-start">Starts</label>
            <input id="ev-start" className={field} type={allDay ? 'date' : 'datetime-local'} value={start} onChange={e => {
              const v = e.target.value;
              // Keep the length when the start moves.
              if (!allDay) {
                const len = new Date(end).getTime() - new Date(start).getTime();
                const ns = new Date(v);
                if (!isNaN(ns.getTime()) && len > 0) setEnd(toLocalInput(new Date(ns.getTime() + len)));
              }
              setStart(v);
            }} />
          </div>
          <div>
            <label className={label} htmlFor="ev-end">Ends</label>
            <input id="ev-end" className={field} type={allDay ? 'date' : 'datetime-local'} value={end} onChange={e => setEnd(e.target.value)} />
          </div>
        </div>

        <div className="mb-3 flex flex-wrap items-center gap-4 text-sm">
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={allDay} onChange={e => {
              const on = e.target.checked;
              setAllDay(on);
              setStart(on ? start.slice(0, 10) : `${start.slice(0, 10)}T09:00`);
              setEnd(on ? end.slice(0, 10) : `${end.slice(0, 10)}T10:00`);
            }} />
            All day
          </label>
          <label className="flex items-center gap-2">
            <span className="text-muted-foreground">Repeats</span>
            <select className="bg-background border border-border px-1 py-1" value={presetLabel} onChange={e => {
              const p = REPEATS.find(r => r.label === e.target.value);
              if (p) setRule(p.rule(startDate));
            }}>
              {REPEATS.map(r => <option key={r.label}>{r.label}</option>)}
              {presetLabel === 'Custom' && <option>Custom</option>}
            </select>
          </label>
        </div>
        {presetLabel === 'Custom' && (
          <div className="mb-3">
            <label className={label} htmlFor="ev-rule">Rule</label>
            <input id="ev-rule" className={`${field} font-mono text-xs`} value={rule} onChange={e => setRule(e.target.value)} placeholder="FREQ=WEEKLY;BYDAY=MO,WE" />
          </div>
        )}

        <div className="mb-3">
          <label className={label} htmlFor="ev-cal">Calendar</label>
          <select id="ev-cal" className={field} value={calendarId} onChange={e => setCalendarId(e.target.value)} disabled={!!ev}>
            {writable.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
        </div>

        <div className="mb-3">
          <label className={label} htmlFor="ev-people">People</label>
          <input id="ev-people" className={field} placeholder="ada@example.com, bob@example.com" value={attendees} onChange={e => setAttendees(e.target.value)} />
          {attendees.trim() && !ev && (
            <p className="mt-1 text-[11px] text-muted-foreground">
              In a Google or Exchange calendar the provider sends the invitations. In your own calendar nothing is sent to them.
            </p>
          )}
        </div>

        <div className="mb-3 grid grid-cols-2 gap-3">
          <div>
            <label className={label} htmlFor="ev-where">Where</label>
            <input id="ev-where" className={field} value={location} onChange={e => setLocation(e.target.value)} />
          </div>
          <div>
            <label className={label} htmlFor="ev-join">Meeting link</label>
            <input id="ev-join" className={field} placeholder="https://" value={conference} onChange={e => setConference(e.target.value)} />
          </div>
        </div>

        <div className="mb-3">
          <label className={label} htmlFor="ev-desc">Description</label>
          <textarea id="ev-desc" className={`${field} min-h-[5rem]`} value={description} onChange={e => setDescription(e.target.value)} />
        </div>

        <div className="mb-4 flex flex-wrap items-center gap-4 text-sm">
          <label className="flex items-center gap-2">
            <span className="text-muted-foreground">Reminder</span>
            <select className="bg-background border border-border px-1 py-1" value={alarm} onChange={e => setAlarm(e.target.value === '' ? '' : Number(e.target.value))}>
              <option value="">None</option>
              {REMINDERS.map(m => <option key={m} value={m}>{m === 0 ? 'At the start' : m < 60 ? `${m} minutes before` : m === 1440 ? 'A day before' : `${m / 60} hours before`}</option>)}
            </select>
          </label>
          <label className="flex items-center gap-2">
            <span className="text-muted-foreground">Show as</span>
            <select className="bg-background border border-border px-1 py-1" value={showAs} onChange={e => setShowAs(e.target.value)}>
              <option value="busy">Busy</option>
              <option value="free">Free</option>
            </select>
          </label>
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={priv} onChange={e => setPriv(e.target.checked)} />
            Private
          </label>
        </div>

        {error && <p className="mb-3 text-sm text-destructive">{error}</p>}

        {asking ? (
          <div className="border border-border bg-background p-3 text-sm">
            <p className="mb-2 font-medium">This event repeats. Change…</p>
            <div className="flex flex-wrap gap-2">
              {seed.occurrence && <button type="button" onClick={() => save('this')} className="border border-border px-3 py-1.5 hover:bg-accent">Only this one</button>}
              {seed.occurrence && <button type="button" onClick={() => save('following')} className="border border-border px-3 py-1.5 hover:bg-accent">This and the ones after</button>}
              <button type="button" onClick={() => save('all')} className="border border-border px-3 py-1.5 hover:bg-accent">All of them</button>
              <button type="button" onClick={() => setAsking(false)} className="px-3 py-1.5 text-muted-foreground">Cancel</button>
            </div>
          </div>
        ) : (
          <div className="flex justify-end gap-2">
            <button type="button" onClick={onClose} className="border border-border px-4 py-1.5 text-sm hover:bg-accent">Cancel</button>
            <button type="submit" disabled={busy} className="bg-primary px-4 py-1.5 text-sm font-semibold text-primary-foreground hover:opacity-90 disabled:opacity-50">
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save'}
            </button>
          </div>
        )}
      </form>
    </div>
  );
};
