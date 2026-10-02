import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, Loader2, Package, Zap } from 'lucide-react';

/**
 * A pack of annotators worth beginning from — as switches, not an install.
 *
 * # Why this tab exists
 *
 * An annotator is the most capable thing here and the hardest to start using:
 * invent a schema, choose an engine, write a scope, run a batch job, then go
 * looking for what it did. The pack is a first draft of eleven, meant to be
 * edited rather than obeyed.
 *
 * # Why a tick cannot mean "exists"
 *
 * `annotations` cascades on the annotator, so deleting one deletes everything
 * it ever said. On a worked mailbox, unticking `receipts` would cost 269
 * extracted values and 19 human corrections — and re-ticking would buy back
 * only the first, at about eleven minutes of CPU. The corrections are somebody's
 * judgement and no amount of re-running recovers them.
 *
 * So the tick means **participation**. Off stops new work: triggers skip it,
 * the message viewer stops offering it, running it by name refuses. Everything
 * it already found stays, stays queryable, and comes straight back.
 *
 * Deleting for real lives in the Annotators panel, where the record count is on
 * screen and it is a deliberate act. Two paths to the same irreversible thing,
 * one of them in a settings page next to a row of checkboxes, is how it gets
 * done by accident.
 *
 * # The mix is the lesson
 *
 * Every span extractor is gated by a cheap rule label — `receipts` is scoped
 * to `label:money`, which is a query and therefore free. The chooser shows
 * that pairing rather than hiding it, because copying it is the point: the
 * label narrows a twenty-second-a-message job to the mail that could possibly
 * match.
 */

interface Starter {
  name: string;
  kind: string;
  engine: string;
  about: string;
  scope?: string;
  fields?: string[];
  installed: boolean;
  /** Only meaningful when installed; absent and off both read as unticked. */
  enabled: boolean;
  /**
   * What an installed one is holding, in annotation rows — what a delete would
   * take. Not coverage: one message can hold many records, and on a worked
   * mailbox the two differ by roughly nine to one.
   */
  records: number;
  /** Rows a person set. Re-running recovers everything except these. */
  corrections: number;
  reach: number;
  /** The label this one waits on, when that label has not run yet. */
  gate?: string;
  /** The label this one is scoped to, when that label is switched off. */
  gateOff?: string;
  slow: boolean;
}

/** What one toggle did, worth saying once rather than logging forever. */
interface Note {
  name: string;
  text: string;
}

export const Starters = () => {
  const [starters, setStarters] = useState<Starter[]>([]);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<Set<string>>(new Set());
  const [note, setNote] = useState<Note | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    (quiet = false) => {
      if (!quiet) setLoading(true);
      fetch('/api/starters')
        .then(r => (r.ok ? r.json() : Promise.reject(new Error('could not read the pack'))))
        .then((list: Starter[]) => setStarters(Array.isArray(list) ? list : []))
        .catch(() => setError('could not read the starter pack'))
        .finally(() => setLoading(false));
    },
    [],
  );

  useEffect(() => load(), [load]);

  const toggle = async (s: Starter) => {
    const on = !(s.installed && s.enabled);
    // Optimistic, because the write is one UPDATE and a switch that waits a
    // round trip to move feels broken. Reconciled from the reload below.
    setStarters(prev =>
      prev.map(x => (x.name === s.name ? { ...x, installed: x.installed || on, enabled: on } : x)),
    );
    setPending(prev => new Set(prev).add(s.name));
    setError(null);
    setNote(null);
    try {
      const r = await fetch(`/api/starters/${encodeURIComponent(s.name)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: on }),
      });
      if (!r.ok) throw new Error((await r.text()) || 'could not change that');
      const out = await r.json();
      setNote({ name: s.name, text: describe(s.name, on, out) });
      load(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'could not change that');
      load(true);
    } finally {
      setPending(prev => {
        const next = new Set(prev);
        next.delete(s.name);
        return next;
      });
    }
  };

  if (loading) {
    return (
      <div className="p-8 text-center text-sm text-muted-foreground">
        <Loader2 className="mx-auto mb-2 h-5 w-5 animate-spin text-primary" />
        Reading the pack…
      </div>
    );
  }

  const on = starters.filter(s => s.installed && s.enabled).length;

  return (
    <div className="space-y-4 p-4">
      <p className="max-w-3xl text-xs text-muted-foreground">
        Starting points, meant to be edited. Each extractor is gated by a cheap rule label,
        because a query costs nothing and a span engine costs about twenty seconds a message.
        Switching one on creates it — nothing runs until you say so. Switching one off keeps
        everything it already found, so it is safe to change your mind.
      </p>

      <div className="space-y-2">
        {starters.map(s => {
          const live = s.installed && s.enabled;
          const busy = pending.has(s.name);
          return (
            <label
              key={s.name}
              className={`flex cursor-pointer items-start gap-3 border px-4 py-3 transition-opacity hover:bg-accent/30 ${
                live ? 'border-border' : 'border-border bg-muted/20 opacity-70'
              }`}
            >
              <input
                type="checkbox"
                className="mt-0.5"
                disabled={busy}
                checked={live}
                onChange={() => toggle(s)}
              />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{s.name}</span>
                  <span className="font-mono text-[11px] text-muted-foreground">
                    {s.kind} · {s.engine}
                  </span>
                  {/* Said before it is chosen, not discovered after. */}
                  {s.slow ? (
                    <span className="font-mono text-[10px] text-amber-600 dark:text-amber-400">
                      ~20s a message
                    </span>
                  ) : (
                    <span className="flex items-center gap-0.5 font-mono text-[10px] text-emerald-600 dark:text-emerald-400">
                      <Zap className="h-2.5 w-2.5" /> instant
                    </span>
                  )}
                  {busy && <Loader2 className="h-3 w-3 animate-spin text-primary" />}
                  {s.installed && !s.enabled && !busy && (
                    <span className="font-mono text-[10px] text-muted-foreground">off</span>
                  )}
                </div>

                <p className="mt-0.5 text-xs text-muted-foreground">{s.about}</p>

                {s.fields && s.fields.length > 0 && (
                  <p className="mt-1 font-mono text-[11px] text-muted-foreground">
                    looks for: {s.fields.join(', ')}
                  </p>
                )}

                <p className="mt-1 text-[11px] text-muted-foreground">
                  {/* What it is holding comes first for an installed one: it is
                      what unticking would stop adding to, and the number that
                      makes "nothing is lost" checkable. */}
                  {s.installed && s.records > 0 ? (
                    <>
                      <span className="font-mono tabular-nums text-foreground">
                        {s.records.toLocaleString()}
                      </span>{' '}
                      {s.records === 1 ? 'record' : 'records'}
                      {s.corrections > 0 && (
                        <>
                          {', '}
                          <span className="font-mono tabular-nums text-foreground">
                            {s.corrections.toLocaleString()}
                          </span>{' '}
                          set by hand
                        </>
                      )}
                      <span className="opacity-70">
                        {' · '}
                        {s.reach.toLocaleString()} {s.reach === 1 ? 'message' : 'messages'} in reach
                      </span>
                    </>
                  ) : (
                    <>
                      <span className="font-mono tabular-nums text-foreground">
                        {s.reach.toLocaleString()}
                      </span>{' '}
                      {s.reach === 1 ? 'message' : 'messages'} here
                      {/* An extractor gated by a label that has not run reaches
                          nothing yet — true, and useless to show as "0". */}
                      {s.gate && <span className="opacity-70"> once `{s.gate}` has run</span>}
                    </>
                  )}
                  {s.scope && <span className="opacity-70"> · scope {s.scope}</span>}
                </p>

                {/* A gate switched off freezes what it gates, for new mail
                    only. Easy to do by accident two rows apart, and invisible
                    without being told. */}
                {live && s.gateOff && (
                  <p className="mt-1 flex items-start gap-1 text-[11px] text-amber-600 dark:text-amber-400">
                    <AlertTriangle className="mt-px h-3 w-3 shrink-0" />
                    <span>
                      `{s.gateOff}` is off, so nothing is labelling new mail for this to read. It
                      still covers what `{s.gateOff}` already labelled.
                    </span>
                  </p>
                )}

                {note?.name === s.name && (
                  <p className="mt-1 text-[11px] text-muted-foreground">{note.text}</p>
                )}
              </div>
            </label>
          );
        })}
      </div>

      {error && <p className="text-xs text-destructive">{error}</p>}

      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Package className="h-3.5 w-3.5" />
        {on} of {starters.length} on. Run the free labels first; the extractors read them. Edit or
        delete them in Tools → Annotators.
      </p>
    </div>
  );
};

/**
 * What a toggle did, in the terms that matter.
 *
 * Switching off says what it kept, because the whole design rests on nothing
 * being lost and a UI that stays silent about that is asking to be distrusted.
 */
function describe(name: string, on: boolean, out: { created?: boolean; records?: number; corrections?: number }): string {
  if (on) {
    if (out.created) return `Created. Nothing has run yet — run it from Tools → Annotators.`;
    const held = out.records ?? 0;
    return held > 0
      ? `Back on, with ${held.toLocaleString()} ${held === 1 ? 'record' : 'records'} still there.`
      : `Back on.`;
  }
  const held = out.records ?? 0;
  const ruled = out.corrections ?? 0;
  if (held === 0) return `Off. It will not run; it had found nothing yet.`;
  const parts = [`${held.toLocaleString()} ${held === 1 ? 'record' : 'records'}`];
  // Named separately because re-running recovers everything except these.
  if (ruled > 0) parts.push(`${ruled.toLocaleString()} ${ruled === 1 ? 'value' : 'values'} set by hand`);
  return `Off. Kept ${parts.join(' and ')} — switch it back on and ${name} picks up where it stopped.`;
}
