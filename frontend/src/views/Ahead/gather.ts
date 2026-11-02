import { dayKey, nextOccurrence, type ImportantDate } from '../../lib/importantDates';

export interface Occ {
  eventId: string; start: string; end: string; allDay: boolean; summary: string;
  location?: string; myStatus?: string; proposed?: boolean; calendarName?: string;
}
export interface Ticket { id: string; title: string; dueAt?: string; status: string; labels?: string[] }

export interface Item {
  key: string;
  day: string;
  /** Minutes after midnight, or -1 for all day / no time. */
  at: number;
  kind: 'Event' | 'All day' | 'Due' | 'Important' | 'Proposed';
  title: string;
  detail?: string;
  important?: boolean;
  hours?: number;
  declined?: boolean;
  ref?: { kind: 'event' | 'ticket'; id: string };
}

const time = (d: Date) => d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });

/** Everything on each day, from events, cards and important dates. Exported for tests. */
export function gather(events: Occ[], tickets: Ticket[], dates: ImportantDate[], from: Date, to: Date): Map<string, Item[]> {
  const by = new Map<string, Item[]>();
  const add = (it: Item) => { const l = by.get(it.day) ?? []; l.push(it); by.set(it.day, l); };
  const marked = new Set(dates.filter(d => d.origin === 'human' && d.refKind && d.refId).map(d => `${d.refKind}:${d.refId}`));
  for (const e of events) {
    const s = new Date(e.start);
    const en = new Date(e.end);
    const important = marked.has(`event:${e.eventId}`);
    add({
      key: `e:${e.eventId}:${e.start}`, day: dayKey(s), at: e.allDay ? -1 : s.getHours() * 60 + s.getMinutes(),
      kind: e.proposed ? 'Proposed' : e.allDay ? 'All day' : 'Event', title: e.summary,
      detail: [e.allDay ? '' : `${time(s)}–${time(en)}`, e.location].filter(Boolean).join(' · '),
      important, declined: e.myStatus === 'declined',
      // Hours booked: timed, not declined, not proposed — a proposal is not on the calendar until approved.
      hours: !e.allDay && !e.proposed && e.myStatus !== 'declined' ? Math.max(0, (en.getTime() - s.getTime()) / 3_600_000) : 0,
      ref: { kind: 'event', id: e.eventId },
    });
  }
  for (const t of tickets) {
    if (!t.dueAt) continue;
    add({ key: `t:${t.id}`, day: dayKey(new Date(t.dueAt)), at: -1, kind: 'Due', title: t.title,
      detail: t.status === 'proposed' ? 'proposed card' : 'card', important: marked.has(`ticket:${t.id}`), ref: { kind: 'ticket', id: t.id } });
  }
  for (const d of dates) {
    if (d.origin !== 'human' || (d.refKind && d.refId)) continue;
    const on = nextOccurrence(d, from);
    if (!on || on > to) continue;
    add({ key: `d:${d.id}`, day: dayKey(on), at: -1, kind: 'Important', title: d.title, important: true });
    if (d.leadDays > 0) {
      const s = new Date(on.getFullYear(), on.getMonth(), on.getDate() - d.leadDays);
      if (s >= from && s <= to) add({ key: `d:${d.id}:lead`, day: dayKey(s), at: -1, kind: 'Important', title: `Start: ${d.title}`, detail: `${d.leadDays} days before`, important: true });
    }
  }
  for (const l of by.values()) l.sort((a, b) => a.at - b.at || a.title.localeCompare(b.title));
  return by;
}

export const mondayOf = (d: Date) => { const x = new Date(d.getFullYear(), d.getMonth(), d.getDate()); x.setDate(x.getDate() - ((x.getDay() + 6) % 7)); return x; };
export const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);

