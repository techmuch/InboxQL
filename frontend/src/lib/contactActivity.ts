/**
 * How often one contact writes: the year a contact card charts, the query that
 * counts it, and the line read above the chart.
 *
 * The year is the last twelve months, not the calendar year — the question is
 * "how often do they write", and in January a calendar year has no answer.
 */
import type { DayCount } from '../views/ActivityCalendar';

const iso = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

/** The 365 days ending today, as YYYY-MM-DD in local time. */
export function activityWindow(today = new Date()): { from: string; to: string } {
  const start = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 364);
  return { from: iso(start), to: iso(today) };
}

/** Their mail per day since `from`. `quoted` is the card's `=address`. */
export const activityQuery = (quoted: string, from: string) => `from:${quoted} after:${from} | count by day`;

/** Their mail on one day, for a click on the chart. */
export const dayQuery = (quoted: string, day: string) => `from:${quoted} on:${day}`;

/**
 * "84 messages in the last year · about 7 a month · busiest in March 2026".
 *
 * Said in words because a grid of mostly empty days, which is what someone who
 * writes twice a month gets, shows "rarely" and little else.
 */
export function activitySummary(days: DayCount[]): string {
  const total = days.reduce((n, d) => n + d.value, 0);
  if (total === 0) return 'Nothing from them in the last year.';

  const parts = [`${total.toLocaleString()} ${total === 1 ? 'message' : 'messages'} in the last year`];
  const perMonth = total / 12;
  parts.push(perMonth < 1 ? 'less than one a month' : `about ${Math.round(perMonth).toLocaleString()} a month`);

  const byMonth = new Map<string, number>();
  for (const d of days) byMonth.set(d.day.slice(0, 7), (byMonth.get(d.day.slice(0, 7)) ?? 0) + d.value);
  // A busiest month only means something when there is more than one month.
  if (byMonth.size > 1) {
    // Ties go to the more recent month.
    const [month] = [...byMonth].sort((a, b) => b[1] - a[1] || b[0].localeCompare(a[0]))[0];
    const [y, m] = month.split('-').map(Number);
    parts.push(`busiest in ${new Date(y, m - 1, 1).toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}`);
  }
  return parts.join(' · ');
}
