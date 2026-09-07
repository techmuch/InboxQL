import { useMemo } from 'react';
import { useState } from 'react';
import { Filter, Mail, MailOpen, Star, X } from 'lucide-react';
import {
  describeSelection, narrowable, selectedRefs, useSelectionStore,
} from '../../lib/selection';

interface SelectionBarProps {
  /** Narrow the query to exactly what is selected. */
  onNarrow: (field: string, values: string[]) => void;
  /** The message ids in the selection, if any. */
  messageIds: string[];
  /** Mark those messages read, unread or starred. */
  onSetFlag: (flag: '\\Seen' | '\\Flagged', on: boolean) => Promise<void>;
}

/**
 * What is selected, and what can be done with it.
 *
 * # Why it is here and not in the message list
 *
 * It used to live inside the message-list branch, so it existed in exactly one
 * of the five modes Desk can be in. Selection is not a property of messages;
 * it is a property of Desk.
 *
 * # Why "Narrow to these" is always offered
 *
 * Everything else in Desk is a query, so a selection should be able to become
 * one — that is the action that needs no new backend and no new concept, and
 * it is the one that composes with everything the user already knows. Bulk
 * actions sit beside it as they arrive.
 */
export const SelectionBar = ({ onNarrow, messageIds, onSetFlag }: SelectionBarProps) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // The selector returns the stored object, which is a stable reference. A
  // selector that derived the array inline returned a new one on every render,
  // and zustand treats that as a changed snapshot — an infinite loop.
  const stored = useSelectionStore(s => s.refs);
  const clear = useSelectionStore(s => s.clear);
  const refs = useMemo(() => selectedRefs(stored), [stored]);

  if (refs.length === 0) return null;

  const narrow = narrowable(refs);

  return (
    // shrink-0 because this sits in a flex column beside a flex-1 results
    // pane. Without it the bar is squashed to zero height — present in the
    // DOM, styled correctly, and invisible.
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border bg-primary/5 px-3 py-1.5 text-xs">
      <span className="font-semibold text-primary">{describeSelection(refs)} selected</span>

      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={() => narrow && onNarrow(narrow.field, narrow.values)}
          disabled={!narrow}
          title={
            narrow
              ? `Show only these — ${narrow.field}:(…)`
              : 'A mixed selection names more than one field, so it is more than one query'
          }
          className="flex items-center gap-1 border border-border px-2 py-0.5 hover:bg-accent/40 disabled:opacity-40"
        >
          <Filter className="h-3 w-3" /> Narrow to these
        </button>

        {/* Only for messages: a conversation and a ticket have no read flag,
            and offering an action that cannot apply is worse than not
            offering it. */}
        {messageIds.length > 0 && (
          <>
            <FlagButton
              icon={<MailOpen className="h-3 w-3" />}
              label="Mark read"
              busy={busy}
              onClick={() => run('\\Seen', true)}
            />
            <FlagButton
              icon={<Mail className="h-3 w-3" />}
              label="Mark unread"
              busy={busy}
              onClick={() => run('\\Seen', false)}
            />
            <FlagButton
              icon={<Star className="h-3 w-3" />}
              label="Star"
              busy={busy}
              onClick={() => run('\\Flagged', true)}
            />
          </>
        )}
      </div>

      {error && <span className="text-destructive">{error}</span>}

      <button
        type="button"
        onClick={clear}
        className="ml-auto flex items-center gap-1 text-muted-foreground hover:text-foreground"
      >
        <X className="h-3 w-3" /> Clear
      </button>
    </div>
  );

  async function run(flag: '\\Seen' | '\\Flagged', on: boolean) {
    setBusy(true);
    setError(null);
    try {
      await onSetFlag(flag, on);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
};

const FlagButton = ({
  icon, label, busy, onClick,
}: {
  icon: React.ReactNode; label: string; busy: boolean; onClick: () => void;
}) => (
  <button
    type="button"
    onClick={onClick}
    disabled={busy}
    // Said on the control rather than in a footnote: this changes what InboxQL
    // knows, and sync is one-way.
    title={`${label} in InboxQL. Sync is one-way, so the mail server is not told.`}
    className="flex items-center gap-1 border border-border px-2 py-0.5 hover:bg-accent/40 disabled:opacity-40"
  >
    {icon} {label}
  </button>
);
