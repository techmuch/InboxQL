import { describe, it, expect, vi, beforeEach } from 'vitest';
import { cleanup, configure, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { Desk } from './index';
import { useQueryStore } from '../../lib/filters';
import { useViewerStore } from '../../lib/tabs';
import { useSelectionStore } from '../../lib/selection';

/**
 * Working through the mail one person or topic at a time: focusing on one,
 * getting back to the plain folder in one step, and going back to the view
 * you chose it from.
 */
// Each step here is several round trips through the mocked server (terms,
// compose, the query), which the whole suite running at once can slow past
// the default second.
configure({ asyncUtilTimeout: 5000 });
vi.setConfig({ testTimeout: 20000 });

describe('Desk focus and return', () => {
  const page = Array.from({ length: 50 }, (_, i) => ({
    id: `m${i}`, from: i % 2 ? 'news@shop.example' : 'alice@acme.com', subject: `Message ${i}`,
    date: '2026-03-01T10:00:00Z', flags: [] as string[], labels: ['inbox'],
  }));
  let ran: string[] = [];
  let slowCompose = false;
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
    slowCompose = false;
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
        const q = params.get('q') ?? '';
        body = { terms: terms(q), stages: q.includes('| timeline') ? [{ verb: 'timeline', text: 'timeline', start: q.indexOf('timeline'), end: q.length }] : [] };
      } else if (url.startsWith('/api/query/compose')) {
        const q = params.get('q') ?? '';
        if (slowCompose) await new Promise(r => setTimeout(r, 300));
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

  const current = () => useQueryStore.getState().text;
  const status = () => screen.getAllByRole('status').map(s => s.textContent).join(' ');

  it('offers a way back to all of the folder only when something is added, and keeps the conversation view', async () => {
    useQueryStore.getState().set('folder:inbox | timeline');
    const { unmount } = render(<Desk />);
    await screen.findByText((_, el) => el?.hasAttribute('data-scope-chip') === true && el.textContent?.startsWith('Inside Inbox') === true);
    await waitFor(() => expect(ran.length).toBeGreaterThan(0));
    expect(screen.queryByRole('button', { name: 'Show all of Inbox' })).toBeNull();
    unmount();

    useQueryStore.getState().set('folder:inbox from:bob | timeline');
    render(<Desk />);
    fireEvent.click(await screen.findByRole('button', { name: 'Show all of Inbox' }));
    await waitFor(() => expect(current()).toBe('folder:inbox | timeline'));
    await waitFor(() => expect(status()).toContain('Showing all of Inbox'));
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Show all of Inbox' })).toBeNull());
  });

  it('shows all of the folder you are in when it is clicked again, ungrouped', async () => {
    useQueryStore.getState().set('folder:inbox label:banking');
    render(<Desk />);
    await screen.findByText('Message 0');
    fireEvent.change(screen.getByLabelText('Group by'), { target: { value: 'domain' } });
    await screen.findByRole('list', { name: 'Groups' });
    const inbox = screen.getAllByRole('button').find(b => b.getAttribute('aria-current') === 'page')!;
    expect(inbox.textContent).toContain('Inbox');
    expect(inbox.title).toMatch(/Show all of Inbox/);
    fireEvent.click(inbox);
    await waitFor(() => expect(current()).toBe('folder:inbox'));
    expect((screen.getByLabelText('Group by') as HTMLSelectElement).value).toBe('');
  });

  it('Escape clears the selection first, then what was added', async () => {
    useQueryStore.getState().set('folder:inbox from:bob');
    render(<Desk />);
    const row = (await screen.findByText('Message 0')).closest('[role="option"]') as HTMLElement;
    fireEvent.click(row, { metaKey: true });
    expect(Object.keys(useSelectionStore.getState().refs)).toHaveLength(1);
    fireEvent.keyDown(row, { key: 'Escape' });
    expect(Object.keys(useSelectionStore.getState().refs)).toHaveLength(0);
    expect(current()).toBe('folder:inbox from:bob');
    fireEvent.keyDown(row, { key: 'Escape' });
    await waitFor(() => expect(current()).toBe('folder:inbox'));
  });

  it('focuses on a sender from a row, then goes back by button', async () => {
    render(<Desk />);
    await screen.findByText('Message 0');
    const back = screen.getByRole('button', { name: /^Back/ }) as HTMLButtonElement;
    expect(back.disabled).toBe(true);

    fireEvent.click(screen.getAllByTitle('Everything from alice@acme.com (f)')[0]);
    await waitFor(() => expect(current()).toBe('folder:inbox from:=alice@acme.com'));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Back to Inbox' })).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: 'Back to Inbox' }));
    await waitFor(() => expect(current()).toBe('folder:inbox'));
    await waitFor(() => expect(status()).toContain('Back to Inbox'));
    // Going back is not a step of its own.
    await waitFor(() => expect((screen.getByRole('button', { name: /^Back/ }) as HTMLButtonElement).disabled).toBe(true));

  });

  it('focuses on a sender with f, and goes back with u and Alt+Left', async () => {
    render(<Desk />);
    await screen.findByText('Message 1');
    const row = () => screen.getByText('Message 1').closest('[role="option"]') as HTMLElement;
    row().focus();
    fireEvent.keyDown(row(), { key: 'f' });
    await waitFor(() => expect(current()).toBe('folder:inbox from:=news@shop.example'));
    fireEvent.keyDown(await screen.findByText('Message 1'), { key: 'u' });
    await waitFor(() => expect(current()).toBe('folder:inbox'));
    row().focus();
    fireEvent.keyDown(row(), { key: 'f' });
    await waitFor(() => expect(current()).toBe('folder:inbox from:=news@shop.example'));
    // m: more like this one.
    row().focus();
    fireEvent.keyDown(row(), { key: 'm' });
    await waitFor(() => expect(current()).toMatch(/^similar:m1$/));
    // Back from there is the sender view it came from, not the inbox.
    fireEvent.keyDown(await screen.findByText('Message 1'), { key: 'u' });
    await waitFor(() => expect(current()).toBe('folder:inbox from:=news@shop.example'));
    const alt = new KeyboardEvent('keydown', { key: 'ArrowLeft', altKey: true, bubbles: true, cancelable: true });
    (await screen.findByText('Message 1')).dispatchEvent(alt);
    await waitFor(() => expect(current()).toBe('folder:inbox'));
    expect(alt.defaultPrevented).toBe(true);
  });

  it('never takes u or Escape from the query box', async () => {
    useQueryStore.getState().set('folder:inbox from:bob');
    render(<Desk />);
    await screen.findByText('Message 0');
    const box = screen.getByLabelText('Query');
    fireEvent.keyDown(box, { key: 'Escape' });
    fireEvent.keyDown(box, { key: 'u' });
    expect(current()).toBe('folder:inbox from:bob');
  });

  it('goes back from a group to the grouped view it was chosen from', async () => {
    render(<Desk />);
    await screen.findByText('Message 0');
    fireEvent.change(screen.getByLabelText('Group by'), { target: { value: 'domain' } });
    const groups = await screen.findByRole('list', { name: 'Groups' });
    fireEvent.click(within(groups).getByRole('button', { name: 'Show acme.com' }));
    await waitFor(() => expect(current()).toBe('folder:inbox from:*@acme.com'));
    const back = await screen.findByRole('button', { name: 'Back to Inbox, by domain' });
    fireEvent.click(back);
    await waitFor(() => expect(current()).toBe('folder:inbox'));
    expect((screen.getByLabelText('Group by') as HTMLSelectElement).value).toBe('domain');
    expect(await screen.findByRole('list', { name: 'Groups' })).toBeTruthy();
  });

  it('shows everything carrying a label from its chip', async () => {
    page[0].labels = ['inbox', 'Receipts'];
    render(<Desk />);
    await screen.findByText('Message 0');
    fireEvent.click(screen.getByTitle('Show everything labelled Receipts'));
    await waitFor(() => expect(current()).toBe('folder:inbox label:Receipts'));
    page[0].labels = ['inbox'];
  });

  it('names each row with what its mouse-only controls show', async () => {
    page[0].labels = ['inbox', 'Receipts'];
    page[0].flags = ['\\Flagged'];
    render(<Desk />);
    const row = (await screen.findByText('Message 0')).closest('[role="option"]') as HTMLElement;
    // Unread and starred, and its labels, are part of what the row is called…
    expect(row.getAttribute('aria-label')).toMatch(/^Unread, Starred\. alice@acme\.com\. Message 0\. labels Receipts\. /);
    // A subject that ends a sentence is not given a second full stop.
    page[1].subject = 'Still there?';
    cleanup();
    render(<Desk />);
    const asked = (await screen.findByText('Still there?')).closest('[role="option"]') as HTMLElement;
    expect(asked.getAttribute('aria-label')).toMatch(/Still there\? \S/);
    page[1].subject = 'Message 1';
    // …and the controls themselves are hidden from assistive technology.
    expect(within(row).queryAllByRole('button')).toHaveLength(0);
    expect(within(row).queryAllByRole('checkbox')).toHaveLength(0);
    // Nothing inside the row takes focus: the row is the control.
    expect(row.querySelectorAll('button, input, [tabindex]')).toHaveLength(0);
    page[0].labels = ['inbox'];
    page[0].flags = [];
  });

  it('lets a later click win over a slower, earlier one', async () => {
    render(<Desk />);
    await screen.findByText('Message 0');
    fireEvent.change(screen.getByLabelText('Group by'), { target: { value: 'domain' } });
    const groups = await screen.findByRole('list', { name: 'Groups' });
    slowCompose = true;
    fireEvent.click(within(groups).getByRole('button', { name: 'Show acme.com' }));
    // Straight after, before the slow Show has come back.
    const inbox = screen.getAllByRole('button').find(b => b.getAttribute('aria-current') === 'page')!;
    fireEvent.click(inbox);
    await new Promise(r => setTimeout(r, 600));
    expect(current()).toBe('folder:inbox');
    expect((screen.getByLabelText('Group by') as HTMLSelectElement).value).toBe('');
  });

  it('drops a selection when the view changes, so nothing off screen can be acted on', async () => {
    render(<Desk />);
    const row = (await screen.findByText('Message 0')).closest('[role="option"]') as HTMLElement;
    fireEvent.click(row, { metaKey: true });
    expect(Object.keys(useSelectionStore.getState().refs)).toHaveLength(1);
    expect(screen.getByText(/1 message selected/)).toBeTruthy();
    // Another folder: the message ticked here is not on screen there.
    fireEvent.click(screen.getByRole('button', { name: /^Drafts/ }));
    await waitFor(() => expect(current()).toMatch(/^folder:drafts/));
    await waitFor(() => expect(Object.keys(useSelectionStore.getState().refs)).toHaveLength(0));
    expect(screen.queryByText(/1 message selected/)).toBeNull();
  });
});
