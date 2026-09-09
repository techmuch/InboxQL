import { useEffect, useRef, useState } from 'react';
import { AlertCircle, Sparkles } from 'lucide-react';
import { openQuery } from '../lib/tabs';

interface Distribution {
  buckets: number[];
  counts: Record<string, number>;
  threshold: number;
}

/**
 * "More like this" — the door onto `similar:`.
 *
 * # Why it opens a menu rather than just running
 *
 * A similarity threshold is not portable. Cosine values sit in a band whose
 * width the embedding model decides, and on a real archive with bge-m3 every
 * neighbour of a message fell between 0.26 and 0.65 — so a threshold that
 * sounds strict matches nothing, silently and forever. Showing how many
 * messages sit at each level turns the choice into a decision instead of a
 * guess, and it costs one request the button is already making.
 *
 * # Why it says why it cannot work
 *
 * A message with no vector cannot have neighbours. Doing nothing, or running a
 * query that matches nothing, would read as "there is nothing similar" — the
 * failure shape this project keeps meeting. It says the mailbox has not been
 * embedded, and how to embed it.
 */
export const SimilarButton = ({ messageId }: { messageId: string }) => {
  const [open, setOpen] = useState(false);
  const [dist, setDist] = useState<Distribution | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setOpen(false);
    setDist(null);
    setError(null);
  }, [messageId]);

  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', away);
    return () => document.removeEventListener('mousedown', away);
  }, [open]);

  const load = async () => {
    setOpen(true);
    if (dist || loading) return;
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`/api/llm/similarity?id=${encodeURIComponent(messageId)}`);
      const body = await res.json().catch(() => null);
      if (!res.ok) throw new Error(body?.error ?? 'could not measure similarity');
      setDist(body as Distribution);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  const run = (threshold: number) => {
    setOpen(false);
    openQuery(`similar:${messageId}>${threshold.toFixed(2)}`);
  };

  return (
    <div ref={box} className="relative">
      <button
        type="button"
        onClick={load}
        aria-expanded={open}
        className="flex items-center gap-2 border border-border px-6 py-2 text-sm font-medium transition-colors hover:bg-accent"
      >
        <Sparkles className="h-4 w-4" /> More like this
      </button>

      {open && (
        <div className="absolute bottom-full left-0 z-30 mb-1 w-72 border border-border bg-popover p-3 shadow-lg">
          {loading && <p className="text-xs text-muted-foreground">Measuring…</p>}

          {error && (
            <div className="flex items-start gap-2 text-xs text-destructive">
              <AlertCircle className="mt-px h-3.5 w-3.5 shrink-0" />
              <span>
                {error}
                <span className="mt-1 block text-muted-foreground">
                  Similarity needs embeddings. Run{' '}
                  <code className="font-mono">iql annotate embed --profile &lt;name&gt;</code>.
                </span>
              </span>
            </div>
          )}

          {dist && (
            <>
              <p className="mb-2 text-[10px] font-bold uppercase tracking-wider text-muted-foreground">
                How alike?
              </p>
              <ul className="space-y-0.5">
                {dist.buckets.map(b => {
                  const n = dist.counts[b.toFixed(2)] ?? 0;
                  const widest = Math.max(...dist.buckets.map(x => dist.counts[x.toFixed(2)] ?? 0), 1);
                  return (
                    <li key={b}>
                      <button
                        type="button"
                        onClick={() => run(b)}
                        disabled={n === 0}
                        className="relative flex w-full items-center gap-2 px-1 py-0.5 text-xs disabled:opacity-40 enabled:hover:bg-accent/60"
                      >
                        {/* The bar is the point: the numbers alone do not show
                            how sharply the band falls off. */}
                        <span
                          aria-hidden="true"
                          className="absolute inset-y-0 left-0 bg-primary/15"
                          style={{ width: `${(n / widest) * 100}%` }}
                        />
                        <span className="relative w-10 text-left font-mono tabular-nums">
                          {b.toFixed(2)}
                        </span>
                        <span className="relative flex-1 text-left text-muted-foreground">
                          {n === 0 ? 'nothing' : `${n} message${n === 1 ? '' : 's'}`}
                        </span>
                        {Math.abs(b - dist.threshold) < 0.001 && (
                          <span className="relative text-[10px] text-primary">default</span>
                        )}
                      </button>
                    </li>
                  );
                })}
              </ul>
              <p className="mt-2 text-[10px] leading-relaxed text-muted-foreground">
                Higher is more alike. The usable range depends on the embedding model, which is
                why the counts are here rather than a bare number.
              </p>
            </>
          )}
        </div>
      )}
    </div>
  );
};
