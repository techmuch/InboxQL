import { useCallback, useEffect, useState } from 'react';
import { Check, Loader2, Package, Zap } from 'lucide-react';

/**
 * A pack of annotators worth beginning from.
 *
 * # Why this tab exists
 *
 * An annotator is the most capable thing here and the hardest to start using:
 * invent a schema, choose an engine, write a scope, run a batch job, then go
 * looking for what it did. The pack is a first draft of eleven, meant to be
 * edited rather than obeyed.
 *
 * # What it is not
 *
 * A second annotator editor. These create ordinary annotators; editing them
 * is the Annotators panel, which already does that well. This gets someone
 * from nothing to something worth editing.
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
  reach: number;
  gate?: string;
  slow: boolean;
}

export const Starters = () => {
  const [starters, setStarters] = useState<Starter[]>([]);
  const [chosen, setChosen] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    fetch('/api/starters')
      .then(r => (r.ok ? r.json() : []))
      .then((list: Starter[]) => {
        setStarters(list);
        // Everything not already there, because that is what someone opening
        // this tab almost always wants. Already-installed ones are never
        // touched: they may have been edited.
        setChosen(new Set(list.filter(s => !s.installed).map(s => s.name)));
      })
      .catch(() => setError('could not read the starter pack'))
      .finally(() => setLoading(false));
  }, []);

  useEffect(load, [load]);

  const toggle = (name: string) => {
    setChosen(prev => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const install = async () => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const r = await fetch('/api/starters', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ names: [...chosen] }),
      });
      if (!r.ok) throw new Error(await r.text());
      const out = await r.json();
      const made: string[] = out.created ?? [];
      setNotice(
        made.length === 0
          ? 'Nothing to create; those already exist.'
          : `Created ${made.length}: ${made.join(', ')}. Nothing has run yet.`,
      );
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'could not install those');
    } finally {
      setBusy(false);
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

  const selectable = starters.filter(s => !s.installed);

  return (
    <div className="space-y-4 p-4">
      <p className="max-w-3xl text-xs text-muted-foreground">
        Starting points, meant to be edited. Each extractor is gated by a cheap rule label,
        because a query costs nothing and a span engine costs about twenty seconds a message.
        Installing creates them — nothing runs until you say so.
      </p>

      <div className="space-y-2">
        {starters.map(s => (
          <label
            key={s.name}
            className={`flex cursor-pointer items-start gap-3 border px-4 py-3 ${
              s.installed
                ? 'border-border bg-muted/20 opacity-70'
                : 'border-border hover:bg-accent/30'
            }`}
          >
            <input
              type="checkbox"
              className="mt-0.5"
              disabled={s.installed || busy}
              checked={s.installed || chosen.has(s.name)}
              onChange={() => toggle(s.name)}
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
                {s.installed && (
                  <span className="flex items-center gap-1 font-mono text-[10px] text-muted-foreground">
                    <Check className="h-3 w-3" /> already here
                  </span>
                )}
              </div>

              <p className="mt-0.5 text-xs text-muted-foreground">{s.about}</p>

              {s.fields && s.fields.length > 0 && (
                <p className="mt-1 font-mono text-[11px] text-muted-foreground">
                  looks for: {s.fields.join(', ')}
                </p>
              )}

              <p className="mt-1 text-[11px] text-muted-foreground">
                <span className="font-mono tabular-nums text-foreground">
                  {s.reach.toLocaleString()}
                </span>{' '}
                {s.reach === 1 ? 'message' : 'messages'} here
                {/* An extractor gated by a label that has not run reaches
                    nothing yet — true, and useless to show as "0". */}
                {s.gate && <span className="opacity-70"> once `{s.gate}` has run</span>}
                {s.scope && <span className="opacity-70"> · scope {s.scope}</span>}
              </p>
            </div>
          </label>
        ))}
      </div>

      {error && <p className="text-xs text-destructive">{error}</p>}
      {notice && <p className="text-xs text-muted-foreground">{notice}</p>}

      <div className="flex items-center gap-3">
        <button
          type="button"
          disabled={busy || chosen.size === 0}
          onClick={install}
          className="flex items-center gap-1.5 bg-primary px-4 py-1.5 text-xs font-bold text-primary-foreground hover:opacity-90 disabled:opacity-50"
        >
          {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Package className="h-3.5 w-3.5" />}
          Install {chosen.size > 0 ? chosen.size : ''}
        </button>
        {selectable.length === 0 ? (
          <span className="text-xs text-muted-foreground">
            The whole pack is already here — edit them in Tools → Annotators.
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">
            Then run the free labels first; the extractors read them.
          </span>
        )}
      </div>
    </div>
  );
};
