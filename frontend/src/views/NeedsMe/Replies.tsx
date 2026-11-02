import { useCallback, useEffect, useState } from 'react';
import { runQuery, queryCount } from '../../lib/counts';
import { openMessage, openQuery } from '../../lib/tabs';
import { openComposeWith } from '../../lib/appUi';
import { replyTo } from '../../lib/replies';
import { mailChanged, onMailChanged } from '../../lib/mail';
import { waited } from '../../lib/waited';
import { Evidence } from '../../shell/Evidence';

interface Msg { id: string; from: string; fromName?: string; subject?: string; preview?: string; date: string; normalizedBody?: string; to?: string[] }

const TWO_WEEKS = 14 * 86_400_000;
const nameOf = (m: Msg) => m.fromName || m.from;

const Row = ({ m, dir, rule }: { m: Msg; dir: 'me' | 'them'; rule: (m: Msg, level: 'no-one' | 'on-me' | 'on-them') => void }) => (
  <li className="px-4 py-3">
    <div className="flex items-baseline gap-3">
      <span className="font-semibold">{dir === 'me' ? nameOf(m) : (m.to?.[0] ?? nameOf(m))}</span>
      <span className="min-w-0 flex-1 truncate text-sm text-muted-foreground">{m.subject}</span>
      <span className="text-xs text-muted-foreground" title={new Date(m.date).toLocaleString()}>{waited(m.date)}</span>
    </div>
    {m.preview && <p className="mt-0.5 truncate text-sm text-foreground/80">{m.preview}</p>}
    <div className="mt-2 flex flex-wrap items-center gap-2">
      <button type="button" className="iql-btn primary" onClick={() => openComposeWith(replyTo({ ...m, body: m.normalizedBody }))}>
        {dir === 'me' ? 'Reply' : 'Nudge'}
      </button>
      <button type="button" className="iql-btn" onClick={() => openMessage(m)}>Open</button>
      <button type="button" className="px-2 text-sm underline underline-offset-2" onClick={() => rule(m, 'no-one')}
        aria-label={`Waiting on no one: ${m.subject ?? nameOf(m)}`}>{dir === 'me' ? 'Not waiting on me' : 'Not waiting any more'}</button>
      <button type="button" className="px-2 text-sm underline underline-offset-2" onClick={() => rule(m, dir === 'me' ? 'on-them' : 'on-me')}
        aria-label={`${dir === 'me' ? 'Waiting on them' : 'Waiting on me'}: ${m.subject ?? nameOf(m)}`}>{dir === 'me' ? 'It is on them' : 'It is on me'}</button>
    </div>
  </li>
);



/**
 * Replies I owe, and the other direction: what you are waiting for.
 *
 * Both are the `waiting` element's judgements (`awaiting:me`, `awaiting:them`),
 * newest message of each conversation. A reply owed for two weeks is asked
 * about rather than left to sink: still owed, or not.
 */
export const Replies = () => {
  const [dir, setDir] = useState<'me' | 'them'>('me');
  const [rows, setRows] = useState<Msg[] | null>(null);
  const [counts, setCounts] = useState<{ me: number | null; them: number | null }>({ me: null, them: null });
  const [note, setNote] = useState<string | null>(null);

  const load = useCallback(() => {
    const ctl = new AbortController();
    runQuery<{ messages?: Msg[] }>(`awaiting:${dir}`, 100, ctl.signal).then(r => { if (!ctl.signal.aborted) setRows(r?.messages ?? []); });
    Promise.all([queryCount('awaiting:me', ctl.signal), queryCount('awaiting:them', ctl.signal)])
      .then(([me, them]) => { if (!ctl.signal.aborted) setCounts({ me, them }); });
    return () => ctl.abort();
  }, [dir]);
  useEffect(() => {
    let stop = load();
    const off = onMailChanged(() => { stop(); stop = load(); });
    return () => { stop(); off(); };
  }, [load]);

  const rule = async (m: Msg, level: 'no-one' | 'on-me' | 'on-them') => {
    const r = await fetch(`/api/messages/${encodeURIComponent(m.id)}/labels/waiting`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ level }),
    }).catch(() => null);
    setNote(r?.ok ? `Recorded: “${m.subject ?? 'that conversation'}” is waiting on ${level === 'no-one' ? 'no one' : level === 'on-me' ? 'you' : 'them'}. Your ruling outranks the agent's.` : 'Could not record that. Nothing changed.');
    if (r?.ok) mailChanged();
  };

  const [now] = useState(() => Date.now());
  const recent = (rows ?? []).filter(m => now - new Date(m.date).getTime() < TWO_WEEKS);
  const old = (rows ?? []).filter(m => now - new Date(m.date).getTime() >= TWO_WEEKS);

  return (
    <div className="mx-auto max-w-4xl px-6 py-5">
      <h1 className="text-2xl font-semibold">{dir === 'me' ? 'Replies I owe' : 'Waiting on others'}</h1>
      <p className="mt-1 text-[15px] text-muted-foreground" data-testid="replies-summary">
        {counts.me == null ? 'Counting…' : <>You owe <strong className="text-foreground">{counts.me}</strong> {counts.me === 1 ? 'reply' : 'replies'}; you are waiting on others in <strong className="text-foreground">{counts.them ?? 0}</strong> {counts.them === 1 ? 'conversation' : 'conversations'}.</>}
      </p>
      <nav aria-label="Direction" className="mt-4 flex gap-1 border-b border-border">
        {(['me', 'them'] as const).map(d => (
          <button key={d} type="button" aria-current={dir === d ? 'page' : undefined} onClick={() => { if (d !== dir) { setRows(null); setDir(d); } }}
            className={`-mb-px border-b-2 px-3 py-2 text-sm ${dir === d ? 'border-primary font-semibold' : 'border-transparent text-muted-foreground'}`}>
            {d === 'me' ? `I owe · ${counts.me ?? '–'}` : `Waiting on others · ${counts.them ?? '–'}`}
          </button>
        ))}
      </nav>
      {note && <p role="status" className="mt-3 border border-border bg-card px-3 py-2 text-sm">{note}</p>}
      {rows == null ? <p className="mt-4 text-sm text-muted-foreground">Loading…</p> : rows.length === 0 ? (
        <p className="mt-4 border border-dashed border-border px-4 py-3 text-sm text-muted-foreground">
          {dir === 'me' ? 'Nothing the agent has judged as waiting on you.' : 'Nothing the agent has judged as waiting on someone else.'} Conversations it has not read are not counted.
        </p>
      ) : (
        <>
          <section className="mt-5" aria-labelledby="r-recent">
            <h2 id="r-recent" className="mb-2 text-lg font-semibold">Last two weeks <span className="font-normal text-muted-foreground">· {recent.length}</span></h2>
            {recent.length === 0 ? <p className="text-sm text-muted-foreground">None.</p> : <ul className="divide-y divide-border border border-border bg-card">{recent.map(m => <Row key={m.id} m={m} dir={dir} rule={rule} />)}</ul>}
          </section>
          {old.length > 0 && (
            <section className="mt-6" aria-labelledby="r-old">
              <h2 id="r-old" className="mb-1 text-lg font-semibold">Older than two weeks <span className="font-normal text-muted-foreground">· {old.length}</span></h2>
              <p className="mb-2 text-sm text-muted-foreground">Still {dir === 'me' ? 'owed' : 'waited for'}? Reply, or say it is not — either way it stops sinking.</p>
              <ul className="divide-y divide-border border border-border bg-card">{old.map(m => <Row key={m.id} m={m} dir={dir} rule={rule} />)}</ul>
            </section>
          )}
        </>
      )}
      <Evidence>
        <h2 className="mb-1 text-base font-semibold">How this was made</h2>
        <p className="text-sm">The <code className="text-xs">waiting</code> element's judgements: <button type="button" className="font-mono text-xs underline" onClick={() => openQuery(`awaiting:${dir}`)}>awaiting:{dir}</button>, the newest message of each conversation.</p>
        <p className="mt-2 text-sm">Your ruling — “not waiting”, “on them” — outranks the agent's, survives its re-reading, and counts as a correction when its accuracy is measured.</p>
      </Evidence>
    </div>
  );
};
