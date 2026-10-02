import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Starters } from './Starters';

/**
 * # What these guard
 *
 * The brief was a chooser that creates no work in either direction. The
 * obvious build — a tick-box over install and delete — fails that twice, and
 * invisibly: deleting an annotator cascades to every annotation it ever wrote,
 * including the human corrections somebody made by hand, and re-ticking buys
 * back only the machine's half of that at about twenty seconds a message.
 *
 * So the tick means participation, and these assert the three things that
 * follow: unticking sends a PUT rather than a DELETE, it says what it kept,
 * and absent and off both draw as unticked without being confused for each
 * other.
 */
describe('Starters', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  const pack = [
    {
      name: 'money', kind: 'label', engine: 'rule',
      about: 'Mail about money.', installed: true, enabled: true,
      records: 188, corrections: 0, reach: 188, slow: false,
    },
    {
      name: 'receipts', kind: 'extract', engine: 'gliner',
      about: 'Amounts and references.', scope: 'label:money',
      fields: ['amount', 'order number'], installed: true, enabled: true,
      records: 269, corrections: 19, reach: 32, slow: true,
    },
    {
      name: 'travel', kind: 'label', engine: 'rule',
      about: 'Flights and hotels.', installed: false, enabled: false,
      records: 0, corrections: 0, reach: 12, slow: false,
    },
  ];

  const mockFetch = (onPut?: (url: string, body: any) => any) =>
    vi.spyOn(window, 'fetch').mockImplementation(((url: any, init?: any) => {
      const u = String(url);
      if (init?.method === 'PUT') {
        const body = JSON.parse(init.body);
        const out = onPut?.(u, body) ?? { name: 'x', enabled: body.enabled };
        return Promise.resolve(new Response(JSON.stringify(out), { status: 200 }));
      }
      return Promise.resolve(new Response(JSON.stringify(pack), { status: 200 }));
    }) as any);

  it('draws an installed-and-on starter as ticked and an absent one as unticked', async () => {
    mockFetch();
    render(<Starters />);

    const boxes = await waitFor(() => {
      const b = screen.getAllByRole('checkbox') as HTMLInputElement[];
      expect(b).toHaveLength(3);
      return b;
    });
    expect(boxes[0].checked).toBe(true);
    expect(boxes[1].checked).toBe(true);
    // Not installed, so unticked — and nothing on the row claims otherwise.
    expect(boxes[2].checked).toBe(false);
    expect(screen.getByText(/Mail about money/)).toBeTruthy();
  });

  it('unticking sends a PUT, never a DELETE', async () => {
    const fetchSpy = mockFetch(() => ({
      name: 'receipts', enabled: false, installed: true, records: 269, corrections: 19,
    }));
    render(<Starters />);

    const boxes = await waitFor(() => {
      const b = screen.getAllByRole('checkbox') as HTMLInputElement[];
      expect(b).toHaveLength(3);
      return b;
    });

    fireEvent.click(boxes[1]);

    await waitFor(() => {
      const calls: [string, string | undefined][] = fetchSpy.mock.calls.map(
        c => [String(c[0]), (c[1] as RequestInit | undefined)?.method],
      );
      expect(calls).toEqual(
        expect.arrayContaining([[expect.stringContaining('/api/starters/receipts'), 'PUT']]),
      );
      // The thing this whole design exists to prevent.
      expect(calls.some(([, method]) => method === 'DELETE')).toBe(false);
    });
  });

  it('says what unticking kept, in rows, naming the hand-set ones', async () => {
    mockFetch(() => ({
      name: 'receipts', enabled: false, installed: true, records: 269, corrections: 19,
    }));
    render(<Starters />);

    const boxes = await waitFor(() => {
      const b = screen.getAllByRole('checkbox') as HTMLInputElement[];
      expect(b).toHaveLength(3);
      return b;
    });

    fireEvent.click(boxes[1]);

    // A number, because "nothing is lost" is only checkable as one — and rows
    // rather than messages, because rows are what a delete would take. The
    // hand-set ones are named separately: they are the half that re-running
    // cannot rebuild at any price.
    const kept = await screen.findByText(/Kept 269 records and 19 values set by hand/i);
    expect(kept).toBeTruthy();
  });

  it('ticking an absent starter says it created it and ran nothing', async () => {
    mockFetch(() => ({ name: 'travel', enabled: true, installed: true, created: true }));
    render(<Starters />);

    const boxes = await waitFor(() => {
      const b = screen.getAllByRole('checkbox') as HTMLInputElement[];
      expect(b).toHaveLength(3);
      return b;
    });

    fireEvent.click(boxes[2]);

    expect(await screen.findByText(/Nothing has run yet/i)).toBeTruthy();
  });

  it('warns when a gate is off, because that freezes what it gates', async () => {
    const frozen = [
      { ...pack[0], enabled: false },
      { ...pack[1], gateOff: 'money' },
      pack[2],
    ];
    vi.spyOn(window, 'fetch').mockImplementation((() =>
      Promise.resolve(new Response(JSON.stringify(frozen), { status: 200 }))) as any);

    render(<Starters />);

    expect(
      await screen.findByText(/nothing is labelling new mail for this to read/i),
    ).toBeTruthy();
  });

  it('survives a response it cannot draw', async () => {
    vi.spyOn(window, 'fetch').mockImplementation((() =>
      Promise.resolve(new Response(JSON.stringify({ unexpected: true }), { status: 200 }))) as any);

    render(<Starters />);

    // No rows, no crash. A shape change on the server must not take the
    // settings page down with it.
    await waitFor(() => {
      expect(screen.queryAllByRole('checkbox')).toHaveLength(0);
    });
  });
});
