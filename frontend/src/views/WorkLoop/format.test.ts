import { afterEach, describe, expect, it, vi } from 'vitest';
import { fillDays } from './format';

describe('fillDays', () => {
  afterEach(() => { vi.useRealTimers(); });

  it('includes today in the morning, not only after noon', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 9, 30, 2, 45)); // 02:45 local
    const days = fillDays([{ day: '2026-10-30', stored: 30, refused: 2, checked: 4 }], new Date(2026, 9, 23, 2, 45).toISOString());
    expect(days[days.length - 1]).toEqual({ day: '2026-10-30', stored: 30, refused: 2, checked: 4 });
    expect(days[0].day).toBe('2026-10-23');
    expect(days).toHaveLength(8);
  });

  it('gives a quiet day an empty bar rather than leaving it out', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 9, 30, 15, 0));
    const days = fillDays([{ day: '2026-10-28', stored: 3, refused: 0, checked: 0 }], new Date(2026, 9, 27, 9).toISOString());
    expect(days.map(d => d.stored)).toEqual([0, 3, 0, 0]);
  });
});
