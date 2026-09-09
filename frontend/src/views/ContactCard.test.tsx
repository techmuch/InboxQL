import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ContactCard } from './ContactCard';
import { useViewerStore } from '../lib/tabs';
import { useQueryStore } from '../lib/filters';

describe('ContactCard', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useViewerStore.getState().clear();
    useQueryStore.getState().set('');
  });

  const contact = {
    address: 'alice@acme.com',
    headerName: 'Alice Alison',
    displayName: 'Alice A.',
    kind: 'person',
    kindSource: 'header',
    messages: 15,
    sent: 5,
    received: 10,
    lastSeen: '2026-03-01T10:00:00Z',
  };

  // Generate 12 correspondents to test complete list and filtering (>10)
  const peers = Array.from({ length: 12 }, (_, i) => ({
    label: `colleague${i + 1}@acme.com`,
    value: 12 - i,
  }));

  function mockServer(contactPeers = peers) {
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      let body: any = {};
      if (url.startsWith('/api/query?')) {
        const q = new URL(url, 'http://x').searchParams.get('q') ?? '';
        if (q.includes('in:contacts')) {
          body = { query: q, kind: 'contacts', count: 1, contacts: [contact] };
        } else if (q.includes('| count by anyone') || q.includes('| participants')) {
          // Include self and peers
          body = {
            query: q,
            kind: 'groups',
            count: contactPeers.length + 1,
            groups: [
              { label: 'alice@acme.com', value: 15 },
              ...contactPeers,
            ],
          };
        } else {
          body = { query: q, kind: 'groups', count: 0, groups: [] };
        }
      } else if (url.startsWith('/api/contacts')) {
        body = contact;
      }
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });
  }

  it('renders the complete list in the Appears alongside section', async () => {
    mockServer();
    render(<ContactCard address="alice@acme.com" />);

    expect(await screen.findByText('Alice A.')).toBeInTheDocument();
    // Header should reflect the complete count (12)
    expect(await screen.findByText('(12)')).toBeInTheDocument();

    // Every single colleague should be rendered in the document, not just top 8
    for (let i = 1; i <= 12; i++) {
      expect(screen.getByText(`colleague${i}@acme.com`)).toBeInTheDocument();
    }
  });

  it('filters correspondents when more than 10 are present', async () => {
    mockServer();
    render(<ContactCard address="alice@acme.com" />);

    const filterInput = await screen.findByPlaceholderText('Filter correspondents…');
    expect(filterInput).toBeInTheDocument();

    fireEvent.change(filterInput, { target: { value: 'colleague12' } });

    expect(screen.getByText('colleague12@acme.com')).toBeInTheDocument();
    expect(screen.queryByText('colleague1@acme.com')).not.toBeInTheDocument();
  });

  it('opens contact card when clicking a correspondent', async () => {
    mockServer();
    render(<ContactCard address="alice@acme.com" />);

    const peer = await screen.findByText('colleague1@acme.com');
    fireEvent.click(peer);

    await waitFor(() => {
      expect(useViewerStore.getState().contact).toBe('colleague1@acme.com');
    });
  });

  it('shows empty message when there are no correspondents', async () => {
    mockServer([]);
    render(<ContactCard address="alice@acme.com" />);

    expect(await screen.findByText('Nobody — every message is one-to-one.')).toBeInTheDocument();
  });
});
