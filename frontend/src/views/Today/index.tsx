import { useCallback, useEffect, useState } from 'react';
import { queryCount, runQuery } from '../../lib/counts';
import { openMessage, openQuery, openTool } from '../../lib/tabs';
import { openComposeWith } from '../../lib/appUi';
import { replyTo } from '../../lib/replies';
import { bulkApply, bulkUndo, onMailChanged, mailChanged } from '../../lib/mail';
import { Evidence } from '../../shell/Evidence';
import { AheadStrip } from './AheadStrip';
import { waited } from '../../lib/waited';
import { Section } from './Section';

/**
 * Today: what needs you, before what arrived.
 *
 * Every section is a query, written out in the evidence panel, so anything
 * here can be opened as a list, saved, or asked of the command line in the
 * same words. Counts come from the same queries as the left-hand pane, so the
 * two never disagree.
 */

interface Msg {
  id: string;
  from: string;
  fromName?: string;
  subject?: string;
  preview?: string;
  date: string;
  accountId?: string;
  messageId?: string;
  normalizedBody?: string;
}

interface Group { label: string; value: number }

interface Occ {
  eventId: string;
  start: string;
  end: string;
  allDay: boolean;
  summary: string;
  location?: string;
  myStatus?: string;
  proposed?: boolean;
  calendarName?: string;
}

const Q = {
  owed: 'awaiting:me',
  queued: 'in:drafts status:queued',
  proposedEvents: 'in:events is:proposed',
  proposedCards: 'in:tickets state:proposed',
  arrived: 'folder:inbox after:1d',
  fromPeople: 'folder:inbox after:1d sentby:person',
  others: 'folder:inbox after:1d -sentby:person',
  soon: 'in:events next:2d',
};

const who = (m: Msg) => m.fromName || m.from;

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

export const Today = () => {
  const [counts, setCounts] = useState<Record<string, number | null>>({});
  const [owed, setOwed] = useState<Msg[] | null>(null);
  const [people, setPeople] = useState<Msg[] | null>(null);
  const [senders, setSenders] = useState<Group[] | null>(null);
  const [soon, setSoon] = useState<Occ[] | null>(null);
  const [checks, setChecks] = useState<number | null>(null);
  const [note, setNote] = useState<{ text: string; undo?: unknown } | null>(null);

  const load = useCallback(() => {
    const ctl = new AbortController();
    const s = ctl.signal;
    const keys = ['owed', 'queued', 'proposedEvents', 'proposedCards', 'arrived'] as const;
    Promise.all(keys.map(k => queryCount(Q[k], s))).then(v => {
      if (!s.aborted) setCounts(Object.fromEntries(keys.map((k, i) => [k, v[i]])));
    });
    runQuery<{ messages?: Msg[] }>(Q.owed, 6, s).then(r => { if (!s.aborted) setOwed(r?.messages ?? []); });
    runQuery<{ messages?: Msg[] }>(Q.fromPeople, 5, s).then(r => { if (!s.aborted) setPeople(r?.messages ?? []); });
    runQuery<{ groups?: Group[] }>(`${Q.others} | top from 6`, 6, s).then(r => { if (!s.aborted) setSenders(r?.groups ?? []); });
    runQuery<{ events?: Occ[] }>(Q.soon, 12, s).then(r => { if (!s.aborted) setSoon(r?.events ?? []); });
    fetch('/api/work/disagreements', { signal: s }).then(r => (r.ok ? r.json() : null)).then(d => {
      if (!s.aborted) setChecks(typeof d?.open === 'number' ? d.open : null);
    }).catch(() => {});
    return () => ctl.abort();
  }, []);

  useEffect(() => {
    let stop = load();
    const off = onMailChanged(() => { stop(); stop = load(); });
    return () => { stop(); off(); };
  }, [load]);

  const review = (counts.queued ?? 0) + (counts.proposedEvents ?? 0) + (counts.proposedCards ?? 0) + (checks ?? 0);
  const today = new Date().toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' });

  const notWaiting = async (m: Msg) => {
    const res = await fetch(`/api/messages/${encodeURIComponent(m.id)}/labels/waiting`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ level: 'no-one' }),
    }).catch(() => null);
    if (res?.ok) {
      setNote({ text: `Marked “${m.subject ?? 'that conversation'}” as waiting on no one. Your ruling outranks the agent's.` });
      mailChanged();
    } else {
      setNote({ text: 'Could not record that. Nothing changed.' });
    }
  };

  const archiveGroup = async (sender: string, n: number) => {
    try {
      const out = await bulkApply(`${Q.others} from:=${sender}`, 'archive');
      setNote({ text: `Archived ${plural(out.changed, 'message')} from ${sender}.`, undo: out.undo });
    } catch (e) {
      setNote({ text: `Could not archive ${plural(n, 'message')} from ${sender}: ${(e as Error).message}` });
    }
  };

  const undo = async (u: unknown) => {
    const n = await bulkUndo(u).catch(() => 0);
    setNote({ text: n ? `Put back ${plural(n, 'message')}.` : 'Nothing to put back.' });
  };

  return (
    <div className="mx-auto max-w-4xl px-6 py-5">
      <p className="text-sm text-muted-foreground">{today}</p>
      <h1 className="text-2xl font-semibold">Today</h1>
      <p className="mt-1 text-[15px] text-muted-foreground" data-testid="today-summary">
        {counts.owed == null ? 'Counting what needs you…' : (
          <>
            You owe <strong className="text-foreground">{plural(counts.owed, 'reply', 'replies')}</strong>,
            {' '}there {review === 1 ? 'is' : 'are'} <strong className="text-foreground">{plural(review, 'thing')} to review</strong>
            {counts.queued ? <> and <strong className="text-foreground">{plural(counts.queued, 'message')} waiting to send</strong></> : null}.
            {' '}{counts.arrived != null && <>{plural(counts.arrived, 'message')} arrived in the inbox since yesterday.</>}
          </>
        )}
      </p>

      {note && (
        <div role="status" className="mt-3 flex flex-wrap items-center gap-3 border border-border bg-card px-3 py-2 text-sm">
          <span>{note.text}</span>
          {note.undo != null && <button type="button" className="iql-btn" onClick={() => undo(note.undo)}>Undo</button>}
          <button type="button" className="ml-auto text-muted-foreground underline" onClick={() => setNote(null)}>Dismiss</button>
        </div>
      )}

      <Section id="t-owed" title="Replies you owe" count={counts.owed} more={{ label: 'Show all', go: () => openTool('replies', 'Replies I owe') }}>
        {owed == null ? <Loading /> : owed.length === 0 ? (
          <Empty>Nothing the agent has judged as waiting on you. Conversations it has not read yet are not counted.</Empty>
        ) : (
          <ul className="divide-y divide-border border border-border bg-card">
            {owed.map(m => (
              <li key={m.id} className="px-4 py-3">
                <div className="flex items-baseline gap-3">
                  <span className="font-semibold">{who(m)}</span>
                  <span className="min-w-0 flex-1 truncate text-sm text-muted-foreground">{m.subject}</span>
                  <span className="text-xs text-muted-foreground" title={new Date(m.date).toLocaleString()}>{waited(m.date)}</span>
                </div>
                {m.preview && <p className="mt-0.5 truncate text-sm text-foreground/80">{m.preview}</p>}
                <div className="mt-2 flex flex-wrap gap-2">
                  <button type="button" className="iql-btn primary" onClick={() => openComposeWith(replyTo({ ...m, body: m.normalizedBody }))}>Reply</button>
                  <button type="button" className="iql-btn" onClick={() => openMessage(m)}>Open</button>
                  <button type="button" className="px-2 text-sm underline underline-offset-2" onClick={() => openQuery(`anyone:=${m.from}`)}
                    aria-label={`Everything with ${who(m)}`}>Everything with them</button>
                  <button type="button" className="px-2 text-sm underline underline-offset-2" onClick={() => notWaiting(m)}
                    aria-label={`Not waiting on me: ${m.subject ?? who(m)}`}>Not waiting on me</button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section id="t-review" title="To review" count={review} more={{ label: 'Review all', go: () => openTool('review', 'To review') }}>
        <ul className="grid gap-2 sm:grid-cols-2">
          <Tile label="Messages to send" n={counts.queued} go={() => openTool('outbox', 'Drafts and outbox')} />
          <Tile label="Calendar proposals" n={counts.proposedEvents} go={() => openTool('review', 'To review')} />
          <Tile label="Cards proposed" n={counts.proposedCards} go={() => openTool('review', 'To review')} />
          <Tile label="Agent answers to settle" n={checks} go={() => openTool('work-loop', 'Work loop')} />
        </ul>
      </Section>

      <Section id="t-arrived" title="Arrived since yesterday" count={counts.arrived} more={{ label: 'Open the inbox', go: () => openQuery('folder:inbox') }}>
        <h3 className="mb-1 mt-1 text-sm font-semibold">From people</h3>
        {people == null ? <Loading /> : people.length === 0 ? <Empty>No new mail from people.</Empty> : (
          <ul className="divide-y divide-border border border-border bg-card">
            {people.map(m => (
              <li key={m.id}>
                <button type="button" className="flex w-full items-baseline gap-3 px-4 py-2 text-left hover:bg-accent" onClick={() => openMessage(m)}>
                  <span className="font-medium">{who(m)}</span>
                  <span className="min-w-0 flex-1 truncate text-sm text-muted-foreground">{m.subject}</span>
                  <span className="text-xs text-muted-foreground">{waited(m.date)}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
        <h3 className="mb-1 mt-4 text-sm font-semibold">Everything else, by sender</h3>
        {senders == null ? <Loading /> : senders.length === 0 ? <Empty>Nothing else arrived.</Empty> : (
          <ul className="divide-y divide-border border border-border bg-card">
            {senders.map(g => (
              <li key={g.label} className="flex flex-wrap items-center gap-3 px-4 py-2">
                <span className="min-w-0 flex-1 truncate">{g.label}</span>
                <span className="text-sm text-muted-foreground">{plural(g.value, 'message')}</span>
                <button type="button" className="iql-btn" onClick={() => openQuery(`${Q.others} from:=${g.label}`)}>Look</button>
                <button type="button" className="iql-btn" onClick={() => archiveGroup(g.label, g.value)}
                  aria-label={`Archive ${plural(g.value, 'message')} from ${g.label}`}>Archive {g.value}</button>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <AheadStrip />

      <Section id="t-soon" title="Next 48 hours" count={soon?.length} more={{ label: 'Calendar', go: () => openTool('calendar', 'Calendar') }}>
        {soon == null ? <Loading /> : soon.length === 0 ? <Empty>Nothing on the calendar.</Empty> : (
          <ul className="divide-y divide-border border border-border bg-card">
            {soon.map(e => (
              <li key={e.eventId + e.start} className="flex flex-wrap items-baseline gap-3 px-4 py-2">
                <span className="w-32 shrink-0 text-sm tabular-nums text-muted-foreground">{when(e)}</span>
                <span className="min-w-0 flex-1">
                  <span className={e.myStatus === 'declined' ? 'line-through text-muted-foreground' : 'font-medium'}>{e.summary}</span>
                  {e.location && <span className="text-sm text-muted-foreground"> · {e.location}</span>}
                </span>
                {e.myStatus && e.myStatus !== 'accepted' && e.myStatus !== 'organizer' && (
                  <span className="text-xs text-muted-foreground">{e.myStatus === 'needs-action' ? 'not answered' : e.myStatus}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Evidence>
        <h2 className="mb-1 text-base font-semibold">How this was made</h2>
        <p className="mb-3 text-sm text-muted-foreground">Every section is a query. Run one to see it as a list.</p>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1.5 text-sm">
          {([
            ['Replies', Q.owed], ['To send', Q.queued], ['Events', Q.proposedEvents], ['Cards', Q.proposedCards],
            ['Arrived', Q.arrived], ['People', Q.fromPeople], ['Soon', Q.soon],
          ] as const).map(([k, q]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{k}</dt>
              <dd><button type="button" className="text-left font-mono text-xs underline underline-offset-2" onClick={() => openQuery(q)}>{q}</button></dd>
            </div>
          ))}
        </dl>
        <AgentCoverage />
      </Evidence>
    </div>
  );
};

/** How much of the mail the agent has read, so an empty section is never mistaken for "nothing there". */
const AgentCoverage = () => {
  const [w, setW] = useState<{ pending: number; total: number } | null>(null);
  useEffect(() => {
    fetch('/api/work/backlog').then(r => (r.ok ? r.json() : null)).then(d => {
      const e = d?.elements?.find((x: { element: string }) => x.element === 'waiting');
      if (e) setW({ pending: e.pending, total: e.total });
    }).catch(() => {});
  }, []);
  if (!w) return null;
  const read = w.total - w.pending;
  return (
    <>
      <h3 className="mb-1 mt-5 text-sm font-semibold">What the agent has read</h3>
      <p className="text-sm">
        It has judged who is waiting in <strong>{read.toLocaleString()}</strong> of {w.total.toLocaleString()} conversations.
        {' '}A reply you owe in one it has not read is not counted above.
      </p>
      <button type="button" className="mt-2 text-sm underline" onClick={() => openTool('work-loop', 'Work loop')}>The work loop</button>
    </>
  );
};

function when(e: Occ): string {
  const s = new Date(e.start);
  const day = s.toLocaleDateString(undefined, { weekday: 'short' });
  if (e.allDay) return `${day} · all day`;
  return `${day} ${s.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })}`;
}

const Tile = ({ label, n, go }: { label: string; n: number | null | undefined; go: () => void }) => (
  <li>
    <button type="button" onClick={go} className="flex w-full items-center justify-between gap-3 border border-border bg-card px-4 py-3 text-left hover:bg-accent">
      <span>{label}</span>
      <span className="text-lg font-semibold tabular-nums">{n ?? '–'}</span>
    </button>
  </li>
);

const Loading = () => <p className="text-sm text-muted-foreground">Loading…</p>;
const Empty = ({ children }: { children: React.ReactNode }) => (
  <p className="border border-dashed border-border px-4 py-3 text-sm text-muted-foreground">{children}</p>
);
