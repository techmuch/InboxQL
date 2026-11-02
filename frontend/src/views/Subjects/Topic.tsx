import { useEffect, useState } from 'react';
import { queryCount, runQuery } from '../../lib/counts';
import { openMessage, openQuery, openTool } from '../../lib/tabs';
import { listTopics, openTopic, useSubjectStore, type TopicCount } from '../../lib/subjects';
import { waited } from '../../lib/waited';
import { Evidence } from '../../shell/Evidence';
import { Section } from '../Today/Section';
import { Stream } from './Stream';

interface Msg { id: string; from: string; fromName?: string; subject?: string; date: string }
interface Group { label: string; value: number }
interface Ticket { id: string; title: string; status: string; dueAt?: string }

/**
 * A topic as a page: what is open in it, what it has cost, its cards and its
 * people, then its mail. A topic is a tag the agent's `topics` element gives a
 * conversation, so everything here is `topic:<name>` and something.
 */
export const Topic = () => {
  const topic = useSubjectStore(s => s.topic);
  if (!topic) return <Topics />;
  return <TopicPage key={topic} topic={topic} />;
};

const TopicPage = ({ topic }: { topic: string }) => {
  const t = `topic:${topic}`;
  const [owed, setOwed] = useState<Msg[] | null>(null);
  const [theirs, setTheirs] = useState<Msg[] | null>(null);
  const [money, setMoney] = useState<number | null>(null);
  const [cards, setCards] = useState<Ticket[] | null>(null);
  const [people, setPeople] = useState<Group[] | null>(null);
  const [n, setN] = useState<number | null>(null);

  useEffect(() => {
    const ctl = new AbortController();
    const s = ctl.signal;
    queryCount(t, s).then(v => !s.aborted && setN(v));
    runQuery<{ messages?: Msg[] }>(`${t} awaiting:me`, 10, s).then(r => !s.aborted && setOwed(r?.messages ?? []));
    runQuery<{ messages?: Msg[] }>(`${t} awaiting:them`, 10, s).then(r => !s.aborted && setTheirs(r?.messages ?? []));
    runQuery<{ groups?: Group[] }>(`${t} extract:receipts | sum receipts.total`, 1, s).then(r => !s.aborted && setMoney(r?.groups?.[0]?.value ?? 0));
    runQuery<{ tickets?: Ticket[] }>(`in:tickets label:${topic} -state:rejected`, 20, s).then(r => !s.aborted && setCards(r?.tickets ?? []));
    runQuery<{ groups?: Group[] }>(`${t} -from:me() | top from 6`, 6, s).then(r => !s.aborted && setPeople(r?.groups ?? []));
    return () => ctl.abort();
  }, [t, topic]);

  const loops = [...(owed ?? []).map(m => ({ m, on: 'you' })), ...(theirs ?? []).map(m => ({ m, on: 'them' }))];

  return (
    <div className="mx-auto max-w-4xl px-6 py-5">
      <p className="text-sm text-muted-foreground"><button type="button" className="underline" onClick={() => openTool('topic-index', 'Topics')}>Topics</button></p>
      <h1 className="text-2xl font-semibold">{topic}</h1>
      <p className="mt-1 text-[15px] text-muted-foreground" data-testid="topic-summary">
        <span className="mr-2 border border-border px-1.5 text-xs">topic</span>
        {n == null ? 'Counting…' : `${n} messages`}{owed && theirs ? ` · ${owed.length} owed by you, ${theirs.length} waiting on others` : ''}{cards ? ` · ${cards.length} cards` : ''}.
      </p>

      <Section id="tp-loops" title="Open loops" count={owed && theirs ? loops.length : null}>
        {owed == null || theirs == null ? <p className="text-sm text-muted-foreground">Loading…</p> : loops.length === 0 ? (
          <p className="border border-dashed border-border px-4 py-3 text-sm text-muted-foreground">Nothing in this topic is judged to be waiting on anyone.</p>
        ) : (
          <ul className="divide-y divide-border border border-border bg-card">
            {loops.map(({ m, on }) => (
              <li key={m.id}>
                <button type="button" onClick={() => openMessage(m)} className="flex w-full flex-wrap items-baseline gap-3 px-4 py-2 text-left hover:bg-accent">
                  <span className={`px-1.5 text-xs ${on === 'you' ? 'bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200' : 'bg-muted text-muted-foreground'}`}>on {on}</span>
                  <span className="font-medium">{m.subject}</span>
                  <span className="text-sm text-muted-foreground">{m.fromName || m.from} · {waited(m.date)}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <div className="mt-7 grid gap-4 sm:grid-cols-3">
        <section className="border border-border bg-card p-4" aria-labelledby="tp-money">
          <h2 id="tp-money" className="font-semibold">Money</h2>
          <p className="mt-1 text-2xl font-semibold tabular-nums">{money == null ? '–' : money.toLocaleString(undefined, { style: 'currency', currency: 'USD' })}</p>
          <p className="text-sm text-muted-foreground">from receipts the agent extracted in this topic</p>
          <button type="button" className="mt-1 text-sm underline" onClick={() => openQuery(`${t} extract:receipts`)}>The receipts</button>
        </section>
        <section className="border border-border bg-card p-4" aria-labelledby="tp-cards">
          <h2 id="tp-cards" className="font-semibold">Cards</h2>
          {cards == null ? <p className="text-sm text-muted-foreground">Loading…</p> : cards.length === 0 ? <p className="text-sm text-muted-foreground">No cards labelled {topic}.</p> : (
            <ul className="mt-1 space-y-1 text-sm">
              {cards.slice(0, 6).map(c => <li key={c.id}>{c.title} <span className="text-muted-foreground">· {c.status}</span></li>)}
            </ul>
          )}
          <button type="button" className="mt-1 text-sm underline" onClick={() => openTool('board', 'Board')}>The board</button>
        </section>
        <section className="border border-border bg-card p-4" aria-labelledby="tp-people">
          <h2 id="tp-people" className="font-semibold">People</h2>
          {people == null ? <p className="text-sm text-muted-foreground">Loading…</p> : (
            <ul className="mt-1 space-y-1 text-sm">
              {people.map(p => <li key={p.label} className="truncate"><button type="button" className="underline" onClick={() => openQuery(`${t} from:=${p.label}`)}>{p.label}</button> <span className="text-muted-foreground">· {p.value}</span></li>)}
            </ul>
          )}
        </section>
      </div>

      <Stream query={t} title={`Mail about ${topic}`} />

      <Evidence>
        <h2 className="mb-1 text-base font-semibold">How this topic is made</h2>
        <p className="text-sm"><code className="text-xs">{t}</code> — conversations the agent's <code className="text-xs">topics</code> element tagged {topic}. A conversation it has not read yet is not here.</p>
        <p className="mt-2 text-sm">Cards are those labelled <code className="text-xs">{topic}</code>: card labels and mail topics are one vocabulary.</p>
      </Evidence>
    </div>
  );
};

/** Every topic in use, most used first. */
export const Topics = () => {
  const [ts, setTs] = useState<TopicCount[] | null>(null);
  useEffect(() => { const c = new AbortController(); listTopics(200, c.signal).then(v => !c.signal.aborted && setTs(v)); return () => c.abort(); }, []);
  return (
    <div className="mx-auto max-w-4xl px-6 py-5">
      <h1 className="text-2xl font-semibold">Topics</h1>
      <p className="mt-1 text-[15px] text-muted-foreground">What your mail is about, as the agent tags it. Each opens as a page.</p>
      {ts == null ? <p className="mt-4 text-sm text-muted-foreground">Loading…</p> : ts.length === 0 ? (
        <p className="mt-4 border border-dashed border-border px-4 py-3 text-sm text-muted-foreground">No topics yet. The agent's topics element tags conversations as it reads them.</p>
      ) : (
        <ul className="mt-4 grid gap-1 sm:grid-cols-3">
          {ts.map(t => (
            <li key={t.topic}>
              <button type="button" className="flex w-full justify-between gap-2 border border-border bg-card px-3 py-2 text-left hover:bg-accent" onClick={() => openTopic(t.topic)}>
                <span className="truncate">{t.topic}</span><span className="tabular-nums text-muted-foreground">{t.count}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};
