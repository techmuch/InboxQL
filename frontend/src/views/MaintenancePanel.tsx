import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AlertTriangle, Check, Loader2, Play, RefreshCw, ShieldAlert, X, XCircle,
} from 'lucide-react';

/**
 * The health checks, with a button on the ones a job can fix.
 *
 * # Why this renders doctor rather than knowing the answers itself
 *
 * `iql doctor` already decides what is wrong and names what fixes it, and the
 * server serves those checks verbatim. A panel with its own opinion about
 * whether attachments have been extracted would be a second answer to a
 * question that has one, and the two would part company the first time either
 * changed. So this knows how to draw a check and how to start a job, and
 * nothing at all about mailboxes.
 *
 * Each check carries a `job` when one applies. That is the whole coupling —
 * the panel never parses the remedy text to work out what to run.
 */

interface Check {
  name: string;
  status: 'ok' | 'warn' | 'fail';
  detail: string;
  remedy?: string;
  job?: string;
}

interface Report {
  checks: Check[];
  notConfigured?: boolean;
}

interface Job {
  id: string;
  kind: string;
  status: 'queued' | 'running' | 'done' | 'failed' | 'cancelled';
  current?: string;
  done: number;
  total: number;
  summary?: string;
  lastError?: string;
}

/** Jobs that send mail content to a model, and so need a decision first. */
const SENDS_CONTENT: Record<string, string> = {
  ocr: 'Every page of every scanned file is sent to the configured model to be read.',
  'embed-attachments': 'The text of every readable file is sent to the configured embedding model.',
};

// Jobs that fetch something large rather than sending something private.
//
// A different question from SENDS_CONTENT and worth asking separately: nothing
// here leaves the machine, but most of a gigabyte arrives on it, and that is
// not a thing to discover from a progress bar after clicking Fix.
const DOWNLOADS: Record<string, string> = {
  'gliner-install': 'Downloads about 800 MB of model weights, once. Afterwards the model runs entirely on this machine and no mail is sent anywhere.',
};

const needsConfirming = (job: string) => Boolean(SENDS_CONTENT[job] || DOWNLOADS[job]);

const running = (j?: Job | null) => j?.status === 'queued' || j?.status === 'running';

export const MaintenancePanel = () => {
  const [report, setReport] = useState<Report | null>(null);
  const [loading, setLoading] = useState(true);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [active, setActive] = useState<Job | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState<string | null>(null);
  const events = useRef<EventSource | null>(null);

  const loadHealth = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/health');
      if (!res.ok) throw new Error(`${res.status}`);
      setReport(await res.json());
      setError(null);
    } catch (e: any) {
      setError(`Could not read the health checks: ${e.message ?? e}`);
    } finally {
      setLoading(false);
    }
  }, []);

  const loadJobs = useCallback(async () => {
    try {
      const res = await fetch('/api/maintenance/jobs');
      if (res.ok) setJobs(await res.json());
    } catch {
      // The history is a convenience; losing it should not break the panel.
    }
  }, []);

  useEffect(() => { loadHealth(); loadJobs(); }, [loadHealth, loadJobs]);
  useEffect(() => () => events.current?.close(), []);

  /** Follow a running job, and re-check health when it ends. */
  const follow = (id: string) => {
    events.current?.close();
    const es = new EventSource(`/api/maintenance/jobs/${id}/events`);
    events.current = es;

    es.onmessage = event => {
      try {
        const next: Job = JSON.parse(event.data);
        if (!next?.id) return;
        setActive(next);
        if (!running(next)) {
          es.close();
          // The whole point of the panel: the warning that prompted this
          // should now be gone, and seeing it go is the confirmation.
          loadHealth();
          loadJobs();
        }
      } catch {
        // A malformed frame is not worth tearing the stream down for.
      }
    };
    es.onerror = () => { es.close(); loadHealth(); loadJobs(); };
  };

  const start = async (kind: string, allowRemote = false) => {
    setError(null);
    setConfirming(null);
    try {
      const res = await fetch('/api/maintenance/jobs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ kind, allowRemote }),
      });
      const body = await res.json().catch(() => ({}));
      if (res.status === 409 && body.jobId) {
        // Already running. Watching it is what the caller wanted anyway.
        follow(body.jobId);
        return;
      }
      if (!res.ok) throw new Error(body.error ?? body.message ?? `${res.status}`);
      setActive(body);
      follow(body.id);
    } catch (e: any) {
      setError(e.message ?? String(e));
    }
  };

  const cancel = async () => {
    if (!active) return;
    await fetch(`/api/maintenance/jobs/${active.id}/cancel`, { method: 'POST' }).catch(() => {});
  };

  const fixable = (c: Check) => Boolean(c.job) && c.status !== 'ok';
  const offered = (c: Check) => Boolean(c.job) && c.status === 'ok';

  return (
    <div className="animate-in fade-in duration-300">
      <div className="flex items-start justify-between mb-2">
        <h2 className="text-2xl font-bold text-foreground">Maintenance</h2>
        <button
          type="button"
          onClick={() => { loadHealth(); loadJobs(); }}
          disabled={loading}
          className="flex items-center gap-2 border border-border px-3 py-1.5 text-xs hover:bg-accent transition-colors disabled:opacity-50"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          Re-check
        </button>
      </div>
      <p className="text-muted-foreground text-sm mb-8">
        The same checks <code className="font-mono text-xs">iql doctor</code> runs. Anything that
        can be fixed by a job has a button; the work happens in the background and this page
        re-checks itself when it finishes.
      </p>

      {error && (
        <div className="mb-6 flex items-start gap-2 border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
          <XCircle className="w-4 h-4 mt-0.5 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {active && (
        <div className="mb-6 border border-border bg-card p-4">
          <div className="flex items-center gap-3">
            {running(active)
              ? <Loader2 className="w-4 h-4 animate-spin text-primary shrink-0" />
              : active.status === 'done'
                ? <Check className="w-4 h-4 text-primary shrink-0" />
                : <XCircle className="w-4 h-4 text-destructive shrink-0" />}
            <span className="text-sm font-medium">{label(active.kind)}</span>
            <span className="text-xs text-muted-foreground">{active.status}</span>
            {running(active) && (
              <button
                type="button"
                onClick={cancel}
                className="ml-auto flex items-center gap-1 text-xs text-muted-foreground hover:text-destructive"
              >
                <X className="w-3 h-3" /> Stop
              </button>
            )}
          </div>

          {running(active) && active.total > 0 && (
            <div className="mt-3">
              <div className="h-1 bg-accent overflow-hidden">
                <div
                  className="h-full bg-primary transition-all"
                  style={{ width: `${Math.round((active.done / active.total) * 100)}%` }}
                />
              </div>
              <p className="mt-1.5 text-xs text-muted-foreground font-mono tabular-nums">
                {active.done} / {active.total}
                {active.current && <span className="ml-2 font-sans">{active.current}</span>}
              </p>
            </div>
          )}

          {active.summary && <p className="mt-2 text-xs text-muted-foreground">{active.summary}</p>}
          {active.lastError && <p className="mt-2 text-xs text-destructive">{active.lastError}</p>}
        </div>
      )}

      {confirming && (
        <div className="mb-6 border border-amber-500/40 bg-amber-500/5 p-4">
          <div className="flex items-start gap-2">
            <ShieldAlert className="w-4 h-4 mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">
                {SENDS_CONTENT[confirming]
                  ? `${label(confirming)} sends your mail to a model`
                  : `${label(confirming)} downloads a large file`}
              </p>
              {/* Said before it happens, not discovered afterwards. Where the
                  content goes — and what arrives — is the user's decision. */}
              <p className="mt-1 text-xs text-muted-foreground">
                {SENDS_CONTENT[confirming] ?? DOWNLOADS[confirming]}
              </p>
              {SENDS_CONTENT[confirming] && (
                <p className="mt-1 text-xs text-muted-foreground">
                  If the configured model runs on this machine nothing leaves it. If it is a hosted
                  API, this uploads that content to it.
                </p>
              )}
              <div className="mt-3 flex gap-2">
                <button
                  type="button"
                  onClick={() => start(confirming, Boolean(SENDS_CONTENT[confirming]))}
                  className="border border-border bg-accent px-3 py-1.5 text-xs font-medium hover:bg-accent/70"
                >
                  Go ahead
                </button>
                <button
                  type="button"
                  onClick={() => setConfirming(null)}
                  className="px-3 py-1.5 text-xs text-muted-foreground hover:text-foreground"
                >
                  Cancel
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {loading && !report ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="w-4 h-4 animate-spin" /> Checking…
        </div>
      ) : (
        <ul className="divide-y divide-border border border-border">
          {(report?.checks ?? []).map(c => (
            <li key={c.name} className="flex items-start gap-3 px-4 py-3">
              <StatusDot status={c.status} />
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">{c.name}</p>
                <p className="text-xs text-muted-foreground">{c.detail}</p>
                {/* The remedy is shown only when there is no button for it —
                    a command to copy is the fallback, not the headline. */}
                {c.remedy && c.status !== 'ok' && !c.job && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    <span className="opacity-70">try: </span>
                    <code className="font-mono">{c.remedy}</code>
                  </p>
                )}
              </div>
              {(fixable(c) || offered(c)) && (
                <button
                  type="button"
                  disabled={running(active)}
                  onClick={() => (needsConfirming(c.job!) ? setConfirming(c.job!) : start(c.job!))}
                  className={`shrink-0 flex items-center gap-1.5 border px-3 py-1.5 text-xs transition-colors disabled:opacity-40 ${
                    fixable(c)
                      ? 'border-primary/50 bg-primary/5 hover:bg-primary/10 font-medium'
                      : 'border-border hover:bg-accent text-muted-foreground'
                  }`}
                >
                  <Play className="w-3 h-3" />
                  {fixable(c) ? 'Fix' : 'Run'}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      {jobs.length > 0 && (
        <section className="mt-8">
          <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">
            Recent runs
          </h3>
          <ul className="divide-y divide-border border border-border">
            {jobs.slice(0, 8).map(j => (
              <li key={j.id} className="flex items-start gap-3 px-4 py-2 text-xs">
                <span className="w-28 shrink-0 font-medium">{label(j.kind)}</span>
                <span className={`w-20 shrink-0 ${j.status === 'failed' ? 'text-destructive' : 'text-muted-foreground'}`}>
                  {j.status}
                </span>
                <span className="min-w-0 flex-1 text-muted-foreground">
                  {j.summary || j.lastError || ''}
                </span>
              </li>
            ))}
          </ul>
          {/* History lives in memory, so saying so beats someone concluding
              their runs were lost. */}
          <p className="mt-2 text-[11px] text-muted-foreground">
            Runs are listed for this server session only. What they produced is in the database.
          </p>
        </section>
      )}
    </div>
  );
};

const StatusDot = ({ status }: { status: Check['status'] }) => {
  if (status === 'ok') return <Check className="w-4 h-4 mt-0.5 shrink-0 text-primary" />;
  if (status === 'warn') {
    return <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" />;
  }
  return <XCircle className="w-4 h-4 mt-0.5 shrink-0 text-destructive" />;
};

/** What to call a job kind in the interface. */
const label = (kind: string): string => ({
  attachments: 'Extract attachments',
  text: 'Read file contents',
  ocr: 'Read scans (OCR)',
  'embed-attachments': 'Embed file text',
  reindex: 'Rebuild indexes',
}[kind] ?? kind);
