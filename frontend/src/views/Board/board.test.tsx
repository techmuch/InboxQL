import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { Board } from './index';
import { describe as describeEvent } from './CardPanel';
import { useBoardNav } from './labels';

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

const lists = [
  { id: 'proposed', title: 'Proposed', kind: 'proposed', position: 1, cards: 0 },
  { id: 'todo', title: 'To do', kind: 'open', position: 2, cards: 2 },
  { id: 'done', title: 'Done', kind: 'closed', position: 3, cards: 0 },
];

const columns = [
  { status: 'proposed', title: 'Proposed', kind: 'proposed', position: 1, total: 0, tickets: [] },
  {
    status: 'todo', title: 'To do', kind: 'open', position: 2, total: 2, tickets: [
      { id: 'c1', title: 'Paint the fence', status: 'todo', origin: 'human', position: 1,
        labels: ['home'], checklistDone: 1, checklistTotal: 3, comments: 2, body: 'Two coats',
        dueAt: '2020-01-01T17:00:00Z' },
      { id: 'c2', title: 'Order primer', status: 'todo', origin: 'human', position: 2 },
    ],
  },
  { status: 'done', title: 'Done', kind: 'closed', position: 3, total: 0, tickets: [] },
];

const detail = {
  ...columns[1].tickets[0],
  bodyHtml: '<p>Two coats</p>',
  listTitle: 'To do',
  checklist: [
    { id: 'i1', ticketId: 'c1', checklist: 'Checklist', text: 'Buy primer', done: true },
    { id: 'i2', ticketId: 'c1', checklist: 'Checklist', text: 'Sand', done: false },
  ],
  commentList: [{ id: 'k1', body: 'Ask Sam', bodyHtml: '<p>Ask Sam</p>', actor: 'human', createdAt: '2026-10-20T10:00:00Z', updatedAt: '2026-10-20T10:00:00Z' }],
  history: [{ id: 'h1', kind: 'status', from: 'proposed', to: 'todo', actor: 'rule', at: '2026-10-19T10:00:00Z' }],
};

type Call = { method: string; url: string; body: any };

const serve = (calls: Call[], boards = [{ id: 'all', title: 'All cards', filter: '', position: 1 }]) =>
  vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    if (url.startsWith('/api/board/boards')) return json(boards);
    if (url.startsWith('/api/board/lists')) return json(lists);
    if (url.startsWith('/api/board/labels')) return json({ labels: [{ name: 'home', color: 'green', cards: 1, messages: 0 }], colors: [] });
    if (url.startsWith('/api/tickets/board')) return json(columns);
    if (url.startsWith('/api/tickets?id=') && method === 'GET') return json(detail);
    if (url.startsWith('/api/tickets') && method === 'POST') return json({ ...detail, id: 'c9' }, 201);
    return json(detail);
  });

describe('Board', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useBoardNav.getState().setOpenCard(null);
    try { localStorage.clear(); } catch { /* none */ }
  });

  it('shows lists by title, and on each card its labels, checklist and comments', async () => {
    serve([]);
    render(<Board />);
    await waitFor(() => expect(screen.getByText('Paint the fence')).toBeTruthy());
    expect(screen.getByRole('heading', { name: 'To do' })).toBeTruthy();
    const card = screen.getByRole('button', { name: 'Paint the fence' });
    expect(within(card).getByText('home').className).toContain('bg-green-600');
    expect(within(card).getByText('1/3')).toBeTruthy();
    expect(within(card).getByText('2')).toBeTruthy();
    // Overdue in an open list is red.
    expect(card.querySelector('.bg-red-600')).toBeTruthy();
  });

  it('adds a card at the bottom of a list, with the labels its board asks for', async () => {
    const calls: Call[] = [];
    serve(calls, [{ id: 'home', title: 'Home', filter: 'label:home', position: 1, labels: ['home'] } as any]);
    render(<Board />);
    await waitFor(() => expect(screen.getByText('Paint the fence')).toBeTruthy());
    const todo = screen.getByRole('region', { name: 'To do' });
    fireEvent.click(within(todo).getByText('Add a card'));
    fireEvent.change(within(todo).getByLabelText('Card title'), { target: { value: 'Fix the gate' } });
    fireEvent.keyDown(within(todo).getByLabelText('Card title'), { key: 'Enter' });
    await waitFor(() => expect(calls.some(c => c.method === 'POST' && c.url === '/api/tickets')).toBe(true));
    const post = calls.find(c => c.method === 'POST' && c.url === '/api/tickets')!;
    expect(post.body).toMatchObject({ title: 'Fix the gate', status: 'todo', labels: ['home'] });
    // The board asked for is the board shown.
    expect(calls.some(c => c.url.includes('/api/tickets/board?board=home'))).toBe(true);
  });

  it('moves a card with the keyboard: shift-arrow down puts it after the next one', async () => {
    const calls: Call[] = [];
    serve(calls);
    render(<Board />);
    await waitFor(() => expect(screen.getByText('Paint the fence')).toBeTruthy());
    fireEvent.keyDown(screen.getByRole('button', { name: 'Paint the fence' }), { key: 'ArrowDown', shiftKey: true });
    await waitFor(() => expect(calls.some(c => c.url === '/api/board/move')).toBe(true));
    expect(calls.find(c => c.url === '/api/board/move')!.body).toEqual({ id: 'c1', list: 'todo', after: 'c2' });
  });

  it('opens a card, and saves each part on its own', async () => {
    const calls: Call[] = [];
    serve(calls);
    render(<Board />);
    await waitFor(() => expect(screen.getByText('Paint the fence')).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: 'Paint the fence' }));
    const dialog = await screen.findByRole('dialog', { name: 'Paint the fence' });
    await waitFor(() => expect(within(dialog).getByText('Buy primer')).toBeTruthy());

    // A rule's move is credited to the rule.
    expect(within(dialog).getByText(/Moved from Proposed to To do/)).toBeTruthy();
    expect(within(dialog).getByText(/by rule/)).toBeTruthy();

    // Ticking an item is one small call, not a save of the whole card.
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Sand' }));
    await waitFor(() => expect(calls.some(c => c.method === 'PATCH' && c.url === '/api/board/checklist')).toBe(true));
    expect(calls.find(c => c.url === '/api/board/checklist')!.body).toEqual({ id: 'i2', done: true });

    fireEvent.change(within(dialog).getByLabelText('Write a comment'), { target: { value: 'Done the first coat' } });
    fireEvent.click(within(dialog).getByText('Save'));
    await waitFor(() => expect(calls.some(c => c.url === '/api/board/comments')).toBe(true));

    fireEvent.change(within(dialog).getByLabelText('Due date'), { target: { value: '2026-11-02' } });
    await waitFor(() => expect(calls.some(c => c.method === 'PATCH' && c.url === '/api/tickets?id=c1')).toBe(true));
    const patch = calls.find(c => c.method === 'PATCH' && c.url === '/api/tickets?id=c1')!;
    expect(Object.keys(patch.body)).toEqual(['dueAt']);
  });

  it('says what a history event means', () => {
    const titles = new Map([['todo', 'To do'], ['done', 'Done']]);
    const ev = (kind: string, from = '', to = '') => describeEvent({ id: 'x', kind, from, to, actor: 'human', at: '' }, titles);
    expect(ev('status', 'todo', 'done')).toBe('Moved from To do to Done');
    expect(ev('label', '', 'home')).toBe('Label “home” added');
    expect(ev('label', 'home', '')).toBe('Label “home” removed');
    expect(ev('checked', '', 'Sand')).toBe('Completed “Sand”');
    expect(ev('archived')).toBe('Archived');
  });
});
