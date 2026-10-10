import { useCallback, useEffect, useState } from 'react';
import {
  AlertTriangle, BatteryCharging, Check, Copy, Cpu, Download, FolderOpen, Globe, Loader2,
  Power, RefreshCw, RotateCw, Server, Settings2,
} from 'lucide-react';
import { expectRestart, useRestartExpectation, clearRestartExpectation } from '../lib/devReload';
import { openDoc } from '../lib/docsNav';

/**
 * Everything about the machine this mailbox is served from: where the mail
 * is, how the server runs, what the settings file says, and the two verbs
 * that act on the server itself — restart and update.
 *
 * # Why the settings file is edited here rather than in its own section
 *
 * Every field in ~/.iql/settings.json is a fact about how this server runs,
 * and most of them only take effect when it restarts. Putting the editor
 * beside the state it changes, and the restart that applies it, is what
 * makes "saved, restart to apply" legible instead of mysterious.
 *
 * # Why the mailbox is not a text field
 *
 * A mistyped path is not a setting; it is a different, empty mailbox. So
 * changing it is an action that checks the target first, and creates one
 * only when asked.
 */

interface Pending {
  field: string;
  running: string;
  saved: string;
  applies: boolean;
  overriddenBy?: string;
}

interface SystemState {
  version: { version: string; revision: string; go: string; platform: string; binary: string; homebrew: boolean };
  run: { mode: 'service' | 'foreground' | 'dev'; pid: number; started: string; uptimeSeconds: number; instanceId: string; canRestart: boolean };
  mailbox: { dataDir: string; source: string; why: string; schema: number; dbBytes?: number; lastBackup?: { name: string; at: string; bytes: number } };
  address: { addr: string; url: string; auth: { passwordless: boolean; reason: string }; hostname?: string; hostnameResolves?: boolean };
  settings: { path: string; exists: boolean; home: string; problem?: string; pendingRestart?: Pending[] };
  models: { installed: string[]; laya: string; gliner: string };
  service: { supported: boolean; platform?: string; installed?: boolean; running?: boolean; starting?: boolean; detail?: string; file?: string };
  power: { source: string; heavyWorkOnBattery: boolean; deferring: boolean };
  ai: { provider: string; model?: string; endpoint?: string; remote: boolean };
}

interface MachineSettings {
  dataDir: string;
  addr?: string;
  models?: string;
  hostname?: string;
  heavyWorkOnBattery?: boolean;
}

interface SettingsResponse {
  path: string;
  exists: boolean;
  problem?: string;
  settings?: MachineSettings;
  defaults: MachineSettings;
  pendingRestart?: Pending[];
  hostsCommand?: string;
  warnings?: string[];
  fields: { name: string; applies: 'now' | 'restart'; help: string }[];
}

interface UpdateCheck {
  current: string;
  latest?: string;
  newer?: boolean;
  url?: string;
  error?: string;
  homebrew: boolean;
  checkedAt: string;
}

const MODE_WORDS: Record<string, { title: string; about: string }> = {
  service: {
    title: 'Login service',
    about: 'Started by the operating system when you log in, and stopped when you log out. A restart goes through the service manager.',
  },
  foreground: {
    title: 'In a terminal',
    about: 'Started by hand with iql start. It stops when that terminal closes; a restart keeps it in the same terminal.',
  },
  dev: {
    title: 'Development (--dev)',
    about: 'Run under the --dev supervisor, which restarts it whenever the binary is rebuilt.',
  },
};

const FIELD_LABEL: Record<string, string> = {
  dataDir: 'Mailbox',
  addr: 'Listen address',
  models: 'Models folder',
  hostname: 'Site name',
  heavyWorkOnBattery: 'Heavy work on battery',
};

export const formatBytes = (n?: number) => {
  if (!n) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / Math.pow(1024, i)).toFixed(i ? 1 : 0)} ${units[i]}`;
};

export const formatUptime = (s: number) => {
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)} min`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min`;
  return `${Math.floor(s / 86400)} d ${Math.floor((s % 86400) / 3600)} h`;
};

const sendJSON = async (method: string, url: string, body?: unknown) => {
  const r = await fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await r.text();
  let data: any = {};
  try { data = text ? JSON.parse(text) : {}; } catch { data = { error: text }; }
  return { ok: r.ok, status: r.status, data };
};

const errorText = (data: any) => data?.message ?? data?.error ?? 'something went wrong';

const CopyButton = ({ text }: { text: string }) => {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      title="Copy"
      aria-label={`Copy ${text}`}
      className="shrink-0 p-1 text-muted-foreground hover:text-foreground"
      onClick={async () => {
        try { await navigator.clipboard.writeText(text); setDone(true); setTimeout(() => setDone(false), 1200); } catch { /* not allowed */ }
      }}
    >
      {done ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
    </button>
  );
};

const Row = ({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) => (
  <div className="grid grid-cols-[9rem_1fr] gap-3 py-1.5 border-b border-border/50 last:border-0 text-xs">
    <dt className="text-muted-foreground">{label}</dt>
    <dd className={`min-w-0 break-words ${mono ? 'font-mono' : ''}`}>{children}</dd>
  </div>
);

const Card = ({ icon: Icon, title, children, aside }: { icon: any; title: string; children: React.ReactNode; aside?: React.ReactNode }) => (
  <section className="border border-border bg-card/40 p-4">
    <h3 className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
      <Icon className="h-3.5 w-3.5" /> {title}
      {aside && <span className="ml-auto normal-case tracking-normal font-normal">{aside}</span>}
    </h3>
    <dl>{children}</dl>
  </section>
);

const Command = ({ text }: { text: string }) => (
  <span className="inline-flex items-center gap-1 bg-muted/60 px-1.5 py-0.5 font-mono text-[11px]">
    {text} <CopyButton text={text} />
  </span>
);

export const SystemPanel = () => {
  const [state, setState] = useState<SystemState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const restart = useRestartExpectation();

  const load = useCallback(() => {
    fetch('/api/system', { cache: 'no-store' })
      .then(r => (r.ok ? r.json() : Promise.reject(new Error(`${r.status}`))))
      .then(setState)
      .catch(() => setError('could not read the system state'));
  }, []);
  useEffect(load, [load]);

  const doRestart = async () => {
    if (!state) return;
    const { ok, data } = await sendJSON('POST', '/api/system/restart');
    if (!ok) { setError(errorText(data)); return; }
    expectRestart('restart', state.run.instanceId);
  };

  if (!state) {
    return (
      <div>
        <h2 className="text-2xl font-bold text-foreground mb-2">System</h2>
        <p className="text-xs text-muted-foreground">{error ?? <Loader2 className="inline h-3.5 w-3.5 animate-spin" />}</p>
      </div>
    );
  }

  const mode = MODE_WORDS[state.run.mode] ?? MODE_WORDS.foreground;
  const pending = (state.settings.pendingRestart ?? []);

  return (
    <div className="animate-in fade-in duration-300 space-y-5">
      <div>
        <h2 className="text-2xl font-bold text-foreground mb-1">System</h2>
        <p className="text-muted-foreground text-sm">
          Where your mail is, how this server is running, and the machine settings that decide both.{' '}
          <button className="text-primary hover:underline" onClick={() => openDoc('settings', 'system')}>What each part means</button>
        </p>
      </div>

      {restart && <RestartNotice />}
      {error && <p className="text-xs text-destructive">{error}</p>}

      {pending.length > 0 && (
        <PendingBanner pending={pending} canRestart={state.run.canRestart} onRestart={doRestart} />
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <Card icon={Server} title="This server" aside={<span className="font-mono">v{state.version.version}</span>}>
          <Row label="Running as">
            <span className="font-medium">{mode.title}</span>
            <p className="text-[11px] text-muted-foreground mt-0.5">{mode.about}</p>
          </Row>
          <Row label="Up for">{formatUptime(state.run.uptimeSeconds)} <span className="text-muted-foreground">· process {state.run.pid}</span></Row>
          <Row label="Program" mono>
            <span className="inline-flex items-start gap-1">{state.version.binary || '—'} {state.version.binary && <CopyButton text={state.version.binary} />}</span>
          </Row>
          <Row label="Built">
            {state.version.platform} · {state.version.go}
            {state.version.revision && <span className="text-muted-foreground font-mono"> · {state.version.revision.slice(0, 10)}</span>}
          </Row>
          <div className="pt-3">
            <button
              type="button"
              disabled={!state.run.canRestart || !!restart}
              onClick={doRestart}
              className="inline-flex items-center gap-1.5 border border-border px-3 py-1 text-xs hover:bg-accent disabled:opacity-50"
            >
              <RotateCw className="h-3 w-3" /> Restart server
            </button>
            <span className="ml-2 text-[11px] text-muted-foreground">This page reconnects by itself.</span>
          </div>
        </Card>

        <Card icon={FolderOpen} title="Mailbox">
          <Row label="Folder" mono>
            <span className="inline-flex items-start gap-1">{state.mailbox.dataDir} <CopyButton text={state.mailbox.dataDir} /></span>
          </Row>
          <Row label="Because">{state.mailbox.why || state.mailbox.source}</Row>
          <Row label="Database">{formatBytes(state.mailbox.dbBytes)} <span className="text-muted-foreground">· schema v{state.mailbox.schema}</span></Row>
          <Row label="Last backup">
            {state.mailbox.lastBackup
              ? <>{new Date(state.mailbox.lastBackup.at).toLocaleString()} <span className="text-muted-foreground">· {formatBytes(state.mailbox.lastBackup.bytes)}</span></>
              : <span className="text-muted-foreground">none in the backups folder</span>}
          </Row>
          {state.mailbox.source === 'local' && (
            <p className="mt-2 text-[11px] text-amber-600 dark:text-amber-400">
              There are no machine settings, so this is whichever ./data folder the server was started in.
              Create settings below to make it this machine's mailbox wherever InboxQL is started from.
            </p>
          )}
        </Card>

        <Card icon={Globe} title="Address">
          <Row label="Open at"><a className="text-primary hover:underline" href={state.address.url}>{state.address.url}</a></Row>
          <Row label="Listening on" mono>{state.address.addr}</Row>
          <Row label="Sign-in">{state.address.auth.reason}</Row>
          {state.address.hostname && (
            <Row label="Site name">
              <span className="font-mono">{state.address.hostname}</span>{' '}
              {state.address.hostnameResolves
                ? <span className="text-emerald-600 dark:text-emerald-400">· in the hosts file</span>
                : <span className="text-amber-600 dark:text-amber-400">· not in the hosts file yet</span>}
            </Row>
          )}
        </Card>

        <Card icon={Power} title="Login service">
          {!state.service.supported ? (
            <p className="text-xs text-muted-foreground">{state.service.detail}</p>
          ) : (
            <>
              <Row label="Installed">{state.service.installed ? `yes (${state.service.platform})` : 'no'}</Row>
              {state.service.installed && (
                <Row label="State">{state.service.running ? 'running' : state.service.starting ? 'starting' : 'not running'}</Row>
              )}
              {state.service.file && state.service.installed && <Row label="Definition" mono>{state.service.file}</Row>}
              {!state.service.installed && (
                <p className="mt-2 text-[11px] text-muted-foreground">
                  To start InboxQL when you log in, run <Command text="iql service install" /> in a terminal.
                </p>
              )}
              {state.service.installed && state.run.mode !== 'service' && state.service.running && (
                <p className="mt-2 text-[11px] text-muted-foreground">
                  The service is running too, but this page is served by a server started another way.
                </p>
              )}
            </>
          )}
        </Card>

        <Card icon={BatteryCharging} title="Power">
          <Row label="Running on">{state.power.source === 'unknown' ? 'unknown (treated as mains)' : state.power.source}</Row>
          <Row label="Background work">
            {state.power.deferring
              ? <span className="text-amber-600 dark:text-amber-400">waiting for mains power</span>
              : state.power.heavyWorkOnBattery ? 'allowed on battery' : 'runs on mains power'}
          </Row>
        </Card>

        <Card icon={Cpu} title="AI and models">
          <Row label="Provider">
            {state.ai.provider
              ? <>{state.ai.provider}{state.ai.model && <span className="text-muted-foreground"> · {state.ai.model}</span>}</>
              : <span className="text-muted-foreground">none — nothing leaves this machine</span>}
          </Row>
          {state.ai.provider && (
            <Row label="Where">
              {state.ai.remote
                ? <span className="text-amber-600 dark:text-amber-400">another machine — {state.ai.endpoint}. Mail you analyse is sent there.</span>
                : 'this machine'}
            </Row>
          )}
          <Row label="Installed">{state.models.installed.length ? state.models.installed.join(', ') : <span className="text-muted-foreground">no local models</span>}</Row>
          <Row label="Kept in" mono>{state.models.laya.replace(/[/\\]laya$/, '')}</Row>
        </Card>
      </div>

      <UpdateSection state={state} />

      <MachineSettingsEditor
        onChanged={load}
        canRestart={state.run.canRestart}
        onRestart={doRestart}
        mode={state.run.mode}
      />
    </div>
  );
};

const RestartNotice = () => {
  const r = useRestartExpectation();
  if (!r) return null;
  if (r.gaveUp) {
    return (
      <div className="border border-destructive/40 bg-destructive/5 p-3 text-xs">
        <p className="font-semibold text-destructive mb-1">The server has not come back.</p>
        <p className="text-muted-foreground">
          In a terminal, <Command text="iql service status" /> says whether the service is running, and the end of{' '}
          <span className="font-mono">~/.iql/logs/service.log</span> says why it stopped.{' '}
          <button className="underline" onClick={() => window.location.reload()}>Reload</button> ·{' '}
          <button className="underline" onClick={clearRestartExpectation}>Dismiss</button>
        </p>
      </div>
    );
  }
  return (
    <div className="flex items-center gap-2 border border-primary/40 bg-primary/5 p-3 text-xs">
      <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" />
      {r.reason === 'update'
        ? 'Updating. The page reloads onto the new version when the server is back.'
        : 'Restarting. The page reloads when the server is back.'}
    </div>
  );
};

const PendingBanner = ({ pending, canRestart, onRestart }: { pending: Pending[]; canRestart: boolean; onRestart: () => void }) => {
  const applying = pending.filter(p => p.applies);
  const blocked = pending.filter(p => !p.applies);
  return (
    <div className="border border-amber-500/40 bg-amber-500/5 p-3 text-xs space-y-2">
      {applying.length > 0 && (
        <div className="flex items-start gap-3">
          <AlertTriangle className="h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" />
          <div className="flex-1">
            <p className="font-semibold">Saved, and waiting for a restart</p>
            <ul className="mt-1 space-y-0.5 text-muted-foreground">
              {applying.map(p => (
                <li key={p.field}>
                  {FIELD_LABEL[p.field] ?? p.field}: <span className="font-mono">{p.running || '(none)'}</span> → <span className="font-mono text-foreground">{p.saved}</span>
                </li>
              ))}
            </ul>
          </div>
          {canRestart && (
            <button onClick={onRestart} className="shrink-0 bg-primary text-primary-foreground px-3 py-1 font-semibold hover:opacity-90">
              Restart now
            </button>
          )}
        </div>
      )}
      {blocked.map(p => (
        <p key={p.field} className="text-muted-foreground">
          {FIELD_LABEL[p.field] ?? p.field} is saved as <span className="font-mono">{p.saved}</span>, but this server was
          started with <span className="font-mono">{p.overriddenBy}</span>, which outranks the settings file — a restart
          keeps <span className="font-mono">{p.running}</span>.
        </p>
      ))}
    </div>
  );
};

const UpdateSection = ({ state }: { state: SystemState }) => {
  const [check, setCheck] = useState<UpdateCheck | null>(null);
  const [checking, setChecking] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [log, setLog] = useState<{ running: boolean; output?: string; error?: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const restart = useRestartExpectation();

  const runCheck = useCallback(async (refresh: boolean) => {
    setChecking(true);
    try {
      const r = await fetch(`/api/system/update${refresh ? '?refresh=1' : ''}`, { cache: 'no-store' });
      setCheck(await r.json());
    } catch {
      setError('could not ask for the newest release');
    } finally {
      setChecking(false);
    }
  }, []);
  useEffect(() => { runCheck(false); }, [runCheck]);

  // While an update runs, show what it is doing. The server may go away
  // part-way — under the service manager the updater stops it — and that is
  // the expected end of this loop, not an error.
  useEffect(() => {
    if (!log?.running) return;
    const t = setInterval(async () => {
      try {
        const r = await fetch('/api/system/update/log', { cache: 'no-store' });
        if (r.ok) setLog(await r.json());
      } catch { /* restarting */ }
    }, 1000);
    return () => clearInterval(t);
  }, [log?.running]);

  const start = async () => {
    setConfirming(false);
    setError(null);
    const { ok, data } = await sendJSON('POST', '/api/system/update');
    if (!ok) { setError(errorText(data)); return; }
    setLog({ running: true });
    expectRestart('update', state.run.instanceId);
  };

  return (
    <section className="border border-border bg-card/40 p-4">
      <h3 className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
        <Download className="h-3.5 w-3.5" /> Updates
      </h3>
      <div className="flex flex-wrap items-center gap-3 text-xs">
        <span>This is <span className="font-mono">v{state.version.version}</span>.</span>
        {check?.error && <span className="text-destructive">Could not check: {check.error}</span>}
        {check && !check.error && check.latest && (
          check.newer
            ? <span className="font-semibold text-primary">v{check.latest} is available.</span>
            : <span className="text-muted-foreground">It is the newest release.</span>
        )}
        <button
          type="button"
          disabled={checking}
          onClick={() => runCheck(true)}
          className="inline-flex items-center gap-1.5 border border-border px-3 py-1 hover:bg-accent disabled:opacity-50"
        >
          <RefreshCw className={`h-3 w-3 ${checking ? 'animate-spin' : ''}`} /> Check now
        </button>
        {check?.newer && !check.homebrew && !confirming && !log?.running && (
          <button
            type="button"
            disabled={!!restart}
            onClick={() => setConfirming(true)}
            className="inline-flex items-center gap-1.5 bg-primary text-primary-foreground px-3 py-1 font-semibold hover:opacity-90 disabled:opacity-50"
          >
            Update to v{check.latest}
          </button>
        )}
        {check?.url && check.newer && <a className="text-primary hover:underline" href={check.url} target="_blank" rel="noreferrer">What changed</a>}
      </div>
      {check?.checkedAt && !check.checkedAt.startsWith('0001') && (
        <p className="mt-1 text-[11px] text-muted-foreground">Checked {new Date(check.checkedAt).toLocaleString()}.</p>
      )}
      {check?.homebrew && (
        <p className="mt-2 text-[11px] text-muted-foreground">
          Installed with Homebrew, so Homebrew updates it: <Command text="brew upgrade inboxql" />
        </p>
      )}
      {confirming && (
        <div className="mt-3 border border-border bg-background p-3 text-xs">
          <p className="mb-2">
            Updating downloads v{check?.latest}, checks it against the release's published checksums,{' '}
            <strong>backs up your mailbox</strong>, replaces the program and restarts the server. Your mailbox
            is upgraded when the new version first opens it. It takes about a minute.
          </p>
          <div className="flex gap-2">
            <button onClick={start} className="bg-primary text-primary-foreground px-3 py-1 font-semibold hover:opacity-90">Update now</button>
            <button onClick={() => setConfirming(false)} className="border border-border px-3 py-1 hover:bg-accent">Cancel</button>
          </div>
        </div>
      )}
      {log && (log.output || log.error) && (
        <pre className="mt-3 max-h-48 overflow-auto bg-muted/40 p-2 font-mono text-[11px] whitespace-pre-wrap">
          {log.output}{log.error && `\n${log.error}`}
        </pre>
      )}
      {error && <p className="mt-2 text-xs text-destructive">{error}</p>}
      {state.version.platform.startsWith('darwin') && (
        <p className="mt-2 text-[11px] text-muted-foreground">
          If you import from Apple Mail, grant Full Disk Access to the program again after an update — macOS ties the grant to the exact file.
        </p>
      )}
    </section>
  );
};

const MachineSettingsEditor = ({ onChanged, canRestart, onRestart, mode }: {
  onChanged: () => void; canRestart: boolean; onRestart: () => void; mode: string;
}) => {
  const [resp, setResp] = useState<SettingsResponse | null>(null);
  const [draft, setDraft] = useState<MachineSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmPublic, setConfirmPublic] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const accept = (r: SettingsResponse) => {
    setResp(r);
    setDraft(r.settings ? { ...r.settings } : null);
  };

  const load = useCallback(() => {
    fetch('/api/system/settings', { cache: 'no-store' })
      .then(r => r.json())
      .then(accept)
      .catch(() => setError('could not read the machine settings'));
  }, []);
  useEffect(load, [load]);

  const save = async (edit: Record<string, unknown>) => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const { ok, status, data } = await sendJSON('PUT', '/api/system/settings', edit);
      if (status === 409 && data?.confirmation === 'public-address') {
        setConfirmPublic(data.message);
        return;
      }
      if (!ok) { setError(errorText(data)); return; }
      setConfirmPublic(null);
      accept(data);
      if (data.warnings?.length) setNotice(data.warnings.join(' '));
      onChanged();
    } finally {
      setBusy(false);
    }
  };

  const create = async () => {
    setBusy(true);
    const { ok, data } = await sendJSON('POST', '/api/system/settings/create');
    setBusy(false);
    if (!ok) { setError(errorText(data)); return; }
    accept(data);
    onChanged();
  };

  if (!resp) return null;

  const fieldInfo = (name: string) => resp.fields.find(f => f.name === name);
  const Tag = ({ name }: { name: string }) => {
    const applies = fieldInfo(name)?.applies;
    return (
      <span className={`ml-2 px-1.5 py-0.5 text-[10px] uppercase tracking-wider ${applies === 'now' ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400' : 'bg-muted text-muted-foreground'}`}>
        {applies === 'now' ? 'applies now' : 'after restart'}
      </span>
    );
  };

  return (
    <section className="border border-border bg-card/40 p-4">
      <h3 className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-1">
        <Settings2 className="h-3.5 w-3.5" /> Machine settings {busy && <Loader2 className="h-3 w-3 animate-spin text-primary" />}
      </h3>
      <p className="text-[11px] text-muted-foreground mb-4">
        Stored in <span className="font-mono">{resp.path}</span> <CopyButton text={resp.path} /> and read by every{' '}
        <span className="font-mono">iql</span> command and the login service on this computer. Everything else —
        accounts, annotators, logging — lives inside the mailbox and moves with it.
      </p>

      {resp.problem && (
        <p className="mb-3 text-xs text-destructive">{resp.problem} — fix the file by hand; it is not overwritten from here.</p>
      )}

      {!resp.exists && (
        <div className="text-xs space-y-2">
          <p>
            There are no machine settings, so InboxQL uses whichever <span className="font-mono">./data</span> folder it is
            started in. Creating them records this mailbox and this address as the machine's, so nothing changes now —
            but every command and the login service will use them from here on.
          </p>
          <button onClick={create} disabled={busy} className="bg-primary text-primary-foreground px-3 py-1 font-semibold hover:opacity-90 disabled:opacity-50">
            Create settings for this mailbox
          </button>
        </div>
      )}

      {resp.exists && draft && (
        <div className="space-y-5 text-xs">
          <MailboxSwitch current={draft.dataDir} onDone={r => { accept(r); onChanged(); }} />

          <TextSetting
            label="Listen address" name="addr" tag={<Tag name="addr" />} help={fieldInfo('addr')?.help}
            value={draft.addr ?? ''} saved={resp.settings?.addr ?? ''} placeholder={resp.defaults.addr}
            onChange={v => setDraft({ ...draft, addr: v })}
            onSave={() => save({ addr: draft.addr ?? '' })}
          />
          {confirmPublic && (
            <div className="border border-destructive/40 bg-destructive/5 p-3">
              <p className="mb-2">{confirmPublic}</p>
              <div className="flex gap-2">
                <button onClick={() => save({ addr: draft.addr ?? '', confirmPublic: true })} className="bg-destructive text-destructive-foreground px-3 py-1 font-semibold">
                  Listen beyond this machine
                </button>
                <button onClick={() => { setConfirmPublic(null); setDraft({ ...draft, addr: resp.settings?.addr }); }} className="border border-border px-3 py-1 hover:bg-accent">
                  Keep it local
                </button>
              </div>
            </div>
          )}

          <TextSetting
            label="Models folder" name="models" tag={<Tag name="models" />} help={fieldInfo('models')?.help}
            value={draft.models ?? ''} saved={resp.settings?.models ?? ''} placeholder={resp.defaults.models}
            onChange={v => setDraft({ ...draft, models: v })}
            onSave={() => save({ models: draft.models ?? '' })}
          />

          <div>
            <TextSetting
              label="Site name" name="hostname" tag={<Tag name="hostname" />} help={fieldInfo('hostname')?.help}
              value={draft.hostname ?? ''} saved={resp.settings?.hostname ?? ''} placeholder="inboxql.localhost"
              onChange={v => setDraft({ ...draft, hostname: v })}
              onSave={() => save({ hostname: draft.hostname ?? '' })}
            />
            {resp.hostsCommand && (
              <p className="mt-1.5 text-[11px] text-muted-foreground">
                Then, in a terminal: <Command text={resp.hostsCommand.split('   ')[0]} />
                {resp.hostsCommand.includes('   ') && <> {resp.hostsCommand.split('   ')[1]}</>}
              </p>
            )}
          </div>

          <label className="flex items-start gap-2.5 cursor-pointer">
            <input
              type="checkbox" className="mt-0.5"
              checked={!!draft.heavyWorkOnBattery}
              onChange={e => { setDraft({ ...draft, heavyWorkOnBattery: e.target.checked }); save({ heavyWorkOnBattery: e.target.checked }); }}
            />
            <span>
              <span className="font-medium">{FIELD_LABEL.heavyWorkOnBattery}</span><Tag name="heavyWorkOnBattery" />
              <span className="block text-muted-foreground mt-0.5">{fieldInfo('heavyWorkOnBattery')?.help}</span>
            </span>
          </label>

          {notice && <p className="text-amber-600 dark:text-amber-400">{notice}</p>}
          {mode === 'foreground' && (
            <p className="text-[11px] text-muted-foreground">
              Flags typed after <span className="font-mono">iql start</span> outrank this file. A restart from here
              re-runs the same command, so they keep winning.
            </p>
          )}
          {!canRestart && <p className="text-[11px] text-muted-foreground">Restart the server yourself to apply changes marked “after restart”.</p>}
          {canRestart && (resp.pendingRestart ?? []).some(p => p.applies) && (
            <button onClick={onRestart} className="inline-flex items-center gap-1.5 border border-border px-3 py-1 hover:bg-accent">
              <RotateCw className="h-3 w-3" /> Restart to apply
            </button>
          )}
        </div>
      )}
      {error && <p className="mt-3 text-xs text-destructive">{error}</p>}
    </section>
  );
};

const TextSetting = ({ label, name, tag, help, value, saved, placeholder, onChange, onSave }: {
  label: string; name: string; tag: React.ReactNode; help?: string; value: string; saved: string;
  placeholder?: string; onChange: (v: string) => void; onSave: () => void;
}) => {
  const dirty = value !== saved;
  return (
    <div>
      <label htmlFor={`machine-${name}`} className="font-medium">{label}</label>{tag}
      <div className="mt-1 flex gap-2">
        <input
          id={`machine-${name}`}
          className="flex-1 bg-background border border-border px-2 py-1 font-mono text-xs"
          value={value}
          placeholder={placeholder}
          onChange={e => onChange(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter' && dirty) onSave(); }}
        />
        {dirty && (
          <>
            <button onClick={onSave} className="bg-primary text-primary-foreground px-3 py-1 font-semibold hover:opacity-90">Save</button>
            <button onClick={() => onChange(saved)} className="border border-border px-3 py-1 hover:bg-accent">Undo</button>
          </>
        )}
      </div>
      {help && <p className="mt-1 text-[11px] text-muted-foreground">{help}</p>}
    </div>
  );
};

const MailboxSwitch = ({ current, onDone }: { current: string; onDone: (r: SettingsResponse) => void }) => {
  const [open, setOpen] = useState(false);
  const [path, setPath] = useState('');
  const [missing, setMissing] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [made, setMade] = useState<{ user: string; password: string } | null>(null);

  const submit = async (create: boolean) => {
    setError(null);
    const { ok, status, data } = await sendJSON('POST', '/api/system/mailbox', { dataDir: path, create });
    if (status === 404 && data?.error === 'no-mailbox') { setMissing(data.message); return; }
    if (!ok) { setError(errorText(data)); return; }
    setMissing(null);
    setOpen(false);
    if (data.adminPassword) setMade({ user: data.adminUser, password: data.adminPassword });
    onDone(data);
  };

  return (
    <div>
      <span className="font-medium">Mailbox</span>
      <span className="ml-2 px-1.5 py-0.5 text-[10px] uppercase tracking-wider bg-muted text-muted-foreground">after restart</span>
      <div className="mt-1 flex items-center gap-2">
        <span className="flex-1 font-mono text-xs break-all">{current}</span>
        {!open && (
          <button onClick={() => { setOpen(true); setPath(''); }} className="border border-border px-3 py-1 hover:bg-accent">
            Use a different mailbox…
          </button>
        )}
      </div>
      {open && (
        <div className="mt-2 border border-border bg-background p-3 space-y-2">
          <p className="text-muted-foreground">
            The full path of a mailbox folder — one holding <span className="font-mono">inboxql.db</span>. Nothing is
            moved or copied: the current mailbox stays where it is, and the server switches when it restarts.
          </p>
          <input
            autoFocus
            className="w-full bg-background border border-border px-2 py-1 font-mono text-xs"
            placeholder="~/Mail/archive-2024"
            value={path}
            onChange={e => { setPath(e.target.value); setMissing(null); }}
            onKeyDown={e => { if (e.key === 'Enter' && path.trim()) submit(false); }}
          />
          {missing && (
            <div className="text-amber-700 dark:text-amber-400">
              <p className="mb-1.5">{missing}</p>
              <button onClick={() => submit(true)} className="bg-primary text-primary-foreground px-3 py-1 font-semibold hover:opacity-90">
                Create an empty mailbox there
              </button>
            </div>
          )}
          {error && <p className="text-destructive">{error}</p>}
          <div className="flex gap-2">
            <button disabled={!path.trim()} onClick={() => submit(false)} className="bg-primary text-primary-foreground px-3 py-1 font-semibold hover:opacity-90 disabled:opacity-50">
              Use this mailbox
            </button>
            <button onClick={() => { setOpen(false); setMissing(null); }} className="border border-border px-3 py-1 hover:bg-accent">Cancel</button>
          </div>
        </div>
      )}
      {made && (
        <div className="mt-2 border border-primary/40 bg-primary/5 p-3">
          <p className="font-semibold mb-1">New mailbox created, with an administrator account</p>
          <p className="text-muted-foreground">
            Username <span className="font-mono text-foreground">{made.user}</span>, password{' '}
            <span className="font-mono text-foreground">{made.password}</span> <CopyButton text={made.password} />.
            It is shown once. On this computer you are not asked for it; it matters when the server listens beyond it.
          </p>
        </div>
      )}
    </div>
  );
};
