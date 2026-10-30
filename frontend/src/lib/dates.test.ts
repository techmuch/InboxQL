import { describe, it, expect } from 'vitest';
import { fullDate, shortDate } from './dates';

const now = new Date(2026, 9, 30, 15, 0); // Oct 30, 2026, 3 pm local

describe('shortDate', () => {
  it('shows a time for today', () => {
    const s = shortDate(new Date(2026, 9, 30, 9, 8), { now });
    expect(s).toMatch(/9:08/);
    expect(s).not.toMatch(/Oct|2026/);
    // No leading zero on the hour.
    expect(s).not.toMatch(/^0/);
  });

  it('shows day and month, no year, earlier this year — including late yesterday', () => {
    expect(shortDate(new Date(2026, 9, 29, 23, 30), { now })).toMatch(/Oct\s*29|29\s*Oct/);
    const march = shortDate(new Date(2026, 2, 1), { now });
    expect(march).toMatch(/Mar/);
    expect(march).not.toMatch(/2026|:/);
  });

  it('shows the year for another year, even days ago across New Year', () => {
    expect(shortDate(new Date(2025, 2, 1, 10), { now })).toMatch(/2025/);
    const jan2 = new Date(2027, 0, 2, 9);
    expect(shortDate(new Date(2026, 11, 30, 18), { now: jan2 })).toMatch(/Dec.*2026|2026.*Dec/);
  });

  it('never shows a time for a date with no time of day', () => {
    const s = shortDate(new Date(2026, 9, 30, 0, 0), { now, dateOnly: true });
    expect(s).toMatch(/Oct/);
    expect(s).not.toMatch(/:/);
  });

  it('is empty for nothing or nonsense rather than "Invalid Date"', () => {
    expect(shortDate(undefined)).toBe('');
    expect(shortDate('not a date')).toBe('');
  });

  it('gives the whole date for a tooltip', () => {
    const s = fullDate(new Date(2026, 9, 30, 21, 8));
    expect(s).toMatch(/2026/);
    expect(s).toMatch(/9:08/);
    expect(s).toMatch(/Friday|Fri/);
  });
});
