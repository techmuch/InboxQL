import { useCallback, useEffect, useState } from 'react';
import { CalendarDays, Check, Loader2, Mail, RefreshCw, Send, SquareKanban, X } from 'lucide-react';
import { sendDraft } from '../lib/sending';
import { approveChange, listChanges, rejectChange, type Change } from './Calendar/api';
import { openCalendar } from '../lib/calendarNav';
import { openMessageByID } from '../lib/tabs';

interface Ticket {
  id: string; title: string; body?: string; status: string; confidence?: number;
  sources?: { messageId: string; subject?: string }[];
  [k: string]: unknown;
}
interface Draft { id: string; to: string[]; subject: string; origin: string; queuedAt?: string; body: string }

/**
 * Waiting for approval: everything proposed and waiting on a person, in one
 * place — cards an extractor proposed, calendar changes an agent proposed,
 * and mail queued in the outbox.
 */
export const Approvals = () => {
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [changes, setChanges] = useState<Change[]>([]);
  const [drafts, setDrafts] = useState<Draft[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true); setError(null);
    const get = async <T,>(url: string, pick: (d: any) => T, empty: T): Promise<T> => {
      try { const r = await fetch(url); return r.ok ? pick(await r.json()) ?? empty : empty; } catch { return empty; }
    };
    const [t, d] = await Promise.all([
      get<Ticket[]>('/api/tickets?q=status:proposed', d => d, []),
      get<Draft[]>('/api/drafts?status=queued', d => d.drafts, []),
    ]);
    let c: Change[] = [];
    try { c = (await listChanges()).changes?.filter(x => x.state === 'proposed') ?? []; } catch { /* no calendar */ }
    setTickets(t); setDrafts(d); setChanges(c); setLoading(false);
  }, []);
  useEffect(() => { void load(); }, [load]);

  const rule = async (t: Ticket, status: 'todo' | 'rejected') => {
    setTickets(ts => ts.filter(x => x.id !== t.id));
    const r = await fetch('/api/tickets', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...t, status }),
    });
    if (!r.ok) { setError(`Could not update “${t.title}”`); void load(); }
  };
  const decide = async (c: Change, approve: boolean) => {
    setChanges(cs => cs.filter(x => x.id !== c.id));
    try { await (approve ? approveChange(c.id) : rejectChange(c.id)); }
    catch (e: any) { setError(e.message ?? 'Could not decide that change'); void load(); }
  };

  const send = async (d: Draft) => {
    setDrafts(ds => ds.filter(x => x.id !== d.id));
    try { await sendDraft(d.id); } catch (e: any) { setError(e.message ?? 'Could not send'); void load(); }
  };
  const reject = async (d: Draft) => {
    setDrafts(ds => ds.filter(x => x.id !== d.id));
    const r = await fetch(`/api/drafts/${encodeURIComponent(d.id)}/reject`, { method: 'POST' }).catch(() => null);
    if (!r?.ok) { setError(`Could not take “${d.subject}” back`); void load(); }
  };

  const total = tickets.length + changes.length + drafts.length;
  const btn = 'inline-flex items-center gap-1 border border-border px-2 py-0.5 text-xs hover:bg-accent';

  return (
    <div className="h-full overflow-auto p-6">
      <div className="mx-auto max-w-3xl space-y-6">
        <header className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold">Waiting for approval</h1>
            <p className="text-sm text-muted-foreground">
              {loading ? 'Looking…' : total === 0 ? 'Nothing is waiting for you.' : `${total} waiting for you.`}
            </p>
          </div>
          <button onClick={() => void load()} className={btn} aria-label="Refresh">
            {loading ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />} Refresh
          </button>
        </header>
        {error && <p className="text-sm text-destructive">{error}</p>}

        <Section icon={<SquareKanban className="h-4 w-4" />} title="Proposed cards" count={tickets.length}
          empty="No cards are proposed. An extractor proposes them with iql ticket propose.">
          {tickets.map(t => (
            <li key={t.id} className="flex items-start justify-between gap-3 py-2">
              <div className="min-w-0">
                <div className="font-medium">{t.title}</div>
                {t.sources?.[0] && (
                  <button onClick={() => openMessageByID(t.sources![0].messageId)} className="truncate text-xs text-muted-foreground hover:underline">
                    from “{t.sources[0].subject ?? 'a message'}”
                  </button>
                )}
              </div>
              <div className="flex shrink-0 gap-1">
                <button className={btn} onClick={() => void rule(t, 'todo')}><Check className="h-3 w-3" /> Accept</button>
                <button className={btn} onClick={() => void rule(t, 'rejected')}><X className="h-3 w-3" /> Reject</button>
              </div>
            </li>
          ))}
        </Section>

        <Section icon={<CalendarDays className="h-4 w-4" />} title="Calendar proposals" count={changes.length}
          empty="No calendar changes are proposed.">
          {changes.map(c => (
            <li key={c.id} className="flex items-start justify-between gap-3 py-2">
              <div className="min-w-0">
                <div className="font-medium">{describeChange(c)}</div>
                <div className="text-xs text-muted-foreground">proposed by {c.origin} · {new Date(c.createdAt).toLocaleString()}</div>
              </div>
              <div className="flex shrink-0 gap-1">
                {c.eventId && <button className={btn} onClick={() => openCalendar({ eventId: c.eventId })}>Open</button>}
                <button className={btn} onClick={() => void decide(c, true)}><Check className="h-3 w-3" /> Approve</button>
                <button className={btn} onClick={() => void decide(c, false)}><X className="h-3 w-3" /> Reject</button>
              </div>
            </li>
          ))}
        </Section>

        <Section icon={<Mail className="h-4 w-4" />} title="Mail in the outbox" count={drafts.length}
          empty="Nothing is queued to send.">
          {drafts.map(d => (
            <li key={d.id} className="py-2">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="font-medium">{d.subject || '(no subject)'}</div>
                  <div className="truncate text-xs text-muted-foreground">
                    to {d.to.join(', ')}{d.origin === 'agent' && <span className="ml-1 font-semibold text-amber-600">· written by an agent</span>}
                  </div>
                </div>
                <div className="flex shrink-0 gap-1">
                  <button className={btn} onClick={() => void send(d)}><Send className="h-3 w-3" /> Send</button>
                  <button className={btn} onClick={() => void reject(d)}><X className="h-3 w-3" /> Back to drafts</button>
                </div>
              </div>
              <p className="mt-1 line-clamp-2 text-xs text-muted-foreground whitespace-pre-wrap">{d.body}</p>
            </li>
          ))}
        </Section>
        {drafts.length > 0 && (
          <p className="text-xs text-muted-foreground">
            Send holds a message for a few seconds, with Undo, then sends it. From a terminal it is
            <code className="mx-1 font-mono">iql outbox approve &lt;id&gt;</code>.
          </p>
        )}
      </div>
    </div>
  );
};

const describeChange = (c: Change) => {
  const what = c.payload?.summary ? `“${c.payload.summary}”` : 'an event';
  switch (c.op) {
    case 'create': return `New event ${what}`;
    case 'update': return `Change ${what}`;
    case 'delete': return `Cancel ${what}`;
    case 'respond': return `Answer ${what}: ${c.payload?.status ?? ''}`;
    default: return `${c.op} ${what}`;
  }
};

const Section = ({ icon, title, count, empty, children }: {
  icon: React.ReactNode; title: string; count: number; empty: string; children: React.ReactNode;
}) => (
  <section className="border border-border bg-card p-4">
    <h2 className="mb-1 flex items-center gap-2 text-sm font-semibold">{icon}{title}<span className="text-muted-foreground font-normal">{count}</span></h2>
    {count === 0 ? <p className="text-xs text-muted-foreground">{empty}</p> : <ul className="divide-y divide-border text-sm">{children}</ul>}
  </section>
);
