import { useEffect, useState } from 'react';

/**
 * Settings, at a glance: what needs attention first, then each group with
 * its state in a line. Most visits end here.
 */
interface Check { name: string; status: 'ok' | 'warn' | 'fail' | string; detail: string; remedy?: string }
interface Backup { at: string; reason?: string }

export interface SettingsGroup { id: string; label: string; sections: { id: string; label: string }[]; about: string }

const ago = (iso: string) => {
  const d = Math.round((Date.now() - new Date(iso).getTime()) / 86_400_000);
  return d <= 0 ? 'today' : d === 1 ? 'yesterday' : `${d} days ago`;
};

export const SettingsOverview = ({ groups, open }: { groups: SettingsGroup[]; open: (section: string) => void }) => {
  const [checks, setChecks] = useState<Check[] | null>(null);
  const [lastBackup, setLastBackup] = useState<string | null | undefined>(undefined);
  const [update, setUpdate] = useState<{ newer: boolean; latest: string; current: string } | null>(null);
  const [accounts, setAccounts] = useState<number | null>(null);

  useEffect(() => {
    const ctl = new AbortController();
    const s = ctl.signal;
    const get = (u: string) => fetch(u, { signal: s }).then(r => (r.ok ? r.json() : null)).catch(() => null);
    get('/api/health').then(d => !s.aborted && setChecks(d?.checks ?? []));
    get('/api/backups').then(d => {
      if (s.aborted) return;
      const bs: Backup[] = d?.backups ?? [];
      setLastBackup(bs.length ? bs.map(b => b.at).sort().at(-1)! : null);
    });
    get('/api/system/update').then(d => !s.aborted && d && setUpdate(d));
    get('/api/accounts').then(d => !s.aborted && setAccounts(Array.isArray(d) ? d.length : null));
    return () => ctl.abort();
  }, []);

  const problems = (checks ?? []).filter(c => c.status !== 'ok' && c.detail !== 'skipped');
  const staleBackup = lastBackup != null && Date.now() - new Date(lastBackup).getTime() > 8 * 86_400_000;
  const attention = problems.length + (staleBackup ? 1 : 0) + (update?.newer ? 1 : 0);

  const state: Record<string, string> = {
    you: accounts == null ? '' : `${accounts} mail ${accounts === 1 ? 'account' : 'accounts'}`,
    data: lastBackup === undefined ? '' : lastBackup ? `last backup ${ago(lastBackup)}` : 'no backups yet',
    computer: update ? `version ${update.current}${update.newer ? ` · ${update.latest} is ready` : ' · up to date'}` : '',
  };

  return (
    <div>
      <h1 className="text-2xl font-semibold">Settings</h1>
      <p className="mt-1 text-[15px] text-muted-foreground" data-testid="settings-summary">
        {checks == null ? 'Checking…' : attention === 0 ? 'Nothing needs you. Everything is as you left it.' : `${attention} ${attention === 1 ? 'thing needs' : 'things need'} you.`}
      </p>

      {(problems.length > 0 || staleBackup || update?.newer) && (
        <section className="mt-5" aria-labelledby="so-attn">
          <h2 id="so-attn" className="mb-2 text-lg font-semibold">Needs attention</h2>
          <ul className="divide-y divide-border border border-border bg-card">
            {update?.newer && (
              <li className="flex flex-wrap items-center gap-3 px-4 py-3">
                <span className="bg-amber-100 px-1.5 text-xs text-amber-900 dark:bg-amber-950 dark:text-amber-200">update</span>
                <span className="min-w-0 flex-1">Version {update.latest} is ready. Updating backs up the mailbox and restarts the server.</span>
                <button type="button" className="iql-btn" onClick={() => open('system')}>Review</button>
              </li>
            )}
            {staleBackup && (
              <li className="flex flex-wrap items-center gap-3 px-4 py-3">
                <span className="bg-amber-100 px-1.5 text-xs text-amber-900 dark:bg-amber-950 dark:text-amber-200">backup</span>
                <span className="min-w-0 flex-1">The last backup was {ago(lastBackup!)}.</span>
                <button type="button" className="iql-btn" onClick={() => open('data')}>Backups</button>
              </li>
            )}
            {problems.map(c => (
              <li key={c.name} className="flex flex-wrap items-start gap-3 px-4 py-3">
                <span className={`px-1.5 text-xs ${c.status === 'fail' ? 'bg-red-100 text-red-900 dark:bg-red-950 dark:text-red-200' : 'bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200'}`}>{c.name}</span>
                <span className="min-w-0 flex-1 text-sm">{c.detail}{c.remedy && <span className="mt-0.5 block font-mono text-xs text-muted-foreground">{c.remedy}</span>}</span>
                <button type="button" className="iql-btn" onClick={() => open('maintenance')}>Fix</button>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="mt-6" aria-labelledby="so-all">
        <h2 id="so-all" className="mb-2 text-lg font-semibold">All settings</h2>
        <ul className="divide-y divide-border border border-border bg-card">
          {groups.map(g => (
            <li key={g.id} className="px-4 py-3">
              <button type="button" className="font-semibold underline underline-offset-2" onClick={() => open(g.sections[0].id)}>{g.label}</button>
              {state[g.id] && <span className="ml-2 text-sm text-muted-foreground">· {state[g.id]}</span>}
              <p className="text-sm text-foreground/80">{g.about}</p>
              <p className="mt-1 flex flex-wrap gap-x-3 text-sm">
                {g.sections.map(s => <button key={s.id} type="button" className="underline underline-offset-2 text-muted-foreground hover:text-foreground" onClick={() => open(s.id)}>{s.label}</button>)}
              </p>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
};
