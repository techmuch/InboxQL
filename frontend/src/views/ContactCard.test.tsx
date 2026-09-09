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
      } else if (url.includes('/api/contacts/responsiveness')) {
        body = {
          address: 'alice@acme.com',
          myMedianReplySecs: 1800,
          theirMedianReplySecs: 3600,
          awaitingMyReplyCount: 1,
          awaitingTheirReplyCount: 0,
          awaitingMyReplyThreads: [
            { threadKey: 't1', subject: 'Project launch', lastMessageAt: '2026-03-01T10:00:00Z', snippet: 'Are we ready?' }
          ],
          awaitingTheirReplyThreads: [],
          toCount: 10,
          ccCount: 2,
          toRatio: 0.83,
          hourlyDistribution: [0, 0, 0, 0, 0, 0, 0, 0, 2, 5, 3, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        };
      } else if (url.includes('/api/contacts/tags')) {
        body = { address: 'alice@acme.com', tags: ['vip', 'client'] };
      } else if (url.includes('/api/contacts/notes')) {
        body = { address: 'alice@acme.com', notes: 'Updated note' };
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

  it('renders communication dynamics metrics and open loops', async () => {
    mockServer();
    render(<ContactCard address="alice@acme.com" />);

    expect(await screen.findByText('Communication Dynamics')).toBeInTheDocument();
    expect(await screen.findByText('Your Turnaround')).toBeInTheDocument();
    expect(await screen.findByText('30m')).toBeInTheDocument(); // 1800s
    expect(await screen.findByText('1h')).toBeInTheDocument(); // 3600s
    expect(await screen.findByText('83% Direct')).toBeInTheDocument();

    // Open loops awaiting your reply
    expect(await screen.findByText(/Awaiting your reply/)).toBeInTheDocument();
    expect(await screen.findByText('Project launch')).toBeInTheDocument();
  });

  it('renders private notes and supports editing', async () => {
    mockServer();
    render(<ContactCard address="alice@acme.com" />);

    expect(await screen.findByText('Private Notes')).toBeInTheDocument();
    const textarea = screen.getByPlaceholderText(/Private notes \(markdown supported\)/);
    expect(textarea).toBeInTheDocument();

    fireEvent.change(textarea, { target: { value: 'Updated test notes' } });
    const saveButton = screen.getByRole('button', { name: /Save note/i });
    expect(saveButton).not.toBeDisabled();
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(screen.getByText('Saved')).toBeInTheDocument();
    });
  });

  it('renders tag controls and supports adding a tag', async () => {
    mockServer();
    render(<ContactCard address="alice@acme.com" />);

    const tagButton = await screen.findByRole('button', { name: /Tag/i });
    expect(tagButton).toBeInTheDocument();
    fireEvent.click(tagButton);

    const tagInput = screen.getByPlaceholderText('tag name...');
    expect(tagInput).toBeInTheDocument();

    fireEvent.change(tagInput, { target: { value: 'client' } });
    const addButton = screen.getByRole('button', { name: 'Add' });
    fireEvent.click(addButton);

    await waitFor(() => {
      expect(screen.getByText('client')).toBeInTheDocument();
    });
  });
});
