import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MessageViewer, parseEmailAddress } from './MessageViewer';
import { useViewerStore } from '../lib/tabs';

describe('parseEmailAddress', () => {
  it('extracts address from various email formats', () => {
    expect(parseEmailAddress('alice@example.com')).toBe('alice@example.com');
    expect(parseEmailAddress('Alice Smith <alice@example.com>')).toBe('alice@example.com');
    expect(parseEmailAddress('<alice@example.com>')).toBe('alice@example.com');
    expect(parseEmailAddress('"Alice Smith" <alice@example.com>')).toBe('alice@example.com');
    expect(parseEmailAddress('mailto:alice@example.com')).toBe('alice@example.com');
    expect(parseEmailAddress('  bob@example.com  ')).toBe('bob@example.com');
    expect(parseEmailAddress('')).toBe('');
    expect(parseEmailAddress(null)).toBe('');
    expect(parseEmailAddress(undefined)).toBe('');
  });
});

describe('MessageViewer email address click navigation', () => {
  beforeEach(() => {
    useViewerStore.getState().clear();
    vi.restoreAllMocks();

    // Mock global fetch for ContactCard queries and attachment loads
    globalThis.fetch = vi.fn().mockImplementation(async (url: string) => {
      if (url.startsWith('/api/message/attachments')) {
        return {
          ok: true,
          json: async () => [],
        };
      }
      if (url.includes('/api/query')) {
        return {
          ok: true,
          json: async () => ({
            query: 'in:contacts',
            kind: 'contacts',
            contacts: [
              {
                address: 'alice@example.com',
                name: 'Alice Smith',
                kind: 'person',
                messages: 42,
                sent: 10,
                received: 32,
              },
            ],
            groups: [],
            topics: [],
          }),
        };
      }
      return { ok: true, json: async () => ({}) };
    });
  });

  const sampleMessage = {
    id: 'msg-123',
    subject: 'Project Update',
    date: new Date('2026-09-08T12:00:00Z').toISOString(),
    from: 'alice@example.com',
    to: ['bob@acme.com', 'Carol Danvers <carol@marvel.com>'],
    cc: ['david@acme.com'],
    body: 'Hello everyone,\nHere is the weekly update.',
  };

  it('clicking sender in header navigates to contact record and back button restores message', async () => {
    useViewerStore.getState().setMessage(sampleMessage);

    render(<MessageViewer />);

    // Initially shows the message subject and sender
    expect(screen.getByText('Project Update')).toBeInTheDocument();
    const fromButtons = screen.getAllByRole('button', { name: 'alice@example.com' });
    expect(fromButtons.length).toBeGreaterThan(0);

    // Click sender button in header
    fireEvent.click(fromButtons[0]);

    // useViewerStore should now have contact set to alice@example.com
    expect(useViewerStore.getState().contact).toBe('alice@example.com');
    expect(useViewerStore.getState().previousMessage).toEqual(sampleMessage);

    // ContactCard should be rendered with Back to message button
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Back to message/i })).toBeInTheDocument();
    });

    // Clicking "Back to message" should restore the message
    fireEvent.click(screen.getByRole('button', { name: /Back to message/i }));

    expect(useViewerStore.getState().message?.id).toBe('msg-123');
    expect(useViewerStore.getState().contact).toBeNull();
    expect(screen.getByText('Project Update')).toBeInTheDocument();
  });

  it('clicking recipient in To field navigates to contact record', async () => {
    useViewerStore.getState().setMessage(sampleMessage);

    render(<MessageViewer />);

    const bobBtn = screen.getByRole('button', { name: 'bob@acme.com' });
    expect(bobBtn).toBeInTheDocument();

    fireEvent.click(bobBtn);

    expect(useViewerStore.getState().contact).toBe('bob@acme.com');
    expect(useViewerStore.getState().previousMessage?.id).toBe('msg-123');
  });

  it('clicking recipient with display name in To field navigates using parsed address', async () => {
    useViewerStore.getState().setMessage(sampleMessage);

    render(<MessageViewer />);

    const carolBtn = screen.getByRole('button', { name: 'Carol Danvers <carol@marvel.com>' });
    expect(carolBtn).toBeInTheDocument();

    fireEvent.click(carolBtn);

    expect(useViewerStore.getState().contact).toBe('carol@marvel.com');
  });

  it('clicking recipient in Cc field navigates to contact record', async () => {
    useViewerStore.getState().setMessage(sampleMessage);

    render(<MessageViewer />);

    const davidBtn = screen.getByRole('button', { name: 'david@acme.com' });
    expect(davidBtn).toBeInTheDocument();

    fireEvent.click(davidBtn);

    expect(useViewerStore.getState().contact).toBe('david@acme.com');
  });

  it('clicking sender avatar navigates to contact record', async () => {
    useViewerStore.getState().setMessage(sampleMessage);

    render(<MessageViewer />);

    const avatar = screen.getByTestId('sender-avatar');
    expect(avatar).toBeInTheDocument();

    fireEvent.click(avatar);

    expect(useViewerStore.getState().contact).toBe('alice@example.com');
  });
});
