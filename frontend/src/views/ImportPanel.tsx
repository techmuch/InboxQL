import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  AlertTriangle, ChevronDown, ChevronRight, Download, Folder, HardDrive,
  Loader2, RefreshCw, Search, ShieldAlert, XCircle, CheckCircle2,
} from 'lucide-react';

/**
 * Settings → Import.
 *
 * Two rules shape this panel. Scanning and importing are separate, so looking
 * at a mailbox can never accidentally import forty thousand messages. And the
 * browser never names a filesystem path: sources and mailboxes are opaque ids
 * the server minted, so nothing here can be talked into reading arbitrary files.
 */

interface Detection {
  available: boolean;
  readable: boolean;
  root?: string;
  detail: string;
  remedy?: string;
}

interface Source extends Detection {
  id: string;
  name: string;
}

interface Mailbox {
  id: string;
  name: string;
  path: string;
  parentId?: string;
  account?: string;
  messages: number;
  bytes: number;
}

interface Stats {
  mailboxId: string;
  depth: 'fast' | 'deep';
  messages: number;
  bytes: number;
  unread: number;
  attachments: number;
  attachmentBytes: number;
  contacts: number;
  partial: number;
  unreadable: number;
  oldest?: string;
  newest?: string;
}

interface Job {
  id: string;
  status: 'pending' | 'running' | 'done' | 'failed' | 'cancelled' | 'interrupted';
  dryRun: boolean;
  total?: number;
  scanned: number;
  imported: number;
  duplicates: number;
  skipped: number;
  failed: number;
  attachments: number;
  bytes: number;
  current?: string;
  lastError?: string;
  createdAt: string;
}

const humanBytes = (n: number): string => {
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = n / 1024;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) { value /= 1024; i++; }
  return `${value.toFixed(1)} ${units[i]}`;
};

type Scope = 'limit' | 'range' | 'all';

export const ImportPanel = ({ accounts }: { accounts: any[] }) => {
  const [sources, setSources] = useState<Source[]>([]);
  const [sourceId, setSourceId] = useState('apple-mail');
  const [mailboxes, setMailboxes] = useState<Mailbox[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [stats, setStats] = useState<Record<string, Stats>>({});
  const [permissionError, setPermissionError] = useState<Detection | null>(null);

  const [loadingBoxes, setLoadingBoxes] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [job, setJob] = useState<Job | null>(null);
  const [history, setHistory] = useState<Job[]>([]);

  const [accountId, setAccountId] = useState('');
  const [scope, setScope] = useState<Scope>('limit');
  const [limit, setLimit] = useState(100);
  const [since, setSince] = useState('');
  const [until, setUntil] = useState('');
  const [attachments, setAttachments] = useState(false);
  const [maxAttachMb, setMaxAttachMb] = useState(25);
  const [dryRun, setDryRun] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const eventsRef = useRef<EventSource | null>(null);

  const source = sources.find(s => s.id === sourceId);

  const loadSources = useCallback(async () => {
    try {
      const res = await fetch('/api/import/sources');
      if (res.ok) setSources(await res.json());
    } catch { /* offline; the panel still renders its empty state */ }
  }, []);

  const loadHistory = useCallback(async () => {
    try {
      const res = await fetch('/api/import/jobs?limit=10');
      if (res.ok) setHistory(await res.json());
    } catch { /* history is a nicety, not a requirement */ }
  }, []);

  const loadMailboxes = useCallback(async () => {
    setLoadingBoxes(true);
    setError(null);
    setPermissionError(null);
    try {
      const res = await fetch(`/api/import/sources/${sourceId}/mailboxes`);
      if (res.status === 409) {
        // Present but unreadable — the Full Disk Access case, which deserves
        // instructions rather than an error toast.
        const body = await res.json();
        setPermissionError({ available: true, readable: false, detail: body.error, remedy: body.remedy });
        setMailboxes([]);
        return;
      }
      if (!res.ok) throw new Error(`${res.status}`);
      const body = await res.json();
      setMailboxes(body.mailboxes ?? []);
      setStats({});
      setSelected(new Set());
    } catch (e: any) {
      setError(`Could not list mailboxes: ${e.message ?? e}`);
    } finally {
      setLoadingBoxes(false);
    }
  }, [sourceId]);

  useEffect(() => { loadSources(); loadHistory(); }, [loadSources, loadHistory]);
  useEffect(() => {
    if (accounts.length && !accountId) setAccountId(accounts[0].id);
  }, [accounts, accountId]);
  useEffect(() => () => eventsRef.current?.close(), []);

  const selectedBoxes = useMemo(
    () => mailboxes.filter(b => selected.has(b.id)),
    [mailboxes, selected],
  );

  const totals = useMemo(() => {
    let messages = 0, bytes = 0, attach = 0, contacts = 0, deep = 0;
    for (const b of selectedBoxes) {
      const s = stats[b.id];
      messages += s?.messages ?? b.messages;
      bytes += s?.bytes ?? b.bytes;
      if (s?.depth === 'deep') { attach += s.attachments; contacts += s.contacts; deep++; }
    }
    return { messages, bytes, attach, contacts, deep, count: selectedBoxes.length };
  }, [selectedBoxes, stats]);

  const toggle = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };

  const deepScan = async () => {
    if (!selectedBoxes.length) return;
    setScanning(true);
    setError(null);
    try {
      const res = await fetch(`/api/import/sources/${sourceId}/scan`, {
        method: 'POST',
        body: JSON.stringify({ mailboxIds: selectedBoxes.map(b => b.id), deep: true }),
      });
      if (!res.ok) throw new Error(await res.text());
      const results: Stats[] = await res.json();
      setStats(prev => {
        const next = { ...prev };
        for (const s of results) next[s.mailboxId] = s;
        return next;
      });
    } catch (e: any) {
      setError(`Scan failed: ${e.message ?? e}`);
    } finally {
      setScanning(false);
    }
  };

  const startImport = async () => {
    if (!selectedBoxes.length || !accountId) return;
    setError(null);
    try {
      const res = await fetch('/api/import/jobs', {
        method: 'POST',
        body: JSON.stringify({
          sourceId,
          mailboxIds: selectedBoxes.map(b => b.id),
          accountId,
          limit: scope === 'limit' ? limit : 0,
          since: scope === 'range' ? since : '',
          until: scope === 'range' ? until : '',
          dryRun,
          attachments,
          maxAttachmentMb: maxAttachMb,
        }),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: `${res.status}` }));
        throw new Error(body.error);
      }
      const created: Job = await res.json();
      setJob(created);
      subscribe(created.id);
    } catch (e: any) {
      setError(`Could not start the import: ${e.message ?? e}`);
    }
  };

  /** Follow a running job over SSE, falling back to nothing if it is over. */
  const subscribe = (id: string) => {
    eventsRef.current?.close();
    const es = new EventSource(`/api/import/jobs/${id}/events`);
    eventsRef.current = es;

    es.onmessage = (event) => {
      try {
        const next: Job = JSON.parse(event.data);
        if (next && next.id) setJob(next);
      } catch { /* a malformed frame is not worth tearing the stream down for */ }
    };
    es.addEventListener('done', () => { es.close(); loadHistory(); });
    es.onerror = () => { es.close(); loadHistory(); };
  };

  const cancel = async () => {
    if (!job) return;
    await fetch(`/api/import/jobs/${job.id}/cancel`, { method: 'POST' }).catch(() => {});
  };

  const roots = mailboxes.filter(b => !b.parentId);
  const childrenOf = (id: string) => mailboxes.filter(b => b.parentId === id);

  const renderMailbox = (box: Mailbox, depth: number): React.ReactNode => {
    const kids = childrenOf(box.id);
    const isCollapsed = collapsed.has(box.id);
    const s = stats[box.id];

    return (
      <div key={box.id}>
        <div
          className="flex items-center gap-2 px-3 py-1.5 hover:bg-accent/40 border-b border-border/30"
          style={{ paddingLeft: `${12 + depth * 18}px` }}
        >
          {kids.length > 0 ? (
            <button
              onClick={() => setCollapsed(p => {
                const n = new Set(p); n.has(box.id) ? n.delete(box.id) : n.add(box.id); return n;
              })}
              className="text-muted-foreground hover:text-foreground"
              aria-label={isCollapsed ? `Expand ${box.path}` : `Collapse ${box.path}`}
            >
              {isCollapsed ? <ChevronRight className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
            </button>
          ) : <span className="w-3" />}

          <input
            type="checkbox"
            checked={selected.has(box.id)}
            onChange={() => toggle(box.id)}
            aria-label={`Select ${box.path}`}
            className="accent-primary"
          />
          <Folder className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
          <span className="text-sm flex-1 truncate">{box.name}</span>

          <span className="text-xs text-muted-foreground tabular-nums w-20 text-right">
            {box.messages.toLocaleString()}
          </span>
          <span className="text-xs text-muted-foreground tabular-nums w-20 text-right">
            {humanBytes(box.bytes)}
          </span>
          <span className="text-xs text-muted-foreground tabular-nums w-28 text-right">
            {s?.depth === 'deep'
              ? `${s.attachments} attach · ${s.contacts} contacts`
              : ''}
          </span>
        </div>
        {!isCollapsed && kids.map(k => renderMailbox(k, depth + 1))}
      </div>
    );
  };

  const running = job && (job.status === 'running' || job.status === 'pending');

  return (
    <div className="animate-in fade-in duration-300">
      <h2 className="text-2xl font-bold text-foreground mb-2">Import Mail</h2>
      <p className="text-muted-foreground text-sm mb-8">
        Bring messages in from a desktop mail client. Nothing in the client is
        modified, moved or deleted.
      </p>

      {/* Source */}
      <section className="mb-8">
        <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">Source</h3>
        <div className="flex flex-wrap gap-3 mb-4">
          {sources.map(s => (
            <button
              key={s.id}
              onClick={() => setSourceId(s.id)}
              className={`px-4 py-2 border text-sm flex items-center gap-2 transition-all ${
                sourceId === s.id ? 'border-primary bg-primary/5 ring-1 ring-primary' : 'border-border hover:bg-accent'
              }`}
            >
              <HardDrive className="w-4 h-4" />
              {s.name}
              {s.readable
                ? <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500" />
                : <AlertTriangle className="w-3.5 h-3.5 text-amber-500" />}
            </button>
          ))}
          {sources.length === 0 && (
            <span className="text-sm text-muted-foreground italic">Looking for mail clients…</span>
          )}
        </div>

        {source && !source.readable && (
          <PermissionNotice detection={permissionError ?? source} onRetry={() => { loadSources(); loadMailboxes(); }} />
        )}
        {permissionError && source?.readable && (
          <PermissionNotice detection={permissionError} onRetry={() => { loadSources(); loadMailboxes(); }} />
        )}

        {source?.readable && !permissionError && (
          <div className="flex items-center gap-3">
            <button
              onClick={loadMailboxes}
              disabled={loadingBoxes}
              className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 text-sm font-semibold shadow hover:opacity-90 disabled:opacity-50"
            >
              {loadingBoxes ? <Loader2 className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />}
              Scan mailboxes
            </button>
            <span className="text-xs text-muted-foreground font-mono">{source.root}</span>
          </div>
        )}
      </section>

      {error && (
        <div className="mb-6 flex items-start gap-3 border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          <XCircle className="w-4 h-4 mt-0.5 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Mailboxes */}
      {mailboxes.length > 0 && (
        <section className="mb-8">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">Mailboxes</h3>
            <button
              onClick={deepScan}
              disabled={scanning || !selectedBoxes.length}
              className="flex items-center gap-2 text-xs border border-border px-3 py-1.5 hover:bg-accent disabled:opacity-40"
              title="Parses every message. Slow on a large mailbox."
            >
              {scanning ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
              Deep scan selected
            </button>
          </div>

          <div className="border border-border">
            <div className="flex items-center gap-2 px-3 py-1.5 bg-muted/40 border-b border-border text-[10px] font-bold uppercase tracking-wider text-muted-foreground">
              <span className="w-3" /><span className="w-3.5" /><span className="w-3.5" />
              <span className="flex-1">Mailbox</span>
              <span className="w-20 text-right">Messages</span>
              <span className="w-20 text-right">Size</span>
              <span className="w-28 text-right">Deep scan</span>
            </div>
            <div className="max-h-72 overflow-y-auto">
              {roots.map(b => renderMailbox(b, 0))}
            </div>
          </div>

          {totals.count > 0 && (
            <p className="text-xs text-muted-foreground mt-2">
              {totals.count} mailbox{totals.count === 1 ? '' : 'es'} selected ·{' '}
              {totals.messages.toLocaleString()} messages · {humanBytes(totals.bytes)}
              {totals.deep > 0 && ` · ${totals.attach} attachments · ${totals.contacts} contacts`}
              {totals.deep === 0 && ' · deep scan for attachments and contacts'}
            </p>
          )}
        </section>
      )}

      {/* Options */}
      {mailboxes.length > 0 && (
        <section className="mb-8 space-y-5">
          <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">Options</h3>

          <div className="space-y-1.5">
            <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Import into</label>
            <select
              value={accountId}
              onChange={e => setAccountId(e.target.value)}
              className="w-full max-w-sm bg-background border border-border px-3 py-2 text-sm"
            >
              {accounts.map(a => <option key={a.id} value={a.id}>{a.name || a.id}</option>)}
              {accounts.length === 0 && <option value="">No accounts — add one first</option>}
            </select>
            <p className="text-[10px] text-muted-foreground">
              Imported mail is deleted if this account is removed. For an archive you
              want to keep, use a dedicated account rather than a live mailbox.
            </p>
          </div>

          <div className="space-y-2">
            <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Scope</label>
            <div className="flex flex-wrap items-center gap-4 text-sm">
              <label className="flex items-center gap-2">
                <input type="radio" checked={scope === 'limit'} onChange={() => setScope('limit')} className="accent-primary" />
                Newest
                <input
                  type="number" min={1} value={limit}
                  onChange={e => setLimit(Math.max(1, Number(e.target.value)))}
                  onFocus={() => setScope('limit')}
                  className="w-20 bg-background border border-border px-2 py-1 text-sm"
                />
                messages
              </label>
              <label className="flex items-center gap-2">
                <input type="radio" checked={scope === 'range'} onChange={() => setScope('range')} className="accent-primary" />
                Between
                <input type="date" value={since} onChange={e => setSince(e.target.value)} onFocus={() => setScope('range')}
                  className="bg-background border border-border px-2 py-1 text-sm" />
                and
                <input type="date" value={until} onChange={e => setUntil(e.target.value)} onFocus={() => setScope('range')}
                  className="bg-background border border-border px-2 py-1 text-sm" />
              </label>
              <label className="flex items-center gap-2">
                <input type="radio" checked={scope === 'all'} onChange={() => setScope('all')} className="accent-primary" />
                Everything
              </label>
            </div>
            <p className="text-[10px] text-muted-foreground">
              A limit spans the whole import, not each mailbox.
            </p>
          </div>

          <div className="space-y-2 text-sm">
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={attachments} onChange={e => setAttachments(e.target.checked)} className="accent-primary" />
              Include attachments, skipping anything over
              <input
                type="number" min={1} value={maxAttachMb}
                onChange={e => setMaxAttachMb(Math.max(1, Number(e.target.value)))}
                className="w-16 bg-background border border-border px-2 py-1 text-sm"
              />
              MB
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={dryRun} onChange={e => setDryRun(e.target.checked)} className="accent-primary" />
              Dry run — report what would happen, write nothing
            </label>
            {attachments && (
              <p className="text-[10px] text-muted-foreground">
                Attachment files are stored outside the database. A plain backup will
                not contain them — use <code>uea backup --include-attachments</code>.
              </p>
            )}
          </div>

          <button
            onClick={startImport}
            disabled={!selectedBoxes.length || !accountId || !!running}
            className="flex items-center gap-2 bg-primary text-primary-foreground px-5 py-2.5 text-sm font-semibold shadow hover:opacity-90 disabled:opacity-40"
          >
            <Download className="w-4 h-4" />
            {dryRun ? 'Preview import' : 'Start import'}
          </button>
        </section>
      )}

      {job && <JobProgress job={job} onCancel={cancel} />}

      {history.length > 0 && (
        <section className="mt-8">
          <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">Recent imports</h3>
          <div className="border border-border divide-y divide-border/50">
            {history.map(h => (
              <div key={h.id} className="flex items-center gap-4 px-3 py-2 text-xs">
                <StatusBadge status={h.status} />
                <span className="text-muted-foreground tabular-nums">
                  {new Date(h.createdAt).toLocaleString()}
                </span>
                <span className="flex-1">
                  {h.imported.toLocaleString()} imported · {h.duplicates} duplicate
                  {h.failed > 0 && ` · ${h.failed} failed`}
                  {h.dryRun && ' · dry run'}
                </span>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
};

const PermissionNotice = ({ detection, onRetry }: { detection: Detection; onRetry: () => void }) => (
  <div className="flex items-start gap-3 border border-amber-500/40 bg-amber-500/10 px-4 py-3 mb-4">
    <ShieldAlert className="w-4 h-4 mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" />
    <div className="text-xs leading-relaxed flex-1">
      <p className="font-bold text-amber-700 dark:text-amber-400 mb-1">{detection.detail}</p>
      {detection.remedy && (
        // Rendered verbatim: it names the exact binary that needs the grant,
        // and the usual mistake is granting access to the wrong program.
        <pre className="whitespace-pre-wrap font-mono text-[11px] text-muted-foreground">{detection.remedy}</pre>
      )}
      <button onClick={onRetry} className="mt-2 border border-border px-3 py-1 hover:bg-accent">
        Re-check
      </button>
    </div>
  </div>
);

const JobProgress = ({ job, onCancel }: { job: Job; onCancel: () => void }) => {
  const running = job.status === 'running' || job.status === 'pending';
  const pct = job.total && job.total > 0
    ? Math.min(100, Math.round((job.scanned / job.total) * 100))
    : null;

  return (
    <section className="border border-border p-4 space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {running ? <Loader2 className="w-4 h-4 animate-spin text-primary" /> : <StatusBadge status={job.status} />}
          <span className="text-sm font-semibold">
            {job.dryRun ? 'Dry run' : 'Import'} {running ? 'in progress' : job.status}
          </span>
        </div>
        {running && (
          <button onClick={onCancel} className="text-xs border border-border px-3 py-1 hover:bg-accent">
            Cancel
          </button>
        )}
      </div>

      {pct !== null && (
        <div className="h-1.5 bg-muted overflow-hidden">
          <div className="h-full bg-primary transition-all duration-300" style={{ width: `${pct}%` }} />
        </div>
      )}

      <div className="grid grid-cols-2 sm:grid-cols-5 gap-3 text-xs">
        <Metric label="Scanned" value={job.scanned} />
        <Metric label="Imported" value={job.imported} />
        <Metric label="Duplicates" value={job.duplicates} />
        <Metric label="Skipped" value={job.skipped} />
        <Metric label="Failed" value={job.failed} tone={job.failed > 0 ? 'bad' : undefined} />
      </div>

      {job.attachments > 0 && (
        <p className="text-xs text-muted-foreground">{job.attachments} attachment(s) stored.</p>
      )}
      {job.current && running && (
        <p className="text-xs text-muted-foreground font-mono truncate">{job.current}</p>
      )}
      {job.lastError && (
        <p className="text-xs text-muted-foreground">{job.lastError}</p>
      )}
      {job.dryRun && !running && job.status === 'done' && (
        <p className="text-xs text-amber-600 dark:text-amber-400">
          This was a dry run — nothing was written. Untick “Dry run” to import for real.
        </p>
      )}
    </section>
  );
};

const Metric = ({ label, value, tone }: { label: string; value: number; tone?: 'bad' }) => (
  <div>
    <div className={`text-lg font-bold tabular-nums ${tone === 'bad' ? 'text-destructive' : ''}`}>
      {value.toLocaleString()}
    </div>
    <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
  </div>
);

const StatusBadge = ({ status }: { status: Job['status'] }) => {
  const tone =
    status === 'done' ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
    : status === 'failed' ? 'bg-destructive/10 text-destructive'
    : status === 'cancelled' || status === 'interrupted' ? 'bg-amber-500/10 text-amber-600 dark:text-amber-400'
    : 'bg-primary/10 text-primary';
  return (
    <span className={`px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider ${tone}`}>
      {status}
    </span>
  );
};
