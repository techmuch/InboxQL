import { describe, it, expect, vi, afterEach } from 'vitest';
import { installKeySequences } from './keySequences';

const press = (key: string, init: KeyboardEventInit = {}, target?: HTMLElement) => {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init });
  (target ?? window).dispatchEvent(e);
  return e;
};

describe('key sequences', () => {
  let off: () => void = () => {};
  afterEach(() => { off(); document.body.innerHTML = ''; });

  it('runs g-then-letter, and single keys', () => {
    const run = vi.fn();
    off = installKeySequences(run, () => {});
    press('g'); press('c');
    press('c');
    press('?', { shiftKey: true });
    expect(run.mock.calls.map(c => c[0])).toEqual(['iql.open-calendar', 'iql.new-message', 'iql.keyboard']);
  });

  it('leaves typing alone', () => {
    const run = vi.fn();
    off = installKeySequences(run, () => {});
    const input = document.createElement('input');
    document.body.appendChild(input);
    press('g', {}, input); press('c', {}, input);
    expect(run).not.toHaveBeenCalled();
  });

  it('opens Find anything with Ctrl+K or ⌘K, even from a field', () => {
    const palette = vi.fn();
    off = installKeySequences(() => {}, palette);
    const input = document.createElement('input');
    document.body.appendChild(input);
    press('k', { ctrlKey: true }, input);
    press('k', { metaKey: true });
    expect(palette).toHaveBeenCalledTimes(2);
  });

  it('forgets an unfinished sequence', () => {
    vi.useFakeTimers();
    const run = vi.fn();
    off = installKeySequences(run, () => {});
    press('g');
    vi.advanceTimersByTime(2000);
    press('c'); // now just "c": new message, not calendar
    expect(run.mock.calls.map(c => c[0])).toEqual(['iql.new-message']);
    vi.useRealTimers();
  });
});
