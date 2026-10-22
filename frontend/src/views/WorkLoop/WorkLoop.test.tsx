import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { WorkLoop } from './index';

const today = new Date();
const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

const route = (element: string, over: Record<string, unknown> = {}) => ({
  element, target: 0.95, cost: 'high', custom: false,
  random: { n: 0, right: 0, wrong: 0, open: 0 }, guessed: { n: 0, right: 0, wrong: 0, open: 0 },
  targeted: { n: 0, right: 0, wrong: 0, open: 0 }, status: 'unmeasured', guess: true,
  why: 'guessed where nothing suggests otherwise', checkRate: 0.15, ...over,
});

const metrics = {
  since: new Date(today.getTime() - 7 * 864e5).toISOString(),
  models: ['fast', 'careful'],
  reading: {
    stored: 42, refused: 3, medianSeconds: 95, batches: 5, leased: 50, expired: 5, refusalRate: 3 / 45,
    perDay: 21, guessedShare: 0.4,
    days: [{ day: iso(today), stored: 30, refused: 2, checked: 4 }],
    problems: [{ problem: 'receipts: quote not found in the conversation', count: 2 }],
  },
  checks: { random: { n: 12, right: 10, wrong: 2, open: 0 }, targeted: { n: 4, right: 2, wrong: 2, open: 1 }, open: 1, due: 3, rate: 0.15 },
  accuracy: { accuracy: 10 / 12, low: 0.55, high: 0.95, n: 12 },
  elements: [
    { element: 'receipts', about: 'what was bought', unit: 'message', answered: 20, prefilled: 2, found: 5, withConfidence: 10,
      reviewed: 0, reviewedRight: 0,
      route: route('receipts', { status: 'missing', guess: false, why: 'checked answers miss the target, so it is read rather than guessed',
        random: { n: 12, right: 10, wrong: 2, open: 0 }, estimate: { accuracy: 10 / 12, low: 0.55, high: 0.95, n: 12 } }) },
    { element: 'topics', about: 'what it is about', unit: 'thread', answered: 22, prefilled: 15, found: 22, withConfidence: 0,
      reviewed: 4, reviewedRight: 3, route: route('topics', { target: 0.8, cost: 'low' }) },
  ],
  actions: { ticket: { total: 4, kept: 3, undone: 1, open: 0 }, label: { total: 10, kept: 8, undone: 2, open: 0 } },
  calibration: { bins: [{ lo: 0.9, hi: 1, n: 10, right: 7, mean: 0.95 }, { lo: 0.5, hi: 0.7, n: 2, right: 2, mean: 0.6 }], unrated: 2 },
  flagged: 2,
};

const disagreement = {
  id: 'wa_1', thread: 't1', messageId: 'm1', target: 'm1', element: 'receipts', about: 'what was bought',
  question: 'What was bought?', shape: 'records', unit: 'message', subject: 'Your Home Depot receipt',
  from: 'Home Depot <hd@x.com>', date: today.toISOString(), snippet: 'TOTAL 287.83', stratum: 'random',
  originalModel: 'fast', originalPrefilled: true, checkerModel: 'careful',
  originalSays: 'nothing here', checkerSays: 'Home Depot · 287.83',
  originalAnswer: { none: 'none' }, checkerAnswer: { records: [{ merchant: 'Home Depot', total: 287.83, quote: 'TOTAL 287.83' }] },
};

let calls: { url: string; method: string; body?: unknown }[] = [];

describe('Work loop', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    calls = [];
    vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
      const url = new URL(String(input), 'http://x');
      const method = init?.method ?? 'GET';
      calls.push({ url: url.pathname + url.search, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      let body: unknown = {};
      if (url.pathname === '/api/work/metrics') body = metrics;
      else if (url.pathname === '/api/work/backlog') body = { elements: [{ element: 'receipts', pending: 1234, total: 5000 }] };
      else if (url.pathname === '/api/work/disagreements') body = { disagreements: [disagreement], open: 1 };
      else if (url.pathname === '/api/work/disagreements/resolve') body = { ...disagreement, resolution: 'checker' };
      else if (url.pathname === '/api/work/targets') body = { elements: {} };
      return { ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body } as Response;
    });
  });

  it('shows the headline numbers, each element against its target, and what you did with its work', async () => {
    render(<WorkLoop />);
    expect(await screen.findByRole('heading', { level: 1, name: /Work loop/ })).toBeTruthy();
    const summary = await screen.findByLabelText('Summary');
    expect(within(summary).getByText('42')).toBeTruthy();
    expect(within(summary).getByText('83%')).toBeTruthy();
    expect(within(summary).getByText(/probably 55%–95%, 12 checks/)).toBeTruthy();

    const elements = screen.getByRole('table', { name: /measured accuracy for each element/ });
    const receipts = within(elements).getByRole('row', { name: /receipts/ });
    expect(within(receipts).getByText('Missing target')).toBeTruthy();
    expect(await within(receipts).findByText('1,234')).toBeTruthy();

    const routing = screen.getByRole('table', { name: /Target accuracy/ });
    expect(within(routing).getByRole('row', { name: /receipts/ }).textContent).toContain('Must read');

    const signals = screen.getByRole('table', { name: /What became of/ });
    expect(within(signals).getByRole('row', { name: /Proposed cards/ }).textContent).toContain('75%');
    expect(screen.getByText('receipts: quote not found in the conversation')).toBeTruthy();
    expect(screen.getByText('over-sure by 25%')).toBeTruthy();
    expect(screen.getByText('too few to tell')).toBeTruthy();
  });

  it('hides who gave which answer until you choose, then saves the right one', async () => {
    render(<WorkLoop />);
    const queue = await screen.findByRole('region', { name: /Waiting for you/ });
    await within(queue).findByText('Your Home Depot receipt');
    // Nothing says which was the first read or which model gave it.
    expect(queue.textContent).not.toMatch(/fast|careful|first read|untouched/);

    const cards = within(queue).getAllByRole('heading', { level: 4 }).map(h => h.closest('div')!);
    const checkerIsA = cards[0].textContent!.includes('Home Depot · 287.83');
    fireEvent.keyDown(queue, { key: checkerIsA ? '1' : '2' });

    await waitFor(() => expect(calls.find(c => c.url === '/api/work/disagreements/resolve')).toBeTruthy());
    expect(calls.find(c => c.url === '/api/work/disagreements/resolve')!.body).toEqual({ id: 'wa_1', choice: 'checker' });
    const status = await within(queue).findByText(/Saved for “Your Home Depot receipt”/);
    expect(status.textContent).toMatch(/the check \(careful\)/);
    expect(status.textContent).toMatch(/the first read \(fast, an untouched guess\)/);
    expect(await within(queue).findByText(/Nothing to settle/)).toBeTruthy();
  });

  it('saves a new target and cost', async () => {
    render(<WorkLoop />);
    const target = await screen.findByLabelText('How often receipts must be right');
    fireEvent.change(target, { target: { value: '0.99' } });
    await waitFor(() => expect(calls.find(c => c.method === 'PUT')).toBeTruthy());
    expect(calls.find(c => c.method === 'PUT')!.body).toEqual({ element: 'receipts', target: 0.99, cost: 'high' });
    expect(await screen.findByText(/Saved: receipts must be 99% right/)).toBeTruthy();

    fireEvent.change(screen.getByLabelText('What a wrong topics answer costs'), { target: { value: 'medium' } });
    await waitFor(() => expect(calls.filter(c => c.method === 'PUT')).toHaveLength(2));
    expect(calls.filter(c => c.method === 'PUT')[1].body).toEqual({ element: 'topics', target: 0.8, cost: 'medium' });
  });

  it('gives the daily chart as a table, and each bar a label', async () => {
    render(<WorkLoop />);
    const bar = await screen.findByRole('img', { name: /30 stored, 2 refused, 4 checked/ });
    fireEvent.focus(bar);
    expect(screen.getByRole('tooltip').textContent).toContain('30 stored');
    fireEvent.click(screen.getByRole('button', { name: 'Show as table' }));
    const table = screen.getByRole('table', { name: /stored, refused and checked per day/ });
    expect(within(table).getAllByRole('row')).toHaveLength(2);
  });

  it('asks for another period and model', async () => {
    render(<WorkLoop />);
    await screen.findByLabelText('Summary');
    fireEvent.click(screen.getByLabelText('90 days'));
    await waitFor(() => expect(calls.some(c => c.url === '/api/work/metrics?since=90d')).toBe(true));
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'careful' } });
    await waitFor(() => expect(calls.some(c => c.url === '/api/work/metrics?since=90d&model=careful')).toBe(true));
  });
});
