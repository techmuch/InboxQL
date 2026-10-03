import { useCallback, useEffect, useState } from 'react';
import { CheckCircle2, Filter, Loader2, RefreshCw, ScrollText, Trash2, Copy, Check } from 'lucide-react';
import { useErrorLogStore } from '../lib/tabs';

/** Lowest first; the order is what makes a floor mean "this and above". */
const LEVELS = ['debug', 'info', 'warn', 'error'] as const;
type Level = (typeof LEVELS)[number];

interface LoggedError {
  id: string;
  level: Level;
  category: string;
  jobId?: string;
  accountId?: string;
  context?: string;
  reference?: string;
  message: string;
  createdAt: string;
}

/**
 * The log, as its own workbench tab.
 *
 * # Why it stopped being the error log
 *
 * It showed one subsystem's failures. The table had a single producer — the
 * importer — while every sync, annotator run, model call and migration printed
 * to stderr and was invisible unless somebody happened to be watching a
 * terminal. So "Error Log" was accurate and almost useless: it answered "what
 * went wrong during an import" and nothing about what the application had been
 * doing.
 *
 * Now everything lands here, which makes two controls necessary that an error
 * log never needed: a floor, because most lines are ordinary work, and a level
 * on each row, because they are no longer all failures.
 *
 * # This is the quick read
 *
 * The same rows are a query kind — `in:logs level>warn after:7d`,
 * `in:logs | count by category` — and that is where the questions get asked.
 * This is for standing in front of it and scrolling.
 */
export const ErrorLog = () => {
  const jobFilter = useErrorLogStore(s => s.jobFilter);
  const setJobFilter = useErrorLogStore(s => s.setJobFilter);

  const [entries, setEntries] = useState<LoggedError[]>([]);
  const [total, setTotal] = useState(0);
  // What is shown, which is not what is recorded: the floor below is the
  // writer's, and this only hides rows that are already stored.
  const [minLevel, setMinLevel] = useState<Level | ''>('');
  const [floor, setFloor] = useState<Level | null>(null);
  const [dropped, setDropped] = useState(0);
  const [loading, setLoading] = useState(true);
  const [clearing, setClearing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ limit: '200' });
      if (jobFilter) params.set('jobId', jobFilter);
      if (minLevel) params.set('level', minLevel);
      const res = await fetch(`/api/errors?${params}`);
      if (!res.ok) throw new Error(`${res.status}`);
      const body = await res.json();
      setEntries(body.entries ?? []);
      setTotal(body.total ?? 0);
    } catch (e: any) {
      setError(`Could not load the log: ${e.message ?? e}`);
    } finally {
      setLoading(false);
    }
  }, [jobFilter, minLevel]);

  // The writer's floor, which is a different thing from the view's and is the
  // one that decides what exists at all.
  useEffect(() => {
    fetch('/api/log/level')
      .then(r => (r.ok ? r.json() : null))
      .then(b => {
        if (!b) return;
        setFloor(b.level ?? null);
        setDropped(b.dropped ?? 0);
      })
      .catch(() => {
        // The log still reads without knowing its floor.
      });
  }, []);

  const changeFloor = async (level: Level) => {
    setFloor(level);
    try {
      await fetch('/api/log/level', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ level }),
      });
    } catch {
      // Left showing what was asked for; the next load corrects it.
    }
  };

  useEffect(() => { load(); }, [load]);

  const clear = async () => {
    setClearing(true);
    try {
      const params = new URLSearchParams();
      if (jobFilter) params.set('jobId', jobFilter);
      await fetch(`/api/errors?${params}`, { method: 'DELETE' });
      await load();
    } finally {
      setClearing(false);
    }
  };

  const copyLogs = async () => {
    if (entries.length === 0) return;
    const text = entries.map(e => {
      const header = `[${new Date(e.createdAt).toLocaleString()}] [${e.category}]`;
      const ref = e.reference ? ` Ref: ${e.reference}` : '';
      const ctx = e.context ? ` Context: ${e.context}` : '';
      return `${header}${ref}${ctx}\n${e.message}`;
    }).join('\n\n');
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error('Failed to copy text: ', err);
    }
  };

  return (
    <div className="flex flex-col h-full bg-background text-foreground">
      <div className="h-12 border-b border-border flex items-center px-4 gap-3 shrink-0">
        <ScrollText className="w-4 h-4 text-muted-foreground" />
        <h2 className="text-sm font-bold">Log</h2>

        {/* Two controls that look alike and are not. This one hides rows that
            exist; the one beside it decides whether they are written at all. */}
        <label className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
          showing
          <select
            value={minLevel}
            onChange={e => setMinLevel(e.target.value as Level | '')}
            className="bg-background border border-border px-1.5 py-0.5 text-[11px]"
          >
            <option value="">everything</option>
            {LEVELS.map(l => (
              <option key={l} value={l}>{l} and above</option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-1.5 text-[10px] text-muted-foreground"
          title="What gets recorded at all. Debug is loud — a sync writes thousands of lines a minute.">
          recording
          <select
            value={floor ?? 'info'}
            onChange={e => changeFloor(e.target.value as Level)}
            className="bg-background border border-border px-1.5 py-0.5 text-[11px]"
          >
            {LEVELS.map(l => (
              <option key={l} value={l}>{l}</option>
            ))}
          </select>
        </label>

        {jobFilter && (
          <button
            onClick={() => setJobFilter(null)}
            className="flex items-center gap-1.5 text-[10px] border border-border px-2 py-1 hover:bg-accent"
            title="Show errors from every operation"
          >
            <Filter className="w-3 h-3" />
            one import · clear filter
          </button>
        )}

        <div className="flex-1" />
        <span className="text-xs text-muted-foreground tabular-nums">
          {total === 0 ? 'none' : `${total} recorded`}
          {total > entries.length && ` · showing ${entries.length}`}
          {/* Said rather than left silent: a burst that overran the writer
              would otherwise read as a quiet period. */}
          {dropped > 0 && (
            <span className="ml-2 text-amber-600 dark:text-amber-400">{dropped} dropped</span>
          )}
        </span>
        <button onClick={load} disabled={loading}
          className="p-2 hover:bg-accent text-muted-foreground disabled:opacity-40" title="Refresh">
          {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
        </button>
        <button onClick={copyLogs} disabled={entries.length === 0}
          className="p-2 hover:bg-accent text-muted-foreground disabled:opacity-40" title="Copy to clipboard">
          {copied ? <Check className="w-4 h-4 text-green-500" /> : <Copy className="w-4 h-4" />}
        </button>
        <button onClick={clear} disabled={clearing || total === 0}
          className="p-2 hover:bg-destructive/10 text-muted-foreground hover:text-destructive disabled:opacity-40"
          title={jobFilter ? 'Clear this import’s errors' : 'Clear the whole log'}>
          {clearing ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
        </button>
      </div>

      {error && (
        <div className="m-4 border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}

      {!loading && entries.length === 0 && !error && (
        <div className="flex flex-col items-center justify-center flex-1 gap-3 text-muted-foreground">
          <CheckCircle2 className="w-8 h-8 opacity-40" />
          <p className="text-sm">
            {jobFilter
              ? 'That run recorded nothing.'
              : minLevel
                ? `Nothing at ${minLevel} or above.`
                : 'Nothing recorded yet.'}
          </p>
        </div>
      )}

      {entries.length > 0 && (
        <div className="flex-1 overflow-auto">
          <table className="w-full text-xs">
            <thead className="sticky top-0 bg-muted/60 backdrop-blur-sm">
              <tr className="text-left text-[10px] uppercase tracking-wider text-muted-foreground">
                <th className="px-4 py-2 font-bold w-44">When</th>
                <th className="px-4 py-2 font-bold w-16">Level</th>
                <th className="px-4 py-2 font-bold w-24">Category</th>
                <th className="px-4 py-2 font-bold w-48">Item</th>
                <th className="px-4 py-2 font-bold">Problem</th>
              </tr>
            </thead>
            <tbody>
              {entries.map(e => (
                <tr key={e.id} className="border-b border-border/40 hover:bg-accent/30 align-top">
                  <td className="px-4 py-2 text-muted-foreground tabular-nums whitespace-nowrap">
                    {new Date(e.createdAt).toLocaleString()}
                  </td>
                  <td className="px-4 py-2">
                    {/* Coloured by severity rather than by being present.
                        Everything here used to be a failure; most of it is now
                        a record of ordinary work, and painting it all red
                        would make the failures harder to find. */}
                    <span className={`px-2 py-0.5 font-bold uppercase text-[10px] ${levelClass(e.level)}`}>
                      {e.level ?? 'error'}
                    </span>
                  </td>
                  <td className="px-4 py-2 font-mono text-muted-foreground">{e.category}</td>
                  <td className="px-4 py-2 font-mono truncate max-w-xs" title={e.reference}>
                    {e.reference || '—'}
                    {e.context && (
                      <div className="text-muted-foreground truncate" title={e.context}>{e.context}</div>
                    )}
                  </td>
                  <td className="px-4 py-2 whitespace-pre-wrap break-words">{e.message}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
};

/** Severity as colour. Info and debug are not failures and must not look it. */
function levelClass(level: Level): string {
  switch (level) {
    case 'error':
      return 'bg-destructive/10 text-destructive';
    case 'warn':
      return 'bg-amber-500/10 text-amber-600 dark:text-amber-400';
    case 'debug':
      return 'bg-muted text-muted-foreground/70';
    default:
      return 'bg-muted text-muted-foreground';
  }
}
