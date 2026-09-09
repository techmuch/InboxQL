import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Desk } from './index';
import { useQueryStore } from '../../lib/filters';
import { useViewerStore } from '../../lib/tabs';
import { useSelectionStore } from '../../lib/selection';

/**
 * The regression these exist for.
 *
 * Desk's keyboard handler used to index into the `messages` array, and every
 * non-message result set that array to empty — so arrow keys silently did
 * nothing in conversations, aggregates, tickets and drafts. Nothing caught it
 * because there were no keyboard tests at all.
 */
describe('Desk keyboard navigation', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useQueryStore.getState().set('folder:inbox');
    useViewerStore.getState().clear();
    // The selection store is module-level and outlives a render, which is the
    // point — it survives a mode change. Tests have to say so.
    useSelectionStore.getState().clear();
  });

  const messages = [
    { id: 'm1', from: 'alice@acme.com', subject: 'Quarterly invoice', date: '2026-03-01T10:00:00Z', flags: [] },
    { id: 'm2', from: 'bob@acme.com', subject: 'Re: Quarterly invoice', date: '2026-03-05T10:00:00Z', flags: ['\\Seen'] },
  ];

  const threads = [
    {
      key: 'root@acme.com', subject: 'Quarterly invoice',
      participants: ['alice@acme.com', 'bob@acme.com'],
      messageCount: 2, ticketCount: 1, draftCount: 0,
      start: '2026-03-01T10:00:00Z', end: '2026-03-08T10:00:00Z',
      entries: [
        { kind: 'message', at: '2026-03-01T10:00:00Z', actor: 'alice@acme.com',
          summary: 'Quarterly invoice', message: messages[0] },
        { kind: 'event', at: '2026-03-02T10:00:00Z', actor: 'human',
          summary: 'ticket raised: Pay the invoice' },
        { kind: 'message', at: '2026-03-05T10:00:00Z', actor: 'bob@acme.com',
          summary: 'Re: Quarterly invoice', message: messages[1] },
      ],
    },
    {
      key: 'other@acme.com', subject: 'Lunch',
      participants: ['carol@acme.com'], messageCount: 1, ticketCount: 0, draftCount: 0,
      start: '2026-03-03T10:00:00Z', end: '2026-03-03T10:00:00Z',
      entries: [],
    },
  ];

  /** Routes every request Desk makes on mount, so only the result kind varies. */
  function mockServer(result: unknown) {
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      const body =
        url.startsWith('/api/query?') ? result
        : url.startsWith('/api/query/terms') ? { terms: [], stages: [] }
        : url.startsWith('/api/queries') ? []
        : url.startsWith('/api/messages/counts') ? {}
        : {};
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });
  }

  const pane = () => document.querySelector('[tabindex]')!.closest('.flex-1') as HTMLElement;
  const press = (key: string, opts: Record<string, unknown> = {}) =>
    fireEvent.keyDown(document.activeElement ?? document.body, { key, bubbles: true, ...opts });

  it('walks the message list and previews without switching tabs', async () => {
    mockServer({ query: 'folder:inbox', kind: 'messages', count: 2, messages });
    render(<Desk />);

    const first = await screen.findByText('Quarterly invoice');
    const row = first.closest('[data-nav-row]') as HTMLElement;
    expect(row).toBeTruthy();

    row.focus();
    expect(document.activeElement).toBe(row);
    // Preview follows focus, so an already-open viewer keeps up.
    expect(useViewerStore.getState().messageId).toBe('m1');

    press('ArrowDown');
    await waitFor(() => expect(useViewerStore.getState().messageId).toBe('m2'));
    // Moving selects, so the checkbox column agrees with the keyboard.
    expect(document.activeElement).toHaveAttribute('data-sel-id', 'm2');
  });

  it('extends the selection with Shift and arrow', async () => {
    mockServer({ query: 'folder:inbox', kind: 'messages', count: 2, messages });
    render(<Desk />);

    const row = (await screen.findByText('Quarterly invoice')).closest('[data-nav-row]') as HTMLElement;
    row.focus();
    press('ArrowDown', { shiftKey: true });

    await waitFor(() => {
      const selected = document.querySelectorAll('[data-sel-kind="message"][aria-selected="true"]');
      expect(selected).toHaveLength(2);
      expect(selected[0].className).toContain('bg-primary/10');
      expect(selected[0].className).toContain('ring-1 ring-inset ring-primary/30');
      expect(selected[1].className).toContain('bg-primary/20');
      expect(selected[1].className).toContain('ring-1 ring-inset ring-primary/50');
    });
  });

  // The reported bug: arrows went dead the moment `| timeline` was in use.
  it('walks conversations, and into an expanded one', async () => {
    mockServer({ query: 'folder:inbox | timeline', kind: 'threads', count: 2, threads });
    render(<Desk />);

    const header = (await screen.findByText('Quarterly invoice')).closest('[data-nav-row]') as HTMLElement;
    expect(header).toBeTruthy();
    header.focus();

    // Collapsed, Down goes to the next conversation rather than nowhere.
    press('ArrowDown');
    expect(document.activeElement).toHaveTextContent('Lunch');

    // Right expands, and then Down walks the moments inside it — the message,
    // the ticket it raised, and the reply, in one sequence.
    header.focus();
    press('ArrowRight');
    await waitFor(() => expect(header).toHaveAttribute('aria-expanded', 'true'));

    press('ArrowDown');
    expect(document.activeElement).toHaveTextContent('Quarterly invoice');
    press('ArrowDown');
    expect(document.activeElement).toHaveTextContent('ticket raised: Pay the invoice');
    press('ArrowDown');
    expect(document.activeElement).toHaveTextContent('Re: Quarterly invoice');

    // A message inside a conversation previews like one in the list does.
    await waitFor(() => expect(useViewerStore.getState().messageId).toBe('m2'));

    // Left climbs back out to the conversation that owns it.
    press('ArrowLeft');
    expect(document.activeElement).toBe(header);
  });

  it('walks aggregate rows', async () => {
    mockServer({
      query: 'folder:inbox | top domain 3', kind: 'groups', count: 2,
      groupField: 'domain',
      groups: [{ label: 'acme.com', value: 12 }, { label: 'stripe.com', value: 4 }],
    });
    render(<Desk />);

    const row = (await screen.findByText('acme.com')).closest('[data-nav-row]') as HTMLElement;
    expect(row).toBeTruthy();
    row.focus();

    press('ArrowDown');
    expect(document.activeElement).toHaveTextContent('stripe.com');
  });

  it('walks ticket rows', async () => {
    mockServer({
      query: 'status:todo', kind: 'tickets', count: 2,
      tickets: [
        { id: 't1', title: 'Pay the invoice', status: 'todo', origin: 'human', threadKey: 'root@acme.com' },
        { id: 't2', title: 'Renew the domain', status: 'todo', origin: 'human' },
      ],
    });
    render(<Desk />);

    const row = (await screen.findByText('Pay the invoice')).closest('[data-nav-row]') as HTMLElement;
    expect(row).toBeTruthy();
    row.focus();

    press('ArrowDown');
    expect(document.activeElement).toHaveTextContent('Renew the domain');
  });

  it('leaves the query bar alone while it is being typed in', async () => {
    mockServer({ query: 'folder:inbox', kind: 'messages', count: 2, messages });
    render(<Desk />);
    await screen.findByText('Quarterly invoice');

    const editor = document.querySelector('textarea') as HTMLTextAreaElement;
    expect(editor).toBeTruthy();
    editor.focus();
    press('ArrowDown');

    // Down in a text field moves the caret. Stealing it would throw you out of
    // a query you are halfway through writing.
    expect(document.activeElement).toBe(editor);
  });

  it('enters the list from the pane when nothing is focused', async () => {
    mockServer({ query: 'folder:inbox', kind: 'messages', count: 2, messages });
    render(<Desk />);
    await screen.findByText('Quarterly invoice');

    pane().focus();
    press('ArrowDown');
    expect(document.activeElement).toHaveAttribute('data-sel-id', 'm1');
  });
});

/**
 * A term and a stage are different halves of a query.
 *
 * Drilling into a ticket's conversation used to pass "thread:X | timeline" as
 * a single term, which lands correctly only when the query has no pipeline —
 * and produces two terminal stages when it has one, which the planner rejects.
 */
describe('Desk drill-down composition', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useQueryStore.getState().set('status:todo | count by week');
  });

  it('composes a term and a stage separately', async () => {
    const composed: string[] = [];
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      let body: unknown = {};
      if (url.startsWith('/api/query/compose')) {
        // Stand in for the server's composer well enough to record what it
        // was asked to do with each half.
        const params = new URL(url, 'http://x').searchParams;
        const q = params.get('q') ?? '';
        const add = params.get('add');
        const stage = params.get('stage');
        composed.push(add ? `add:${add}` : `stage:${stage}`);
        body = { query: add ? `${q} ${add}` : `${q} | ${stage}` };
      } else if (url.startsWith('/api/query?')) {
        body = {
          query: 'status:todo', kind: 'tickets', count: 1,
          tickets: [{ id: 't1', title: 'Pay the invoice', status: 'todo',
                      origin: 'human', threadKey: 'root@acme.com' }],
        };
      } else if (url.startsWith('/api/query/terms')) {
        body = { terms: [], stages: [] };
      } else if (url.startsWith('/api/queries')) {
        body = [];
      }
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });

    render(<Desk />);
    const row = (await screen.findByText('Pay the invoice')).closest('[data-nav-row]') as HTMLElement;
    fireEvent.click(row);

    await waitFor(() => {
      expect(composed).toEqual(['add:thread:root@acme.com', 'stage:timeline']);
    });
  });
});

/**
 * Selection is a property of Desk, not of the message list.
 *
 * It used to live inside the message-list branch, so it existed in exactly one
 * of the five modes this pane can be in.
 */
describe('Desk selection across modes', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useQueryStore.getState().set('folder:inbox');
    useViewerStore.getState().clear();
    useSelectionStore.getState().clear();
  });

  const server = (result: unknown, onCompose?: (url: string) => void) =>
    vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      let body: any = {};
      if (url.startsWith('/api/query?')) body = result;
      else if (url.startsWith('/api/query/terms')) body = { terms: [], stages: [] };
      else if (url.startsWith('/api/queries')) body = [];
      else if (url.startsWith('/api/query/compose')) {
        onCompose?.(url);
        body = { query: 'composed' };
      } else if (url.startsWith('/api/messages/flags')) {
        onCompose?.(url + '|' + String(init?.body));
        body = { changed: 2 };
      }
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });

  const press = (key: string, opts: Record<string, unknown> = {}) =>
    fireEvent.keyDown(document.activeElement ?? document.body, { key, bubbles: true, ...opts });

  it('selects conversations and narrows to them', async () => {
    const calls: string[] = [];
    server({
      query: 'folder:inbox | timeline', kind: 'threads', count: 2,
      threads: [
        { key: 'root@acme.com', subject: 'Quarterly invoice', participants: [],
          messageCount: 1, ticketCount: 0, draftCount: 0,
          start: '2026-03-01T10:00:00Z', end: '2026-03-01T10:00:00Z', entries: [] },
        { key: 'other@acme.com', subject: 'Lunch', participants: [],
          messageCount: 1, ticketCount: 0, draftCount: 0,
          start: '2026-03-03T10:00:00Z', end: '2026-03-03T10:00:00Z', entries: [] },
      ],
    }, url => calls.push(url));
    render(<Desk />);

    const first = (await screen.findByText('Quarterly invoice')).closest('[data-nav-row]') as HTMLElement;
    first.focus();
    press('ArrowDown', { shiftKey: true });

    // The bar exists in this mode at all, which it did not before, and it
    // counts conversations rather than calling them messages.
    expect(await screen.findByText('2 conversations selected')).toBeInTheDocument();

    fireEvent.click(screen.getByText('Narrow to these'));
    await waitFor(() => {
      const compose = calls.find(c => c.includes('/api/query/compose'));
      expect(compose).toBeTruthy();
      const params = new URL(compose!, 'http://x').searchParams;
      // The server assembles the term; the client sends the parts.
      expect(params.get('field')).toBe('thread');
      expect(params.getAll('values')).toEqual(['root@acme.com', 'other@acme.com']);
    });
  });

  it('offers bulk flag actions only for messages', async () => {
    const calls: string[] = [];
    server({
      query: 'folder:inbox', kind: 'messages', count: 2,
      messages: [
        { id: 'm1', from: 'a@x.com', subject: 'One', date: '2026-03-01T10:00:00Z', flags: [] },
        { id: 'm2', from: 'b@x.com', subject: 'Two', date: '2026-03-02T10:00:00Z', flags: [] },
      ],
    }, url => calls.push(url));
    render(<Desk />);

    const row = (await screen.findByText('One')).closest('[data-nav-row]') as HTMLElement;
    row.focus();
    press('ArrowDown', { shiftKey: true });

    expect(await screen.findByText('2 messages selected')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Mark read'));

    await waitFor(() => {
      const call = calls.find(c => c.includes('/api/messages/flags'));
      expect(call).toBeTruthy();
      const body = JSON.parse(call!.split('|')[1]);
      expect(body).toEqual({ ids: ['m1', 'm2'], flag: '\\Seen', on: true });
    });
  });

  it('does not offer flag actions for a conversation selection', async () => {
    server({
      query: 'folder:inbox | timeline', kind: 'threads', count: 1,
      threads: [{ key: 'root@acme.com', subject: 'Quarterly invoice', participants: [],
        messageCount: 1, ticketCount: 0, draftCount: 0,
        start: '2026-03-01T10:00:00Z', end: '2026-03-01T10:00:00Z', entries: [] }],
    });
    render(<Desk />);

    const row = (await screen.findByText('Quarterly invoice')).closest('[data-nav-row]') as HTMLElement;
    fireEvent.click(row);

    expect(await screen.findByText('1 conversation selected')).toBeInTheDocument();
    // A conversation has no read flag, and offering an action that cannot
    // apply is worse than not offering it.
    expect(screen.queryByText('Mark read')).not.toBeInTheDocument();
  });
});

/**
 * The viewer is a tab, so opening it hides the list.
 *
 * A modified click is a selection gesture, not a reading one — making it open
 * the viewer threw the user out of the list they were selecting in.
 */
describe('Desk click gestures', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useQueryStore.getState().set('folder:inbox');
    useViewerStore.getState().clear();
    useSelectionStore.getState().clear();
  });

  const messages = [
    { id: 'm1', from: 'a@x.com', subject: 'One', date: '2026-03-01T10:00:00Z', flags: [] },
    { id: 'm2', from: 'b@x.com', subject: 'Two', date: '2026-03-02T10:00:00Z', flags: [] },
  ];

  const mount = () => {
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      const body =
        url.startsWith('/api/query?') ? { query: 'folder:inbox', kind: 'messages', count: 2, messages }
        : url.startsWith('/api/query/terms') ? { terms: [], stages: [] }
        : url.startsWith('/api/queries') ? []
        : {};
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });
    render(<Desk />);
  };

  it('opens the viewer on a plain click', async () => {
    mount();
    const row = (await screen.findByText('One')).closest('[data-nav-row]') as HTMLElement;
    fireEvent.click(row);

    await waitFor(() => expect(useViewerStore.getState().messageId).toBe('m1'));
  });

  it('selects without opening on a modified click', async () => {
    mount();
    const first = (await screen.findByText('One')).closest('[data-nav-row]') as HTMLElement;
    const second = (await screen.findByText('Two')).closest('[data-nav-row]') as HTMLElement;

    fireEvent.click(first, { metaKey: true });
    fireEvent.click(second, { metaKey: true });

    expect(await screen.findByText('2 messages selected')).toBeInTheDocument();
    // The viewer was never pointed anywhere, so the list is still on screen.
    expect(useViewerStore.getState().messageId).toBeNull();

    // Cmd-clicking an already-selected row removes it.
    fireEvent.click(second, { metaKey: true });
    expect(await screen.findByText('1 message selected')).toBeInTheDocument();
  });
});
