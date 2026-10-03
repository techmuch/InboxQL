import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, act } from '@testing-library/react';
import { Desk } from './index';
import { useQueryStore, openingQuery } from '../../lib/filters';
import { useViewerStore } from '../../lib/tabs';
import { useSelectionStore } from '../../lib/selection';
import { useThreadingStore } from '../../lib/threading';

/**
 * # What this guards
 *
 * The preference is a default, not a force. Forcing would break three things,
 * and the first is silent: a terminal aggregate would be replaced, so
 * "threading" a count would delete the count; entities with no conversations
 * would be asked for theirs; and the Threads button would have nothing left to
 * turn off.
 *
 * So these assert the two halves that matter — that it applies where it should,
 * and that it leaves alone everything it should not.
 */
describe('Desk threading preference', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    useViewerStore.getState().clear();
    useSelectionStore.getState().clear();
    useThreadingStore.setState({ threading: 'flat' });
  });

  /** Records every query the server was asked to run. */
  function mockServer(ran: string[]) {
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      const params = new URL(url, 'http://x').searchParams;
      let body: any = {};
      if (url.startsWith('/api/query?')) {
        const q = params.get('q') ?? '';
        ran.push(q);
        body = { query: q, kind: 'messages', count: 0, messages: [] };
      } else if (url.startsWith('/api/query/terms')) {
        // The stages a query carries, as the lexer sees them.
        const q = params.get('q') ?? '';
        const after = q.split('|').slice(1);
        body = {
          terms: [],
          stages: after.map(s => ({ verb: s.trim().split(/\s+/)[0] })),
        };
      } else if (url.startsWith('/api/query/compose')) {
        const q = params.get('q') ?? '';
        if (params.get('stage')) body = { query: `${q} | ${params.get('stage')}` };
        else if (params.get('dropStage')) {
          body = { query: q.split('|')[0].trim() };
        }
      } else if (url.startsWith('/api/annotators') || url.startsWith('/api/queries')) {
        body = [];
      }
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });
  }

  it('opens flat by default', () => {
    expect(openingQuery()).toBe('folder:inbox');
  });

  // Seeded rather than checked after the fact: an async check would run the
  // flat query first and replace it, which is a flash of the wrong list on
  // every load.
  it('opens threaded when the preference says so, with no flat query first', () => {
    localStorage.setItem('inboxql.threading', 'threaded');
    expect(openingQuery()).toBe('folder:inbox | timeline');

    const ran: string[] = [];
    mockServer(ran);
    useQueryStore.setState({ text: openingQuery() });
    render(<Desk />);

    return waitFor(() => {
      expect(ran.length).toBeGreaterThan(0);
      expect(ran[0]).toBe('folder:inbox | timeline');
    });
  });

  it('applies the stage when the preference is switched on with the Desk open', async () => {
    const ran: string[] = [];
    mockServer(ran);
    useQueryStore.setState({ text: 'folder:inbox' });
    render(<Desk />);
    await waitFor(() => expect(ran).toContain('folder:inbox'));

    act(() => useThreadingStore.getState().setThreading('threaded'));

    await waitFor(() => {
      expect(useQueryStore.getState().text).toBe('folder:inbox | timeline');
    });
  });

  it('removes it again when switched off', async () => {
    const ran: string[] = [];
    mockServer(ran);
    useThreadingStore.setState({ threading: 'threaded' });
    useQueryStore.setState({ text: 'folder:inbox | timeline' });
    render(<Desk />);
    await waitFor(() => expect(ran.length).toBeGreaterThan(0));

    act(() => useThreadingStore.getState().setThreading('flat'));

    await waitFor(() => {
      expect(useQueryStore.getState().text).toBe('folder:inbox');
    });
  });

  // The silent one: a query may end in only one terminal stage, and adding a
  // stage replaces whichever is there. Threading a count would delete it.
  it('leaves a counting query alone', async () => {
    const ran: string[] = [];
    mockServer(ran);
    useQueryStore.setState({ text: 'folder:inbox | count by week' });
    render(<Desk />);
    await waitFor(() => expect(ran.length).toBeGreaterThan(0));

    act(() => useThreadingStore.getState().setThreading('threaded'));

    // Given a moment to do the wrong thing.
    await new Promise(r => setTimeout(r, 60));
    expect(useQueryStore.getState().text).toBe('folder:inbox | count by week');
  });

  it('leaves a query about something other than mail alone', async () => {
    const ran: string[] = [];
    mockServer(ran);
    useQueryStore.setState({ text: 'in:contacts kind:person' });
    render(<Desk />);
    await waitFor(() => expect(ran.length).toBeGreaterThan(0));

    act(() => useThreadingStore.getState().setThreading('threaded'));

    await new Promise(r => setTimeout(r, 60));
    expect(useQueryStore.getState().text).toBe('in:contacts kind:person');
  });

  // The Threads button has to keep working. If the preference re-applied on
  // every query change it would put the stage straight back and the button
  // would appear broken.
  it('does not fight the Threads button', async () => {
    const ran: string[] = [];
    mockServer(ran);
    useThreadingStore.setState({ threading: 'threaded' });
    useQueryStore.setState({ text: 'folder:inbox | timeline' });
    render(<Desk />);
    await waitFor(() => expect(ran.length).toBeGreaterThan(0));

    // What pressing Threads does: drop the stage.
    act(() => useQueryStore.getState().set('folder:inbox'));

    await new Promise(r => setTimeout(r, 80));
    expect(useQueryStore.getState().text).toBe('folder:inbox');
  });
});

/**
 * # Coming back to mail
 *
 * A rail folder click composes onto the current query so a cross-filter
 * survives changing folder. From a contacts view that produced
 * `in:contacts kind:person folder:inbox` — still contacts, now with a term
 * that means nothing there, and unthreaded however the preference is set.
 *
 * A folder is a statement about mail, so switching kind starts over.
 */
describe('Desk rail across kinds', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    useViewerStore.getState().clear();
    useSelectionStore.getState().clear();
    useThreadingStore.setState({ threading: 'flat' });
  });

  function mockServer(ran: string[]) {
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      const params = new URL(url, 'http://x').searchParams;
      let body: any = {};
      if (url.startsWith('/api/query?')) {
        const q = params.get('q') ?? '';
        ran.push(q);
        body = { query: q, kind: q.includes('in:contacts') ? 'contacts' : 'messages', count: 0, messages: [], contacts: [] };
      } else if (url.startsWith('/api/query/terms')) {
        const q = params.get('q') ?? '';
        body = { terms: [], stages: q.split('|').slice(1).map(x => ({ verb: x.trim().split(/\s+/)[0] })) };
      } else if (url.startsWith('/api/query/compose')) {
        const q = params.get('q') ?? '';
        body = { query: `${q} ${params.get('term') ?? ''}`.trim() };
      } else if (url.startsWith('/api/annotators') || url.startsWith('/api/queries')) {
        body = [];
      }
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });
  }

  it('returns to a threaded mail query when leaving contacts', async () => {
    localStorage.setItem('inboxql.threading', 'threaded');
    useThreadingStore.setState({ threading: 'threaded' });
    const ran: string[] = [];
    mockServer(ran);
    useQueryStore.setState({ text: 'in:contacts kind:person' });

    const { getByText } = render(<Desk />);
    await waitFor(() => expect(ran.length).toBeGreaterThan(0));

    act(() => { getByText('Inbox').click(); });

    await waitFor(() => {
      expect(useQueryStore.getState().text).toBe('folder:inbox | timeline');
    });
  });

  it('returns to a flat mail query when that is the preference', async () => {
    const ran: string[] = [];
    mockServer(ran);
    useQueryStore.setState({ text: 'in:attachments filetype:pdf' });

    const { getByText } = render(<Desk />);
    await waitFor(() => expect(ran.length).toBeGreaterThan(0));

    act(() => { getByText('Inbox').click(); });

    await waitFor(() => {
      expect(useQueryStore.getState().text).toBe('folder:inbox');
    });
  });
});
