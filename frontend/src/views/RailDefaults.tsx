import { useCallback, useEffect, useState } from 'react';
import { Check, Loader2, Plus } from 'lucide-react';
import { listRailDefaults, installRailDefaults, type RailDefault } from './Desk/api';

/**
 * Entries to add to the rail.
 *
 * # Why the numbers are the feature
 *
 * Six of these were hardcoded in the rail component. The rest are queries this
 * mailbox turned out to want. Either way, the thing that makes the choice is
 * **what each would match here** — an entry that finds nothing is a row that
 * teaches somebody the feature does not work, and zero is a real answer worth
 * showing rather than hiding.
 *
 * The same shape as the annotator starter pack, for the same reason.
 */
export const RailDefaults = () => {
  const [rows, setRows] = useState<RailDefault[]>([]);
  const [chosen, setChosen] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    listRailDefaults()
      .then(list => {
        setRows(Array.isArray(list) ? list : []);
        // Everything not already there and that matches something: the two
        // reasons not to tick a row by default.
        setChosen(new Set(list.filter(r => !r.present && r.reach > 0).map(r => r.name)));
      })
      .catch(() => setError('could not read the defaults'))
      .finally(() => setLoading(false));
  }, []);

  useEffect(load, [load]);

  const add = async () => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const out = await installRailDefaults([...chosen]);
      setNotice(
        (out.added ?? []).length === 0
          ? 'Nothing added; those are already in the rail.'
          : `Added ${out.added.length} to the rail: ${out.added.join(', ')}.`,
      );
      load();
    } catch (e: any) {
      setError(e?.message ?? 'could not add those');
    } finally {
      setBusy(false);
    }
  };

  if (loading) {
    return (
      <div className="p-8 text-center text-sm text-muted-foreground">
        <Loader2 className="mx-auto mb-2 h-5 w-5 animate-spin text-primary" />
        Reading the defaults…
      </div>
    );
  }

  return (
    <section>
      <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Rail entries</h3>
      <p className="max-w-2xl text-xs text-muted-foreground mb-4">
        Queries to add to the rail. Each says how much of this mailbox it matches, because an
        entry that finds nothing is a row that teaches you the feature does not work. Added
        entries are ordinary saved queries — rename, reorder or remove them from the rail itself.
      </p>

      <div className="space-y-2">
        {rows.map(r => (
          <label
            key={r.name}
            className={`flex items-start gap-3 border px-4 py-2.5 ${
              r.present ? 'border-border bg-muted/20 opacity-70' : 'border-border hover:bg-accent/30 cursor-pointer'
            }`}
          >
            <input
              type="checkbox"
              className="mt-0.5"
              disabled={r.present || busy}
              checked={r.present || chosen.has(r.name)}
              onChange={() =>
                setChosen(prev => {
                  const next = new Set(prev);
                  next.has(r.name) ? next.delete(r.name) : next.add(r.name);
                  return next;
                })
              }
            />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium text-sm">{r.title}</span>
                <span className="font-mono text-[11px] text-muted-foreground">{r.query}</span>
                {r.present && (
                  <span className="flex items-center gap-1 font-mono text-[10px] text-muted-foreground">
                    <Check className="h-3 w-3" /> in the rail
                  </span>
                )}
              </div>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {r.about} ·{' '}
                <span className={r.reach === 0 ? 'text-amber-600 dark:text-amber-400' : 'text-foreground'}>
                  {/* Zero is shown rather than hidden: it is the answer that
                      should stop somebody adding the row. */}
                  {r.reach.toLocaleString()} {r.reach === 1 ? 'match' : 'matches'} here
                </span>
              </p>
            </div>
          </label>
        ))}
      </div>

      {error && <p className="mt-3 text-xs text-destructive">{error}</p>}
      {notice && <p className="mt-3 text-xs text-muted-foreground">{notice}</p>}

      <button
        type="button"
        disabled={busy || chosen.size === 0}
        onClick={add}
        className="mt-4 flex items-center gap-1.5 bg-primary px-4 py-1.5 text-xs font-bold
                   text-primary-foreground hover:opacity-90 disabled:opacity-50"
      >
        {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
        Add {chosen.size > 0 ? chosen.size : ''}
      </button>
    </section>
  );
};
