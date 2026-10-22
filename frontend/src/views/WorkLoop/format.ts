import type { RouteStatus, WorkDay } from './api';

export const pct = (f: number, digits = 0) => `${(f * 100).toFixed(digits)}%`;

export const dayLabel = (d: string, opts: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' }) =>
  new Date(`${d}T12:00:00`).toLocaleDateString(undefined, opts);

/** Every day from the first to today, so a quiet day is a gap and not a missing bar. */
export const fillDays = (days: WorkDay[], since: string): WorkDay[] => {
  const by = new Map(days.map(d => [d.day, d]));
  const start = new Date(since).getFullYear() > 1970 ? new Date(since) : days[0] ? new Date(`${days[0].day}T12:00:00`) : new Date();
  const out: WorkDay[] = [];
  const cur = new Date(start.getFullYear(), start.getMonth(), start.getDate(), 12);
  const end = new Date();
  while (cur <= end && out.length < 400) {
    const key = `${cur.getFullYear()}-${String(cur.getMonth() + 1).padStart(2, '0')}-${String(cur.getDate()).padStart(2, '0')}`;
    out.push(by.get(key) ?? { day: key, stored: 0, refused: 0, checked: 0 });
    cur.setDate(cur.getDate() + 1);
  }
  return out.slice(-90);
};

export const STATUS_TEXT: Record<RouteStatus, { label: string; describe: string }> = {
  meeting: { label: 'Meeting target', describe: 'Random checks show it meets its target, even at the low end of the range.' },
  uncertain: { label: 'Not sure yet', describe: 'Measured accuracy is at or above target, but the range still dips below it. More checks will settle it.' },
  missing: { label: 'Missing target', describe: 'Random checks show it is wrong more often than its target allows. The template stops guessing it.' },
  unmeasured: { label: 'Not measured', describe: 'Fewer than ten settled random checks: not enough to act on.' },
};

export const statusDescription = (status: RouteStatus) => STATUS_TEXT[status].describe;
