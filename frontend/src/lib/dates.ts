/**
 * Dates in lists: only the detail that differs from now.
 *
 *   today            9:08 PM
 *   earlier this year  Oct 24
 *   another year       Mar 1, 2025
 *
 * As Gmail and Apple Mail show them. "Today" is the calendar day here, not
 * the last 24 hours — mail from 11 pm yesterday says Oct 29 — and a year is
 * the calendar year, so on January 2 a message from December 30 carries its
 * year. Wording and order follow the reader's locale.
 *
 * A date with no time of day (a due date) never shows a time: today is its
 * day and month.
 */
export function shortDate(input: string | number | Date | undefined | null, opts: { dateOnly?: boolean; now?: Date } = {}): string {
  const d = toDate(input);
  if (!d) return '';
  const now = opts.now ?? new Date();
  if (!opts.dateOnly && sameDay(d, now)) {
    return d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
  }
  if (d.getFullYear() === now.getFullYear()) {
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  }
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

/** Everything, for a tooltip or a screen reader that wants it: Thursday, October 30, 2026, 9:08 PM. */
export function fullDate(input: string | number | Date | undefined | null, opts: { dateOnly?: boolean } = {}): string {
  const d = toDate(input);
  if (!d) return '';
  return d.toLocaleString(undefined, {
    weekday: 'long', year: 'numeric', month: 'long', day: 'numeric',
    ...(opts.dateOnly ? {} : { hour: 'numeric', minute: '2-digit' }),
  });
}

function toDate(input: string | number | Date | undefined | null): Date | null {
  if (input === undefined || input === null || input === '') return null;
  const d = input instanceof Date ? input : new Date(input);
  return Number.isNaN(d.getTime()) ? null : d;
}

const sameDay = (a: Date, b: Date) =>
  a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
