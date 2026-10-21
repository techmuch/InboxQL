import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { Desk } from './index';
import { useQueryStore } from '../../lib/filters';
import { useViewerStore } from '../../lib/tabs';
import { useSelectionStore } from '../../lib/selection';

/**
 * The Desk as a folder you add to: the folder chip, one-click filters, every
 * match selected at once, the Group view, and All Items.
 */
describe('Desk folder frame', () => {
  const page = Array.from({ length: 50 }, (_, i) => ({
    id: `m${i}`, from: i % 2 ? 'news@shop.example' : 'alice@acme.com', subject: `Message ${i}`,
    date: '2026-03-01T10:00:00Z', flags: [], labels: ['inbox'],
  }));
  let ran: string[] = [];
  let posts: { url: string; body: any }[] = [];

  const terms = (q: string) => {
    let at = 0;
    const out: any[] = [];
    for (const text of q.split(' ').filter(Boolean)) {
      if (text === '|') break;
      const start = q.indexOf(text, at);
      at = start + text.length;
      out.push({ text, field: text.includes(':') ? text.replace(/^-/, '').slice(0, text.replace(/^-/, '').indexOf(':')) : undefined,
        negated: text.startsWith('-'), start, end: at });
    }
    return out;
  };

  beforeEach(() => {
    vi.restoreAllMocks();
    ran = [];
    posts = [];
    useQueryStore.getState().set('folder:inbox');
    useViewerStore.getState().clear();
    useSelectionStore.getState().clear();
    vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      const params = new URL(url, 'http://x').searchParams;
      if (init?.method === 'POST') posts.push({ url, body: JSON.parse(String(init.body)) });
      let body: any = {};
      if (url.startsWith('/api/query?')) {
        const q = params.get('q') ?? '';
        ran.push(q);
        body = q.includes('| count by')
          ? { kind: 'groups', count: 2, groups: [{ label: 'shop.example', value: 25 }, { label: 'acme.com', value: 25 }] }
          : { query: q, kind: 'messages', count: 50, messages: page };
      } else if (url.startsWith('/api/query/terms')) {
        body = { terms: terms(params.get('q') ?? ''), stages: [] };
      } else if (url.startsWith('/api/query/compose')) {
        const q = params.get('q') ?? '';
        if (params.get('add')) body = { query: `${q} ${params.get('add')}`.trim() };
        else if (params.get('remove')) body = { query: q.split(' ').filter(t => t !== params.get('remove')).join(' ') };
        else if (params.get('dropStage')) body = { query: q.replace(/\s*\|.*$/, '') };
        else body = { query: q };
      } else if (url.startsWith('/api/messages/bulk/count')) {
        body = { count: 120, max: 50000 };
      } else if (url === '/api/messages/bulk/undo') {
        body = { changed: 120 };
      } else if (url === '/api/messages/bulk') {
        body = { matched: 120, changed: 118, undo: { labels: [{ ids: ['m1'], add: ['inbox'] }] } };
      } else if (url.startsWith('/api/search')) {
        body = { query: params.get('q'), groups: [
          { id: 'mail', label: 'Mail', query: 'acme', total: 23, items: { messages: page.slice(0, 2) } },
          { id: 'cards', label: 'Cards', query: 'in:tickets acme', total: 1, items: { tickets: [{ id: 't1', title: 'Pay Acme', status: 'todo' }] } },
          { id: 'events', label: 'Events', query: 'in:events acme', total: 0, error: 'acme: not something events have' },
        ] };
      } else if (url.startsWith('/api/queries')) body = [];
      else if (url.startsWith('/api/labels')) body = { labels: [] };
      return { ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body } as Response;
    });
  });

  it('keeps the folder in front of what is typed', async () => {
    render(<Desk />);
    expect(await screen.findByLabelText('Inside Inbox')).toBeTruthy();
    const box = screen.getByLabelText('Query') as HTMLTextAreaElement;
    await waitFor(() => expect(box.value).toBe(''));
    fireEvent.change(box, { target: { value: 'from:bob' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(ran).toContain('folder:inbox from:bob'));
  });

  it('adds a quick filter, and the other in its group comes off', async () => {
    render(<Desk />);
    const filters = await screen.findByRole('group', { name: 'Quick filters' });
    fireEvent.click(within(filters).getByText('Low'));
    await waitFor(() => expect(ran).toContain('folder:inbox importance:low'));
    await waitFor(() => expect(within(filters).getByText('Low').getAttribute('aria-pressed')).toBe('true'));
    fireEvent.click(within(filters).getByText('Important'));
    await waitFor(() => expect(ran[ran.length - 1]).toBe('folder:inbox importance:important'));
  });

  it('selects every match, acts on all of them, and undoes it', async () => {
    render(<Desk />);
    await screen.findByText('Message 0');
    fireEvent.click(screen.getByTitle('Select all'));
    fireEvent.click(await screen.findByText('Select all 120 in Inbox'));
    const bar = await screen.findByRole('region', { name: 'Every match selected' });
    expect(within(bar).getByText('All 120 in Inbox selected')).toBeTruthy();
    fireEvent.click(within(bar).getByText('Archive'));
    await waitFor(() => expect(posts.some(p => p.url === '/api/messages/bulk')).toBe(true));
    expect(posts.find(p => p.url === '/api/messages/bulk')!.body).toEqual({ query: 'folder:inbox', action: 'archive', label: '' });
    expect(await screen.findByText('Archived 118 of 120.')).toBeTruthy();
    fireEvent.click(screen.getByText('Undo'));
    await waitFor(() => expect(posts.some(p => p.url === '/api/messages/bulk/undo')).toBe(true));
    expect(posts.find(p => p.url === '/api/messages/bulk/undo')!.body.undo.labels[0].add).toEqual(['inbox']);
  });

  it('groups the folder and clears a whole group at once', async () => {
    render(<Desk />);
    await screen.findByText('Message 0');
    fireEvent.change(screen.getByLabelText('Group by'), { target: { value: 'domain' } });
    await waitFor(() => expect(ran).toContain('folder:inbox | count by domain'));
    const groups = await screen.findByRole('list', { name: 'Groups' });
    const shop = within(groups).getByText('shop.example').closest('[role="listitem"]') as HTMLElement;
    fireEvent.click(within(shop).getByText('Archive all'));
    await waitFor(() => expect(posts.some(p => p.url === '/api/messages/bulk')).toBe(true));
    expect(posts.find(p => p.url === '/api/messages/bulk')!.body.query).toBe('folder:inbox from:*@shop.example');
  });

  it('searches every kind from All Items, and says what it left out', async () => {
    render(<Desk />);
    fireEvent.click(await screen.findByText('All Items'));
    expect(await screen.findByLabelText('Inside All Items')).toBeTruthy();
    const box = screen.getByLabelText('Query');
    fireEvent.change(box, { target: { value: 'acme' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(await screen.findByText('Pay Acme')).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Mail' })).toBeTruthy();
    expect(screen.getByText(/Events: not searched/)).toBeTruthy();
    // See all opens that kind as a folder.
    fireEvent.click(within(screen.getByRole('region', { name: 'Cards' })).getByText(/See all/));
    await waitFor(() => expect(ran).toContain('in:tickets acme'));
  });
});
