import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Desk } from './index';
import { useQueryStore } from '../../lib/filters';
import { useViewerStore } from '../../lib/tabs';
import { useSelectionStore } from '../../lib/selection';

/**
 * Contacts were reachable only by knowing to type `in:contacts`.
 *
 * The entity, the fields, the compiler and the renderer all existed and
 * nothing in the UI pointed at any of it — so these assert the doors, not the
 * capability.
 */
describe('Desk contacts', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useQueryStore.getState().set('folder:inbox');
    useViewerStore.getState().clear();
    useSelectionStore.getState().clear();
  });

  const contacts = [
    {
      address: 'sarah@acme.com', headerName: 'Sarah Fullmer', kind: 'person',
      messages: 75, sent: 0, received: 75, lastSeen: '2026-03-01T10:00:00Z',
    },
    {
      address: 'noreply@news.test', headerName: 'News', kind: 'system',
      messages: 4, sent: 4, received: 0, lastSeen: '2026-02-01T10:00:00Z',
    },
  ];

  function mockServer(onQuery?: (q: string) => void) {
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      let body: any = {};
      if (url.startsWith('/api/query?')) {
        const q = new URL(url, 'http://x').searchParams.get('q') ?? '';
        onQuery?.(q);
        body = q.includes('in:contacts')
          ? { query: q, kind: 'contacts', count: contacts.length, contacts }
          : { query: q, kind: 'messages', count: 0, messages: [] };
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
  }

  it('offers contacts in the rail', async () => {
    mockServer();
    render(<Desk />);

    // The gap that shipped: every one of these existed as a query and none of
    // them existed as a thing you could click.
    for (const label of ['People', 'Systems', 'Unclassified']) {
      expect(await screen.findByText(label)).toBeInTheDocument();
    }
  });

  it('runs a contact query when a rail entry is clicked', async () => {
    const queries: string[] = [];
    mockServer(q => queries.push(q));
    render(<Desk />);

    fireEvent.click(await screen.findByText('People'));

    await waitFor(() => {
      expect(queries).toContain('in:contacts kind:person');
    });
    expect(await screen.findByText('Sarah Fullmer')).toBeInTheDocument();
  });

  it('renders contacts with their kind and derived counts', async () => {
    mockServer();
    useQueryStore.getState().set('in:contacts');
    render(<Desk />);

    expect(await screen.findByText('Sarah Fullmer')).toBeInTheDocument();
    expect(screen.getByText('sarah@acme.com')).toBeInTheDocument();
    // System is the badge worth seeing at a glance: it is why a contact list
    // is not just an address book.
    expect(screen.getByText('system')).toBeInTheDocument();
    expect(screen.getByText('75')).toBeInTheDocument();
  });

  it('opens the contact rather than filtering by them', async () => {
    mockServer();
    useQueryStore.getState().set('in:contacts');
    render(<Desk />);

    fireEvent.click(await screen.findByText('Sarah Fullmer'));

    // Clicking a person's name should show you the person. It used to narrow
    // the query to their mail, which is a different question.
    await waitFor(() => {
      expect(useViewerStore.getState().contact).toBe('sarah@acme.com');
    });
  });

  it('lets contacts be selected like any other row', async () => {
    mockServer();
    useQueryStore.getState().set('in:contacts');
    render(<Desk />);

    const row = (await screen.findByText('Sarah Fullmer')).closest('[data-nav-row]') as HTMLElement;
    fireEvent.click(row, { metaKey: true });

    expect(await screen.findByText('1 contact selected')).toBeInTheDocument();
  });
});
