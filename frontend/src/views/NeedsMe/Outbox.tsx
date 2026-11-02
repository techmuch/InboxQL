import { useCallback, useEffect, useState } from 'react';
import { sendDraft } from '../../lib/sending';
import { onMailChanged } from '../../lib/mail';
import { Evidence } from '../../shell/Evidence';

interface Draft {
  id: string; to: string[]; subject: string; body: string; status: string; origin: string;
  updatedAt?: string; queuedAt?: string; lastError?: string;
}

const Item = ({ d, actions }: { d: Draft; actions?: React.ReactNode }) => (
  <li className="px-4 py-3">
    <div className="flex flex-wrap items-baseline gap-2">
      <span className="font-semibold">{d.to.length ? `To ${d.to.join(', ')}` : 'No recipient'}</span>
      <span className="text-sm text-muted-foreground">· {d.subject || '(no subject)'}</span>
      <span className={`ml-auto px-1.5 text-xs ${d.origin === 'agent' ? 'bg-violet-100 text-violet-800 dark:bg-violet-950 dark:text-violet-200' : 'bg-muted text-muted-foreground'}`}>
        {d.origin === 'agent' ? 'written by an agent' : 'yours'}
      </span>
    </div>
    {d.lastError && <p className="mt-1 text-sm text-destructive">The server refused it: {d.lastError}</p>}
    <details className="mt-1 text-sm">
      <summary className="cursor-pointer truncate text-foreground/80">{d.body.split('\n').find(l => l.trim()) ?? '(empty)'}</summary>
      <pre className="mt-2 whitespace-pre-wrap border border-border bg-background p-3 font-sans text-sm">{d.body}</pre>
    </details>
    {actions && <div className="mt-2 flex flex-wrap gap-2">{actions}</div>}
  </li>
);

const List = ({ id, title, items, empty, actions }: { id: string; title: string; items: Draft[]; empty: string; actions?: (d: Draft) => React.ReactNode }) => (
  <section className="mt-6" aria-labelledby={id}>
    <h2 id={id} className="mb-2 text-lg font-semibold">{title} <span className="font-normal text-muted-foreground">· {items.length}</span></h2>
    {items.length === 0 ? <p className="text-sm text-muted-foreground">{empty}</p> : (
      <ul className="divide-y divide-border border border-border bg-card">{items.map(d => <Item key={d.id} d={d} actions={actions?.(d)} />)}</ul>
    )}
  </section>
);


/** Every draft not yet sent. */
async function fetchDrafts(): Promise<Draft[]> {
  try {
    const r = await fetch('/api/drafts');
    const d = r.ok ? await r.json() : { drafts: [] };
    return (d.drafts ?? []).filter((x: Draft) => x.status !== 'sent');
  } catch { return []; }
}

/**
 * Drafts and outbox: outgoing mail that has not gone.
 *
 * What waits for your Send — including everything an agent wrote, which never
 * sends itself — what failed and why, and drafts. The same queue `iql send`
 * fills, so an agent's mail always stops here.
 */
export const Outbox = () => {
  const [drafts, setDrafts] = useState<Draft[] | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const load = useCallback(() => { void fetchDrafts().then(setDrafts); }, []);
  useEffect(() => {
    const get = () => { void fetchDrafts().then(setDrafts); };
    get();
    return onMailChanged(get);
  }, []);

  const send = async (d: Draft) => {
    if (!window.confirm(`Send “${d.subject}” to ${d.to.join(', ')}?${d.origin === 'agent' ? '\n\nAn agent wrote this message.' : ''}`)) return;
    try { await sendDraft(d.id); setNote(`Sending “${d.subject}”. Undo is in the corner for a few seconds.`); }
    catch (e) { setNote(`Could not send: ${(e as Error).message}`); }
    void load();
  };
  const back = async (d: Draft) => {
    const r = await fetch(`/api/drafts/${encodeURIComponent(d.id)}/reject`, { method: 'POST' }).catch(() => null);
    setNote(r?.ok ? `“${d.subject}” is back in drafts.` : 'Could not take it back.');
    void load();
  };

  const queued = (drafts ?? []).filter(d => d.status === 'queued');
  const failed = (drafts ?? []).filter(d => d.status === 'failed');
  const plain = (drafts ?? []).filter(d => d.status === 'draft');

  return (
    <div className="mx-auto max-w-4xl px-6 py-5">
      <h1 className="text-2xl font-semibold">Drafts and outbox</h1>
      <p className="mt-1 text-[15px] text-muted-foreground" data-testid="outbox-summary">
        {drafts == null ? 'Looking…' : <><strong className="text-foreground">{queued.length}</strong> waiting for you to send, {failed.length} failed, {plain.length} {plain.length === 1 ? 'draft' : 'drafts'}. Nothing sends until you press Send.</>}
      </p>
      {note && <p role="status" className="mt-3 border border-border bg-card px-3 py-2 text-sm">{note}</p>}
      <List id="o-q" title="Waiting for you to send" items={queued} empty="Nothing is waiting to send."
        actions={d => <><button type="button" className="iql-btn primary" onClick={() => send(d)}>Send…</button><button type="button" className="iql-btn" onClick={() => back(d)}>Return to drafts</button></>} />
      <List id="o-f" title="Failed" items={failed} empty="Nothing failed."
        actions={d => <button type="button" className="iql-btn" onClick={() => back(d)}>Return to drafts to fix</button>} />
      <List id="o-d" title="Drafts" items={plain} empty="No drafts." />
      <Evidence>
        <h2 className="mb-1 text-base font-semibold">Who sends</h2>
        <p className="text-sm">Only you. An agent composes with <code className="text-xs">iql draft create --origin agent</code> and queues with <code className="text-xs">iql send</code>, which sends nothing — the message waits here, marked as written by an agent, until you press Send.</p>
        <p className="mt-2 text-sm">Send asks once, then waits a few seconds with Undo before the mail goes.</p>
      </Evidence>
    </div>
  );
};
