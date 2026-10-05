import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Desk } from './index';
import { useQueryStore } from '../../lib/filters';
import { useViewerStore } from '../../lib/tabs';
import { useSelectionStore } from '../../lib/selection';

/**
 * The bug these exist for.
 *
 * Only a message list paged. Every other kind returned early with `hasMore`
 * false — which was not "no infinite scroll", it was silent truncation:
 * `in:contacts` on a real mailbox showed 50 of 57, with nothing on screen
 * saying so and no way to reach the other seven.
 *
 * The planner has had OFFSET for every kind all along. Only the client stopped
 * asking for it.
 */
describe('Desk paging across result kinds', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useQueryStore.getState().set('in:contacts');
    useViewerStore.getState().clear();
    useSelectionStore.getState().clear();
  });

  const contact = (i: number) => ({
    address: `person${i}@acme.com`, headerName: `Person ${i}`, kind: 'person',
    messages: 100 - i, sent: 1, received: 1, lastSeen: '2026-03-01T10:00:00Z',
  });

  /** A server with `total` contacts, paging 50 at a time. */
  function mockPagedContacts(total: number) {
    const seen: number[] = [];
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      let body: unknown = {};
      if (url.startsWith('/api/query?')) {
        const offset = Number(new URL(url, 'http://x').searchParams.get('offset') ?? 0);
        seen.push(offset);
        const page = Array.from({ length: Math.max(0, Math.min(50, total - offset)) },
          (_, i) => contact(offset + i));
        body = { query: 'in:contacts', kind: 'contacts', count: page.length, contacts: page };
      } else if (url.startsWith('/api/query/terms')) body = { terms: [], stages: [] };
      else if (url.startsWith('/api/queries')) body = [];
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });
    return seen;
  }

  /**
   * The list's own scroll container.
   *
   * Found from the table rather than by class: the rail is also `overflow-auto`
   * and comes first in document order, so querying for the class picks the
   * sidebar and the test scrolls something nobody is looking at.
   */
  const scroller = () => {
    const table = document.querySelector('table');
    if (!table) throw new Error('no result table rendered');
    return table.closest('.overflow-auto') as HTMLElement;
  };

  /** Scroll to within the 1.5-viewport threshold the handler uses. */
  const scrollToEnd = (el: HTMLElement) => {
    Object.defineProperty(el, 'scrollHeight', { value: 1000, configurable: true });
    Object.defineProperty(el, 'clientHeight', { value: 500, configurable: true });
    Object.defineProperty(el, 'scrollTop', { value: 400, configurable: true });
    fireEvent.scroll(el);
  };

  it('asks for the next page when a contact list is scrolled', async () => {
    const offsets = mockPagedContacts(57);
    render(<Desk />);

    await screen.findByText('person0@acme.com');
    expect(offsets).toEqual([0]);

    // Past the threshold the handler uses: within 1.5 viewports of the end.
    scrollToEnd(scroller());

    await waitFor(() => expect(offsets).toContain(50));
  });

  // The seven that used to be unreachable.
  it('keeps the rows it already had when the next page arrives', async () => {
    mockPagedContacts(57);
    render(<Desk />);
    await screen.findByText('person0@acme.com');

    scrollToEnd(scroller());

    // The 57th, which no amount of scrolling could reach before.
    await waitFor(() => expect(screen.getByText('person56@acme.com')).toBeTruthy());
    // And the first is still there rather than replaced by the new page.
    expect(screen.getByText('person0@acme.com')).toBeTruthy();
  });

  /**
   * An aggregate has no next page, and that is a statement about aggregates:
   * paging a GROUP BY by row offset returns a different answer every time the
   * data moves. `Options.Offset` in the planner says the same from its side.
   */
  it('does not page an aggregate', async () => {
    const offsets: number[] = [];
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      let body: unknown = {};
      if (url.startsWith('/api/query?')) {
        offsets.push(Number(new URL(url, 'http://x').searchParams.get('offset') ?? 0));
        body = {
          query: 'x | count by from', kind: 'groups', count: 2, groupField: 'from',
          groups: [{ label: 'a@x.com', value: 9 }, { label: 'b@x.com', value: 4 }],
        };
      } else if (url.startsWith('/api/query/terms')) body = { terms: [], stages: [] };
      else if (url.startsWith('/api/queries')) body = [];
      return { ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body } as Response;
    });
    useQueryStore.getState().set('folder:inbox | count by from');
    render(<Desk />);

    await screen.findByText('a@x.com');
    scrollToEnd(scroller());

    await new Promise(r => setTimeout(r, 30));
    expect(offsets.filter(o => o > 0)).toEqual([]);
  });
});
