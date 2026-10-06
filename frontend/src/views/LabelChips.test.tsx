import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { LabelChips, type MessageLabel } from './LabelChips';

const loops: MessageLabel = {
  annotator: 'loops', engine: 'laya', enabled: true, unit: 'thread',
  machine: { matched: true, score: 0.81 },
};
const receipt: MessageLabel = {
  annotator: 'receipt', engine: 'laya', enabled: true, unit: 'message',
  machine: { matched: false, score: 0.12 },
};
const importance: MessageLabel = {
  annotator: 'importance', engine: 'laya', enabled: true, unit: 'thread',
  levels: [{ label: 'important' }, { label: 'normal' }, { label: 'low' }, { label: 'ignorable' }],
  machine: { matched: true, level: 'normal', score: 0.6 },
};

describe('LabelChips', () => {
  let calls: { url: string; init?: RequestInit }[];

  beforeEach(() => {
    calls = [];
    globalThis.fetch = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      calls.push({ url, init });
      const after = init?.method === 'PUT'
        ? [{ ...loops, ruling: { matched: JSON.parse(String(init.body)).matched, via: 'inflow' } }]
        : [loops, receipt, importance];
      return { ok: true, json: async () => after } as Response;
    }) as never;
  });

  it('shows what was said, and hides the noes behind a count', async () => {
    render(<LabelChips messageId="m1" />);
    expect(await screen.findByText('loops')).toBeTruthy();
    expect(screen.getByText('importance: normal')).toBeTruthy();
    expect(screen.queryByText('not receipt')).toBeNull();

    // A "no" can be wrong too — a missed open loop is the mistake worth
    // catching — so it is one click away rather than absent.
    fireEvent.click(screen.getByText('+1 said no'));
    expect(screen.getByText('not receipt')).toBeTruthy();
  });

  /**
   * Agreeing has to cost exactly what disagreeing costs. A ruling set made of
   * corrections alone says the model is never right.
   */
  it('records "right" as a ruling, made while reading', async () => {
    render(<LabelChips messageId="m1" />);
    fireEvent.contextMenu(await screen.findByText('loops'));
    fireEvent.click(screen.getByRole('menuitem', { name: /^Right/ }));

    await waitFor(() => expect(calls.some(c => c.init?.method === 'PUT')).toBe(true));
    const put = calls.find(c => c.init?.method === 'PUT')!;
    expect(put.url).toBe('/api/messages/m1/labels/loops');
    expect(JSON.parse(String(put.init!.body))).toEqual({ matched: true });
  });

  it('records "wrong" as the opposite answer', async () => {
    render(<LabelChips messageId="m1" />);
    fireEvent.contextMenu(await screen.findByText('loops'));
    fireEvent.click(screen.getByRole('menuitem', { name: /^Wrong/ }));

    await waitFor(() => expect(calls.some(c => c.init?.method === 'PUT')).toBe(true));
    const put = calls.find(c => c.init?.method === 'PUT')!;
    expect(JSON.parse(String(put.init!.body))).toEqual({ matched: false });
  });

  // For a level, "wrong" says nothing. The menu offers what it should be.
  it('offers every other level for a levelled label', async () => {
    render(<LabelChips messageId="m1" />);
    fireEvent.contextMenu(await screen.findByText('importance: normal'));

    expect(screen.getByRole('menuitem', { name: /Right — normal/ })).toBeTruthy();
    for (const l of ['important', 'low', 'ignorable']) {
      expect(screen.getByRole('menuitem', { name: `Should be ${l}` })).toBeTruthy();
    }
    expect(screen.queryByRole('menuitem', { name: /^Wrong/ })).toBeNull();

    fireEvent.click(screen.getByRole('menuitem', { name: 'Should be important' }));
    await waitFor(() => expect(calls.some(c => c.init?.method === 'PUT')).toBe(true));
    const put = calls.find(c => c.init?.method === 'PUT')!;
    expect(JSON.parse(String(put.init!.body))).toEqual({ matched: true, level: 'important' });
  });

  it('opens from the keyboard and closes on Escape', async () => {
    render(<LabelChips messageId="m1" />);
    const chip = await screen.findByText('loops');
    fireEvent.keyDown(chip, { key: 'ContextMenu' });
    expect(screen.getByRole('menu')).toBeTruthy();

    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('marks your ruling as yours once made', async () => {
    render(<LabelChips messageId="m1" />);
    fireEvent.contextMenu(await screen.findByText('loops'));
    fireEvent.click(screen.getByRole('menuitem', { name: /^Right/ }));
    expect(await screen.findByLabelText('your ruling')).toBeTruthy();
  });
});

describe('LabelChips, after ruling', () => {
  // Ruling a label wrong and watching it vanish into "+1 said no" reads as the
  // click having done nothing.
  it('keeps showing a label you ruled "no"', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [{ ...loops, ruling: { matched: false, via: 'inflow' } }],
    }) as never;
    render(<LabelChips messageId="m1" />);
    expect(await screen.findByText('not loops')).toBeTruthy();
    expect(screen.getByLabelText('your ruling')).toBeTruthy();
    expect(screen.queryByText(/said no/)).toBeNull();
  });
});
