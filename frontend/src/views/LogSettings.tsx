import { useCallback, useEffect, useState } from 'react';
import { Loader2, ScrollText } from 'lucide-react';

/**
 * What the log records — as opposed to how much.
 *
 * # Why the level is not here
 *
 * It lives in the Log tab, where somebody is standing when they want it. A
 * second copy here would be two controls for one piece of state, which is the
 * thing this interface has deliberately avoided everywhere else.
 *
 * These are the set-once decisions: how slow is slow, which of the noisy
 * categories earn their volume, how much to keep, and whether the words of a
 * query are written down. You reach for them once and forget them; you reach
 * for the level in the middle of looking at something.
 *
 * # Why not in General
 *
 * That panel says in its own text that what it holds is stored per-browser.
 * These are server-side — they change what the server records for every window.
 */

interface Settings {
  slowMs: number;
  categories: Record<string, boolean>;
  retain: number;
  queryText: boolean;
}

const ABOUT: Record<string, string> = {
  http: 'every request, which is the noisiest by far',
  query: 'every query and how long it took',
  sync: 'syncs and imports, per account',
  annotate: 'annotator runs and model loads',
};

export const LogSettings = () => {
  const [s, setS] = useState<Settings | null>(null);
  const [level, setLevel] = useState<string>('');
  const [categories, setCategories] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    fetch('/api/log/settings')
      .then(r => (r.ok ? r.json() : Promise.reject(new Error(`${r.status}`))))
      .then(b => {
        setS(b.settings ?? null);
        setCategories(b.categories ?? []);
        setLevel(b.level ?? '');
      })
      .catch(() => setError('could not read the logging settings'));
  }, []);

  useEffect(load, [load]);

  const save = async (next: Settings) => {
    setS(next);
    setSaving(true);
    setError(null);
    try {
      const r = await fetch('/api/log/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(next),
      });
      if (!r.ok) throw new Error(await r.text());
    } catch (e: any) {
      setError(e?.message ?? 'could not save that');
      load();
    } finally {
      setSaving(false);
    }
  };

  if (!s) {
    return (
      <section>
        <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">Logging</h3>
        <p className="text-xs text-muted-foreground">
          {error ?? <Loader2 className="inline h-3.5 w-3.5 animate-spin" />}
        </p>
      </section>
    );
  }

  return (
    <section>
      <h3 className="flex items-center gap-2 text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">
        Logging {saving && <Loader2 className="h-3 w-3 animate-spin text-primary" />}
      </h3>
      <p className="max-w-2xl text-xs text-muted-foreground mb-5">
        What the log records. <strong>How much</strong> it records is the level, and that lives in
        the Log tab{level && <> — currently <span className="font-mono">{level}</span></>}, because
        it is what you reach for in the middle of looking at something.
      </p>

      <div className="space-y-5 max-w-2xl">
        <label className="flex items-start gap-3">
          <input
            type="number" min={0} step={100} value={s.slowMs}
            onChange={e => save({ ...s, slowMs: Math.max(0, Number(e.target.value) || 0) })}
            className="w-24 bg-background border border-border px-2 py-1 text-sm font-mono tabular-nums"
          />
          <span className="text-xs text-muted-foreground leading-snug pt-1.5">
            <span className="text-foreground font-medium">Slow queries, in milliseconds.</span>{' '}
            Anything slower is recorded as a warning — with the SQL, so it can be pasted into{' '}
            <span className="font-mono">iql sql --explain</span> — even when the level would
            otherwise pass it over.
          </span>
        </label>

        <div>
          <p className="text-xs font-medium mb-2">Also record</p>
          <div className="space-y-1.5">
            {categories.map(c => (
              <label key={c} className="flex items-start gap-2.5 cursor-pointer">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={!!s.categories?.[c]}
                  onChange={e => save({ ...s, categories: { ...s.categories, [c]: e.target.checked } })}
                />
                <span className="text-xs">
                  <span className="font-mono">{c}</span>
                  <span className="text-muted-foreground"> — {ABOUT[c] ?? ''}</span>
                </span>
              </label>
            ))}
          </div>
          <p className="mt-2 text-[11px] text-muted-foreground">
            {/* The reason this exists: debug is otherwise all-or-nothing. */}
            These sit on top of the level rather than inside it, so debug can be used to chase one
            thing without ten thousand request lines burying it.
          </p>
        </div>

        <label className="flex items-start gap-3">
          <input
            type="number" min={1000} step={1000} value={s.retain}
            onChange={e => save({ ...s, retain: Math.max(1, Number(e.target.value) || 1) })}
            className="w-28 bg-background border border-border px-2 py-1 text-sm font-mono tabular-nums"
          />
          <span className="text-xs text-muted-foreground leading-snug pt-1.5">
            <span className="text-foreground font-medium">Lines to keep.</span>{' '}
            Around four days of ordinary use, or a few hours at debug. Older lines go when{' '}
            <span className="font-mono">iql maintenance prune-log</span> runs.
          </span>
        </label>

        <label className="flex items-start gap-2.5 cursor-pointer">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={s.queryText}
            onChange={e => save({ ...s, queryText: e.target.checked })}
          />
          <span className="text-xs">
            <span className="text-foreground font-medium">Record the text of queries.</span>
            <span className="text-muted-foreground">
              {' '}A query can name a person, or a subject you searched for, and the log outlives
              the query. Off keeps the timings and the counts without the words.
            </span>
          </span>
        </label>
      </div>

      {error && <p className="mt-4 text-xs text-destructive">{error}</p>}

      <p className="mt-5 flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <ScrollText className="h-3 w-3" />
        Ask it things with <span className="font-mono">in:logs duration&gt;1000 | count by category</span>.
      </p>
    </section>
  );
};
