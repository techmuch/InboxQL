import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import {
  AttachmentChip, AttachmentViewer, AttachmentOccurrences,
  attachmentURL, fileTypeLabel, formatBytes,
  type AttachmentFile, type AttachmentOccurrence,
} from './AttachmentPreview';

const pdf: AttachmentFile = {
  key: 'a'.repeat(64),
  contentHash: 'a'.repeat(64),
  filename: 'invoice.pdf',
  mimeType: 'application/pdf',
  size: 63500,
  inline: false,
  storagePath: '/data/attachments/aa/aaa',
  messageId: 'm1',
  subject: 'Invoice attached',
  from: 'alice@acme.com',
  messages: 2,
  threads: 2,
  names: 2,
};

describe('attachmentURL', () => {
  it('asks for inline only when previewing', () => {
    expect(attachmentURL('abc', false)).toBe('/api/attachments/content?key=abc');
    expect(attachmentURL('abc', true)).toBe('/api/attachments/content?key=abc&disposition=inline');
  });

  it('escapes the key rather than pasting it into the URL', () => {
    expect(attachmentURL('a&b=c', false)).toBe('/api/attachments/content?key=a%26b%3Dc');
  });
});

describe('fileTypeLabel', () => {
  it('names types the way a person would', () => {
    expect(fileTypeLabel('application/pdf')).toBe('pdf');
    expect(fileTypeLabel('image/png')).toBe('image');
    expect(fileTypeLabel('application/vnd.openxmlformats-officedocument.wordprocessingml.document')).toBe('doc');
    expect(fileTypeLabel('application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')).toBe('sheet');
    expect(fileTypeLabel('text/vcard')).toBe('contact');
  });

  it('falls back to the subtype rather than showing "application"', () => {
    expect(fileTypeLabel('application/x-something-odd')).toBe('x-something-odd');
    expect(fileTypeLabel('')).toBe('unknown');
  });

  it('ignores parameters and case, as the server does', () => {
    expect(fileTypeLabel('IMAGE/PNG; name=logo.png')).toBe('image');
  });
});

describe('formatBytes', () => {
  it('scales to the unit a person would use', () => {
    expect(formatBytes(500)).toBe('500 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(1958009)).toBe('1.9 MB');
  });
});

describe('AttachmentChip', () => {
  it('is clickable when the bytes are stored', () => {
    const onOpen = vi.fn();
    render(
      <AttachmentChip
        filename="invoice.pdf" mimeType="application/pdf" size={63500}
        contentHash={'a'.repeat(64)} onOpen={onOpen}
      />
    );
    fireEvent.click(screen.getByRole('button'));
    expect(onOpen).toHaveBeenCalled();
  });

  // A row with no bytes is a record of what the message carried, not a broken
  // link — so it must not look like something to click.
  it('is not a button when the bytes were never kept', () => {
    render(
      <AttachmentChip
        filename="huge.zip" mimeType="application/zip" size={999999999}
        skipped="larger than the configured limit"
      />
    );
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.getByText('not stored')).toBeTruthy();
  });

  it('says how far a file reached only when it reached more than once', () => {
    const { rerender } = render(
      <AttachmentChip filename="a.pdf" mimeType="application/pdf" size={10}
        contentHash={'a'.repeat(64)} shared={1} />
    );
    expect(screen.queryByText(/messages/)).toBeNull();

    rerender(
      <AttachmentChip filename="a.pdf" mimeType="application/pdf" size={10}
        contentHash={'a'.repeat(64)} shared={3} />
    );
    expect(screen.getByText('3 messages')).toBeTruthy();
  });
});

describe('AttachmentViewer', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => [] }) as never;
  });

  // The viewer embeds the occurrences panel, which fetches on mount. Every test
  // here waits for that to land before asserting, so a React state update never
  // arrives after the test has finished.
  const settled = async () =>
    waitFor(() => expect(screen.getByText(/appears on one message only/)).toBeTruthy());

  // A PDF frame carries no sandbox attribute, because Chrome will not run its
  // PDF viewer inside one at all. That is a deliberate, documented exception
  // — see sandboxFor — and the assertion is here so it stays deliberate rather
  // than becoming something someone copies to the next file type.
  it('renders a PDF in a frame with no sandbox, which Chrome requires', async () => {
    const { container } = render(<AttachmentViewer file={pdf} onClose={() => {}} />);
    await settled();

    const frame = container.querySelector('iframe');
    expect(frame).toBeTruthy();
    expect(frame!.hasAttribute('sandbox')).toBe(false);
    expect(frame!.getAttribute('src')).toContain('disposition=inline');
  });

  // Text has no such excuse, so it keeps the full sandbox. Losing this
  // silently is exactly what a test should catch: nothing about the page looks
  // different when it goes.
  it('renders text in a fully sandboxed frame', async () => {
    const txt = { ...pdf, filename: 'notes.txt', mimeType: 'text/plain' };
    const { container } = render(<AttachmentViewer file={txt} onClose={() => {}} />);
    await settled();

    const frame = container.querySelector('iframe');
    expect(frame).toBeTruthy();
    expect(frame!.getAttribute('sandbox')).toBe('');
  });

  it('renders an image as an image rather than a frame', async () => {
    const png = { ...pdf, filename: 'photo.png', mimeType: 'image/png' };
    const { container } = render(<AttachmentViewer file={png} onClose={() => {}} />);
    await settled();

    expect(container.querySelector('iframe')).toBeNull();
    expect(container.querySelector('img')).toBeTruthy();
  });

  // Offering an empty frame for a .docx would look like a failed load. Saying
  // there is no preview, and offering the download, is the honest version.
  it('offers a download instead of an empty frame for types it cannot show', async () => {
    const doc = {
      ...pdf, filename: 'notes.docx',
      mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    };
    const { container } = render(<AttachmentViewer file={doc} onClose={() => {}} />);
    await settled();

    expect(container.querySelector('iframe')).toBeNull();
    expect(screen.getByText(/No preview for doc files/)).toBeTruthy();
    expect(screen.getByText(/Download notes.docx/)).toBeTruthy();
  });

  it('downloads without asking for inline', async () => {
    const { container } = render(<AttachmentViewer file={pdf} onClose={() => {}} />);
    await settled();

    const link = container.querySelector('a[download]') as HTMLAnchorElement;
    expect(link).toBeTruthy();
    expect(link.getAttribute('href')).not.toContain('disposition=inline');
    expect(link.getAttribute('download')).toBe('invoice.pdf');
  });

  it('closes', async () => {
    const onClose = vi.fn();
    render(<AttachmentViewer file={pdf} onClose={onClose} />);
    await settled();

    fireEvent.click(screen.getByTitle('Close preview'));
    expect(onClose).toHaveBeenCalled();
  });
});

describe('AttachmentOccurrences', () => {
  const occurrences: AttachmentOccurrence[] = [
    {
      attachmentId: 'a1', messageId: 'm1', threadKey: 't1',
      filename: 'invoice.pdf', subject: 'Invoice attached', from: 'alice@acme.com',
      date: '2026-03-01T12:00:00Z', inline: false,
    },
    {
      attachmentId: 'a2', messageId: 'm2', threadKey: 't2',
      filename: 'invoice-copy.pdf', subject: 'Fwd: Invoice attached', from: 'bob@acme.com',
      date: '2026-03-02T12:00:00Z', inline: false,
    },
  ];

  const mockOccurrences = (list: AttachmentOccurrence[]) => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => list }) as never;
  };

  it('lists every message the file arrived on, with threads counted apart', async () => {
    mockOccurrences(occurrences);
    render(<AttachmentOccurrences file={pdf} />);

    await waitFor(() => expect(screen.getByText('Invoice attached')).toBeTruthy());
    expect(screen.getByText('Fwd: Invoice attached')).toBeTruthy();
    // Messages and threads are different questions and are shown as such.
    expect(screen.getByText(/2 messages/)).toBeTruthy();
    expect(screen.getByText(/2 threads/)).toBeTruthy();
  });

  // The name a file arrived under here is worth showing when it differs from
  // the name the file is listed under; otherwise it is noise on every row.
  it('notes a different name only where there is one', async () => {
    mockOccurrences(occurrences);
    render(<AttachmentOccurrences file={pdf} />);

    await waitFor(() => expect(screen.getByText(/as invoice-copy.pdf/)).toBeTruthy());
    expect(screen.queryByText(/as invoice\.pdf/)).toBeNull();
  });

  it('marks the message being viewed rather than hiding it', async () => {
    mockOccurrences(occurrences);
    render(<AttachmentOccurrences file={pdf} currentMessageId="m1" />);

    await waitFor(() => expect(screen.getByText('this message')).toBeTruthy());
    // And the other one is still listed.
    expect(screen.getByText('Fwd: Invoice attached')).toBeTruthy();
  });

  it('says plainly when a file is on one message only', async () => {
    mockOccurrences([occurrences[0]]);
    render(<AttachmentOccurrences file={{ ...pdf, messages: 1, threads: 1 }} />);

    await waitFor(() =>
      expect(screen.getByText('This file appears on one message only.')).toBeTruthy());
  });
});
