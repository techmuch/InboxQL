import { describe, it, expect } from 'vitest';
import { layoutColumns, startOfWeek, monthGrid, snap15 } from './dates';

const at = (h: number, m = 0) => new Date(2026, 9, 12, h, m);

describe('calendar dates', () => {
  it('starts weeks on Monday', () => {
    expect(startOfWeek(new Date(2026, 9, 18)).getDate()).toBe(12); // Sunday -> Monday 12th
    expect(startOfWeek(new Date(2026, 9, 12)).getDate()).toBe(12);
  });

  it('shows six weeks for a month, from the Monday on or before the 1st', () => {
    const g = monthGrid(new Date(2026, 9, 15));
    expect(g.length).toBe(42);
    expect(g[0].getDay()).toBe(1);
    expect(g[0] <= new Date(2026, 9, 1)).toBe(true);
  });

  it('puts overlapping events side by side and lone ones full width', () => {
    const laid = layoutColumns([
      { id: 'a', start: at(9), end: at(10) },
      { id: 'b', start: at(9, 30), end: at(11) },
      { id: 'c', start: at(12), end: at(13) },
    ]);
    const by = Object.fromEntries(laid.map(l => [l.id, l]));
    expect([by.a.col, by.a.cols, by.b.col, by.b.cols]).toEqual([0, 2, 1, 2]);
    expect([by.c.col, by.c.cols]).toEqual([0, 1]);
  });

  it('snaps to quarter hours', () => {
    expect(snap15(7)).toBe(0);
    expect(snap15(8)).toBe(15);
  });
});
