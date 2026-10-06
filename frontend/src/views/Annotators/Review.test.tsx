import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ReviewPanel, ScoreLine } from './Review';

const items = [
  { messageId: 'm1', from: 'alice@x.com', subject: 'Contract', date: '2026-10-01T00:00:00Z',
    snippet: 'Can you send it by Friday?', machine: { matched: false, score: 0.27 } },
  { messageId: 'm2', from: 'news@x.com', subject: 'Sale', date: '2026-10-02T00:00:00Z',
    snippet: '20% off', machine: { matched: false, score: 0.05 } },
];

describe('ReviewPanel', () => {
  let puts: { url: string; body: any }[];
  beforeEach(() => {
    vi.useRealTimers();
    puts = [];
    globalThis.fetch = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') puts.push({ url, body: JSON.parse(String(init.body)) });
      return { ok: true, json: async () => (url.startsWith('/api/annotators/review') ? items : []) } as Response;
    }) as never;
  });

  // Shown first, the model's answer anchors the reviewer, which inflates the
  // very agreement this measures.
  it('asks the question without showing the model\'s answer', async () => {
    render(<ReviewPanel name="loops" question="this message expects a reply" levels={[]} onClose={() => {}} />);
    expect(await screen.findByText('Contract')).toBeTruthy();
    expect(screen.getByText('this message expects a reply')).toBeTruthy();
    expect(screen.queryByText(/The model said/)).toBeNull();
  });

  it('records a key press as a review ruling, then reveals the answer', async () => {
    render(<ReviewPanel name="loops" question="q" levels={[]} onClose={() => {}} />);
    await screen.findByText('Contract');

    fireEvent.keyDown(window, { key: 'y' });
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0].url).toBe('/api/messages/m1/labels/loops');
    expect(puts[0].body).toEqual({ matched: true, via: 'review' });
    // You said yes, it said no: shown as a disagreement.
    expect(await screen.findByText('The model said no.')).toBeTruthy();
  });

  it('rules on levels by number for a levelled label', async () => {
    render(<ReviewPanel name="importance" question="q"
      levels={[{ label: 'important' }, { label: 'normal' }, { label: 'low' }]} onClose={() => {}} />);
    await screen.findByText('Contract');

    fireEvent.keyDown(window, { key: '1' });
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0].body).toEqual({ matched: true, level: 'important', via: 'review' });
  });

  it('skips without ruling', async () => {
    render(<ReviewPanel name="loops" question="q" levels={[]} onClose={() => {}} />);
    await screen.findByText('Contract');
    act(() => { fireEvent.keyDown(window, { key: 's' }); });
    expect(await screen.findByText('Sale')).toBeTruthy();
    expect(puts).toHaveLength(0);
  });
});

describe('ScoreLine', () => {
  it('reports corrections made while reading without scoring them', () => {
    render(<ScoreLine score={{ annotator: 'loops', engine: 'laya', reviewed: 0, inflow: 3, right: 0, accuracy: 0 }} />);
    expect(screen.getByText(/Not reviewed yet · 3 corrections while reading/)).toBeTruthy();
  });

  it('shows the better cutoff when it would have been right more often', () => {
    render(<ScoreLine score={{ annotator: 'loops', engine: 'laya', reviewed: 6, inflow: 0,
      right: 3, accuracy: 0.5, cutoff: 0.25, atCutoff: 6 }} />);
    expect(screen.getByText(/3 of 6 right \(50%\)/)).toBeTruthy();
    expect(screen.getByText(/6 at a cutoff of 0.25/)).toBeTruthy();
    expect(screen.getByText(/too few to trust/)).toBeTruthy();
  });
});

describe('ReviewPanel, pressed quickly', () => {
  // `reveal` is only set once the ruling is saved, so a guard on it alone let a
  // second press through while the first was in flight.
  it('rules once for a double press', async () => {
    const puts: string[] = [];
    globalThis.fetch = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') puts.push(url);
      return { ok: true, json: async () => (url.startsWith('/api/annotators/review') ? items : []) } as Response;
    }) as never;
    render(<ReviewPanel name="loops" question="q" levels={[]} onClose={() => {}} />);
    await screen.findByText('Contract');

    fireEvent.keyDown(window, { key: 'y' });
    fireEvent.keyDown(window, { key: 'y' });
    await waitFor(() => expect(puts.length).toBeGreaterThan(0));
    await new Promise(r => setTimeout(r, 50));
    expect(puts).toHaveLength(1);
  });
});
