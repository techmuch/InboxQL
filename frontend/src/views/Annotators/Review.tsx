import { useCallback, useEffect, useRef, useState } from 'react';
import { Check, X } from 'lucide-react';

interface Level { label: string; describe?: string }

interface ReviewItem {
  messageId: string;
  from: string;
  subject: string;
  date: string;
  snippet: string;
  machine: { matched: boolean; level?: string; score?: number };
}

export interface AnnotatorScore {
  annotator: string;
  engine: string;
  reviewed: number;
  inflow: number;
  right: number;
  accuracy: number;
  cutoff?: number;
  atCutoff?: number;
}

/** Fewer reviewed rulings than this and the number is shown with a warning. */
const ENOUGH = 20;

/**
 * How often an annotator agrees with you, on the rulings that can say so.
 *
 * Only reviewed rulings count — a random draw. Rulings made while reading are
 * mostly corrections, so scoring on them would say the model is mostly wrong
 * whatever it does. They are counted here so nobody wonders where they went.
 */
export const ScoreLine = ({ score }: { score: AnnotatorScore | null }) => {
  if (!score) return null;
  if (score.reviewed === 0) {
    return (
      <span className="text-[11px] text-muted-foreground">
        Not reviewed yet{score.inflow > 0 ? ` · ${score.inflow} correction${score.inflow === 1 ? '' : 's'} while reading` : ''}
      </span>
    );
  }
  return (
    <span className="text-[11px] tabular-nums text-muted-foreground">
      <span className="font-medium text-foreground">
        {score.right} of {score.reviewed} right ({Math.round(score.accuracy * 100)}%)
      </span>
      {score.cutoff !== undefined && score.atCutoff !== undefined && score.atCutoff > score.right && (
        // The cutoff is where an uncalibrated model is most often wrong: it
        // can rank correctly and still put 0.5 in the wrong place.
        <> · {score.atCutoff} at a cutoff of {score.cutoff > 1 ? 'never' : score.cutoff.toFixed(2)}</>
      )}
      {score.reviewed < ENOUGH && <span className="text-amber-600 dark:text-amber-400"> · too few to trust</span>}
    </span>
  );
};

/**
 * Ten messages drawn at random across the annotator's scores, ruled one key at
 * a time.
 *
 * # The model's answer is hidden until you have given yours
 *
 * Shown first, it anchors: reviewers agree with an answer they have just seen
 * more often than they would have reached it themselves, and that inflates
 * exactly the number this exists to measure. It appears afterwards, for a
 * moment, so you can see where it was wrong.
 */
export const ReviewPanel = ({ name, question, levels, onClose }: {
  name: string;
  /** The annotator's instruction, phrased as the question you are answering. */
  question: string;
  levels: Level[];
  onClose: (ruled: number) => void;
}) => {
  const [items, setItems] = useState<ReviewItem[] | null>(null);
  const [index, setIndex] = useState(0);
  const [ruled, setRuled] = useState(0);
  const [reveal, setReveal] = useState<{ agreed: boolean; said: string } | null>(null);

  useEffect(() => {
    fetch(`/api/annotators/review?name=${encodeURIComponent(name)}&n=10`)
      .then(r => (r.ok ? r.json() : []))
      .then(list => setItems(Array.isArray(list) ? list : []))
      .catch(() => setItems([]));
  }, [name]);

  const item = items?.[index];

  const next = useCallback(() => {
    busy.current = false;
    setReveal(null);
    setIndex(i => i + 1);
  }, []);

  // Set synchronously, because `reveal` is only set once the ruling has been
  // saved. Between the key press and that, a second press — a held key, a
  // quick double tap — would otherwise rule the same message twice.
  const busy = useRef(false);

  const rule = useCallback(async (matched: boolean, level?: string) => {
    if (!item || reveal || busy.current) return;
    busy.current = true;
    const res = await fetch(
      `/api/messages/${encodeURIComponent(item.messageId)}/labels/${encodeURIComponent(name)}`,
      {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ matched, level, via: 'review' }),
      },
    );
    if (!res.ok) { busy.current = false; return; }
    setRuled(n => n + 1);
    const m = item.machine;
    const agreed = level ? m.level === level : m.matched === matched;
    setReveal({ agreed, said: m.level ?? (m.matched ? 'yes' : 'no') });
    setTimeout(next, 900);
  }, [item, name, reveal, next]);

  // One listener for the panel's whole life, reading the latest state through a
  // ref. Re-subscribing on every change left a frame after each one in which a
  // key press reached the old handler and was dropped.
  const onKey = useRef<(e: KeyboardEvent) => void>(() => {});
  onKey.current = (e: KeyboardEvent) => {
    if (e.key === 'Escape') { onClose(ruled); return; }
    if (!item || reveal || busy.current) return;
    if (e.key === 's') { next(); return; }
    if (levels.length > 0) {
      const n = Number(e.key);
      if (n >= 1 && n <= levels.length) rule(true, levels[n - 1].label);
    } else if (e.key === 'y') {
      rule(true);
    } else if (e.key === 'n') {
      rule(false);
    }
  };
  useEffect(() => {
    const listener = (e: KeyboardEvent) => onKey.current(e);
    window.addEventListener('keydown', listener);
    return () => window.removeEventListener('keydown', listener);
  }, []);

  const done = items !== null && index >= items.length;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4"
      role="dialog" aria-label={`Review ${name}`}>
      <div className="w-full max-w-xl border border-border bg-card shadow-xl">
        <div className="flex items-center justify-between border-b border-border px-4 py-2 text-xs">
          <span className="font-medium">Review · {name}</span>
          <span className="tabular-nums text-muted-foreground">
            {items === null ? 'drawing…' : done ? `${ruled} ruled` : `${index + 1} of ${items.length}`}
          </span>
        </div>

        {items !== null && items.length === 0 && (
          <p className="px-4 py-6 text-sm text-muted-foreground">
            Nothing to review: every message it has answered has a ruling, or it has not answered any yet.
          </p>
        )}

        {item && !done && (
          <div className="space-y-3 px-4 py-4">
            {/* The question, not the model's answer. */}
            <p className="text-sm font-medium">{question}</p>
            <div className="border border-border bg-muted/20 px-3 py-2">
              <div className="truncate text-xs text-muted-foreground">{item.from} · {new Date(item.date).toLocaleDateString()}</div>
              <div className="truncate text-sm font-medium">{item.subject || '(no subject)'}</div>
              <p className="mt-1 line-clamp-4 whitespace-pre-line text-xs text-muted-foreground">{item.snippet}</p>
            </div>

            {reveal ? (
              <p className={`flex items-center gap-1.5 text-xs ${reveal.agreed ? 'text-green-600' : 'text-destructive'}`}>
                {reveal.agreed ? <Check className="h-3.5 w-3.5" /> : <X className="h-3.5 w-3.5" />}
                The model said {reveal.said}.
              </p>
            ) : levels.length > 0 ? (
              <div className="flex flex-wrap gap-2">
                {levels.map((l, i) => (
                  <button key={l.label} type="button" onClick={() => rule(true, l.label)}
                    title={l.describe}
                    className="border border-border px-2 py-1 text-xs hover:bg-accent">
                    <kbd className="mr-1 text-muted-foreground">{i + 1}</kbd>{l.label}
                  </button>
                ))}
              </div>
            ) : (
              <div className="flex gap-2">
                <button type="button" onClick={() => rule(true)} className="border border-border px-3 py-1 text-xs hover:bg-accent">
                  <kbd className="mr-1 text-muted-foreground">y</kbd>Yes
                </button>
                <button type="button" onClick={() => rule(false)} className="border border-border px-3 py-1 text-xs hover:bg-accent">
                  <kbd className="mr-1 text-muted-foreground">n</kbd>No
                </button>
              </div>
            )}
          </div>
        )}

        <div className="flex items-center justify-between border-t border-border px-4 py-2 text-[11px] text-muted-foreground">
          <span><kbd>s</kbd> skip · <kbd>Esc</kbd> close</span>
          <button type="button" onClick={() => onClose(ruled)} className="hover:text-foreground">
            {done ? 'Done' : 'Close'}
          </button>
        </div>
      </div>
    </div>
  );
};
