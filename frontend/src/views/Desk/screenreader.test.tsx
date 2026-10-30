import { afterEach, describe, it, expect, vi, beforeEach } from 'vitest';
import { configure, render, screen, waitFor } from '@testing-library/react';
import { virtual } from '@guidepup/virtual-screen-reader';
import { Desk } from './index';
import { useQueryStore } from '../../lib/filters';
import { useViewerStore } from '../../lib/tabs';
import { useSelectionStore } from '../../lib/selection';

/**
 * What a screen reader says, not what the markup claims.
 *
 * Guidepup's virtual screen reader walks the page by the ARIA rules a screen
 * reader follows — names, roles, states, landmarks, live regions — and keeps
 * a log of what it would speak. So these assert the spoken output of the
 * one-person-at-a-time loop: a row's name, the keys it is described with,
 * focusing on a sender, going back, and clearing.
 */
// Each step here is several round trips through the mocked server (terms,
// compose, the query), which the whole suite running at once can slow past
// the default second.
configure({ asyncUtilTimeout: 5000 });
vi.setConfig({ testTimeout: 20000 });

describe('Desk, as a screen reader hears it', () => {
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
  const spoken = async () => (await virtual.spokenPhraseLog()).join('\n');

  afterEach(async () => { await virtual.stop(); });

  it('reads a row in one breath, with its state and the list\'s keys', async () => {
    page[0].flags = ['\\Flagged'];
    page[0].labels = ['inbox', 'Receipts'];
    render(<Desk />);
    await screen.findByText('Message 0');
    await virtual.start({ container: document.body });
    const row = screen.getByText('Message 0').closest('[role="option"]') as HTMLElement;
    row.focus();
    await waitFor(async () => expect(await spoken()).toMatch(/Unread, Starred\. alice@acme\.com\. Message 0\. labels Receipts\./));
    // The row's own words, not its preview text or the buttons drawn on it.
    const said = (await virtual.spokenPhraseLog()).find(p => p.includes('Message 0'))!;
    expect(said).not.toMatch(/Everything from|Find messages like|Star \(s\)/);
    expect(said).toMatch(/option/);
    page[0].flags = [];
    page[0].labels = ['inbox'];
  });

  it('announces focusing on a sender, going back, and clearing', async () => {
    useQueryStore.getState().set('folder:inbox');
    render(<Desk />);
    await screen.findByText('Message 1');
    await virtual.start({ container: document.body });
    const row = () => screen.getByText('Message 1').closest('[role="option"]') as HTMLElement;
    row().focus();
    await virtual.press('f');
    await waitFor(() => expect(current()).toBe('folder:inbox from:=news@shop.example'));
    await waitFor(async () => expect(await spoken()).toMatch(/Showing everything from news@shop\.example/));

    // Clear and Back say where they go.
    await waitFor(() => expect(screen.getByRole('button', { name: 'Show all of Inbox' })).toBeTruthy());
    screen.getByRole('button', { name: 'Back to Inbox' }).focus();
    await waitFor(async () => expect(await virtual.lastSpokenPhrase()).toMatch(/^button, Back to Inbox/));
    screen.getByRole('button', { name: 'Show all of Inbox' }).focus();
    await waitFor(async () => expect(await virtual.lastSpokenPhrase()).toMatch(/^button, Show all of Inbox/));

    (await screen.findByText('Message 1')).closest<HTMLElement>('[role="option"]')!.focus();
    await virtual.press('u');
    await waitFor(() => expect(current()).toBe('folder:inbox'));
    await waitFor(async () => expect(await spoken()).toMatch(/Back to Inbox/));
  });

  it('can move between the rail and the Desk by landmark', async () => {
    render(<Desk />);
    await screen.findByText('Message 0');
    await virtual.start({ container: document.body });
    await virtual.perform(virtual.commands.moveToNextLandmark);
    await virtual.perform(virtual.commands.moveToNextLandmark);
    const log = await spoken();
    expect(log).toMatch(/navigation, Mailbox/);
    expect(log).toMatch(/region, Desk/);
  });
});
