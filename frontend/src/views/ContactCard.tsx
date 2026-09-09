import { useCallback, useEffect, useState } from 'react';
import { Bot, Building2, Check, HelpCircle, Mail, User, Users } from 'lucide-react';
import { openQuery } from '../lib/tabs';
import { contactName, runQuery, type Contact } from './Desk/api';

interface ContactTopic {
  topic: string;
  messages: number;
  share: number;
  lift: number;
}

/**
 * One contact, and everything the mailbox knows about them.
 *
 * # Why it reads through the query language
 *
 * The contact, their topics and their correspondents are three queries —
 * `in:contacts email:=x`, `| topics`, `anyone:x | network` — rather than three
 * endpoints. Contacts are an entity in the language, and a second read path
 * would be a second set of filtering and ordering semantics to keep in step
 * with the first.
 *
 * The one thing that is not a query is the correction, because the language
 * reads and does not write.
 */
export const ContactCard = ({ address }: { address: string }) => {
  const [contact, setContact] = useState<Contact | null>(null);
  const [topics, setTopics] = useState<ContactTopic[]>([]);
  const [network, setNetwork] = useState<{ label: string; value: number }[]>([]);
  const [loading, setLoading] = useState(true);
  const [saved, setSaved] = useState(false);

  const quoted = `=${address}`;

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [who, theirTopics, theirNetwork] = await Promise.all([
        runQuery(`in:contacts email:${quoted}`, 1, 0),
        runQuery(`in:contacts email:${quoted} | topics 8`, 8, 0),
        runQuery(`anyone:${address} | network 8`, 8, 0),
      ]);
      setContact(who.contacts?.[0] ?? null);
      setTopics((theirTopics as any).topics ?? []);
      // `| network` returns every pair among the matched messages, which
      // includes pairs between two *other* people who were both cc'd
      // alongside this contact. Those are real edges and they are not this
      // contact's edges, so they do not belong on their card.
      setNetwork((theirNetwork.groups ?? []).filter(g => g.label.includes(address)));
    } catch {
      setContact(null);
    } finally {
      setLoading(false);
    }
  }, [address, quoted]);

  useEffect(() => { load(); }, [load]);

  const correct = async (patch: Partial<Contact>) => {
    await fetch('/api/contacts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ address, ...patch }),
    });
    setSaved(true);
    window.setTimeout(() => setSaved(false), 2000);
    await load();
  };

  if (loading) return <div className="p-8 text-sm text-muted-foreground">Loading…</div>;
  if (!contact) {
    return (
      <div className="p-8 text-sm text-muted-foreground">
        Nothing is known about <span className="font-mono">{address}</span> yet.
      </div>
    );
  }

  return (
    <div className="h-full overflow-auto p-6">
      <div className="mx-auto max-w-3xl space-y-6">
        <header className="flex items-start gap-4">
          <KindIcon kind={contact.kind} />
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-2xl font-bold">{contactName(contact)}</h2>
            <p className="truncate font-mono text-sm text-muted-foreground">{contact.address}</p>
          </div>
          <button
            type="button"
            onClick={() => openQuery(`anyone:${contact.address}`)}
            className="flex shrink-0 items-center gap-1 border border-border px-3 py-1.5 text-xs hover:bg-accent/40"
          >
            <Mail className="h-3 w-3" /> Their mail
          </button>
        </header>

        {/* Counted from the participant edges on every read, never cached —
            which is why they cannot disagree with the mailbox. */}
        <section className="grid grid-cols-4 gap-px border border-border bg-border">
          <Stat label="Messages" value={contact.messages.toLocaleString()} />
          <Stat label="Sent" value={contact.sent.toLocaleString()} />
          <Stat label="Received" value={contact.received.toLocaleString()} />
          <Stat
            label="Last seen"
            value={contact.lastSeen ? new Date(contact.lastSeen).toLocaleDateString() : '—'}
          />
        </section>

        <section className="space-y-2 border border-border bg-card p-4">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
              What they are
            </h3>
            {saved && (
              <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
                <Check className="h-3 w-3" /> Saved
              </span>
            )}
          </div>

          <div className="flex flex-wrap gap-1">
            {(['person', 'organization', 'system', 'unknown'] as const).map(k => (
              <button
                key={k}
                type="button"
                onClick={() => correct({ kind: k })}
                className={`border px-2 py-1 text-xs ${
                  contact.kind === k
                    ? 'border-primary bg-primary/10 text-primary font-semibold'
                    : 'border-border hover:bg-accent/40'
                }`}
              >
                {k}
              </button>
            ))}
          </div>
          <p className="text-[11px] text-muted-foreground">
            {contact.kindSource
              ? <>Currently set by <span className="font-mono">{contact.kindSource}</span>. Choosing here records it as yours, and no rule or model run will change it back.</>
              : <>Not classified. Choosing here records it as yours.</>}
          </p>
        </section>

        {(contact.firstName || contact.lastName || contact.phone || contact.org || contact.title) && (
          <section className="space-y-1 border border-border bg-card p-4">
            <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
              Details
            </h3>
            <dl className="divide-y divide-border/60 text-sm">
              <Detail term="First name" value={contact.firstName} />
              <Detail term="Last name" value={contact.lastName} />
              <Detail term="Phone" value={contact.phone} />
              <Detail term="Organisation" value={contact.org} />
              <Detail term="Title" value={contact.title} />
            </dl>
            {contact.enrichedBy && (
              <p className="pt-1 text-[11px] text-muted-foreground">
                Extracted by <span className="font-mono">{contact.enrichedBy}</span>.
              </p>
            )}
          </section>
        )}

        <section className="space-y-2 border border-border bg-card p-4">
          <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
            Topics
          </h3>
          {topics.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              None recorded. Topics come from an extractor — see the Annotators tab.
            </p>
          ) : (
            <>
              <ul className="divide-y divide-border/60">
                {topics.map(t => (
                  <li key={t.topic} className="flex items-baseline gap-3 py-1.5 text-sm">
                    <button
                      type="button"
                      onClick={() => openQuery(`topic:${t.topic} anyone:${contact.address}`)}
                      className="min-w-0 flex-1 truncate text-left hover:text-primary"
                    >
                      {t.topic}
                    </button>
                    <span className="w-16 text-right font-mono text-xs tabular-nums text-muted-foreground">
                      {t.messages}
                    </span>
                    {/* Lift, not volume: whoever you exchange the most mail
                        with would otherwise top every topic. */}
                    <span className={`w-14 text-right font-mono text-xs tabular-nums ${
                      t.lift >= 1.5 ? 'text-primary font-semibold' : 'text-muted-foreground'
                    }`}>
                      {t.lift.toFixed(1)}×
                    </span>
                  </li>
                ))}
              </ul>
              <p className="text-[11px] text-muted-foreground">
                × is how much more they discuss it than the mailbox does. Ranked by that rather
                than by count, so the answer is not always whoever writes most.
              </p>
            </>
          )}
        </section>

        <section className="space-y-2 border border-border bg-card p-4">
          <h3 className="flex items-center gap-1.5 text-xs font-bold uppercase tracking-wider text-muted-foreground">
            <Users className="h-3 w-3" /> Appears alongside
          </h3>
          {network.length === 0 ? (
            <p className="text-xs text-muted-foreground">Nobody — every message is one-to-one.</p>
          ) : (
            <ul className="divide-y divide-border/60">
              {network.map(edge => (
                <li key={edge.label} className="flex items-baseline gap-3 py-1.5 text-sm">
                  <button
                    type="button"
                    onClick={() => openQuery(`anyone:${other(edge.label, contact.address)}`)}
                    className="min-w-0 flex-1 truncate text-left font-mono text-xs hover:text-primary"
                  >
                    {other(edge.label, contact.address)}
                  </button>
                  <span className="font-mono text-xs tabular-nums text-muted-foreground">
                    {edge.value}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </div>
  );
};

/** The other end of an edge, given one of its ends. */
function other(label: string, self: string): string {
  const ends = label.split(' — ');
  return ends.find(a => a !== self) ?? label;
}

const KindIcon = ({ kind }: { kind: Contact['kind'] }) => {
  const Icon = kind === 'system' ? Bot : kind === 'organization' ? Building2 : kind === 'unknown' ? HelpCircle : User;
  return (
    <span className="flex h-10 w-10 shrink-0 items-center justify-center border border-border bg-muted">
      <Icon className="h-5 w-5 text-muted-foreground" />
    </span>
  );
};

const Stat = ({ label, value }: { label: string; value: string }) => (
  <div className="bg-card p-3">
    <div className="text-[10px] font-bold uppercase tracking-wider text-muted-foreground">{label}</div>
    <div className="font-mono text-lg tabular-nums">{value}</div>
  </div>
);

const Detail = ({ term, value }: { term: string; value?: string }) =>
  value ? (
    <div className="grid grid-cols-[8rem_1fr] gap-3 py-1.5">
      <dt className="text-xs uppercase tracking-wider text-muted-foreground">{term}</dt>
      <dd>{value}</dd>
    </div>
  ) : null;
