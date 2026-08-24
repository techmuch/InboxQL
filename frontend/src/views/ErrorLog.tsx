import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, Filter, Loader2, RefreshCw, Trash2 } from 'lucide-react';
import { useErrorLogStore } from '../lib/tabs';

interface LoggedError {
  id: string;
  category: string;
  jobId?: string;
  accountId?: string;
  context?: string;
  reference?: string;
  message: string;
  createdAt: string;
}

/**
 * The error log, as its own workbench tab.
 *
 * Imports count their failures, but a count is not actionable — "3 failed"
 * says nothing about which three or why. Every failure is recorded with the
 * item that caused it, and this is where those records are read.
 */
export const ErrorLog = () => {
  const jobFilter = useErrorLogStore(s => s.jobFilter);
  const setJobFilter = useErrorLogStore(s => s.setJobFilter);

  const [entries, setEntries] = useState<LoggedError[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [clearing, setClearing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ limit: '200' });
      if (jobFilter) params.set('jobId', jobFilter);
      const res = await fetch(`/api/errors?${params}`);
      if (!res.ok) throw new Error(`${res.status}`);
      const body = await res.json();
      setEntries(body.entries ?? []);
      setTotal(body.total ?? 0);
    } catch (e: any) {
      setError(`Could not load the error log: ${e.message ?? e}`);
    } finally {
      setLoading(false);
    }
  }, [jobFilter]);

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

  return (
    <div className="flex flex-col h-full bg-background text-foreground">
      <div className="h-12 border-b border-border flex items-center px-4 gap-3 shrink-0">
        <AlertTriangle className="w-4 h-4 text-muted-foreground" />
        <h2 className="text-sm font-bold">Error Log</h2>

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
        </span>
        <button onClick={load} disabled={loading}
          className="p-2 hover:bg-accent text-muted-foreground disabled:opacity-40" title="Refresh">
          {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
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
            {jobFilter ? 'That import recorded no errors.' : 'Nothing has failed.'}
          </p>
        </div>
      )}

      {entries.length > 0 && (
        <div className="flex-1 overflow-auto">
          <table className="w-full text-xs">
            <thead className="sticky top-0 bg-muted/60 backdrop-blur-sm">
              <tr className="text-left text-[10px] uppercase tracking-wider text-muted-foreground">
                <th className="px-4 py-2 font-bold w-44">When</th>
                <th className="px-4 py-2 font-bold w-20">Category</th>
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
                    <span className="bg-destructive/10 text-destructive px-2 py-0.5 font-bold uppercase text-[10px]">
                      {e.category}
                    </span>
                  </td>
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
