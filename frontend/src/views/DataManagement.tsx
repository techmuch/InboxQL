import { useCallback, useEffect, useState } from 'react';
import {
  AlertTriangle, Archive, CheckCircle2, Database, HardDrive, Loader2, RotateCcw, ShieldCheck, Trash2, XCircle,
} from 'lucide-react';
import { settingAnchor } from '../lib/settingsIndex';
import { Help } from './Help';

interface Backup {
  name: string;
  at: string;
  bytes: number;
  extraBytes?: number;
  reason: 'update' | 'manual' | 'scheduled' | 'restore';
  version?: string;
  schema?: number;
  key: boolean;
  attachments: boolean;
  checked?: 'ok' | 'failed';
  checkNote?: string;
  legacy?: boolean;
}

interface Policy {
  keepUpdate: number;
  keepScheduled: number;
  keepRestore: number;
  schedule: 'off' | 'daily' | 'weekly';
}

interface Job {
  id: string;
  kind: string;
  status: string;
  current?: string;
  done: number;
  total: number;
  summary?: string;
  lastError?: string;
}

interface State {
  backups: Backup[];
  totalBytes: number;
  freeBytes: number;
  databaseBytes: number;
  needBytes: number;
  attachmentFiles: number;
  attachmentBytes: number;
  policy: Policy;
  staged?: string;
  canRestart: boolean;
  jobs: Job[];
}

export const formatBytes = (n?: number): string => {
  if (n === undefined || n === null) return '—';
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(1)} GB`;
  if (n >= 1 << 20) return `${Math.round(n / (1 << 20))} MB`;
  if (n >= 1 << 10) return `${Math.round(n / (1 << 10))} KB`;
  return `${n} bytes`;
};

const REASON: Record<Backup['reason'], string> = {
  update: 'Before an update',
  manual: 'Made by you',
  scheduled: 'Scheduled',
  restore: 'Set aside by a restore',
};

async function call<T = any>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let data: any = {};
  try { data = text ? JSON.parse(text) : {}; } catch { data = { error: text }; }
  if (!res.ok) throw new Error(data.error ?? `${res.status}`);
  return data as T;
}

/**
 * Settings → Data Management: the mailbox's backups first, and erasing it
 * last — the one action in Settings with no undo, which is why a backup is
 * offered on its way.
 */
export const DataManagement = () => {
  const [state, setState] = useState<State | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [lastJob, setLastJob] = useState<Job | null>(null);

  const load = useCallback(async () => {
    try {
      const s = await call<State>('GET', '/api/backups');
      setState(s);
      setError(null);
      return s;
    } catch (e: any) {
      setError(e.message);
      return null;
    }
  }, []);
  useEffect(() => { void load(); }, [load]);

  // While a backup or a check runs, the list is read again every second or
  // so; when it finishes, what it said is kept on screen.
  const running = state?.jobs ?? [];
  useEffect(() => {
    if (running.length === 0) return;
    const ids = running.map(j => j.id);
    const t = window.setInterval(async () => {
      const s = await load();
      if (s && s.jobs.length === 0) {
        const done = await call<Job>('GET', `/api/maintenance/jobs/${ids[0]}`).catch(() => null);
        if (done) setLastJob(done);
      }
    }, 1200);
    return () => window.clearInterval(t);
  }, [running.map(j => j.id).join(','), load]); // eslint-disable-line react-hooks/exhaustive-deps

  const act = async (op: () => Promise<unknown>, said?: string) => {
    try {
      await op();
      setNote(said ?? null);
      setError(null);
    } catch (e: any) {
      setError(e.message ?? String(e));
    }
    await load();
  };

  const startBackup = (opts: { key: boolean; attachments: boolean }) =>
    act(() => call('POST', '/api/backups', opts).catch(e => {
      if (/already running/.test(e.message)) return;
      throw e;
    }));

  return (
    <div className="animate-in fade-in duration-300 space-y-8">
      <div>
        <h2 className="text-2xl font-bold text-foreground mb-2 flex items-center gap-2">
          <Database className="w-6 h-6" />
          Data Management
          <Help topic="settings-data" className="ml-1" />
        </h2>
        <p className="text-muted-foreground text-sm">Back up the mailbox, choose what to keep, restore — and, last, erase it.</p>
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}
      {note && <p className="text-sm text-muted-foreground">{note}</p>}

      {!state ? (
        <p className="text-sm text-muted-foreground flex items-center gap-2"><Loader2 className="w-4 h-4 animate-spin" /> Loading…</p>
      ) : (
        <>
          <Backups state={state} lastJob={lastJob} onStart={startBackup} act={act} />
          <KeepRules policy={state.policy} onSave={p => act(() => call('PUT', '/api/backups/policy', p), 'Saved.')} />
        </>
      )}

      <Erase state={state} onBackup={() => startBackup({ key: false, attachments: false })} />
    </div>
  );
};

const card = 'border border-border bg-card/40 p-5';

const Backups = ({
  state, lastJob, onStart, act,
}: {
  state: State;
  lastJob: Job | null;
  onStart: (o: { key: boolean; attachments: boolean }) => void;
  act: (op: () => Promise<unknown>, said?: string) => Promise<void>;
}) => {
  const [key, setKey] = useState(false);
  const [attachments, setAttachments] = useState(false);
  const job = state.jobs.find(j => j.kind === 'backup');
  const checking = state.jobs.find(j => j.kind === 'backup-check');
  const need = state.needBytes + (attachments ? state.attachmentBytes : 0);
  // Room the keep rules could make is not counted: the warning is about what
  // is free now, and the backup itself says when it made room.
  const short = state.freeBytes < need;
  const usedPct = Math.min(100, Math.round((state.totalBytes / Math.max(1, state.totalBytes + state.freeBytes)) * 100));

  return (
    <section className={card} {...settingAnchor('backups')} aria-label="Backups">
      <div className="flex flex-wrap items-start gap-4">
        <div className="flex-1 min-w-64">
          <h3 className="text-base font-semibold flex items-center gap-2"><Archive className="w-4 h-4" /> Backups</h3>
          <p className="text-sm text-muted-foreground mt-1">
            A backup is a full copy of the mailbox's database ({formatBytes(state.databaseBytes)}), checked after it is
            written. Attachment files live outside it.
          </p>
        </div>
        <div className="text-sm text-right" aria-label="Disk space">
          <div className="flex items-center gap-1.5 justify-end"><HardDrive className="w-4 h-4 text-muted-foreground" />
            <span><b>{formatBytes(state.totalBytes)}</b> in backups · <b>{formatBytes(state.freeBytes)}</b> free</span>
          </div>
          <div className="mt-1 h-1.5 w-56 bg-muted ml-auto" role="meter" aria-valuenow={usedPct} aria-valuemin={0} aria-valuemax={100}
            aria-label="Backups' share of the disk they could use">
            <div className="h-full bg-primary" style={{ width: `${usedPct}%` }} />
          </div>
        </div>
      </div>

      {state.staged && (
        <div className="mt-4 flex items-center gap-3 border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm">
          <RotateCcw className="w-4 h-4 text-amber-600" />
          <span className="flex-1">A restore from <b>{state.staged.split('/').pop()}</b> is waiting for the server to restart.</span>
          <button type="button" onClick={() => act(() => call('DELETE', '/api/backups/restore'), 'Restore cancelled.')}
            className="px-2 py-1 border border-border hover:bg-accent/40">Cancel it</button>
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center gap-3">
        <button type="button" disabled={!!job} onClick={() => onStart({ key, attachments })}
          className="flex items-center gap-2 bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-60">
          {job ? <Loader2 className="w-4 h-4 animate-spin" /> : <Archive className="w-4 h-4" />}
          {job ? 'Backing up…' : 'Back up now'}
        </button>
        <label className="flex items-center gap-1.5 text-sm">
          <input type="checkbox" checked={key} onChange={e => setKey(e.target.checked)} /> Include the vault key
        </label>
        <label className="flex items-center gap-1.5 text-sm">
          <input type="checkbox" checked={attachments} onChange={e => setAttachments(e.target.checked)} />
          Include attachments ({state.attachmentFiles} files, {formatBytes(state.attachmentBytes)})
        </label>
      </div>
      {key && (
        <p className="mt-2 text-xs text-amber-700 dark:text-amber-400">
          A backup with its vault key can decrypt your account passwords. Keep it somewhere as private as this machine.
        </p>
      )}
      {short && !job && (
        <p className="mt-2 text-xs text-amber-700 dark:text-amber-400 flex items-start gap-1.5">
          <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
          A backup needs about {formatBytes(need)} and {formatBytes(state.freeBytes)} is free. Backing up first removes the
          backups the keep rules would remove anyway; if that is not enough, it stops before writing anything.
        </p>
      )}
      {job && <Progress job={job} />}
      {checking && <Progress job={checking} />}
      {lastJob && !job && !checking && (
        <p className={`mt-3 text-sm ${lastJob.status === 'failed' ? 'text-destructive' : 'text-muted-foreground'}`}>
          {lastJob.status === 'failed' ? lastJob.lastError : lastJob.summary}
        </p>
      )}

      <BackupList state={state} act={act} busy={!!job || !!checking} />
    </section>
  );
};

const Progress = ({ job }: { job: Job }) => {
  const pct = job.total > 0 ? Math.round((job.done / job.total) * 100) : 0;
  const what = job.kind === 'backup-check' ? `Checking ${job.current ?? ''}`
    : job.current === 'checking' ? 'Checking the copy'
    : job.current === 'attachments' ? 'Archiving attachments'
    : `Copying · ${formatBytes(job.done * 1024)} of ${formatBytes(job.total * 1024)}`;
  return (
    <div className="mt-3 flex items-center gap-3 text-sm" aria-live="polite">
      <span className="w-56 text-muted-foreground truncate">{what}</span>
      <div className="flex-1 h-1.5 bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
        <div className="h-full bg-primary transition-all" style={{ width: `${job.current === 'copying' || !job.current ? pct : 100}%` }} />
      </div>
    </div>
  );
};

const BackupList = ({
  state, act, busy,
}: { state: State; act: (op: () => Promise<unknown>, said?: string) => Promise<void>; busy: boolean }) => {
  const [confirm, setConfirm] = useState<{ name: string; what: 'delete' | 'restore' } | null>(null);
  const [typed, setTyped] = useState('');
  const newest = state.backups[0]?.name;

  if (state.backups.length === 0) {
    return <p className="mt-5 text-sm text-muted-foreground">No backups yet.</p>;
  }
  return (
    <div className="mt-5 overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground border-b border-border">
            <th className="py-1.5 pr-3 font-semibold">When</th>
            <th className="py-1.5 pr-3 font-semibold">Why</th>
            <th className="py-1.5 pr-3 font-semibold">Size</th>
            <th className="py-1.5 pr-3 font-semibold">With</th>
            <th className="py-1.5 pr-3 font-semibold">Checked</th>
            <th className="py-1.5" />
          </tr>
        </thead>
        <tbody>
          {state.backups.flatMap(b => [
            <tr key={b.name} className="border-b border-border/60 align-top">
              <td className="py-2 pr-3">
                <div>{new Date(b.at).toLocaleString()}</div>
                <div className="text-[11px] text-muted-foreground font-mono">{b.name}</div>
              </td>
              <td className="py-2 pr-3">
                {REASON[b.reason]}
                {b.reason === 'update' && b.version && <span className="text-muted-foreground"> to {b.version}</span>}
                {b.name === newest && <span className="ml-1.5 text-[10px] uppercase tracking-wide text-primary">newest</span>}
              </td>
              <td className="py-2 pr-3 tabular-nums">{formatBytes(b.bytes + (b.extraBytes ?? 0))}</td>
              <td className="py-2 pr-3 text-xs text-muted-foreground">
                {[b.key && 'key', b.attachments && 'attachments'].filter(Boolean).join(', ') || '—'}
              </td>
              <td className="py-2 pr-3">
                {b.checked === 'ok' && <span className="flex items-center gap-1 text-green-700 dark:text-green-400" title={b.checkNote}><CheckCircle2 className="w-3.5 h-3.5" /> whole</span>}
                {b.checked === 'failed' && <span className="flex items-center gap-1 text-destructive" title={b.checkNote}><XCircle className="w-3.5 h-3.5" /> failed</span>}
                {!b.checked && (
                  <button type="button" disabled={busy} onClick={() => act(() => call('POST', '/api/backups/check', { name: b.name }))}
                    className="flex items-center gap-1 text-xs text-primary hover:underline disabled:opacity-50">
                    <ShieldCheck className="w-3.5 h-3.5" /> Check it
                  </button>
                )}
              </td>
              <td className="py-2 text-right whitespace-nowrap">
                <span className="inline-flex items-center gap-1">
                  <button type="button" disabled={busy || b.checked === 'failed'} onClick={() => { setTyped(''); setConfirm({ name: b.name, what: 'restore' }); }}
                    className="px-2 py-0.5 text-xs border border-border hover:bg-accent/40 disabled:opacity-40">Restore…</button>
                  <button type="button" aria-label={`Delete ${b.name}`} onClick={() => setConfirm({ name: b.name, what: 'delete' })}
                    className="p-1 text-muted-foreground hover:text-destructive"><Trash2 className="w-3.5 h-3.5" /></button>
                </span>
              </td>
            </tr>,
            confirm?.name === b.name && (
              <tr key={`${b.name}-confirm`} className="border-b border-border/60 bg-muted/30">
                <td colSpan={6} className="px-3 py-2.5">
                  {confirm.what === 'delete' ? (
                    <span className="flex flex-wrap items-center gap-2">
                      <span className="text-sm">Delete <b>{b.name}</b> for good? {formatBytes(b.bytes + (b.extraBytes ?? 0))} is freed.</span>
                      <button type="button" onClick={() => { setConfirm(null); void act(() => call('DELETE', `/api/backups?name=${encodeURIComponent(b.name)}`), `Deleted ${b.name}.`); }}
                        className="px-2.5 py-1 text-xs bg-destructive text-destructive-foreground">Delete</button>
                      <button type="button" onClick={() => setConfirm(null)} className="px-2 py-1 text-xs hover:bg-accent/40">Keep</button>
                    </span>
                  ) : (
                    <RestoreConfirm backup={b} canRestart={state.canRestart} typed={typed} setTyped={setTyped}
                      onCancel={() => { setConfirm(null); setTyped(''); }}
                      onRestore={() => {
                        setConfirm(null); setTyped('');
                        void act(() => call<{ instanceId: string }>('POST', '/api/backups/restore', { name: b.name }).then(r => waitForRestart(r.instanceId)),
                          'Restoring: the server is restarting with this backup. The current mailbox is kept as a backup.');
                      }} />
                  )}
                </td>
              </tr>
            ),
          ])}
        </tbody>
      </table>
    </div>
  );
};

/** Restoring replaces the mailbox, so it is confirmed by typing the word. */
const RestoreConfirm = ({
  backup, canRestart, typed, setTyped, onCancel, onRestore,
}: {
  backup: Backup;
  canRestart: boolean;
  typed: string;
  setTyped: (s: string) => void;
  onCancel: () => void;
  onRestore: () => void;
}) => {
  if (!canRestart) {
    return (
      <span className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
        This server cannot restart itself. Stop it, then run <code className="font-mono">iql restore {backup.name}</code>.
        <button type="button" onClick={onCancel} className="underline">Close</button>
      </span>
    );
  }
  return (
    <span className="flex flex-col gap-2">
      <span className="text-sm">
        The mailbox becomes this backup from {new Date(backup.at).toLocaleString()}; mail since then syncs back from
        the server, and anything imported since is gone. What is there now is kept as a backup. Type <b>restore</b> to go on.
      </span>
      <span className="inline-flex items-center gap-2">
        <input autoFocus aria-label="Type restore to confirm" value={typed} onChange={e => setTyped(e.target.value)}
          className="w-28 bg-background border border-border px-2 py-1 text-sm" />
        <button type="button" disabled={typed.trim().toLowerCase() !== 'restore'} onClick={onRestore}
          className="px-3 py-1 text-sm bg-destructive text-destructive-foreground disabled:opacity-40">Restore and restart</button>
        <button type="button" onClick={onCancel} className="px-2 py-1 text-sm hover:bg-accent/40">Cancel</button>
      </span>
    </span>
  );
};

/**
 * After a restore is asked for, the page reloads once a new server answers —
 * known by an instance id other than the one that took the request.
 */
const waitForRestart = async (before: string) => {
  const start = Date.now();
  while (Date.now() - start < 120_000) {
    await new Promise(r => setTimeout(r, 1500));
    const now = await fetch('/api/version').then(r => (r.ok ? r.json() : null)).catch(() => null);
    if (now && now.instanceId !== before) {
      window.location.reload();
      return;
    }
  }
};

const KeepRules = ({ policy, onSave }: { policy: Policy; onSave: (p: Policy) => void }) => {
  const [p, setP] = useState(policy);
  useEffect(() => setP(policy), [policy]);
  const dirty = JSON.stringify(p) !== JSON.stringify(policy);
  const num = (k: keyof Policy, label: string) => (
    <label className="flex items-center justify-between gap-3 text-sm">
      <span>{label}</span>
      <input type="number" min={1} max={100} aria-label={label} value={p[k] as number}
        onChange={e => setP({ ...p, [k]: Number(e.target.value) })}
        className="w-16 bg-background border border-border px-1.5 py-0.5 text-sm text-right" />
    </label>
  );
  return (
    <section className={card} {...settingAnchor('backup-keep')} aria-label="Keep rules">
      <h3 className="text-base font-semibold">Which to keep, and how often</h3>
      <p className="text-sm text-muted-foreground mt-1 mb-4">
        After each new backup, older ones beyond these are removed — never the newest, and never one you made yourself.
      </p>
      <div className="grid gap-2 max-w-md">
        {num('keepUpdate', 'Backups taken before updates')}
        {num('keepScheduled', 'Scheduled backups')}
        {num('keepRestore', 'Mailboxes set aside by a restore')}
        <label className="flex items-center justify-between gap-3 text-sm">
          <span>Back up automatically</span>
          <select aria-label="Back up automatically" value={p.schedule} onChange={e => setP({ ...p, schedule: e.target.value as Policy['schedule'] })}
            className="bg-background border border-border px-1.5 py-0.5 text-sm">
            <option value="off">Never</option>
            <option value="daily">Every day</option>
            <option value="weekly">Every week</option>
          </select>
        </label>
      </div>
      {p.schedule !== 'off' && (
        <p className="mt-2 text-xs text-muted-foreground">
          Each is a full copy. Made while InboxQL is running, and on battery it waits for mains power.
        </p>
      )}
      <button type="button" disabled={!dirty} onClick={() => onSave(p)}
        className="mt-4 px-3 py-1.5 text-sm bg-primary text-primary-foreground disabled:opacity-40">Save</button>
    </section>
  );
};

/**
 * Erasing the mailbox, which has no undo — so it says when the last backup
 * was, offers one, and asks for the word.
 */
const Erase = ({ state, onBackup }: { state: State | null; onBackup: () => void }) => {
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState('');
  const [error, setError] = useState<string | null>(null);
  const newest = state?.backups[0];
  const recent = newest && Date.now() - new Date(newest.at).getTime() < 24 * 3600_000;

  const erase = async () => {
    try {
      const res = await fetch('/api/data', { method: 'DELETE' });
      if (!res.ok) throw new Error((await res.text()) || 'could not erase');
      window.location.reload();
    } catch (e: any) {
      setError(e.message);
    }
  };

  return (
    <section className="border border-destructive/20 bg-destructive/5 p-6" {...settingAnchor('erase-data')} aria-label="Erase">
      <div className="flex items-start gap-4">
        <AlertTriangle className="w-6 h-6 text-destructive shrink-0 mt-0.5" />
        <div className="flex-1">
          <h3 className="text-sm font-bold text-destructive mb-1">Delete All Stored Messages</h3>
          <p className="text-sm text-muted-foreground mb-3 leading-relaxed">
            This permanently erases every stored message and attachment from this mailbox. Mail on your server is
            untouched and will sync again; imported mail will not come back.
          </p>
          <p className="text-sm mb-4">
            {newest
              ? <>Last backup: <b>{new Date(newest.at).toLocaleString()}</b>{!recent && ' — more than a day ago'}.</>
              : <b>There is no backup of this mailbox.</b>}
          </p>
          {!open ? (
            <div className="flex flex-wrap gap-2">
              {!recent && (
                <button type="button" onClick={onBackup} className="flex items-center gap-2 border border-border bg-background px-4 py-2 text-sm font-semibold hover:bg-accent/40">
                  <Archive className="w-4 h-4" /> Back up first
                </button>
              )}
              <button type="button" onClick={() => setOpen(true)}
                className="bg-destructive hover:bg-destructive/90 text-destructive-foreground px-4 py-2 text-sm font-semibold flex items-center gap-2">
                <Trash2 className="w-4 h-4" /> Erase All Local Data
              </button>
            </div>
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm">Type <b>erase</b> to erase this mailbox:</span>
              <input autoFocus aria-label="Type erase to confirm" value={typed} onChange={e => setTyped(e.target.value)}
                className="w-28 bg-background border border-border px-2 py-1 text-sm" />
              <button type="button" disabled={typed.trim().toLowerCase() !== 'erase'} onClick={erase}
                className="bg-destructive text-destructive-foreground px-3 py-1.5 text-sm font-semibold disabled:opacity-40">Erase</button>
              <button type="button" onClick={() => { setOpen(false); setTyped(''); }} className="px-2 py-1.5 text-sm hover:bg-accent/40">Cancel</button>
            </div>
          )}
          {error && <p className="mt-2 text-sm text-destructive">{error}</p>}
        </div>
      </div>
    </section>
  );
};
