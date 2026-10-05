import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MessageViewer, parseEmailAddress } from './MessageViewer';
import { useViewerStore } from '../lib/tabs';
import { useAttachmentTargetStore } from '../lib/attachmentTarget';

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

/**
 * Where an attachment chip puts its preview.
 *
 * A file has two homes — inline on the message it arrived on, and the viewer's
 * own file slot — and the application used to answer for the user. These check
 * that the preference is obeyed, and that the one combination it cannot honour
 * degrades to something rather than to nothing.
 */
describe('MessageViewer attachment previews', () => {
  const message = {
    id: 'm1', from: 'alice@example.com', to: ['bob@example.com'],
    subject: 'Invoice attached', date: '2026-03-01T10:00:00Z',
    body: 'See attached.', flags: ['\\Seen'],
  };

  const attachment = {
    id: 'a1', filename: 'invoice.pdf', mimeType: 'application/pdf',
    size: 63500, contentHash: 'a'.repeat(64),
  };

  const file = {
    key: 'a'.repeat(64), contentHash: 'a'.repeat(64), filename: 'invoice.pdf',
    mimeType: 'application/pdf', size: 63500, inline: false,
    storagePath: '/data/attachments/aa/aaa', messageId: 'm1',
    messages: 1, threads: 1, names: 1,
  };

  /** Counts the file-by-key loads, which is how "went to the viewer" is seen. */
  let fileLoads: number;

  beforeEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    useViewerStore.getState().clear();
    useAttachmentTargetStore.setState({ target: 'message' });
    fileLoads = 0;

    globalThis.fetch = vi.fn().mockImplementation(async (input: unknown) => {
      const url = String(input);
      if (url.startsWith('/api/message/attachments')) {
        return { ok: true, json: async () => [attachment] } as Response;
      }
      if (url.startsWith('/api/attachments/file')) {
        fileLoads++;
        return { ok: true, json: async () => file } as Response;
      }
      return { ok: true, json: async () => ({}) } as Response;
    }) as never;
  });

  /**
   * `only="message"` is how the real Message tab is mounted in split mode, and
   * it matters here: an instance willing to render every kind takes over with
   * the file view the moment the file slot is set, so "is the inline preview
   * there" would have no answer. This instance renders the message and nothing
   * else, which is the question these tests are asking about.
   */
  const clickChip = async (only?: 'message') => {
    useViewerStore.getState().setMessage(message);
    render(<MessageViewer only={only} />);
    const chip = await screen.findByRole('button', { name: /invoice\.pdf/ });
    fireEvent.click(chip);
    return chip;
  };

  it('keeps it inline by default, leaving the viewer alone', async () => {
    await clickChip();
    await waitFor(() => expect(fileLoads).toBeGreaterThan(0));
    // Loaded for the inline preview, but never handed to the viewer's slot.
    expect(useViewerStore.getState().file).toBeNull();
  });

  it('sends it to the viewer when that is the choice, and not inline as well', async () => {
    useViewerStore.setState({ mode: 'split' });
    useAttachmentTargetStore.setState({ target: 'file' });
    await clickChip('message');
    await waitFor(() => expect(useViewerStore.getState().file?.key).toBe(file.key));
    // "In the File tab" means there, not there as well as here.
    expect(screen.queryByTitle('Close preview')).toBeNull();
  });

  it('does both in split mode', async () => {
    useViewerStore.setState({ mode: 'split' });
    useAttachmentTargetStore.setState({ target: 'both' });
    await clickChip('message');

    await waitFor(() => expect(useViewerStore.getState().file?.key).toBe(file.key));
    // And the inline copy is rendered too — the chip is still the one that
    // opened it, so the close button belongs to the message.
    expect(await screen.findByTitle('Close preview')).toBeTruthy();
  });

  /**
   * The chip is a toggle, and a toggle needs its state to mean something.
   *
   * The render already hides an inline preview once inline is switched off, so
   * this is not about what is on screen. It is that a key left behind makes the
   * next click on that chip *close* a preview nobody can see — so nothing
   * appears and the chip looks broken.
   */
  it('forgets the inline preview when inline previews are switched off', async () => {
    useViewerStore.setState({ mode: 'split' });
    await clickChip('message');
    expect(await screen.findByTitle('Close preview')).toBeTruthy();

    act(() => { useAttachmentTargetStore.setState({ target: 'file' }); });
    expect(screen.queryByTitle('Close preview')).toBeNull();

    // Back again, and the chip opens rather than closing something invisible.
    act(() => { useAttachmentTargetStore.setState({ target: 'message' }); });
    expect(screen.queryByTitle('Close preview')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /invoice\.pdf/ }));
    expect(await screen.findByTitle('Close preview')).toBeTruthy();
  });

  /**
   * One tab cannot hold the message and the file at once, so "both" keeps the
   * inline half — the one attached to the message the chip belongs to. Sending
   * it to the viewer would clear the message and take the inline preview with
   * it, leaving one place.
   */
  it('keeps the inline half of "both" when one tab has to hold everything', async () => {
    useViewerStore.setState({ mode: 'reuse' });
    useAttachmentTargetStore.setState({ target: 'both' });
    await clickChip();

    expect(await screen.findByTitle('Close preview')).toBeTruthy();
    expect(useViewerStore.getState().file).toBeNull();
    // The message is still what the tab is showing.
    expect(useViewerStore.getState().messageId).toBe('m1');
  });
});
