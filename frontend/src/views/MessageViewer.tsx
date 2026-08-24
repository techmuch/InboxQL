import { useEffect, useState } from 'react';
import {
  Archive, AlertOctagon, Trash, Mail, MoreVertical,
  CornerUpLeft, CornerUpRight, Inbox, Paperclip,
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
        <span className="text-xs text-muted-foreground truncate flex-1">
          {message.from}
        </span>
        <button className="p-2 hover:bg-accent text-muted-foreground" title="Archive"><Archive className="w-4 h-4" /></button>
        <button className="p-2 hover:bg-accent text-muted-foreground" title="Report spam"><AlertOctagon className="w-4 h-4" /></button>
        <button className="p-2 hover:bg-accent text-muted-foreground" title="Delete"><Trash className="w-4 h-4" /></button>
        <div className="w-px h-6 bg-border mx-1" />
        <button className="p-2 hover:bg-accent text-muted-foreground" title="Mark unread"><Mail className="w-4 h-4" /></button>
        <button className="p-2 hover:bg-accent text-muted-foreground"><MoreVertical className="w-4 h-4" /></button>
      </div>

      <div className="flex-1 overflow-auto p-8 max-w-5xl mx-auto w-full">
        <h1 className="text-2xl font-normal mb-8 text-foreground/90">
          {message.subject || '(No Subject)'}
        </h1>

        <div className="flex items-start gap-4 mb-8">
          <div className="w-10 h-10 bg-primary/20 flex items-center justify-center text-primary font-bold shrink-0">
            {message.from?.[0]?.toUpperCase()}
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex justify-between items-center mb-1 gap-4">
              <div className="font-bold truncate">{message.from}</div>
              <div className="text-xs text-muted-foreground shrink-0">
                {message.date ? new Date(message.date).toLocaleString() : ''}
              </div>
            </div>
            <div className="text-xs text-muted-foreground truncate">
              to {message.to?.join(', ') || '(undisclosed)'}
            </div>
          </div>
          <div className="flex gap-2 shrink-0">
            <button className="p-2 hover:bg-accent transition-colors" title="Reply"><CornerUpLeft className="w-4 h-4" /></button>
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
                    carried something UEA chose not to keep, not a broken link. */}
                {!a.storagePath && <span className="italic">not stored</span>}
              </span>
            ))}
          </div>
        )}

        <div className="prose prose-sm dark:prose-invert max-w-none border-t border-border pt-8 font-sans leading-relaxed whitespace-pre-wrap text-foreground/90">
          {message.body || <span className="italic text-muted-foreground">No text content available.</span>}
        </div>

        <div className="mt-12 flex gap-3">
          <button className="px-6 py-2 border border-border flex items-center gap-2 hover:bg-accent text-sm transition-colors font-medium">
            <CornerUpLeft className="w-4 h-4" /> Reply
          </button>
          <button className="px-6 py-2 border border-border flex items-center gap-2 hover:bg-accent text-sm transition-colors font-medium">
            <CornerUpRight className="w-4 h-4" /> Forward
          </button>
        </div>
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
