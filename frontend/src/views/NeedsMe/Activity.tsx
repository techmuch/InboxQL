import { useEffect, useState } from 'react';
import { queryCount } from '../../lib/counts';
import { openQuery, openTool } from '../../lib/tabs';
import { WorkLoop } from '../WorkLoop';

/**
 * Agent activity: what agents did, and how often they are right.
 *
 * The lead is what agents produced that is still yours to rule on, each a
 * query you can open; below it, the work loop — passes, accuracy on your
 * checks, and the disagreements to settle.
 */
export const Activity = () => {
  const [n, setN] = useState<Record<string, number | null>>({});
  useEffect(() => {
    const ctl = new AbortController();
    const qs: Record<string, string> = {
      drafts: 'in:drafts origin:agent', queuedDrafts: 'in:drafts origin:agent status:queued',
      cards: 'in:tickets raised:agent', proposedCards: 'in:tickets raised:agent state:proposed',
      events: 'in:events is:proposed',
    };
    Promise.all(Object.entries(qs).map(async ([k, q]) => [k, await queryCount(q, ctl.signal)] as const))
      .then(v => { if (!ctl.signal.aborted) setN(Object.fromEntries(v)); });
    return () => ctl.abort();
  }, []);
  const tile = (label: string, total: number | null | undefined, waiting: number | null | undefined, q: string) => (
    <li className="border border-border bg-card px-4 py-3">
      <button type="button" className="block w-full text-left" onClick={() => openQuery(q)}>
        <span className="block text-2xl font-semibold tabular-nums">{total ?? '–'}</span>
        <span className="block font-medium">{label}</span>
        {waiting != null && <span className="block text-sm text-muted-foreground">{waiting} waiting for you</span>}
      </button>
    </li>
  );
  return (
    <div className="h-full overflow-auto">
      <div className="mx-auto max-w-5xl px-6 pt-5">
        <h1 className="text-2xl font-semibold">Agent activity</h1>
        <p className="mt-1 text-[15px] text-muted-foreground">What agents wrote and proposed, and how often they are right. Nothing an agent proposes takes effect until you approve it.</p>
        <ul className="mt-4 grid gap-2 sm:grid-cols-3">
          {tile('Messages written', n.drafts, n.queuedDrafts, 'in:drafts origin:agent')}
          {tile('Cards raised', n.cards, n.proposedCards, 'in:tickets raised:agent')}
          {tile('Calendar proposals', n.events, n.events, 'in:events is:proposed')}
        </ul>
        <p className="mt-2 text-sm"><button type="button" className="underline" onClick={() => openTool('review', 'To review')}>Go to To review</button> to rule on what is waiting.</p>
      </div>
      <WorkLoop />
    </div>
  );
};
