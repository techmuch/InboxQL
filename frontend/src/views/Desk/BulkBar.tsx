import { useState } from 'react';
import { AlertOctagon, Archive, Inbox, Mail, MailOpen, Star, Tag, Trash2, X } from 'lucide-react';
import { bulkApply, type BulkAction, type BulkResult } from '../../lib/mail';

/**
 * Every message the query matches is selected, not just the page on screen.
 *
 * The actions go to the server as the query, so they reach all of them — the
 * bar says how many before anything is pressed, and the result comes back
 * with an Undo for exactly what changed.
 */
export const BulkBar = ({
  count, where, query, labels, onDone, onClear,
}: {
  count: number;
  /** The folder and additions, as a person reads them: "Inbox › Low". */
  where: string;
  query: string;
  /** Labels in use, offered when labelling. */
  labels: string[];
  onDone: (said: string, result: BulkResult) => void;
  onClear: () => void;
}) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [labelling, setLabelling] = useState(false);
  const [label, setLabel] = useState('');
  const n = count.toLocaleString();

  const run = async (action: BulkAction, said: string, value = '') => {
    setBusy(true);
    setError(null);
    try {
      const out = await bulkApply(query, action, value);
      onDone(`${said} ${out.changed.toLocaleString()} of ${out.matched.toLocaleString()}${out.capped ? ' (the newest; run it again for the rest)' : ''}.`, out);
    } catch (e: any) {
      setError(e.message ?? String(e));
    } finally {
      setBusy(false);
    }
  };

  const button = (icon: React.ReactNode, text: string, onClick: () => void) => (
    <button type="button" disabled={busy} onClick={onClick}
      className="flex items-center gap-1 border border-border px-2 py-0.5 hover:bg-accent/40 disabled:opacity-50">
      {icon}{text}
    </button>
  );

  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-primary/40 bg-primary/10 px-3 py-1.5 text-xs"
      role="region" aria-label="Every match selected">
      <span className="font-semibold text-primary">All {n} in {where} selected</span>
      <div className="flex flex-wrap items-center gap-1">
        {button(<Archive className="h-3 w-3" />, 'Archive', () => run('archive', 'Archived'))}
        {button(<MailOpen className="h-3 w-3" />, 'Mark read', () => run('read', 'Marked read'))}
        {button(<Mail className="h-3 w-3" />, 'Mark unread', () => run('unread', 'Marked unread'))}
        {button(<Star className="h-3 w-3" />, 'Star', () => run('star', 'Starred'))}
        {button(<Inbox className="h-3 w-3" />, 'Move to Inbox', () => run('inbox', 'Moved to the inbox'))}
        {button(<Trash2 className="h-3 w-3" />, 'Trash', () => run('trash', 'Trashed'))}
        {button(<AlertOctagon className="h-3 w-3" />, 'Spam', () => run('spam', 'Marked as spam'))}
        {labelling ? (
          <form className="flex items-center gap-1"
            onSubmit={e => { e.preventDefault(); if (label.trim()) { setLabelling(false); void run('label', `Labelled “${label.trim()}”`, label.trim()); } }}>
            <input autoFocus list="bulk-labels" aria-label="Label to add" value={label} onChange={e => setLabel(e.target.value)}
              onKeyDown={e => { if (e.key === 'Escape') setLabelling(false); }}
              className="w-32 bg-background border border-border px-1.5 py-0.5" placeholder="Label" />
            <datalist id="bulk-labels">{labels.map(l => <option key={l} value={l} />)}</datalist>
            <button type="submit" className="border border-border px-2 py-0.5 hover:bg-accent/40">Add</button>
          </form>
        ) : button(<Tag className="h-3 w-3" />, 'Label…', () => setLabelling(true))}
      </div>
      {error && <span className="text-destructive">{error}</span>}
      <button type="button" onClick={onClear} className="ml-auto flex items-center gap-1 text-muted-foreground hover:text-foreground">
        <X className="h-3 w-3" /> Clear
      </button>
    </div>
  );
};
