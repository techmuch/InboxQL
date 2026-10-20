import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AlertCircle, ArrowLeft, Check, ExternalLink, Loader2, LogIn, Plug, Plus, RefreshCw, Server, Settings2, Trash2, Unplug, X,
} from 'lucide-react';
import { settingAnchor } from '../lib/settingsIndex';
import { openHelp } from '../lib/helpTopics';

/**
 * Settings → Mail Accounts.
 *
 * Adding starts from the address: the servers are worked out (a built-in
 * table, the domain's own published settings, DNS) and both are signed in to
 * before anything is saved. The full form is still there, under "Enter
 * settings manually", and is what Edit opens.
 */

export interface Account {
  id: string; name: string; email: string; host: string; port: number; user: string; ssl: boolean;
  smtpHost: string; smtpPort: number; smtpUser?: string; auth?: string; oauthClientId?: string;
  disconnected?: boolean; lastSyncStatus: string; lastSyncError: string; lastSyncedAt?: string;
}
interface Stats { totalMessages: number; storageSize: number }
interface Check { ok: boolean; skipped?: boolean; kind?: string; advice?: string; detail?: string }
interface Verdict { imap: Check; smtp: Check }
interface Found {
  provider?: string; source: string; imap: { host: string; port: number; tls: boolean };
  smtp: { host: string; port: number; tls: boolean }; username: string; note?: string; passwordUrl?: string;
  google?: boolean; unsupported?: string;
}
interface Attempt { source: string; asked: string; result: string }

const field = 'w-full bg-background border border-border px-3 py-2 text-sm focus:ring-1 focus:ring-primary outline-none';
const label = 'block text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-1';
const btn = 'inline-flex items-center gap-1.5 border border-border px-3 py-1.5 text-xs hover:bg-accent disabled:opacity-50';
const primary = 'inline-flex items-center gap-1.5 bg-primary px-4 py-1.5 text-xs font-semibold text-primary-foreground hover:opacity-90 disabled:opacity-50';

async function send<T>(method: string, url: string, body?: unknown): Promise<{ ok: boolean; status: number; data: T; text: string }> {
  const r = await fetch(url, {
    method, headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await r.text();
  let data: any = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* plain-text error */ }
  return { ok: r.ok, status: r.status, data, text: (data?.error as string) ?? text.trim() };
}

/** Saving an account starts its first sync. */
function firstSync(id?: string) {
  if (id) void send('POST', `/api/accounts/sync?id=${encodeURIComponent(id)}`).catch(() => {});
}

export const formatSize = (bytes?: number) => {
  if (!bytes) return '0 B';
  const k = 1024, sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(sizes.length - 1, Math.floor(Math.log(bytes) / Math.log(k)));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(i ? 1 : 0))} ${sizes[i]}`;
};

export function ago(iso?: string, now = Date.now()): string {
  if (!iso) return 'never';
  const s = Math.max(0, (now - new Date(iso).getTime()) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
}

/** What the server's live connection to an account is doing. */
export interface Live {
  worker: { state: 'connecting' | 'live' | 'history' | 'waiting' | 'stopped'; since?: string; lastError?: string; nextTry?: string };
  progress: { onServer: number; stored: number; historyDone: boolean; folders: number };
  pending: { waiting: number; failed: number; lastError?: string };
}

/** Why a sync failed, in words. */
const why = (err?: string) => {
  const e = (err || '').toLowerCase();
  return /auth|password|credential|login/.test(e) ? 'the server rejected the sign-in'
    : /no such host/.test(e) ? 'the server name was not found'
    : /certificate|x509/.test(e) ? "the server's certificate is not trusted"
    : /asleep/.test(e) ? 'the computer was asleep'
    : /timeout|refused|unreachable/.test(e) ? 'the server did not answer' : 'it failed';
};

/** The card's one-line state, and how much it wants attention. */
export function statusOf(a: Account, live?: Live): { text: string; tone: 'ok' | 'busy' | 'bad' | 'off' } {
  if (a.disconnected) return { text: 'Disconnected — its mail is kept; it does not sync or send', tone: 'off' };
  switch (live?.worker.state) {
    case 'live':
      return { text: `Live — new mail arrives as it comes${a.lastSyncedAt ? ` · checked ${ago(a.lastSyncedAt)}` : ''}`, tone: 'ok' };
    case 'history': {
      const { stored, onServer } = live.progress;
      return { text: `Live, and fetching older mail: ${stored.toLocaleString()} of ${onServer.toLocaleString()}`, tone: 'busy' };
    }
    case 'connecting':
      return { text: 'Connecting…', tone: 'busy' };
    case 'waiting': {
      const at = live.worker.nextTry ? new Date(live.worker.nextTry).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '';
      return { text: `Not connected: ${why(live.worker.lastError)}${at ? ` — trying again at ${at}` : ''}`, tone: 'bad' };
    }
  }
  if (a.lastSyncStatus === 'syncing') return { text: 'Syncing…', tone: 'busy' };
  if (a.lastSyncStatus === 'error') {
    return { text: `Last sync: ${why(a.lastSyncError)} — ${ago(a.lastSyncedAt) === 'never' ? 'never synced' : `last good sync ${ago(a.lastSyncedAt)}`}`, tone: 'bad' };
  }
  if (!a.lastSyncedAt) return { text: 'Not synced yet', tone: 'off' };
  return { text: `Synced ${ago(a.lastSyncedAt)}`, tone: 'ok' };
}

export const MailAccounts = () => {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [stats, setStats] = useState<Record<string, Stats>>({});
  const [live, setLive] = useState<Record<string, Live>>({});
  const [mode, setMode] = useState<{ kind: 'list' } | { kind: 'add' } | { kind: 'edit'; account: Account | null; prefill?: Partial<Form> }>({ kind: 'list' });
  const [removing, setRemoving] = useState<Account | null>(null);

  const load = useCallback(async () => {
    const r = await send<Account[]>('GET', '/api/accounts').catch(() => null);
    if (!r?.ok) return;
    const list = r.data ?? [];
    setAccounts(list);
    send<Record<string, Live>>('GET', '/api/accounts/live')
      .then(l => l.ok && setLive(l.data ?? {}))
      .catch(() => {});
    for (const a of list) {
      send<Stats>('GET', `/api/accounts/stats?id=${encodeURIComponent(a.id)}`)
        .then(s => s.ok && setStats(p => ({ ...p, [a.id]: s.data })))
        .catch(() => {});
    }
  }, []);
  useEffect(() => { void load(); }, [load]);
  // Quicker while something is syncing, so the card follows it.
  const syncing = accounts.some(a => a.lastSyncStatus === 'syncing'
    || ['connecting', 'history'].includes(live[a.id]?.worker.state ?? ''));
  useEffect(() => {
    const t = setInterval(() => void load(), syncing ? 3000 : 15000);
    return () => clearInterval(t);
  }, [load, syncing]);

  if (mode.kind === 'add') {
    return <AddAccount onDone={() => { setMode({ kind: 'list' }); void load(); }}
      onManual={prefill => setMode({ kind: 'edit', account: null, prefill })} />;
  }
  if (mode.kind === 'edit') {
    return <AccountForm account={mode.account} prefill={mode.prefill}
      onDone={() => { setMode({ kind: 'list' }); void load(); }} />;
  }

  return (
    <div className="animate-in fade-in duration-300" {...settingAnchor('mail-accounts')}>
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h2 className="text-2xl font-bold text-foreground">Mail Accounts</h2>
          <p className="mt-1 text-sm text-muted-foreground">The mailboxes InboxQL reads from and sends through.</p>
        </div>
        <button onClick={() => setMode({ kind: 'add' })} className={primary}><Plus className="h-3 w-3" /> Add account</button>
      </div>
      {accounts.length === 0 ? (
        <div className="border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
          No accounts yet. <button className="text-primary underline" onClick={() => setMode({ kind: 'add' })}>Add one</button> with
          its address and password — InboxQL works out the servers.
        </div>
      ) : (
        <div className="space-y-4">
          {accounts.map(a => (
            <AccountCard key={a.id} account={a} stats={stats[a.id]} live={live[a.id]} onChanged={load}
              onEdit={() => setMode({ kind: 'edit', account: a })} onRemove={() => setRemoving(a)} />
          ))}
        </div>
      )}
      {removing && <RemoveDialog account={removing} onClose={() => setRemoving(null)}
        onRemoved={() => { setRemoving(null); void load(); }}
        onDisconnect={async () => { await send('POST', `/api/accounts/disconnect?id=${encodeURIComponent(removing.id)}`); setRemoving(null); void load(); }} />}
    </div>
  );
};

// --- the card ----------------------------------------------------------------

const AccountCard = ({ account: a, stats, live, onChanged, onEdit, onRemove }: {
  account: Account; stats?: Stats; live?: Live; onChanged: () => void; onEdit: () => void; onRemove: () => void;
}) => {
  const [verdict, setVerdict] = useState<Verdict | null>(null);
  const [testing, setTesting] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const status = statusOf(a, live);
  const pending = live?.pending;
  const dot = { ok: 'bg-green-500', busy: 'bg-primary animate-pulse', bad: 'bg-red-500', off: 'bg-muted-foreground/40' }[status.tone];

  const test = async () => {
    setTesting(true); setVerdict(null); setNote(null);
    const r = await send<Verdict>('POST', '/api/accounts/verify', { id: a.id }).catch(() => null);
    setTesting(false);
    if (r?.ok) setVerdict(r.data); else setNote(r?.text || 'Could not reach InboxQL.');
  };
  const sync = async () => {
    setNote(null);
    const r = await send('POST', `/api/accounts/sync?id=${encodeURIComponent(a.id)}`).catch(() => null);
    if (!r?.ok) setNote(r?.text || 'Could not start a sync.');
    setTimeout(onChanged, 500);
  };
  const disconnect = async () => {
    await send('POST', `/api/accounts/disconnect?id=${encodeURIComponent(a.id)}`);
    onChanged();
  };

  return (
    <div className="border border-border bg-card p-5" data-account={a.id}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className={`h-2.5 w-2.5 shrink-0 ${dot}`} aria-hidden />
            <span className="text-lg font-bold">{a.name}</span>
            {a.auth === 'google' && <span className="border border-border px-1.5 text-[10px] text-muted-foreground">Signed in with Google</span>}
          </div>
          <div className="text-sm text-muted-foreground">{a.email}</div>
          <div className={`mt-1 text-xs ${status.tone === 'bad' ? 'text-destructive' : 'text-muted-foreground'}`}>{status.text}</div>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {a.disconnected ? (
            <button className={primary} onClick={onEdit}><Plug className="h-3 w-3" /> Connect again</button>
          ) : (
            <>
              <button className={btn} onClick={sync} disabled={a.lastSyncStatus === 'syncing'}>
                <RefreshCw className={`h-3 w-3 ${a.lastSyncStatus === 'syncing' ? 'animate-spin' : ''}`} /> Sync now
              </button>
              <button className={btn} onClick={test} disabled={testing}>
                {testing ? <Loader2 className="h-3 w-3 animate-spin" /> : <Server className="h-3 w-3" />} Test
              </button>
              <button className={btn} onClick={onEdit}><Settings2 className="h-3 w-3" /> Edit</button>
              <button className={btn} onClick={disconnect} title="Forget the login and stop syncing; the mail stays">
                <Unplug className="h-3 w-3" /> Disconnect
              </button>
            </>
          )}
          <button className={`${btn} hover:text-destructive`} onClick={onRemove}><Trash2 className="h-3 w-3" /> Remove</button>
        </div>
      </div>

      {status.tone === 'bad' && (live?.worker.lastError || a.lastSyncError) && (() => {
        const err = live?.worker.lastError || a.lastSyncError;
        return (
          <p className="mt-3 border border-red-500/20 bg-red-500/5 p-2 text-xs text-red-600 dark:text-red-400">
            {err}
            {/auth|password|credential/i.test(err) && (
              <> — <button className="underline" onClick={onEdit}>update the password</button></>
            )}
          </p>
        );
      })()}

      {/* Changes made here that the server has not heard about yet. */}
      {pending && pending.waiting > 0 && (
        <p className="mt-2 text-xs text-muted-foreground">
          {pending.waiting === 1 ? '1 change' : `${pending.waiting.toLocaleString()} changes`} waiting to reach the server.
        </p>
      )}
      {pending && pending.failed > 0 && (
        <p className="mt-2 text-xs text-destructive">
          {pending.failed === 1 ? '1 change' : `${pending.failed.toLocaleString()} changes`} could not be made on the server
          {pending.lastError ? `: ${pending.lastError}` : ''} — kept here.{' '}
          <button className="underline" onClick={async () => {
            await send('POST', `/api/accounts/retry?id=${encodeURIComponent(a.id)}`);
            onChanged();
          }}>Try again</button>
        </p>
      )}

      <dl className="mt-4 grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-4">
        <div><dt className="text-muted-foreground">Messages</dt><dd className="font-semibold tabular-nums">{(stats?.totalMessages ?? 0).toLocaleString()}</dd></div>
        <div><dt className="text-muted-foreground">Stored</dt><dd className="font-semibold tabular-nums">{formatSize(stats?.storageSize)}</dd></div>
        <div className="min-w-0"><dt className="text-muted-foreground">Incoming</dt><dd className="truncate font-mono">{a.host}:{a.port}</dd></div>
        <div className="min-w-0"><dt className="text-muted-foreground">Outgoing</dt>
          <dd className="truncate font-mono">{a.smtpHost ? `${a.smtpHost}:${a.smtpPort || 587}` : 'none — cannot send'}</dd></div>
      </dl>

      {note && <p className="mt-3 text-xs text-destructive">{note}</p>}
      {verdict && <VerdictView verdict={verdict} />}
    </div>
  );
};

const VerdictView = ({ verdict }: { verdict: Verdict }) => (
  <div className="mt-3 space-y-1.5">
    {(['imap', 'smtp'] as const).map(k => {
      const c = verdict[k];
      const name = k === 'imap' ? 'Incoming (IMAP)' : 'Outgoing (SMTP)';
      return (
        <div key={k} className={`flex items-start gap-2 border p-2 text-xs ${c.ok ? 'border-green-500/30 bg-green-500/5' : c.skipped ? 'border-border' : 'border-red-500/30 bg-red-500/5'}`}>
          {c.ok ? <Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-green-600" /> : c.skipped ? <span className="w-3.5" /> : <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-red-600" />}
          <div className="min-w-0">
            <span className="font-semibold">{name}: </span>
            {c.ok ? 'signed in.' : c.skipped ? c.advice : c.advice}
            {c.detail && !c.ok && <div className="mt-0.5 break-words font-mono text-[10px] text-muted-foreground">{c.detail}</div>}
          </div>
        </div>
      );
    })}
  </div>
);

// --- remove -----------------------------------------------------------------

const RemoveDialog = ({ account: a, onClose, onRemoved, onDisconnect }: {
  account: Account; onClose: () => void; onRemoved: () => void; onDisconnect: () => void;
}) => {
  const [foot, setFoot] = useState<{ messages: number; bytes: number; drafts: number } | null>(null);
  const [typed, setTyped] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    send<{ messages: number; bytes: number; drafts: number }>('GET', `/api/accounts/footprint?id=${encodeURIComponent(a.id)}`)
      .then(r => r.ok && setFoot(r.data)).catch(() => {});
  }, [a.id]);
  const remove = async () => {
    setBusy(true);
    const r = await send('DELETE', `/api/accounts?id=${encodeURIComponent(a.id)}`).catch(() => null);
    setBusy(false);
    if (r?.ok) onRemoved(); else setError(r?.text || 'Could not remove the account.');
  };
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/30 pt-[12vh]" onMouseDown={onClose}>
      <div role="dialog" aria-label={`Remove ${a.name}`} onMouseDown={e => e.stopPropagation()} className="w-[30rem] max-w-[95vw] border border-border bg-card p-5 shadow-xl">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-lg font-semibold">Remove {a.name}?</h3>
          <button onClick={onClose} aria-label="Close" className="p-1 text-muted-foreground hover:text-foreground"><X className="h-4 w-4" /></button>
        </div>
        <p className="text-sm">
          This deletes the account and, from this computer,{' '}
          <strong>{foot ? `${foot.messages.toLocaleString()} message${foot.messages === 1 ? '' : 's'} (${formatSize(foot.bytes)})` : 'every message stored for it'}</strong>
          {foot && foot.drafts > 0 && <> and {foot.drafts} draft{foot.drafts === 1 ? '' : 's'}</>}. Mail on the server is not touched. It cannot be undone here.
        </p>
        {!a.disconnected && (
          <p className="mt-3 border border-border bg-muted/30 p-3 text-xs">
            To keep the mail and stop using the account, <button className="font-semibold text-primary underline" onClick={onDisconnect}>disconnect it instead</button>:
            the login is forgotten, nothing syncs, and the mail stays searchable.
          </p>
        )}
        <label className="mt-4 block text-xs">
          Type <span className="font-mono font-semibold">{a.name}</span> to confirm
          <input autoFocus className={`${field} mt-1`} value={typed} onChange={e => setTyped(e.target.value)} />
        </label>
        {error && <p className="mt-2 text-xs text-destructive">{error}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button className={btn} onClick={onClose}>Cancel</button>
          <button disabled={busy || typed.trim() !== a.name.trim()} onClick={remove}
            className="inline-flex items-center gap-1.5 bg-destructive px-4 py-1.5 text-xs font-semibold text-destructive-foreground disabled:opacity-40">
            <Trash2 className="h-3 w-3" /> Remove and delete the mail
          </button>
        </div>
      </div>
    </div>
  );
};

// --- adding, from the address -------------------------------------------------

const AddAccount = ({ onDone, onManual }: { onDone: () => void; onManual: (prefill: Partial<Form>) => void }) => {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [found, setFound] = useState<Found | null>(null);
  const [tried, setTried] = useState<Attempt[] | null>(null);
  const [looking, setLooking] = useState(false);
  const [askedMozilla, setAskedMozilla] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [verdict, setVerdict] = useState<Verdict | null>(null);
  const [busy, setBusy] = useState(false);

  const discover = async (ispdb = false) => {
    setLooking(true); setError(null); setVerdict(null);
    if (ispdb) setAskedMozilla(true); else { setFound(null); setTried(null); setAskedMozilla(false); }
    const r = await send<{ found: boolean; settings?: Found; tried: Attempt[] }>('GET',
      `/api/accounts/discover?email=${encodeURIComponent(email.trim())}${ispdb ? '&ispdb=1' : ''}`).catch(() => null);
    setLooking(false);
    if (!r?.ok) { setError(r?.text || 'Could not reach InboxQL.'); return; }
    setTried(r.data.tried);
    setFound(r.data.found ? r.data.settings! : null);
  };

  const asAccount = (f: Found) => ({
    name: email.trim(), email: email.trim().toLowerCase(), user: f.username, password,
    host: f.imap.host, port: f.imap.port, ssl: f.imap.tls, smtpHost: f.smtp.host, smtpPort: f.smtp.port,
  });

  const connect = async (anyway = false) => {
    if (!found) return;
    setBusy(true); setError(null);
    if (!anyway) {
      const v = await send<Verdict>('POST', '/api/accounts/verify', asAccount(found)).catch(() => null);
      if (!v?.ok) { setBusy(false); setError(v?.text || 'Could not reach InboxQL.'); return; }
      setVerdict(v.data);
      if (!v.data.imap.ok || !(v.data.smtp.ok || v.data.smtp.skipped)) { setBusy(false); return; }
    }
    const s = await send<Account>('POST', '/api/accounts', { ...asAccount(found), lastSyncStatus: 'idle' }).catch(() => null);
    setBusy(false);
    if (!s?.ok) { setError(s?.text || 'Could not save the account.'); return; }
    firstSync(s.data?.id);
    onDone();
  };

  const manual = () => {
    const at = email.indexOf('@');
    onManual(found ? { name: email.trim(), email: email.trim(), user: found.username, pass: password, imap: found.imap.host,
      port: String(found.imap.port), ssl: found.imap.tls, smtp: found.smtp.host, smtpPort: String(found.smtp.port) }
      : { name: email.trim(), email: email.trim(), user: email.trim(), pass: password, imap: at > 0 ? `imap.${email.slice(at + 1)}` : '' });
  };

  const valid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());

  return (
    <div className="max-w-xl animate-in fade-in duration-200">
      <button onClick={onDone} className="mb-4 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"><ArrowLeft className="h-3 w-3" /> Mail Accounts</button>
      <h2 className="text-2xl font-bold">Add an account</h2>
      <p className="mb-6 mt-1 text-sm text-muted-foreground">Your address first: InboxQL works out the servers.</p>

      <div className="space-y-4">
        <div>
          <label className={label} htmlFor="add-email">Email address</label>
          <div className="flex gap-2">
            <input id="add-email" type="email" autoFocus className={field} value={email} placeholder="you@example.com"
              onChange={e => { setEmail(e.target.value); setFound(null); setTried(null); setVerdict(null); }}
              onKeyDown={e => { if (e.key === 'Enter' && valid) void discover(); }} />
            <button className={btn} disabled={!valid || looking} onClick={() => discover()}>
              {looking && !askedMozilla ? <Loader2 className="h-3 w-3 animate-spin" /> : null} Continue
            </button>
          </div>
        </div>

        {found && (
          <div className="border border-border bg-muted/20 p-4 text-sm">
            <div className="font-semibold">{found.provider || 'Found the servers'}</div>
            <div className="text-[11px] text-muted-foreground">from {SOURCE[found.source] ?? found.source}</div>
            {found.unsupported ? (
              <p className="mt-2 text-sm">{found.unsupported}</p>
            ) : (
              <>
                <div className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 font-mono text-xs">
                  <span className="text-muted-foreground">in</span><span>{found.imap.host}:{found.imap.port} {found.imap.tls ? 'TLS' : 'STARTTLS'}</span>
                  <span className="text-muted-foreground">out</span><span>{found.smtp.host ? `${found.smtp.host}:${found.smtp.port} ${found.smtp.tls ? 'TLS' : 'STARTTLS'}` : 'none found'}</span>
                  <span className="text-muted-foreground">user</span><span>{found.username}</span>
                </div>
                {found.note && (
                  <p className="mt-3 text-xs">
                    {found.note}{' '}
                    {found.passwordUrl && <a href={found.passwordUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-0.5 text-primary underline">Make one <ExternalLink className="h-3 w-3" /></a>}
                  </p>
                )}
              </>
            )}
          </div>
        )}

        {found?.google && <GoogleSignIn email={email.trim()} onConnected={onDone} />}

        {found && !found.unsupported && (
          <div>
            <label className={label} htmlFor="add-pass">{found.passwordUrl ? 'App password' : 'Password'}</label>
            <input id="add-pass" type="password" className={field} value={password} onChange={e => setPassword(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter' && password) void connect(); }} />
            <p className="mt-1 text-[11px] text-muted-foreground">Encrypted on this computer; never sent anywhere but these servers.</p>
          </div>
        )}

        {tried && !found && (
          <div className="border border-border p-4 text-sm">
            <p className="font-semibold">No settings found for this domain.</p>
            <ul className="mt-2 space-y-0.5 text-xs text-muted-foreground">
              {tried.map((t, i) => <li key={i}><span className="font-mono">{t.asked}</span> — {t.result}</li>)}
            </ul>
            {!askedMozilla && (
              <p className="mt-3 text-xs">
                Mozilla keeps a public directory of mail providers' settings.{' '}
                <button className={`${btn} mt-1`} disabled={looking} onClick={() => discover(true)}>
                  {looking ? <Loader2 className="h-3 w-3 animate-spin" /> : null} Ask Mozilla's directory
                </button>
                <span className="mt-1 block text-[11px] text-muted-foreground">This tells Mozilla the domain ({email.slice(email.indexOf('@') + 1)}), not your address.</span>
              </p>
            )}
          </div>
        )}

        {verdict && <VerdictView verdict={verdict} />}
        {error && <p className="text-xs text-destructive">{error}</p>}

        <div className="flex flex-wrap items-center justify-between gap-2 pt-2">
          <button className="text-xs text-muted-foreground underline hover:text-foreground" onClick={manual}>Enter settings manually</button>
          <div className="flex gap-2">
            {verdict && !verdict.imap.ok && verdict.imap.kind !== 'auth' && (
              <button className={btn} disabled={busy} onClick={() => connect(true)}>Save anyway</button>
            )}
            {found && !found.unsupported && (
              <button className={primary} disabled={busy || !password} onClick={() => connect()}>
                {busy ? <Loader2 className="h-3 w-3 animate-spin" /> : <Check className="h-3 w-3" />} Sign in and add
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

const SOURCE: Record<string, string> = {
  builtin: "InboxQL's list of providers",
  mx: "the domain's mail records — it is hosted by this provider",
  autoconfig: "the settings the domain publishes",
  srv: "the domain's DNS service records",
  ispdb: "Mozilla's directory",
};

// --- Sign in with Google --------------------------------------------------------

export const GoogleSignIn = ({ email, accountId, onConnected }: { email: string; accountId?: string; onConnected: () => void }) => {
  const [saved, setSaved] = useState<boolean | null>(null);
  const [redirect, setRedirect] = useState('');
  const [clientId, setClientId] = useState('');
  const [secret, setSecret] = useState('');
  const [alsoCalendar, setAlsoCalendar] = useState(true);
  const [waiting, setWaiting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const poll = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    send<{ savedClient: boolean; redirect: string }>('GET', '/api/accounts/google')
      .then(r => { if (r.ok) { setSaved(r.data.savedClient); setRedirect(r.data.redirect); } }).catch(() => setSaved(false));
    return () => { if (poll.current) clearInterval(poll.current); };
  }, []);

  const start = async () => {
    setError(null);
    const r = await send<{ authUrl: string }>('POST', '/api/accounts/google',
      { clientId: clientId.trim(), clientSecret: secret.trim(), alsoCalendar, accountId: accountId ?? '' }).catch(() => null);
    if (!r?.ok) { setError(r?.text || 'Could not start the sign-in.'); return; }
    window.open(r.data.authUrl, '_blank', 'noopener');
    setWaiting(true);
    const began = Date.now();
    // The sign-in finishes in the other tab; this one notices the account.
    poll.current = setInterval(async () => {
      const a = await send<Account[]>('GET', '/api/accounts').catch(() => null);
      const done = a?.ok && a.data.some(x => x.auth === 'google' && x.email.toLowerCase() === email.toLowerCase() && !x.disconnected);
      if (done || Date.now() - began > 10 * 60_000) {
        if (poll.current) clearInterval(poll.current);
        setWaiting(false);
        if (done) onConnected();
      }
    }, 2500);
  };

  return (
    <div className="border border-primary/30 bg-primary/5 p-4 text-sm">
      <div className="font-semibold">Sign in with Google</div>
      <p className="mt-1 text-xs text-muted-foreground">No app password: Google asks you to allow InboxQL, and this computer keeps the sign-in.</p>
      {saved === false && (
        <div className="mt-3 space-y-2 text-xs">
          <p>
            Google lets an app read mail only through an OAuth client. Use the one from your own Google Cloud project — the same
            kind the calendar uses; <button className="text-primary underline" onClick={() => openHelp('gmail-app-password')}>how to make one</button>.
            Its redirect address is <span className="font-mono">{redirect}</span>.
          </p>
          <input aria-label="Client ID" className={field} placeholder="Client ID — ….apps.googleusercontent.com" value={clientId} onChange={e => setClientId(e.target.value)} />
          <input aria-label="Client secret" type="password" className={field} placeholder="Client secret" value={secret} onChange={e => setSecret(e.target.value)} />
        </div>
      )}
      <label className="mt-3 flex items-center gap-2 text-xs">
        <input type="checkbox" checked={alsoCalendar} onChange={e => setAlsoCalendar(e.target.checked)} /> Connect this Google account's calendar too
      </label>
      {error && <p className="mt-2 text-xs text-destructive">{error}</p>}
      <div className="mt-3">
        {waiting ? (
          <span className="inline-flex items-center gap-2 text-xs"><Loader2 className="h-3 w-3 animate-spin" /> Finish signing in in the Google tab…</span>
        ) : (
          <button className={primary} disabled={saved === null || (saved === false && !clientId.trim())} onClick={start}>
            <LogIn className="h-3 w-3" /> Sign in with Google
          </button>
        )}
      </div>
    </div>
  );
};

// --- the full form: manual entry and Edit -------------------------------------------

export interface Form {
  name: string; email: string; user: string; pass: string; imap: string; port: string; ssl: boolean;
  smtp: string; smtpPort: string; smtpUser: string; smtpPass: string;
}

const AccountForm = ({ account, prefill, onDone }: { account: Account | null; prefill?: Partial<Form>; onDone: () => void }) => {
  const [f, setF] = useState<Form>(() => ({
    name: account?.name ?? '', email: account?.email ?? '', user: account?.user ?? '', pass: '',
    imap: account?.host ?? '', port: String(account?.port ?? 993), ssl: account?.ssl ?? true,
    smtp: account?.smtpHost ?? '', smtpPort: String(account?.smtpPort || 587), smtpUser: account?.smtpUser ?? '', smtpPass: '',
    ...prefill,
  }));
  const [ownSmtpLogin, setOwnSmtpLogin] = useState(!!account?.smtpUser);
  const [verdict, setVerdict] = useState<Verdict | null>(null);
  const [busy, setBusy] = useState<'test' | 'save' | null>(null);
  const [error, setError] = useState<string | null>(null);
  const set = (k: keyof Form) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setF({ ...f, [k]: e.target.type === 'checkbox' ? e.target.checked : e.target.value });
  const google = account?.auth === 'google';

  const payload = () => ({
    id: account?.id, name: f.name.trim() || f.email.trim(), email: f.email.trim(), user: f.user.trim() || f.email.trim(),
    password: f.pass, host: f.imap.trim(), port: parseInt(f.port) || 993, ssl: f.ssl,
    smtpHost: f.smtp.trim(), smtpPort: parseInt(f.smtpPort) || 587,
    smtpUser: ownSmtpLogin ? f.smtpUser.trim() : '', smtpPassword: ownSmtpLogin ? f.smtpPass : '',
  });
  const test = async () => {
    setBusy('test'); setError(null); setVerdict(null);
    const r = await send<Verdict>('POST', '/api/accounts/verify', payload()).catch(() => null);
    setBusy(null);
    if (r?.ok) setVerdict(r.data); else setError(r?.text || 'Could not reach InboxQL.');
  };
  const save = async () => {
    setBusy('save'); setError(null);
    const r = await send<Account>('POST', '/api/accounts', payload()).catch(() => null);
    setBusy(null);
    // The server's reason, said: a taken name, or a changed server that needs
    // the password again. These used to close the form as if it had saved.
    if (!r?.ok) { setError(r?.text || 'Could not save the account.'); return; }
    // A new account, or one just connected again, starts syncing.
    if (!account || account.disconnected) firstSync((r.data as Account | null)?.id);
    onDone();
  };

  return (
    <div className="max-w-2xl animate-in fade-in duration-200">
      <button onClick={onDone} className="mb-4 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"><ArrowLeft className="h-3 w-3" /> Mail Accounts</button>
      <h2 className="text-2xl font-bold">{account ? `Edit ${account.name}` : 'Enter settings manually'}</h2>
      {account?.disconnected && <p className="mt-1 text-sm text-muted-foreground">This account is disconnected. Type its password to connect it again.</p>}

      {google ? (
        <div className="mt-6 space-y-4">
          <p className="text-sm">This account signs in with Google. To sign in again — after a password change, or if Google has revoked access:</p>
          <GoogleSignIn email={account!.email} accountId={account!.id} onConnected={onDone} />
        </div>
      ) : (
        <>
          <section className="mt-6 grid grid-cols-2 gap-4">
            <div><label className={label} htmlFor="f-name">Name</label><input id="f-name" className={field} value={f.name} onChange={set('name')} placeholder="Work" /></div>
            <div><label className={label} htmlFor="f-email">Email address</label><input id="f-email" type="email" className={field} value={f.email} onChange={set('email')} /></div>
          </section>

          <h3 className="mb-2 mt-6 text-xs font-semibold uppercase tracking-wider text-muted-foreground">Incoming — IMAP</h3>
          <section className="grid grid-cols-[1fr_6rem] gap-4">
            <div><label className={label} htmlFor="f-imap">Server</label><input id="f-imap" className={field} value={f.imap} onChange={set('imap')} placeholder="imap.example.com" /></div>
            <div><label className={label} htmlFor="f-port">Port</label><input id="f-port" className={field} value={f.port} onChange={set('port')} inputMode="numeric" /></div>
            <div><label className={label} htmlFor="f-user">Username</label><input id="f-user" className={field} value={f.user} onChange={set('user')} placeholder="usually the address" /></div>
            <div />
            <div>
              <label className={label} htmlFor="f-pass">Password</label>
              <input id="f-pass" type="password" className={field} value={f.pass} onChange={set('pass')}
                placeholder={account && !account.disconnected ? 'Unchanged — type to replace' : ''} />
            </div>
            <div />
            <label className="col-span-2 flex items-start gap-2 text-xs">
              <input type="checkbox" className="mt-0.5" checked={f.ssl} onChange={set('ssl')} />
              <span><span className="font-medium">Encrypted from the start (TLS, usually port 993).</span> Off means the connection is
                upgraded with STARTTLS (usually port 143). A password is never sent unencrypted either way.</span>
            </label>
          </section>

          <h3 className="mb-2 mt-6 text-xs font-semibold uppercase tracking-wider text-muted-foreground">Outgoing — SMTP</h3>
          <section className="grid grid-cols-[1fr_6rem] gap-4">
            <div><label className={label} htmlFor="f-smtp">Server</label><input id="f-smtp" className={field} value={f.smtp} onChange={set('smtp')} placeholder="smtp.example.com — blank: receive only" /></div>
            <div><label className={label} htmlFor="f-smtpport">Port</label><input id="f-smtpport" className={field} value={f.smtpPort} onChange={set('smtpPort')} inputMode="numeric" /></div>
            <p className="col-span-2 -mt-2 text-[11px] text-muted-foreground">465 is encrypted from the start; any other port (usually 587) is upgraded with STARTTLS.</p>
            <label className="col-span-2 flex items-center gap-2 text-xs">
              <input type="checkbox" checked={ownSmtpLogin} onChange={e => setOwnSmtpLogin(e.target.checked)} /> The outgoing server has its own username and password
            </label>
            {ownSmtpLogin && (
              <>
                <div><label className={label} htmlFor="f-smtpuser">SMTP username</label><input id="f-smtpuser" className={field} value={f.smtpUser} onChange={set('smtpUser')} /></div>
                <div />
                <div><label className={label} htmlFor="f-smtppass">SMTP password</label>
                  <input id="f-smtppass" type="password" className={field} value={f.smtpPass} onChange={set('smtpPass')}
                    placeholder={account?.smtpUser ? 'Unchanged — type to replace' : ''} /></div>
              </>
            )}
          </section>

          {verdict && <VerdictView verdict={verdict} />}
          {error && <p className="mt-3 text-xs text-destructive">{error}</p>}
          <div className="mt-6 flex items-center justify-between">
            <button className={btn} disabled={busy !== null || !f.imap.trim()} onClick={test}>
              {busy === 'test' ? <Loader2 className="h-3 w-3 animate-spin" /> : <Server className="h-3 w-3" />} Test sign-in
            </button>
            <div className="flex gap-2">
              <button className={btn} onClick={onDone}>Cancel</button>
              <button className={primary} disabled={busy !== null || !f.imap.trim() || !f.email.trim() || (!account && !f.pass)} onClick={save}>
                {busy === 'save' ? <Loader2 className="h-3 w-3 animate-spin" /> : <Check className="h-3 w-3" />} {account ? 'Save' : 'Add account'}
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  );
};
