import { describe, it, expect } from 'vitest';
import { activityQuery, activitySummary, activityWindow, dayQuery } from './contactActivity';

describe('contactActivity', () => {
  it('covers the 365 days ending today, across New Year', () => {
    expect(activityWindow(new Date(2026, 0, 15))).toEqual({ from: '2025-01-16', to: '2026-01-15' });
    expect(activityWindow(new Date(2024, 11, 31))).toEqual({ from: '2024-01-02', to: '2024-12-31' });
  });

  it('asks for their mail only, and their mail on a clicked day', () => {
    expect(activityQuery('=bob@x.com', '2025-10-20')).toBe('from:=bob@x.com after:2025-10-20 | count by day');
    expect(dayQuery('=bob@x.com', '2026-03-14')).toBe('from:=bob@x.com on:2026-03-14');
  });

  it('puts the year in words', () => {
    expect(activitySummary([])).toBe('Nothing from them in the last year.');
    expect(activitySummary([{ day: '2026-03-02', value: 1 }])).toBe('1 message in the last year · less than one a month');
    expect(activitySummary([
      { day: '2025-11-02', value: 30 },
      { day: '2026-03-02', value: 40 },
      { day: '2026-03-20', value: 14 },
    ])).toBe('84 messages in the last year · about 7 a month · busiest in March 2026');
  });

  it('gives a tie for busiest month to the more recent one', () => {
    expect(activitySummary([
      { day: '2026-01-05', value: 3 },
      { day: '2026-02-05', value: 3 },
    ])).toMatch(/busiest in February 2026$/);
  });
});

describe('dayAfter', () => {
  it('steps over month and year ends, so the last day is drawn', async () => {
    const { dayAfter } = await import('../views/ActivityCalendar');
    expect(dayAfter('2026-10-19')).toBe('2026-10-20');
    expect(dayAfter('2026-01-31')).toBe('2026-02-01');
    expect(dayAfter('2025-12-31')).toBe('2026-01-01');
  });
});
