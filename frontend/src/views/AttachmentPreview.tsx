import { useEffect, useState } from 'react';
import { Paperclip, Download, X, FileText, Image as ImageIcon, Mail, MessagesSquare } from 'lucide-react';
import { openMessageByID } from '../lib/tabs';

export interface AttachmentFile {
  key: string;
  contentHash?: string;
  filename: string;
  mimeType: string;
  size: number;
  inline: boolean;
  storagePath?: string;
  skipped?: string;
  messageId: string;
  subject?: string;
  from?: string;
  messages: number;
  threads: number;
  names: number;
  firstSeen?: string;
  lastSeen?: string;
  /** What reading the file found: ok, empty, unsupported, failed, or absent. */
  textStatus?: string;
  textPages?: number;
  /** What read it: "pdf" and "plain" copied the file's text, "ocr" guessed it. */
  textExtractor?: string;
}

/**
 * What to say about a file's searchability, or "" when there is nothing worth
 * saying.
 *
 * A scanned PDF is the case that needs words. Left unsaid, it looks like a
 * file whose contents just did not match — when in fact nothing has ever read
 * them, and no search ever will until something does.
 */
export const textStatusLabel = (file: AttachmentFile): string => {
  // OCR first, because it qualifies a success rather than describing a gap.
  // Text a document contains and text a model believes it can see in a picture
  // of one are different claims, and somebody deciding whether to trust a
  // match needs to know which they have — this model read "POWERS FERRY ROAD"
  // as "FOULERS" and invented a line of loyalty-scheme text that was not on
  // the paper.
  if (file.textStatus === 'ok' && file.textExtractor === 'ocr') {
    return 'text read by OCR — may contain errors';
  }

  switch (file.textStatus) {
    case 'empty':
      return 'scanned — no text to search';
    case 'failed':
      return 'could not be read';
    case '':
    case undefined:
      return 'not read yet';
    default:
      // 'ok' says nothing: searchable is the unremarkable case, and a badge on
      // every readable file would bury the ones that need a badge.
      // 'unsupported' says nothing either: nobody expects to search a JPEG.
      return '';
  }
};

export interface AttachmentOccurrence {
  attachmentId: string;
  messageId: string;
  threadKey: string;
  filename: string;
  subject: string;
  from: string;
  date: string;
  mailbox?: string;
  inline: boolean;
}

/** A row from /api/message/attachments, which is one arrival rather than a file. */
export interface MessageAttachment {
  id: string;
  messageId: string;
  filename: string;
  mimeType: string;
  size: number;
  contentHash?: string;
  storagePath?: string;
  inline: boolean;
  skipped?: string;
}

export const formatBytes = (n: number): string => {
  if (!n) return '';
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB'];
  let v = n / 1024, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(1)} ${units[i]}`;
};

/**
 * Which types this app will render rather than download.
 *
 * Kept in step with the server's allowlist in internal/api/attachmentapi.go,
 * and deliberately not the authority: the server decides what it will serve
 * inline, and a browser that asked for a preview of something else gets a
 * download regardless of what this list says. This exists so the UI does not
 * offer a preview it knows will not appear.
 */
const previewable = (mimeType: string): 'pdf' | 'image' | 'text' | null => {
  const base = (mimeType || '').toLowerCase().split(';')[0].trim();
  if (base === 'application/pdf') return 'pdf';
  if (base === 'text/plain') return 'text';
  if (['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/avif'].includes(base)) return 'image';
  return null;
};

/**
 * The sandbox a preview frame gets.
 *
 * # Why a PDF gets none, and what covers it instead
 *
 * `sandbox=""` is right and is what everything else gets. Chrome, though,
 * renders a PDF through a viewer that it refuses to instantiate inside a
 * sandboxed frame at all — with the attribute present the panel shows a broken
 * -document icon and nothing else, whatever tokens are granted. So for PDFs it
 * is a real preview without the attribute, or the attribute and no preview.
 *
 * Being straight about what that costs, because it is tempting to write
 * "the CSP covers it" and move on. It does not: the response carries
 * `Content-Security-Policy: sandbox`, but Chrome renders a PDF through a
 * same-origin shell document that the directive does not put in an opaque
 * origin — the parent really can reach the frame's `contentDocument`. That was
 * measured, not assumed.
 *
 * What does hold the line for a PDF:
 *
 *   - Only a real PDF ever gets here. The server picks the content type from
 *     an allowlist over what it stored and serves everything else as
 *     application/octet-stream with `Content-Disposition: attachment`, so a
 *     mailed .html or .svg can never reach a frame at all.
 *   - `X-Content-Type-Options: nosniff`, so the browser cannot reinterpret it.
 *   - JavaScript inside a PDF runs in PDFium, in its own process, and cannot
 *     script the shell document or reach this page.
 *   - The session cookie is HttpOnly, so it is unreadable from script whatever
 *     origin the frame ends up in.
 *
 * The residual exposure is a PDFium escape, which is a browser vulnerability
 * that the sandbox attribute would not reliably contain either. If that trade
 * ever stops being acceptable, the fix is to render PDFs to a canvas with
 * pdf.js — no plugin, no frame document, and the sandbox comes back.
 */
const sandboxFor = (kind: 'pdf' | 'image' | 'text'): { sandbox?: string } =>
  kind === 'pdf' ? {} : { sandbox: '' };

export const attachmentURL = (key: string, inline: boolean): string =>
  `/api/attachments/content?key=${encodeURIComponent(key)}${inline ? '&disposition=inline' : ''}`;

/**
 * A short label for a MIME type.
 *
 * The same categories the query language uses for `type:`, so what a chip says
 * is what you can type to find more like it.
 */
export const fileTypeLabel = (mimeType: string): string => {
  const base = (mimeType || '').toLowerCase().split(';')[0].trim();
  if (!base) return 'unknown';
  if (base === 'application/pdf') return 'pdf';
  if (base.startsWith('image/')) return 'image';
  if (base.startsWith('audio/')) return 'audio';
  if (base.startsWith('video/')) return 'video';
  if (base.includes('wordprocessingml') || base === 'application/msword') return 'doc';
  if (base.includes('spreadsheetml') || base === 'application/vnd.ms-excel' || base === 'text/csv') return 'sheet';
  if (base.includes('presentationml') || base === 'application/vnd.ms-powerpoint') return 'slides';
  if (base === 'text/calendar') return 'calendar';
  if (base === 'text/vcard' || base === 'text/x-vcard') return 'contact';
  const sub = base.split('/')[1];
  return sub || base;
};

/**
 * AttachmentChip is one file, clickable.
 *
 * A file with no stored bytes stays a chip and says why: the row is a record
 * that the message carried something InboxQL chose not to keep — too large, or
 * imported before attachments were extracted — rather than a broken link. It
 * is not made to look clickable, because there is nothing to click through to.
 */
export const AttachmentChip = ({
  filename, mimeType, size, contentHash, skipped, shared, onOpen,
}: {
  filename: string;
  mimeType: string;
  size: number;
  contentHash?: string;
  skipped?: string;
  shared?: number;
  onOpen?: () => void;
}) => {
  const stored = Boolean(contentHash);
  const kind = previewable(mimeType);
  const Icon = kind === 'image' ? ImageIcon : kind === 'pdf' || kind === 'text' ? FileText : Paperclip;

  if (!stored) {
    return (
      <span
        title={skipped || 'the bytes were not kept'}
        className="flex items-center gap-2 border border-amber-500/40 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-700 dark:text-amber-400"
      >
        <Paperclip className="w-3 h-3" />
        {filename}
        {size > 0 && <span className="opacity-70">{formatBytes(size)}</span>}
        <span className="italic">not stored</span>
      </span>
    );
  }

  return (
    <button
      type="button"
      onClick={onOpen}
      title={`${filename} — ${fileTypeLabel(mimeType)}, ${formatBytes(size)}`}
      className="flex items-center gap-2 border border-border px-3 py-1.5 text-xs hover:bg-accent transition-colors text-left"
    >
      <Icon className="w-3 h-3 shrink-0" />
      <span className="truncate max-w-[22rem]">{filename}</span>
      <span className="text-muted-foreground shrink-0">{formatBytes(size)}</span>
      {/* Only worth saying when it is more than once — "1 message" on every
          chip is noise that hides the chips where the number is the point. */}
      {shared !== undefined && shared > 1 && (
        <span className="shrink-0 text-muted-foreground border-l border-border pl-2">
          {shared} messages
        </span>
      )}
    </button>
  );
};

/**
 * AttachmentOccurrences lists every message a file arrived on.
 *
 * # Why threads and messages are counted separately
 *
 * They answer different questions. The same document quoted down one long
 * thread is one conversation; the same document sent to three separate people
 * is three. Reporting only "on 6 messages" conflates a file that went around
 * with a file that was replied to a lot.
 *
 * The message this panel was opened from is marked rather than hidden, so the
 * list is the file's whole history and the current position is visible in it.
 */
export const AttachmentOccurrences = ({ file, currentMessageId }: {
  file: AttachmentFile;
  currentMessageId?: string;
}) => {
  const [occurrences, setOccurrences] = useState<AttachmentOccurrence[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    setOccurrences(null);
    fetch(`/api/attachments/occurrences?key=${encodeURIComponent(file.key)}`)
      .then(r => (r.ok ? r.json() : []))
      .then(list => { if (!cancelled) setOccurrences(Array.isArray(list) ? list : []); })
      .catch(() => { if (!cancelled) setOccurrences([]); });
    return () => { cancelled = true; };
  }, [file.key]);

  if (occurrences === null) {
    return <div className="text-xs text-muted-foreground px-3 py-2">Looking for other copies…</div>;
  }
  if (occurrences.length <= 1) {
    return (
      <div className="text-xs text-muted-foreground px-3 py-2">
        This file appears on one message only.
      </div>
    );
  }

  return (
    <div className="border-t border-border">
      <div className="flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground">
        <MessagesSquare className="w-3.5 h-3.5" />
        <span>
          {file.messages} messages
          {file.threads > 1 && <> · {file.threads} threads</>}
          {file.names > 1 && <> · {file.names} names</>}
        </span>
      </div>
      <ul className="max-h-56 overflow-y-auto">
        {occurrences.map(o => {
          const here = o.messageId === currentMessageId;
          return (
            <li key={o.attachmentId}>
              <button
                type="button"
                onClick={() => openMessageByID(o.messageId)}
                className={`w-full text-left px-3 py-2 text-xs hover:bg-accent transition-colors flex items-start gap-2 ${
                  here ? 'bg-accent/50' : ''
                }`}
              >
                <Mail className="w-3.5 h-3.5 mt-0.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{o.subject || '(no subject)'}</span>
                  <span className="block truncate text-muted-foreground">
                    {o.from}
                    {o.date && <> · {new Date(o.date).toLocaleDateString()}</>}
                    {/* The name it arrived under here, when that is not the
                        name the file is listed under. */}
                    {o.filename && o.filename !== file.filename && <> · as {o.filename}</>}
                  </span>
                </span>
                {here && <span className="shrink-0 text-muted-foreground italic">this message</span>}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
};

/**
 * AttachmentViewer previews a file and shows where else it has been.
 *
 * The preview runs in a sandboxed iframe with no allow-* tokens, which is a
 * second barrier rather than the first: the server already refuses to serve
 * anything renderable as a renderable type, and sends CSP sandbox with the
 * bytes. Both, because the cost of the iframe attribute is nothing and the
 * cost of being wrong once is the session cookie.
 */
export const AttachmentViewer = ({ file, currentMessageId, onClose }: {
  file: AttachmentFile;
  currentMessageId?: string;
  onClose: () => void;
}) => {
  const kind = previewable(file.mimeType);
  const src = attachmentURL(file.key, true);

  return (
    <div className="border border-border bg-card">
      <div className="flex items-center gap-2 px-3 py-2 border-b border-border">
        <FileText className="w-4 h-4 shrink-0 text-muted-foreground" />
        <span className="text-sm font-medium truncate flex-1" title={file.filename}>
          {file.filename}
        </span>
        <span className="text-xs text-muted-foreground shrink-0">
          {fileTypeLabel(file.mimeType)} · {formatBytes(file.size)}
          {textStatusLabel(file) && <> · {textStatusLabel(file)}</>}
        </span>
        <a
          href={attachmentURL(file.key, false)}
          download={file.filename}
          className="p-1.5 hover:bg-accent transition-colors shrink-0"
          title="Download"
        >
          <Download className="w-4 h-4" />
        </a>
        <button
          type="button"
          onClick={onClose}
          className="p-1.5 hover:bg-accent transition-colors shrink-0"
          title="Close preview"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {kind === 'image' ? (
        <div className="p-3 bg-muted/30 flex justify-center">
          <img src={src} alt={file.filename} className="max-h-[32rem] max-w-full object-contain" />
        </div>
      ) : kind ? (
        <iframe
          src={src}
          title={file.filename}
          // A fully sandboxed frame everywhere it works — which is everywhere
          // except a PDF. See sandboxFor below for why PDFs are the exception
          // and what covers them instead.
          {...sandboxFor(kind)}
          className="w-full h-[32rem] bg-muted/30"
        />
      ) : (
        // Nothing to render, and saying so beats an empty frame that looks
        // like a failed load.
        <div className="px-3 py-8 text-center text-sm text-muted-foreground">
          <p>No preview for {fileTypeLabel(file.mimeType)} files.</p>
          <a
            href={attachmentURL(file.key, false)}
            download={file.filename}
            className="inline-flex items-center gap-1.5 mt-3 border border-border px-3 py-1.5 text-xs hover:bg-accent transition-colors"
          >
            <Download className="w-3.5 h-3.5" />
            Download {file.filename}
          </a>
        </div>
      )}

      <AttachmentOccurrences file={file} currentMessageId={currentMessageId} />
    </div>
  );
};

/**
 * AttachmentSection lists the files reached by a query, with a preview.
 *
 * # Why a query rather than a panel per place
 *
 * A ticket's files, a contact's files and a search for files are the same
 * question asked with a different scope: `in:attachments` plus a filter. The
 * attachments table is an edge from a message to a file, structurally the same
 * as message_participants, so anything that already reaches messages reaches
 * files in one join — and the scope is the only thing each caller has to know.
 *
 * Building three bespoke panels instead would mean three definitions of "this
 * thing's attachments" that drift, which is the shape this project has been
 * bitten by before.
 *
 * `scopes` takes more than one so a caller can offer a narrower and a wider
 * reading — a contact's files default to what they *sent*, because "files from
 * Alice" means files Alice sent, not every file on a thread she was copied on.
 * The wider reading is a click away rather than the default.
 */
export const AttachmentSection = ({ scopes, emptyLabel, limit = 24 }: {
  scopes: { label: string; query: string; hint?: string }[];
  emptyLabel: string;
  limit?: number;
}) => {
  const [active, setActive] = useState(0);
  const [files, setFiles] = useState<AttachmentFile[] | null>(null);
  const [openKey, setOpenKey] = useState<string | null>(null);

  const scope = scopes[Math.min(active, scopes.length - 1)];

  useEffect(() => {
    let cancelled = false;
    setFiles(null);
    setOpenKey(null);
    const params = new URLSearchParams({ q: scope.query, limit: String(limit) });
    fetch(`/api/query?${params}`)
      .then(r => (r.ok ? r.json() : null))
      .then(result => {
        if (cancelled) return;
        setFiles(Array.isArray(result?.attachments) ? result.attachments : []);
      })
      .catch(() => { if (!cancelled) setFiles([]); });
    return () => { cancelled = true; };
  }, [scope.query, limit]);

  const open = files?.find(f => f.key === openKey) ?? null;

  return (
    <div className="space-y-3">
      {scopes.length > 1 && (
        <div className="flex items-center gap-1 text-xs">
          {scopes.map((s, i) => (
            <button
              key={s.query}
              type="button"
              onClick={() => setActive(i)}
              title={s.hint}
              className={`px-2 py-1 border transition-colors ${
                i === active
                  ? 'border-border bg-accent'
                  : 'border-transparent text-muted-foreground hover:bg-accent'
              }`}
            >
              {s.label}
            </button>
          ))}
        </div>
      )}

      {files === null ? (
        <div className="text-xs text-muted-foreground">Looking for files…</div>
      ) : files.length === 0 ? (
        <div className="text-xs text-muted-foreground italic">{emptyLabel}</div>
      ) : (
        <>
          <div className="flex flex-wrap gap-2">
            {files.map(f => (
              <AttachmentChip
                key={f.key}
                filename={f.filename}
                mimeType={f.mimeType}
                size={f.size}
                contentHash={f.contentHash}
                skipped={f.skipped}
                shared={f.messages}
                onOpen={() => setOpenKey(k => (k === f.key ? null : f.key))}
              />
            ))}
          </div>
          {open && (
            <AttachmentViewer file={open} onClose={() => setOpenKey(null)} />
          )}
        </>
      )}
    </div>
  );
};

/**
 * useAttachmentFile loads a file's metadata by key, for a chip that only knows
 * its content hash.
 */
export const useAttachmentFile = (key: string | null) => {
  const [file, setFile] = useState<AttachmentFile | null>(null);

  useEffect(() => {
    if (!key) { setFile(null); return; }
    let cancelled = false;
    fetch(`/api/attachments/file?key=${encodeURIComponent(key)}`)
      .then(r => (r.ok ? r.json() : null))
      .then(f => { if (!cancelled) setFile(f); })
      .catch(() => { if (!cancelled) setFile(null); });
    return () => { cancelled = true; };
  }, [key]);

  return file;
};
