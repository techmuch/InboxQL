import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ChevronLeft, ChevronRight, ExternalLink, Loader2 } from 'lucide-react';
import { openMessageByID } from '../../lib/tabs';
import { getDisagreements, resolveDisagreement, type Answer, type Choice, type Disagreement } from './api';

/** Stable per disagreement, so the two answers do not swap places on a re-render. */
const flipped = (id: string) => [...id].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7) % 2 === 1;

const isTyping = (t: EventTarget | null) =>
  t instanceof HTMLElement && (t.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName));

const show = (v: unknown) => (typeof v === 'number' ? v.toLocaleString() : String(v));

const AnswerCard = ({ label, answer, says }: { label: string; answer: Answer; says: string }) => (
  <div className="border border-border bg-background p-3">
    <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{label}</h4>
    <p className="mt-1 text-sm font-medium">{says || '—'}</p>
    {answer.records && answer.records.length > 0 && (
      <ul className="mt-2 space-y-2 text-xs">
        {answer.records.map((r, i) => (
          <li key={i}>
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5">
              {Object.entries(r).filter(([k, v]) => k !== 'quote' && k !== 'why' && v !== '' && v !== null && typeof v !== 'object').map(([k, v]) => (
                <div key={k} className="contents"><dt className="text-muted-foreground">{k}</dt><dd className="tabular-nums">{show(v)}</dd></div>
              ))}
              {Array.isArray(r.items) && (
                <div className="contents"><dt className="text-muted-foreground">items</dt>
                  <dd>{(r.items as Record<string, unknown>[]).map(it => String(it.name ?? '')).filter(Boolean).join('; ')}</dd></div>
              )}
            </dl>
            {typeof r.quote === 'string' && r.quote && <blockquote className="mt-1 border-l-2 border-border pl-2 italic text-muted-foreground">“{r.quote}”</blockquote>}
          </li>
        ))}
      </ul>
    )}
    {answer.quote && <blockquote className="mt-2 border-l-2 border-border pl-2 text-xs italic text-muted-foreground">“{answer.quote}”</blockquote>}
    {(answer.why || answer.none || answer.skip) && (
      <p className="mt-2 text-xs text-muted-foreground">{answer.why || answer.none || answer.skip}</p>
    )}
    {answer.confidence !== undefined && (
      <p className="mt-1 text-[11px] text-muted-foreground">Said it was {Math.round(answer.confidence * 100)}% sure</p>
    )}
  </div>
);

/**
 * Where a check and the first read disagreed, one at a time, for you to say
 * which was right.
 *
 * # The two answers are not labelled
 *
 * Shown which was the first read and which the check — or which model gave
 * each, or that one was an untouched guess — you would lean towards one before
 * reading. So they are Answer A and Answer B, in an order fixed per
 * disagreement but random across them, and who said what is told afterwards.
 */
export const Disagreements = ({ onSettled }: { onSettled: () => void }) => {
  const [items, setItems] = useState<Disagreement[] | null>(null);
  const [index, setIndex] = useState(0);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState('');
  const [error, setError] = useState<string | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);

  const load = useCallback(async () => {
    try {
      const r = await getDisagreements();
      setItems(r.disagreements);
      setIndex(0);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not load the disagreements');
      setItems([]);
    }
  }, []);
  useEffect(() => { void load(); }, [load]);

  const item = items?.[index];
  const flip = item ? flipped(item.id) : false;
  const [a, b] = useMemo(() => {
    if (!item) return [null, null];
    const first = { answer: item.originalAnswer, says: item.originalSays, who: 'original' as const };
    const check = { answer: item.checkerAnswer, says: item.checkerSays, who: 'checker' as const };
    return flip ? [check, first] : [first, check];
  }, [item, flip]);

  const decide = useCallback(async (pick: 'a' | 'b' | 'neither') => {
    if (!item || !a || !b || busy) return;
    const choice: Choice = pick === 'neither' ? 'neither' : (pick === 'a' ? a.who : b.who);
    setBusy(true); setError(null);
    try {
      await resolveDisagreement(item.id, choice);
      const first = `the first read${item.originalModel ? ` (${item.originalModel}${item.originalPrefilled ? ', an untouched guess' : ''})` : item.originalPrefilled ? ' (an untouched guess)' : ''}`;
      const check = `the check${item.checkerModel ? ` (${item.checkerModel})` : ''}`;
      const reveal = `A was ${a.who === 'original' ? first : check}; B was ${b.who === 'original' ? first : check}.`;
      setStatus(`Saved for “${item.subject || 'this conversation'}”: ${pick === 'neither'
        ? 'neither — flagged to be read again.' : `answer ${pick.toUpperCase()} was right.`} ${reveal}`);
      setItems(list => {
        const next = (list ?? []).filter(x => x.id !== item.id);
        setIndex(i => Math.min(i, Math.max(0, next.length - 1)));
        return next;
      });
      onSettled();
      headingRef.current?.focus();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not save that');
    } finally {
      setBusy(false);
    }
  }, [item, a, b, busy, onSettled]);

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (isTyping(e.target) || e.metaKey || e.ctrlKey || e.altKey) return;
    const keys: Record<string, () => void> = {
      '1': () => void decide('a'),
      '2': () => void decide('b'),
      '3': () => void decide('neither'),
      j: () => setIndex(i => Math.min(i + 1, (items?.length ?? 1) - 1)),
      k: () => setIndex(i => Math.max(i - 1, 0)),
    };
    const run = keys[e.key];
    if (run) { e.preventDefault(); run(); }
  };

  const btn = 'inline-flex items-center gap-1 border border-border px-3 py-1.5 text-sm hover:bg-accent disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary';

  return (
    // Focusable, so a click anywhere in it puts the keys below in reach.
    <section aria-labelledby="wl-disagreements" onKeyDown={onKeyDown} tabIndex={-1}
      className="border border-border bg-card p-4 focus:outline-none focus-visible:ring-1 focus-visible:ring-primary">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="wl-disagreements" className="text-sm font-semibold">
          Waiting for you{items ? <span className="ml-2 font-normal text-muted-foreground">{items.length}</span> : null}
        </h2>
        <p className="text-[11px] text-muted-foreground">Keys: <kbd>1</kbd> A right · <kbd>2</kbd> B right · <kbd>3</kbd> neither · <kbd>j</kbd>/<kbd>k</kbd> next/previous</p>
      </div>
      <p role="status" aria-live="polite" className="mt-1 min-h-4 text-xs text-muted-foreground">{status}</p>
      {error && <p role="alert" className="mt-1 text-xs text-destructive">{error}</p>}

      {items === null ? (
        <p className="mt-2 flex items-center gap-2 text-xs text-muted-foreground"><Loader2 className="h-3 w-3 animate-spin" aria-hidden /> Loading…</p>
      ) : !item ? (
        <p className="mt-2 text-xs text-muted-foreground">
          Nothing to settle. When a check disagrees with the first read, it waits here for you. Checks are run by your
          harness with <code className="font-mono">iql work audit next</code>.
        </p>
      ) : (
        <article aria-labelledby="wl-item" className="mt-3 space-y-3">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <p className="text-xs text-muted-foreground">{index + 1} of {items.length} · {item.element}{item.about ? ` — ${item.about}` : ''}</p>
              <h3 id="wl-item" ref={headingRef} tabIndex={-1} className="truncate text-base font-medium focus:outline-none">
                {item.subject || '(no subject)'}
              </h3>
              <p className="truncate text-xs text-muted-foreground">
                {item.from}{item.date ? ` · ${new Date(item.date).toLocaleDateString()}` : ''}{item.unit === 'sender' ? ` · about ${item.target}` : ''}
              </p>
            </div>
            <div className="flex shrink-0 gap-1">
              <button type="button" className={btn} onClick={() => openMessageByID(item.messageId)}>
                <ExternalLink className="h-3.5 w-3.5" aria-hidden /> Open the message
              </button>
              <button type="button" className={btn} aria-label="Previous disagreement" disabled={index === 0} onClick={() => setIndex(i => i - 1)}>
                <ChevronLeft className="h-3.5 w-3.5" aria-hidden />
              </button>
              <button type="button" className={btn} aria-label="Next disagreement" disabled={index >= items.length - 1} onClick={() => setIndex(i => i + 1)}>
                <ChevronRight className="h-3.5 w-3.5" aria-hidden />
              </button>
            </div>
          </div>
          {item.snippet && <p className="line-clamp-3 text-xs text-muted-foreground">{item.snippet}</p>}
          {item.question && (
            <details className="text-xs">
              <summary className="cursor-pointer text-muted-foreground hover:text-foreground">What was asked</summary>
              <p className="mt-1 whitespace-pre-wrap">{item.question}</p>
            </details>
          )}
          <div className="grid gap-3 md:grid-cols-2">
            <AnswerCard label="Answer A" answer={a!.answer} says={a!.says} />
            <AnswerCard label="Answer B" answer={b!.answer} says={b!.says} />
          </div>
          <div className="flex flex-wrap gap-2" role="group" aria-label="Which answer is right?">
            <button type="button" className={btn} disabled={busy} aria-keyshortcuts="1" onClick={() => void decide('a')}>A is right</button>
            <button type="button" className={btn} disabled={busy} aria-keyshortcuts="2" onClick={() => void decide('b')}>B is right</button>
            <button type="button" className={btn} disabled={busy} aria-keyshortcuts="3" onClick={() => void decide('neither')}>Neither — read it again</button>
          </div>
          <p className="text-[11px] text-muted-foreground">
            Your answer becomes a ruling that outranks both. Who gave which answer is shown after you choose.
          </p>
        </article>
      )}
    </section>
  );
};
