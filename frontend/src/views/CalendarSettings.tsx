import { useCallback, useEffect, useState } from 'react';
import { settingAnchor } from '../lib/settingsIndex';
import { CalendarDays, ExternalLink, Loader2, RefreshCw, Trash2 } from 'lucide-react';
import { syncAccount, type CalendarAccount } from './Calendar/api';
import { openDoc } from '../lib/docsNav';

/**
 * Settings → Calendars: the accounts calendars come from, and connecting new
 * ones.
 *
 * Google is signed in to with a client ID from the person's own Google Cloud
 * project. That is a nuisance, and it is the honest arrangement until InboxQL
 * has a verified client of its own: Google asks a published app with calendar
 * access to pass a review, and a client nobody owns cannot pass one.
 */

const post = async (url: string, body?: unknown) => {
  const r = await fetch(url, {
    method: 'POST',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await r.text();
  let data: any = {};
  try { data = text ? JSON.parse(text) : {}; } catch { data = { error: text }; }
  if (!r.ok) throw new Error(data.error ?? text ?? `${r.status}`);
  return data;
};

export const CalendarSettings = () => {
  const [accounts, setAccounts] = useState<CalendarAccount[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);

  const load = useCallback(() => {
    fetch('/api/calendar/accounts', { cache: 'no-store' }).then(r => r.json()).then(d => setAccounts(d.accounts ?? [])).catch(() => {});
  }, []);
  useEffect(load, [load]);

  const sync = async (a: CalendarAccount) => {
    setBusy(a.id); setError(null);
    try { await syncAccount(a.id); setNote(`Synced ${a.name}.`); } catch (e: any) { setError(`${a.name}: ${e.message}`); }
    finally { setBusy(null); load(); }
  };
  const disconnect = async (a: CalendarAccount) => {
    setBusy(a.id); setError(null);
    try {
      const r = await fetch(`/api/calendar/accounts/${encodeURIComponent(a.id)}`, { method: 'DELETE' });
      if (!r.ok) throw new Error(await r.text());
      setNote(`Disconnected ${a.name}. Nothing at ${a.provider === 'google' ? 'Google' : 'the server'} was touched.`);
    } catch (e: any) { setError(e.message); } finally { setBusy(null); load(); }
  };
  const rescan = async () => {
    try { await post('/api/calendar/invitations/rescan'); setNote('Reading invitations from all your mail again, in the background.'); }
    catch (e: any) { setError(e.message); }
  };

  return (
    <div className="animate-in fade-in duration-300 space-y-6">
      <div>
        <h2 className="text-2xl font-bold text-foreground mb-1">Calendars</h2>
        <p className="text-muted-foreground text-sm">
          Where your calendars come from. Invitations in your mail are read whether or not anything is connected.{' '}
          <button className="text-primary hover:underline" onClick={() => openDoc('calendar', 'connecting-google-calendar')}>How to connect</button>
        </p>
      </div>

      {(error || note) && (
        <p className={`text-xs ${error ? 'text-destructive' : 'text-emerald-700 dark:text-emerald-400'}`}>{error ?? note}</p>
      )}

      <section className="border border-border bg-card/40 p-4" {...settingAnchor('calendars-connected')}>
        <h3 className="mb-3 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          <CalendarDays className="h-3.5 w-3.5" /> Connected
        </h3>
        {accounts.length === 0 ? (
          <p className="text-xs text-muted-foreground">Nothing connected yet. Your own calendar and your invitations work without an account.</p>
        ) : (
          <ul className="divide-y divide-border/50 text-sm">
            {accounts.map(a => (
              <li key={a.id} className="flex flex-wrap items-center gap-3 py-2">
                <span className="w-20 shrink-0 text-xs text-muted-foreground">{a.provider === 'google' ? 'Google' : a.provider === 'ews' ? 'Exchange' : a.provider}</span>
                <span className="flex-1 min-w-[12rem]">
                  <span className="font-medium">{a.name}</span>
                  <span className={`block text-[11px] ${a.status === 'error' ? 'text-destructive' : 'text-muted-foreground'}`}>
                    {a.status === 'error' ? a.lastError : a.syncedAt ? `Synced ${new Date(a.syncedAt).toLocaleString()}` : 'Not synced yet'}
                    {a.status === 'ok' && a.lastError ? ` · ${a.lastError}` : ''}
                  </span>
                </span>
                <button disabled={busy === a.id} onClick={() => sync(a)} className="inline-flex items-center gap-1 border border-border px-2 py-1 text-xs hover:bg-accent disabled:opacity-50">
                  {busy === a.id ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />} Sync now
                </button>
                <button disabled={busy === a.id} onClick={() => disconnect(a)} className="inline-flex items-center gap-1 border border-border px-2 py-1 text-xs hover:bg-accent disabled:opacity-50" title="Removes the account and its calendars from InboxQL only">
                  <Trash2 className="h-3 w-3" /> Disconnect
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <ConnectGoogle onConnected={load} />
      <ConnectExchange onConnected={load} />

      <section className="border border-border bg-card/40 p-4 text-xs" {...settingAnchor('calendar-invitations')}>
        <h3 className="mb-1 font-semibold uppercase tracking-wider text-muted-foreground">Invitations in mail</h3>
        <p className="mb-2 text-muted-foreground">
          Every message carrying an invitation is read into the Invitations calendar as it arrives — updates, cancellations
          and replies included. An invitation also in a connected calendar shows once, from there.
        </p>
        <button onClick={rescan} className="border border-border px-3 py-1 hover:bg-accent">Read all my mail for invitations again</button>
      </section>
    </div>
  );
};

const ConnectGoogle = ({ onConnected }: { onConnected: () => void }) => {
  const [open, setOpen] = useState(false);
  const [clientId, setClientId] = useState('');
  const [secret, setSecret] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [waiting, setWaiting] = useState(false);

  const start = async () => {
    setError(null);
    try {
      const r = await post('/api/calendar/accounts/google/start', { clientId: clientId.trim(), clientSecret: secret.trim() });
      window.open(r.authUrl, '_blank', 'noopener');
      setWaiting(true);
      // The sign-in finishes in the other tab; watch for the account to appear.
      const before = await fetch('/api/calendar/accounts').then(x => x.json()).then(d => (d.accounts ?? []).length);
      const t0 = Date.now();
      const tick = async () => {
        const now = await fetch('/api/calendar/accounts').then(x => x.json()).then(d => (d.accounts ?? []).length).catch(() => before);
        if (now > before) { setWaiting(false); setOpen(false); onConnected(); return; }
        if (Date.now() - t0 < 10 * 60_000) setTimeout(tick, 2000); else setWaiting(false);
      };
      setTimeout(tick, 2000);
    } catch (e: any) { setError(e.message); }
  };

  const field = 'w-full bg-background border border-border px-2 py-1.5 text-xs font-mono';
  return (
    <section className="border border-border bg-card/40 p-4 text-xs" {...settingAnchor('google-calendar')}>
      <div className="flex items-center justify-between">
        <h3 className="font-semibold uppercase tracking-wider text-muted-foreground">Google Calendar</h3>
        {!open && <button onClick={() => setOpen(true)} className="bg-primary px-3 py-1 font-semibold text-primary-foreground hover:opacity-90">Connect Google</button>}
      </div>
      {open && (
        <div className="mt-3 space-y-3">
          <ol className="list-decimal space-y-1 pl-4 text-muted-foreground">
            <li>In the <a className="text-primary hover:underline" href="https://console.cloud.google.com/apis/library/calendar-json.googleapis.com" target="_blank" rel="noreferrer">Google Cloud console <ExternalLink className="inline h-2.5 w-2.5" /></a>, create a project and enable the <strong>Google Calendar API</strong>.</li>
            <li>Under <strong>OAuth consent screen</strong>, choose External, and add your own address as a test user.</li>
            <li>Under <strong>Credentials</strong>, create an <strong>OAuth client ID</strong> of type <strong>Desktop app</strong>.</li>
            <li>Paste its ID and secret here, then sign in.</li>
          </ol>
          <input className={field} placeholder="1234-abcd.apps.googleusercontent.com" value={clientId} onChange={e => setClientId(e.target.value)} aria-label="Client ID" />
          <input className={field} placeholder="Client secret" value={secret} onChange={e => setSecret(e.target.value)} aria-label="Client secret" type="password" />
          <p className="text-[11px] text-muted-foreground">
            Google signs you in in a new tab and sends you back to this computer. InboxQL keeps a refresh token, encrypted,
            and talks to Google directly; nothing goes through anyone else.
          </p>
          {error && <p className="text-destructive">{error}</p>}
          <div className="flex items-center gap-2">
            <button disabled={!clientId.trim() || waiting} onClick={start} className="bg-primary px-3 py-1 font-semibold text-primary-foreground hover:opacity-90 disabled:opacity-50">
              Sign in with Google
            </button>
            <button onClick={() => setOpen(false)} className="border border-border px-3 py-1 hover:bg-accent">Cancel</button>
            {waiting && <span className="inline-flex items-center gap-1 text-muted-foreground"><Loader2 className="h-3 w-3 animate-spin" /> Waiting for you to finish in the other tab…</span>}
          </div>
        </div>
      )}
    </section>
  );
};

const ConnectExchange = ({ onConnected }: { onConnected: () => void }) => {
  const [open, setOpen] = useState(false);
  const [url, setUrl] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [email, setEmail] = useState('');
  const [auth, setAuth] = useState('ntlm');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const connect = async () => {
    setBusy(true); setError(null);
    try {
      await post('/api/calendar/accounts/ews', { url: url.trim(), username: username.trim(), password, email: email.trim(), auth });
      setOpen(false); setPassword(''); onConnected();
    } catch (e: any) { setError(e.message); } finally { setBusy(false); }
  };

  const field = 'w-full bg-background border border-border px-2 py-1.5 text-xs';
  return (
    <section className="border border-border bg-card/40 p-4 text-xs" {...settingAnchor('exchange-server')}>
      <div className="flex items-center justify-between">
        <h3 className="font-semibold uppercase tracking-wider text-muted-foreground">Exchange Server</h3>
        {!open && <button onClick={() => setOpen(true)} className="border border-border px-3 py-1 font-semibold hover:bg-accent">Connect Exchange</button>}
      </div>
      {open && (
        <div className="mt-3 space-y-3">
          <p className="text-muted-foreground">
            For an Exchange Server your organisation runs itself. Exchange Online (Microsoft 365) is switching EWS off and is not
            supported this way.
          </p>
          <input className={`${field} font-mono`} placeholder="https://mail.example.com/EWS/Exchange.asmx" value={url} onChange={e => setUrl(e.target.value)} aria-label="EWS address" />
          <div className="grid grid-cols-2 gap-2">
            <input className={field} placeholder="DOMAIN\\user or user@example.com" value={username} onChange={e => setUsername(e.target.value)} aria-label="Username" />
            <input className={field} placeholder="Password" type="password" value={password} onChange={e => setPassword(e.target.value)} aria-label="Password" />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <input className={field} placeholder="Your address, e.g. me@example.com" value={email} onChange={e => setEmail(e.target.value)} aria-label="Email address" />
            <select className={field} value={auth} onChange={e => setAuth(e.target.value)} aria-label="Sign-in method">
              <option value="ntlm">Windows sign-in (NTLM)</option>
              <option value="basic">Basic sign-in</option>
            </select>
          </div>
          {error && <p className="text-destructive">{error}</p>}
          <div className="flex gap-2">
            <button disabled={busy || !url || !username || !password} onClick={connect} className="bg-primary px-3 py-1 font-semibold text-primary-foreground hover:opacity-90 disabled:opacity-50">
              {busy ? <Loader2 className="h-3 w-3 animate-spin" /> : 'Test and connect'}
            </button>
            <button onClick={() => setOpen(false)} className="border border-border px-3 py-1 hover:bg-accent">Cancel</button>
          </div>
        </div>
      )}
    </section>
  );
};
