import { create } from 'zustand';

/**
 * Important dates: what the person marked, counted down on Today and Ahead.
 *
 * The server keeps them (`/api/dates`) and sends their reminders; this is the
 * window's copy and the arithmetic of counting down.
 */
export interface ImportantDate {
  id: string;
  title: string;
  /** YYYY-MM-DD. */
  date: string;
  leadDays: number;
  yearly: boolean;
  refKind?: '' | 'event' | 'ticket' | 'message';
  refId?: string;
  note?: string;
  origin: 'human' | 'agent';
  createdAt?: number;
}

interface DatesState {
  dates: ImportantDate[] | null;
  load: () => Promise<void>;
  save: (d: Partial<ImportantDate>) => Promise<ImportantDate>;
  remove: (id: string) => Promise<void>;
}

export const useImportantDates = create<DatesState>((set, get) => ({
  dates: null,
  load: async () => {
    try {
      const res = await fetch('/api/dates');
      if (!res.ok) { set({ dates: get().dates ?? [] }); return; }
      const body = await res.json();
      set({ dates: body.dates ?? [] });
    } catch {
      set({ dates: get().dates ?? [] });
    }
  },
  save: async (d) => {
    const res = await fetch('/api/dates', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ leadDays: 0, yearly: false, origin: 'human', ...d }),
    });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error ?? 'could not save the date');
    await get().load();
    return body as ImportantDate;
  },
  remove: async (id) => {
    await fetch(`/api/dates/${encodeURIComponent(id)}`, { method: 'DELETE' });
    await get().load();
  },
}));

/** A local day as YYYY-MM-DD. */
export const dayKey = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

/** Midnight, local, of a YYYY-MM-DD. */
export const parseDay = (s: string) => {
  const [y, m, d] = s.slice(0, 10).split('-').map(Number);
  return new Date(y, (m ?? 1) - 1, d ?? 1);
};

const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate());

/** Whole days from today to a day; 0 is today. */
export const daysBetween = (from: Date, to: Date) =>
  Math.round((startOfDay(to).getTime() - startOfDay(from).getTime()) / 86_400_000);

/** The next time a date falls, from today: itself, or this or next year's for a yearly one. Null once a one-off has passed. */
export function nextOccurrence(d: Pick<ImportantDate, 'date' | 'yearly'>, from = new Date()): Date | null {
  const day = parseDay(d.date);
  const today = startOfDay(from);
  if (!d.yearly) return day < today ? null : day;
  let next = new Date(today.getFullYear(), day.getMonth(), day.getDate());
  if (next < today) next = new Date(today.getFullYear() + 1, day.getMonth(), day.getDate());
  return next;
}

export interface Countdown {
  date: ImportantDate;
  on: Date;
  days: number;
  /** When there is something to do, for a date with a lead time. */
  start?: { on: Date; days: number };
  /** Inside its lead time, or the last two weeks: the only time it is amber. */
  soon: boolean;
}

/** The dates the person marked, soonest first, with how far away each is. */
export function countdowns(dates: ImportantDate[], from = new Date()): Countdown[] {
  const out: Countdown[] = [];
  for (const d of dates) {
    if (d.origin !== 'human') continue;
    const on = nextOccurrence(d, from);
    if (!on) continue;
    const days = daysBetween(from, on);
    let start: Countdown['start'];
    if (d.leadDays > 0) {
      const s = new Date(on.getFullYear(), on.getMonth(), on.getDate() - d.leadDays);
      start = { on: s, days: daysBetween(from, s) };
    }
    const soon = days <= 14 || (start != null && start.days <= 0);
    out.push({ date: d, on, days, start, soon });
  }
  return out.sort((a, b) => a.days - b.days);
}

/** "today", "tomorrow", "in 10 days". */
export const inDays = (n: number) => (n === 0 ? 'today' : n === 1 ? 'tomorrow' : n < 0 ? `${-n} days ago` : `in ${n} days`);

/** "Tue, Nov 10", with the year when it is not this one. */
export const shortDay = (d: Date, now = new Date()) =>
  d.toLocaleDateString(undefined, {
    weekday: 'short', month: 'short', day: 'numeric',
    ...(d.getFullYear() !== now.getFullYear() ? { year: 'numeric' } : {}),
  });
