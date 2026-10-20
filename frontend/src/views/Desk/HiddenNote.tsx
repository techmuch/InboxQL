import { Trash2 } from 'lucide-react';
import type { QueryResult } from './api';
import { Help } from '../Help';

/** As far as the server counts; past it the note says "1,000+". */
export const HIDDEN_CAP = 1000;

/**
 * What a query left out of Trash and Spam, and the way to get it back.
 *
 * A query that does not mention them leaves them out, as Gmail's search does.
 * Saying so is what keeps that from being a mystery: a message you are sure
 * exists and cannot find is worse than one you can see is in the bin.
 */
export function hiddenText(result: Pick<QueryResult, 'hidden' | 'leftOut'>): string | null {
  const n = result.hidden ?? 0;
  if (n <= 0) return null;
  const count = n >= HIDDEN_CAP ? `${HIDDEN_CAP.toLocaleString()}+` : n.toLocaleString();
  const bins = (result.leftOut ?? ['trash', 'spam'])
    .map(b => b.charAt(0).toUpperCase() + b.slice(1))
    .join(' or ');
  return `${count} more in ${bins}`;
}

export const HiddenNote = ({ result, onShow }: {
  result: Pick<QueryResult, 'hidden' | 'leftOut'> | null;
  onShow: () => void;
}) => {
  const text = result && hiddenText(result);
  if (!text) return null;
  return (
    <div className="flex items-center gap-2 border-b border-border bg-muted/30 px-4 py-1.5 text-xs text-muted-foreground" role="status">
      <Trash2 className="h-3 w-3 shrink-0" />
      <span>{text}, not shown because the query does not mention {(result?.leftOut?.length ?? 2) === 1 ? 'it' : 'them'}.</span>
      <button
        type="button"
        onClick={onShow}
        title="Add folder:all to the query"
        className="font-semibold text-primary hover:underline cursor-pointer"
      >
        Show them
      </button>
      <Help topic="trash-and-spam" />
    </div>
  );
};
