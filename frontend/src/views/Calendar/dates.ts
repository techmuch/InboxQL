/**
 * Dates for the calendar views, in the browser's own zone.
 *
 * Weeks start on Monday, as in the query language's `| count by week` and in
 * most of the calendars people use outside the United States; a setting can
 * change that later without these changing shape.
 */

export const DAY_MS = 24 * 60 * 60 * 1000;

export const startOfDay = (d: Date): Date => new Date(d.getFullYear(), d.getMonth(), d.getDate());

export const addDays = (d: Date, n: number): Date => {
  const out = new Date(d);
  out.setDate(out.getDate() + n);
  return out;
};

export const addMonths = (d: Date, n: number): Date => new Date(d.getFullYear(), d.getMonth() + n, 1);

export const startOfWeek = (d: Date): Date => {
  const day = startOfDay(d);
  const dow = (day.getDay() + 6) % 7; // Monday = 0
  return addDays(day, -dow);
};

export const startOfMonth = (d: Date): Date => new Date(d.getFullYear(), d.getMonth(), 1);

export const sameDay = (a: Date, b: Date): boolean =>
  a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();

/** The six weeks a month view shows: from the Monday on or before the 1st. */
export const monthGrid = (d: Date): Date[] => {
  const first = startOfWeek(startOfMonth(d));
  return Array.from({ length: 42 }, (_, i) => addDays(first, i));
};

export const minutesIntoDay = (d: Date): number => d.getHours() * 60 + d.getMinutes();

export const fmtTime = (d: Date): string =>
  d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });

export const fmtDay = (d: Date): string =>
  d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' });

export const fmtRange = (start: Date, end: Date, allDay: boolean): string => {
  if (allDay) {
    const last = addDays(end, -1);
    return sameDay(start, last) ? fmtDay(start) : `${fmtDay(start)} – ${fmtDay(last)}`;
  }
  if (sameDay(start, end)) return `${fmtDay(start)}, ${fmtTime(start)} – ${fmtTime(end)}`;
  return `${fmtDay(start)} ${fmtTime(start)} – ${fmtDay(end)} ${fmtTime(end)}`;
};

/** A value for <input type="datetime-local">, in local time. */
export const toLocalInput = (d: Date): string => {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
};

export const toDateInput = (d: Date): string => toLocalInput(d).slice(0, 10);

/** Snap to the nearest quarter hour, the unit drag and click work in. */
export const snap15 = (minutes: number): number => Math.round(minutes / 15) * 15;

/**
 * Lay out overlapping events side by side: each gets a column index and the
 * number of columns in its cluster, the way every calendar shares a slot.
 */
export function layoutColumns<T extends { start: Date; end: Date }>(items: T[]): (T & { col: number; cols: number })[] {
  const sorted = [...items].sort((a, b) => a.start.getTime() - b.start.getTime() || b.end.getTime() - a.end.getTime());
  const out: (T & { col: number; cols: number })[] = [];
  let cluster: (T & { col: number; cols: number })[] = [];
  let clusterEnd = 0;
  const flush = () => {
    const cols = Math.max(1, ...cluster.map(c => c.col + 1));
    cluster.forEach(c => { c.cols = cols; });
    out.push(...cluster);
    cluster = [];
  };
  for (const it of sorted) {
    if (cluster.length && it.start.getTime() >= clusterEnd) flush();
    const taken = new Set(cluster.filter(c => c.end.getTime() > it.start.getTime()).map(c => c.col));
    let col = 0;
    while (taken.has(col)) col++;
    cluster.push({ ...it, col, cols: 1 });
    clusterEnd = Math.max(clusterEnd, it.end.getTime());
  }
  flush();
  return out;
}
