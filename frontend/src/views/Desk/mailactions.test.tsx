import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Desk } from './index';
import { LabelPicker } from '../LabelPicker';
import { useQueryStore } from '../../lib/filters';
import { useViewerStore } from '../../lib/tabs';
import { useSelectionStore } from '../../lib/selection';
import { moveMessages } from '../../lib/mail';

/**
 * Archive, trash, labels and stars: the actions that used to be dead buttons,
 * and the keys that do them.
 */
describe('Desk mail actions', () => {
  const messages = [
    { id: 'm1', from: 'alice@acme.com', subject: 'Quarterly invoice', date: '2026-03-01T10:00:00Z',
      flags: [], labels: ['inbox', 'Receipts'] },
    { id: 'm2', from: 'bob@acme.com', subject: 'Lunch', date: '2026-03-05T10:00:00Z',
      flags: ['\\Seen'], labels: ['inbox'] },
  ];
  let posts: { url: string; body: any }[] = [];

  beforeEach(() => {
    vi.restoreAllMocks();
    posts = [];
    useQueryStore.getState().set('folder:inbox');
    useViewerStore.getState().clear();
    useSelectionStore.getState().clear();
    vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      if (init?.method === 'POST') posts.push({ url, body: JSON.parse(String(init.body)) });
      const body =
        url.startsWith('/api/query?') ? { query: 'folder:inbox', kind: 'messages', count: 2, messages }
        : url.startsWith('/api/query/terms') ? { terms: [], stages: [] }
        : url.startsWith('/api/queries') ? []
        : url.startsWith('/api/labels') ? { labels: [
            { name: 'inbox', system: true, total: 2, unread: 1 },
            { name: 'Receipts', system: false, total: 1, unread: 1 },
            { name: 'Travel', system: false, total: 0, unread: 0 },
          ] }
        : url.startsWith('/api/messages/') ? { changed: 1 }
        : {};
      return { ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body } as Response;
    });
  });

  it('shows your labels on the row and in the rail', async () => {
    render(<Desk />);
    await screen.findByText('Quarterly invoice');
    // The chip on the row, and the rail entry; never the system label.
    await waitFor(() => expect(screen.getAllByText('Receipts').length).toBeGreaterThanOrEqual(2));
    expect(screen.getByTitle('label:Receipts')).toBeTruthy();
    expect(screen.queryByText('inbox')).toBeNull();
  });

  it('archives the focused row with e, and trashes with #', async () => {
    render(<Desk />);
    const row = (await screen.findByText('Quarterly invoice')).closest('[data-nav-row]') as HTMLElement;
    row.focus();
    fireEvent.keyDown(row, { key: 'e', bubbles: true });
    await waitFor(() => expect(posts).toContainEqual({ url: '/api/messages/move', body: { ids: ['m1'], to: 'archive' } }));
    fireEvent.keyDown(row, { key: '#', bubbles: true });
    await waitFor(() => expect(posts).toContainEqual({ url: '/api/messages/move', body: { ids: ['m1'], to: 'trash' } }));
  });

  it('does not take keys typed into the query', async () => {
    render(<Desk />);
    await screen.findByText('Quarterly invoice');
    const editor = document.querySelector('textarea, input, [contenteditable="true"]') as HTMLElement;
    fireEvent.keyDown(editor, { key: 'e', bubbles: true });
    expect(posts.filter(p => p.url === '/api/messages/move')).toHaveLength(0);
  });

  it('stars from the row', async () => {
    render(<Desk />);
    await screen.findByText('Quarterly invoice');
    fireEvent.click(screen.getAllByLabelText('Star')[0]);
    await waitFor(() => expect(posts).toContainEqual({
      url: '/api/messages/flags', body: { ids: ['m1'], flag: '\\Flagged', on: true },
    }));
  });

  it('updates the open message when it is moved', async () => {
    useViewerStore.getState().setMessage({ ...messages[0] });
    await moveMessages(['m1'], 'archive');
    expect(useViewerStore.getState().message.labels).toEqual(['Receipts']);
  });
});

describe('LabelPicker', () => {
  let posts: { url: string; body: any }[] = [];
  beforeEach(() => {
    vi.restoreAllMocks();
    posts = [];
    vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      if (init?.method === 'POST') posts.push({ url, body: JSON.parse(String(init.body)) });
      const body = url.startsWith('/api/labels')
        ? { labels: [{ name: 'Receipts', system: false, total: 1, unread: 0 }, { name: 'Travel', system: false, total: 0, unread: 0 }] }
        : { changed: 2 };
      return { ok: true, status: 200, json: async () => body } as Response;
    });
  });

  it('ticks a label every message has, dashes one some have, and adds to all', async () => {
    render(<LabelPicker ids={['a', 'b']} current={[['Receipts', 'Travel'], ['Receipts']]} />);
    fireEvent.click(screen.getByLabelText('Label'));
    const receipts = await screen.findByRole('option', { name: 'Receipts' });
    expect(receipts.getAttribute('aria-selected')).toBe('true');
    const travel = screen.getByRole('option', { name: 'Travel' });
    expect(travel.getAttribute('aria-selected')).toBe('false');

    // Some have Travel: choosing it puts it on all of them.
    fireEvent.click(travel);
    await waitFor(() => expect(posts).toContainEqual({
      url: '/api/messages/labels', body: { ids: ['a', 'b'], add: ['Travel'], remove: [] },
    }));
    // All have Receipts: choosing it takes it off.
    fireEvent.click(receipts);
    await waitFor(() => expect(posts).toContainEqual({
      url: '/api/messages/labels', body: { ids: ['a', 'b'], add: [], remove: ['Receipts'] },
    }));
  });

  it('offers to create a label that does not exist', async () => {
    render(<LabelPicker ids={['a']} current={[[]]} />);
    fireEvent.click(screen.getByLabelText('Label'));
    fireEvent.change(await screen.findByLabelText('Find or create a label'), { target: { value: 'Taxes 2026' } });
    fireEvent.click(screen.getByText(/Create “Taxes 2026”/));
    await waitFor(() => expect(posts).toContainEqual({
      url: '/api/messages/labels', body: { ids: ['a'], add: ['Taxes 2026'], remove: [] },
    }));
  });
});
