import { useEffect, useMemo, useState } from 'react';
import {
  Archive, AlertOctagon, Trash, Mail, MoreVertical,
  CornerUpLeft, CornerUpRight, Inbox, Paperclip, FileText,
  Globe, Code, Copy, Check,
} from 'lucide-react';
import { useViewerStore } from '../lib/tabs';

/**
 * The message viewer, as its own workbench tab.
 *
 * This used to be a mode of the mail client: selecting a message replaced the
 * list, and a back button restored it. As a tab it can sit beside the list
 * instead, so picking the next message does not mean navigating back first.
 *
 * The message comes from a store rather than props because the tab is mounted
 * by the layout engine, which knows nothing about what the list has selected.
 */
export const MessageViewer = () => {
  const message = useViewerStore(s => s.message);
  const [attachments, setAttachments] = useState<any[]>([]);
  const [viewMode, setViewMode] = useState<'html' | 'text' | 'raw'>('html');
  const [copied, setCopied] = useState(false);

  // Set default view mode based on message content
  useEffect(() => {
    if (message?.htmlBody) {
      setViewMode('html');
    } else {
      setViewMode('text');
    }
  }, [message?.id]);

  useEffect(() => {
    if (!message?.id) { setAttachments([]); return; }
    let cancelled = false;
    // Imported mail can carry attachments; synced mail never does yet. Either
    // way an empty list is the normal case, so a failure here is silent.
    fetch(`/api/message/attachments?id=${encodeURIComponent(message.id)}`)
      .then(r => (r.ok ? r.json() : []))
      .then(list => { if (!cancelled) setAttachments(Array.isArray(list) ? list : []); })
      .catch(() => { if (!cancelled) setAttachments([]); });
    return () => { cancelled = true; };
  }, [message?.id]);

  // A draft is not received mail: it has no sender, it was never delivered,
  // and offering Reply on it would be nonsense. It reaches this viewer through
  // the Drafts folder, which maps drafts into the message shape.
  const isDraft = message?.flags?.includes('\\Draft');

  const rawContent = useMemo(() => {
    if (!message) return '';
    let rawHeaders = '';
    if (message.header) {
      if (typeof message.header === 'string') {
        try {
          rawHeaders = atob(message.header);
        } catch {
          rawHeaders = message.header;
        }
      } else if (Array.isArray(message.header)) {
        rawHeaders = String.fromCharCode(...message.header);
      }
    }

    const lines: string[] = [];
    if (rawHeaders) {
      lines.push(rawHeaders.trimEnd());
    } else {
      lines.push(`Message-ID: <${message.messageId || message.id}>`);
      if (message.date) lines.push(`Date: ${new Date(message.date).toUTCString()}`);
      if (message.from) lines.push(`From: ${message.from}`);
      if (message.to?.length) lines.push(`To: ${message.to.join(', ')}`);
      if (message.cc?.length) lines.push(`Cc: ${message.cc.join(', ')}`);
      if (message.bcc?.length) lines.push(`Bcc: ${message.bcc.join(', ')}`);
      if (message.subject) lines.push(`Subject: ${message.subject}`);
      if (message.mailbox) lines.push(`X-InboxQL-Mailbox: ${message.mailbox}`);
    }

    lines.push(''); // blank line separating headers and body

    if (message.htmlBody && message.body) {
      lines.push('--- Plain Text Body ---');
      lines.push(message.body);
      lines.push('');
      lines.push('--- HTML Body ---');
      lines.push(message.htmlBody);
    } else if (message.htmlBody) {
      lines.push(message.htmlBody);
    } else {
      lines.push(message.body || '');
    }

    return lines.join('\n');
  }, [message]);

  const handleCopyRaw = () => {
    navigator.clipboard.writeText(rawContent);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  if (!message) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-3">
        <Inbox className="w-8 h-8 opacity-40" />
        <p className="text-sm italic">No message selected.</p>
        <p className="text-xs">Pick one in the Mailbox to read it here.</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full bg-background text-foreground">
      <div className="h-12 border-b border-border flex items-center px-4 gap-2 shrink-0 bg-background/80 backdrop-blur-md">
        <span className="text-xs text-muted-foreground truncate flex-1 flex items-center gap-2">
          {isDraft && (
            <span className="shrink-0 px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider bg-amber-500/15 text-amber-700 dark:text-amber-400">
              Draft
            </span>
          )}
          <span className="truncate">{isDraft ? 'Not sent' : message.from}</span>
        </span>

        {/* View Mode Toggle: HTML / Text / Raw */}
        <div className="flex items-center bg-muted/60 p-0.5 border border-border text-xs rounded">
          <button
            type="button"
            onClick={() => setViewMode('html')}
            className={`px-2.5 py-1 flex items-center gap-1.5 font-medium rounded transition-all ${
              viewMode === 'html'
                ? 'bg-blue-600 text-white shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
            title="Rendered HTML View"
          >
            <Globe className="w-3.5 h-3.5" />
            <span>HTML</span>
          </button>
          <button
            type="button"
            onClick={() => setViewMode('text')}
            className={`px-2.5 py-1 flex items-center gap-1.5 font-medium rounded transition-all ${
              viewMode === 'text'
                ? 'bg-emerald-600 text-white shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
            title="Plain Text View"
          >
            <FileText className="w-3.5 h-3.5" />
            <span>Text</span>
          </button>
          <button
            type="button"
            onClick={() => setViewMode('raw')}
            className={`px-2.5 py-1 flex items-center gap-1.5 font-medium rounded transition-all ${
              viewMode === 'raw'
                ? 'bg-purple-600 text-white shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
            title="Raw Source & Headers"
          >
            <Code className="w-3.5 h-3.5" />
            <span>Raw</span>
          </button>
        </div>

        <div className="w-px h-6 bg-border mx-1" />
        {!isDraft && <>
          <button className="p-2 hover:bg-accent text-muted-foreground" title="Archive"><Archive className="w-4 h-4" /></button>
          <button className="p-2 hover:bg-accent text-muted-foreground" title="Report spam"><AlertOctagon className="w-4 h-4" /></button>
        </>}
        <button className="p-2 hover:bg-accent text-muted-foreground" title="Delete"><Trash className="w-4 h-4" /></button>
        <div className="w-px h-6 bg-border mx-1" />
        {!isDraft && <button className="p-2 hover:bg-accent text-muted-foreground" title="Mark unread"><Mail className="w-4 h-4" /></button>}
        <button className="p-2 hover:bg-accent text-muted-foreground"><MoreVertical className="w-4 h-4" /></button>
      </div>

      <div className="flex-1 overflow-auto p-8 max-w-5xl mx-auto w-full">
        <h1 className="text-2xl font-normal mb-8 text-foreground/90">
          {message.subject || '(No Subject)'}
        </h1>

        <div className="flex items-start gap-4 mb-8">
          <div className="w-10 h-10 bg-primary/20 flex items-center justify-center text-primary font-bold shrink-0">
            {isDraft ? <FileText className="w-4 h-4" /> : message.from?.[0]?.toUpperCase()}
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex justify-between items-center mb-1 gap-4">
              <div className="font-bold truncate">
                {isDraft ? <span className="italic font-normal text-muted-foreground">Draft — never sent</span> : message.from}
              </div>
              <div className="text-xs text-muted-foreground shrink-0">
                {message.date ? `${isDraft ? 'Edited ' : ''}${new Date(message.date).toLocaleString()}` : ''}
              </div>
            </div>
            <div className="text-xs text-muted-foreground truncate">
              to {message.to?.join(', ') || (isDraft ? '(no recipient yet)' : '(undisclosed)')}
            </div>
          </div>
          <div className="flex gap-2 shrink-0">
            {!isDraft && <button className="p-2 hover:bg-accent transition-colors" title="Reply"><CornerUpLeft className="w-4 h-4" /></button>}
            <button className="p-2 hover:bg-accent transition-colors"><MoreVertical className="w-4 h-4" /></button>
          </div>
        </div>

        {attachments.length > 0 && (
          <div className="mb-6 flex flex-wrap gap-2">
            {attachments.map(a => (
              <span
                key={a.id}
                title={a.skipped || undefined}
                className={`flex items-center gap-2 border px-3 py-1.5 text-xs ${
                  a.storagePath ? 'border-border' : 'border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400'
                }`}
              >
                <Paperclip className="w-3 h-3" />
                {a.filename}
                <span className="text-muted-foreground">{formatBytes(a.size)}</span>
                {/* A row with no stored bytes is a record that the message
                    carried something InboxQL chose not to keep, not a broken link. */}
                {!a.storagePath && <span className="italic">not stored</span>}
              </span>
            ))}
          </div>
        )}

        {/* View Mode Content */}
        {viewMode === 'html' && (
          <div className="mt-2 space-y-3">
            <div className="flex items-center justify-between text-xs px-3 py-1.5 rounded bg-blue-500/10 border border-blue-500/25 text-blue-700 dark:text-blue-300">
              <span className="flex items-center gap-1.5 font-medium">
                <Globe className="w-3.5 h-3.5" />
                HTML Rendered View
              </span>
              {message.htmlBody && (
                <span className="text-[10px] opacity-75 font-mono">sandboxed iframe</span>
              )}
            </div>

            {message.htmlBody ? (
              <div className="border border-blue-500/20 bg-white dark:bg-zinc-950 rounded overflow-hidden shadow-sm">
                <iframe
                  srcDoc={message.htmlBody}
                  sandbox="allow-same-origin allow-popups"
                  title="HTML Message"
                  className="w-full min-h-[500px] border-0"
                  style={{ display: 'block' }}
                  onLoad={(e) => {
                    try {
                      const doc = e.currentTarget.contentWindow?.document;
                      if (doc?.body) {
                        const height = doc.body.scrollHeight;
                        if (height > 0) {
                          e.currentTarget.style.height = `${height + 40}px`;
                        }
                      }
                    } catch {
                      // cross-origin / sandboxed fallback
                    }
                  }}
                />
              </div>
            ) : (
              <div className="space-y-4">
                <div className="bg-amber-500/10 border border-amber-500/30 px-3 py-2 text-xs text-amber-700 dark:text-amber-300 flex items-center justify-between rounded">
                  <span>No HTML body available for this message. Displaying plain text:</span>
                  <button
                    onClick={() => setViewMode('text')}
                    className="text-amber-800 dark:text-amber-200 underline font-medium text-xs"
                  >
                    Switch to Text view
                  </button>
                </div>
                <div className="p-6 rounded border border-border bg-muted/15 prose prose-sm dark:prose-invert max-w-none font-sans leading-relaxed whitespace-pre-wrap text-foreground/90">
                  {message.body || <span className="italic text-muted-foreground">No text content available.</span>}
                </div>
              </div>
            )}
          </div>
        )}

        {viewMode === 'text' && (
          <div className="mt-2 space-y-3">
            <div className="flex items-center justify-between text-xs px-3 py-1.5 rounded bg-emerald-500/10 border border-emerald-500/25 text-emerald-700 dark:text-emerald-300">
              <span className="flex items-center gap-1.5 font-medium">
                <FileText className="w-3.5 h-3.5" />
                Plain Text View
              </span>
              <span className="text-[10px] opacity-75 font-mono">utf-8 text</span>
            </div>
            <div className="p-6 rounded border border-emerald-500/20 bg-emerald-500/[0.04] dark:bg-emerald-950/20 prose prose-sm dark:prose-invert max-w-none font-sans leading-relaxed whitespace-pre-wrap text-foreground/90">
              {message.body || <span className="italic text-muted-foreground">No text content available.</span>}
            </div>
          </div>
        )}

        {viewMode === 'raw' && (
          <div className="mt-2 space-y-3">
            <div className="flex items-center justify-between text-xs px-3 py-1.5 rounded bg-purple-500/10 border border-purple-500/25 text-purple-700 dark:text-purple-300">
              <span className="flex items-center gap-1.5 font-medium">
                <Code className="w-3.5 h-3.5" />
                Raw RFC822 Source & Headers
              </span>
              <button
                type="button"
                onClick={handleCopyRaw}
                className="px-2 py-0.5 text-xs rounded border border-purple-500/30 bg-purple-500/15 hover:bg-purple-500/25 text-purple-700 dark:text-purple-300 flex items-center gap-1 transition-colors font-mono"
                title="Copy Raw Content"
              >
                {copied ? <Check className="w-3 h-3 text-emerald-500" /> : <Copy className="w-3 h-3" />}
                <span>{copied ? 'Copied' : 'Copy Raw'}</span>
              </button>
            </div>
            <pre className="font-mono text-xs whitespace-pre bg-zinc-950 text-zinc-100 dark:bg-black p-5 rounded border border-purple-500/30 overflow-x-auto leading-relaxed select-all shadow-inner">
              {rawContent}
            </pre>
          </div>
        )}

        {isDraft ? (
          /* Sending is gated on a person approving it at a terminal, so the
             viewer states the command rather than offering a button that
             cannot do what it says. */
          <div className="mt-12 border border-border bg-muted/30 px-4 py-3 text-xs text-muted-foreground">
            This draft has not been sent. Queue it with{' '}
            <code className="font-mono text-foreground">iql send {message.id}</code>, then approve it with{' '}
            <code className="font-mono text-foreground">iql outbox approve {message.id}</code> from a terminal.
          </div>
        ) : (
          <div className="mt-12 flex gap-3">
            <button className="px-6 py-2 border border-border flex items-center gap-2 hover:bg-accent text-sm transition-colors font-medium">
              <CornerUpLeft className="w-4 h-4" /> Reply
            </button>
            <button className="px-6 py-2 border border-border flex items-center gap-2 hover:bg-accent text-sm transition-colors font-medium">
              <CornerUpRight className="w-4 h-4" /> Forward
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

const formatBytes = (n: number): string => {
  if (!n) return '';
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB'];
  let v = n / 1024, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(1)} ${units[i]}`;
};
