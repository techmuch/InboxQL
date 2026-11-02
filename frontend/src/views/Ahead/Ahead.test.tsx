import { describe, it, expect } from 'vitest';
import { gather } from './gather';
import { countdowns, nextOccurrence, type ImportantDate } from '../../lib/importantDates';

const from = new Date(2026, 10, 1); // Sun Nov 1, 2026
const to = new Date(2026, 10, 15);
const date = (p: Partial<ImportantDate>): ImportantDate => ({ id: p.title ?? 'x', title: 'x', date: '2026-11-10', leadDays: 0, yearly: false, origin: 'human', ...p });

describe('gather', () => {
  it('puts events, due cards and important dates on their days, in time order', () => {
    const by = gather(
      [
        { eventId: 'e2', start: new Date(2026, 10, 2, 14).toISOString(), end: new Date(2026, 10, 2, 15).toISOString(), allDay: false, summary: 'Review call' },
        { eventId: 'e1', start: new Date(2026, 10, 2, 9).toISOString(), end: new Date(2026, 10, 2, 11).toISOString(), allDay: false, summary: 'Planning' },
        { eventId: 'e3', start: new Date(2026, 10, 2, 13).toISOString(), end: new Date(2026, 10, 2, 14).toISOString(), allDay: false, summary: 'Proposed', proposed: true },
      ],
      [{ id: 't1', title: 'Send numbers', dueAt: new Date(2026, 10, 6).toISOString(), status: 'todo' }],
      [date({ title: 'Q4 report', date: '2026-11-10' })],
      from, to,
    );
    const mon = by.get('2026-11-02')!;
    expect(mon.map(i => i.title)).toEqual(['Planning', 'Proposed', 'Review call']);
    // A proposal is shown but not booked.
    expect(mon.reduce((h, i) => h + (i.hours ?? 0), 0)).toBe(3);
    expect(by.get('2026-11-06')![0]).toMatchObject({ kind: 'Due', title: 'Send numbers' });
    expect(by.get('2026-11-10')![0]).toMatchObject({ kind: 'Important', important: true });
  });

  it('shows when a lead time starts, and stars an event that was marked', () => {
    const by = gather(
      [{ eventId: 'trip', start: new Date(2026, 10, 12, 7).toISOString(), end: new Date(2026, 10, 12, 9).toISOString(), allDay: false, summary: 'Flight' }],
      [],
      [date({ title: 'Passport', date: '2026-11-14', leadDays: 7 }), date({ title: 'Flight', date: '2026-11-12', refKind: 'event', refId: 'trip' })],
      from, to,
    );
    expect(by.get('2026-11-07')![0].title).toBe('Start: Passport');
    expect(by.get('2026-11-12')![0]).toMatchObject({ title: 'Flight', important: true });
  });
});

describe('countdowns', () => {
  it('counts down the marked dates, soonest first, and leaves suggestions out', () => {
    const c = countdowns([
      date({ title: 'Anniversary', date: '2019-12-12', yearly: true }),
      date({ title: 'Q4 report', date: '2026-11-10' }),
      date({ title: 'Suggested', date: '2026-11-03', origin: 'agent' }),
      date({ title: 'Passed', date: '2026-10-01' }),
    ], from);
    expect(c.map(x => [x.date.title, x.days])).toEqual([['Q4 report', 9], ['Anniversary', 41]]);
    expect(c[0].soon).toBe(true);
    expect(c[1].soon).toBe(false);
  });

  it('is soon once its lead time has started', () => {
    const [c] = countdowns([date({ title: 'Passport', date: '2026-12-10', leadDays: 42 })], from);
    expect(c.start?.days).toBe(-3);
    expect(c.soon).toBe(true);
  });

  it('a yearly date that falls today counts today', () => {
    expect(nextOccurrence({ date: '2001-11-01', yearly: true }, from)?.getFullYear()).toBe(2026);
  });
});
