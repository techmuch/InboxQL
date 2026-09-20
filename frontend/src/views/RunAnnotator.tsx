import { useCallback, useEffect, useState } from 'react';
import { Loader2, Play } from 'lucide-react';

/**
 * Running one annotator against the message in front of you.
 *
 * # Why this is a button and not a trigger
 *
 * The wish it answers — "annotate this when I open it" — is better served
 * explicitly. Reading a message should not write annotations as a side effect,
 * and at fifteen to twenty seconds for a span engine the result would not be
 * there when you looked anyway. So it is an act, with a spinner that tells the
 * truth about how long it takes.
 *
 * # Why only the ones that have not run
 *
 * "Has this annotator seen this message" is the three-valued status: a missing
 * row means never evaluated, which is exactly the set worth offering. One that
 * has already answered is not re-offered, because re-running it would produce
 * the same answer more slowly.
 */

export interface Offer {
  name: string;
  kind: string;
  engine: string;
  evaluated: boolean;
  records: number;
  ruled: boolean;
  slow: boolean;
}

/**
 * Which annotators could say something about this message.
 *
 * Lifted out of the control because the viewer needs it too: a Values tab
 * shown only when there are already marks would hide the one button that
 * could produce the first ones.
 */
export const useMessageOffers = (messageId: string | undefined) => {
  const [offers, setOffers] = useState<Offer[]>([]);

  const load = useCallback(() => {
    if (!messageId) {
      setOffers([]);
      return;
    }
    fetch(`/api/messages/${encodeURIComponent(messageId)}/annotators`)
      .then(r => (r.ok ? r.json() : []))
      .then(setOffers)
      .catch(() => setOffers([]));
  }, [messageId]);

  useEffect(load, [load]);
  return { offers, reload: load };
};

export const RunAnnotator = ({
  messageId,
  offers,
  reload,
  onFinished,
}: {
  messageId: string;
  offers: Offer[];
  reload: () => void;
  /** Called when a run completes, so the spans can be re-read. */
  onFinished: () => void;
}) => {
  const [running, setRunning] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const load = reload;

  const run = async (name: string) => {
    setRunning(name);
    setError(null);
    setNote(null);
    try {
      const r = await fetch('/api/annotators/run', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        // One message, by id. The same scope a run over a mailbox takes,
        // narrowed to the thing being read.
        body: JSON.stringify({ name, scope: `id:${messageId}`, limit: 1 }),
      });
      if (!r.ok) throw new Error((await r.text()) || 'the run failed');
      const out = await r.json();
      // An annotator that looked and found nothing is a real answer, and
      // saying so is better than a button that appears to have done nothing.
      if (!out.records) setNote(`${name} found nothing here`);
      load();
      onFinished();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'the run failed');
    } finally {
      setRunning(null);
    }
  };

  const unrun = offers.filter(o => !o.evaluated);
  if (offers.length === 0) return null;

  return (
    <div className="flex flex-wrap items-center gap-2 rounded border border-border bg-muted/20 px-3 py-2 text-xs">
      <span className="font-bold uppercase tracking-wider text-muted-foreground">Run</span>

      {unrun.length === 0 ? (
        <span className="text-muted-foreground">
          every annotator has seen this message
        </span>
      ) : (
        unrun.map(o => (
          <button
            key={o.name}
            type="button"
            disabled={running !== null}
            onClick={() => run(o.name)}
            title={
              o.slow
                ? `${o.engine} — around 20 seconds for one message`
                : `${o.engine} — instant`
            }
            className="flex items-center gap-1.5 border border-border px-2 py-1 font-mono hover:bg-accent/50 disabled:opacity-50"
          >
            {running === o.name ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Play className="h-3 w-3" />
            )}
            {o.name}
            {/* Said before it is pressed, not discovered after. */}
            {o.slow && <span className="opacity-60">~20s</span>}
          </button>
        ))
      )}

      {running && (
        <span className="text-muted-foreground">
          reading the message with {running}…
        </span>
      )}
      {note && <span className="text-muted-foreground">{note}</span>}
      {error && <span className="text-destructive">{error}</span>}

      {offers.some(o => o.ruled) && (
        <span className="ml-auto text-[11px] text-muted-foreground">
          {/* A re-run will not touch a message someone has ruled on, so a
              button that looked available would do nothing. */}
          you have corrected this message · re-runs leave it alone
        </span>
      )}
    </div>
  );
};
