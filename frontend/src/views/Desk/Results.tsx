import { Inbox } from 'lucide-react';
import { openMessage, previewMessage } from '../../lib/tabs';
import { navRow } from '../../lib/rovingFocus';
import { contactName, drillDownTerm, type Contact, type QueryResult } from './api';
import { ThreadResult } from './Timeline';

interface ResultsProps {
  result: QueryResult;
  /**
   * Narrow the query to one row.
   *
   * The stage is separate from the term on purpose. Passing
   * `"thread:X | timeline"` as a term happens to work on a query with no
   * pipeline and produces two terminal stages on one that has one, which the
   * planner rejects — a term and a stage are different halves of a query.
   */
  onDrillDown: (terms: string | string[], stage?: string) => void;
}

/**
 * The result pane.
 *
 * One endpoint returns three shapes, and `kind` says which — so this switches
 * rather than guessing from whether an array happens to be populated. Getting
 * that wrong would render an aggregate as an empty message list, which reads as
 * "nothing matched" rather than "you asked a different question".
 */
export const Results = ({ result, onDrillDown }: ResultsProps) => {
  switch (result.kind) {
    case 'count':
      return <CountResult total={result.total ?? 0} />;
    case 'groups':
      return <GroupResult result={result} onDrillDown={onDrillDown} />;
    case 'tickets':
      return <TicketResult result={result} onDrillDown={onDrillDown} />;
    case 'drafts':
      return <DraftResult result={result} />;
    case 'contacts': {
      const contacts = result.contacts ?? [];
      if (contacts.length === 0) return <Empty label="No contacts matched" />;
      return <ContactResult contacts={contacts} onDrillDown={onDrillDown} />;
    }
    case 'threads': {
      const threads = result.threads ?? [];
      if (threads.length === 0) return <Empty label="No conversations matched" />;
      return <ThreadResult threads={threads} onDrillDown={onDrillDown} />;
    }
    default:
      return <MessageResult result={result} />;
  }
};

const CountResult = ({ total }: { total: number }) => (
  <div className="flex flex-col items-center justify-center h-full gap-1 py-16">
    <div className="font-mono text-5xl font-semibold tabular-nums">{total.toLocaleString()}</div>
    <div className="text-sm text-muted-foreground">
      {total === 1 ? 'message matches' : 'messages match'}
    </div>
  </div>
);

/**
 * Aggregate results, as a table with the magnitude drawn behind each row.
 *
 * The bar is a background on the label cell rather than a charting library:
 * one dependency less, it stays legible at any row count, and it lines up with
 * the number it describes instead of sitting in a separate panel.
 */
const GroupResult = ({ result, onDrillDown }: ResultsProps) => {
  const groups = result.groups ?? [];
  if (groups.length === 0) return <Empty label="No groups" />;

  const max = Math.max(...groups.map(g => Math.abs(g.value)), 1);
  const isNumeric = groups.every(g => Number.isInteger(g.value));

  return (
    <div className="overflow-auto h-full">
      <table className="w-full text-sm">
        <thead className="sticky top-0 bg-background border-b border-border">
          <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
            <th className="px-4 py-2 font-medium">{result.groupField ?? 'Group'}</th>
            <th className="px-4 py-2 font-medium text-right w-32">Value</th>
          </tr>
        </thead>
        <tbody>
          {groups.map(g => {
            const term = drillDownTerm(result, g.label);
            const width = `${Math.max((Math.abs(g.value) / max) * 100, 1)}%`;
            return (
              <tr
                key={g.label}
                {...(term ? navRow : {})}
                // An aggregate bucket is named by the field it grouped on, so
                // narrowing to several buckets is one term on that field.
                data-sel-kind="group"
                data-sel-id={g.label}
                data-sel-field={result.groupField}
                data-sel-label={g.label}
                onClick={() => term && onDrillDown(term)}
                title={term ? `Narrow to ${term}` : undefined}
                className={`border-b border-border/50 ${
                  term ? 'cursor-pointer hover:bg-accent/40' : ''
                }`}
              >
                <td className="px-4 py-1.5 relative">
                  <span
                    aria-hidden="true"
                    className="absolute inset-y-0.5 left-0 bg-primary/15"
                    style={{ width }}
                  />
                  <span className="relative font-mono">{g.label || '(none)'}</span>
                </td>
                <td className="px-4 py-1.5 text-right font-mono tabular-nums">
                  {isNumeric ? g.value.toLocaleString() : g.value.toFixed(2)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};

const MessageResult = ({ result }: { result: QueryResult }) => {
  const messages = result.messages ?? [];
  if (messages.length === 0) return <Empty label="No messages matched" />;

  return (
    <div className="overflow-auto h-full">
      <table className="w-full text-sm">
        <thead className="sticky top-0 bg-background border-b border-border">
          <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
            <th className="px-4 py-2 font-medium w-28">Date</th>
            <th className="px-4 py-2 font-medium w-64">From</th>
            <th className="px-4 py-2 font-medium">Subject</th>
          </tr>
        </thead>
        <tbody>
          {messages.map(m => {
            const unread = !(m.flags ?? []).includes('\\Seen');
            return (
              <tr
                key={m.id}
                {...navRow}
                data-sel-kind="message"
                data-sel-id={m.id}
                data-sel-field="id"
                data-sel-label={m.subject}
                onFocus={() => previewMessage(m)}
                onClick={(e) => {
                  if (e.shiftKey || e.metaKey || e.ctrlKey) return;
                  openMessage(m);
                }}
                className="border-b border-border/50 cursor-pointer hover:bg-accent/40"
              >
                <td className="px-4 py-1.5 font-mono text-xs text-muted-foreground whitespace-nowrap">
                  {new Date(m.date).toLocaleDateString()}
                </td>
                <td className="px-4 py-1.5 truncate max-w-0">{m.from}</td>
                <td className={`px-4 py-1.5 truncate max-w-0 ${unread ? 'font-medium' : ''}`}>
                  {m.subject || <span className="text-muted-foreground">(no subject)</span>}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};

/**
 * Tickets, with the mail behind each one.
 *
 * The evidence column is not decoration: a ticket that cannot be traced back to
 * the message that made it is a task in a worse task manager.
 */
const TicketResult = ({ result, onDrillDown }: ResultsProps) => {
  const tickets = result.tickets ?? [];
  if (tickets.length === 0) return <Empty label="No tickets matched" />;

  return (
    <div className="overflow-auto h-full">
      <table className="w-full text-sm">
        <thead className="sticky top-0 bg-background border-b border-border">
          <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
            <th className="px-4 py-2 font-medium w-24">Status</th>
            <th className="px-4 py-2 font-medium w-28">Due</th>
            <th className="px-4 py-2 font-medium">Title</th>
            <th className="px-4 py-2 font-medium w-56">From this mail</th>
          </tr>
        </thead>
        <tbody>
          {tickets.map(t => {
            const overdue = t.dueAt ? new Date(t.dueAt) < new Date() : false;
            const source = t.sources?.[0];
            return (
              <tr
                key={t.id}
                {...navRow}
                data-sel-kind="ticket"
                data-sel-id={t.id}
                data-sel-field="id"
                data-sel-label={t.title}
                // Activating a ticket opens the conversation it came from,
                // which is the whole reason provenance is recorded.
                onClick={() => t.threadKey && onDrillDown(`thread:${t.threadKey}`, 'timeline')}
                title={t.threadKey ? 'Show the conversation this came from' : undefined}
                className={`border-b border-border/50 hover:bg-accent/40 ${
                  t.threadKey ? 'cursor-pointer' : ''
                }`}
              >
                <td className="px-4 py-1.5 font-mono text-xs">{t.status}</td>
                <td className={`px-4 py-1.5 font-mono text-xs whitespace-nowrap ${
                  overdue ? 'text-destructive' : 'text-muted-foreground'
                }`}>
                  {t.dueAt ? new Date(t.dueAt).toLocaleDateString() : ''}
                </td>
                <td className="px-4 py-1.5 truncate max-w-0">{t.title}</td>
                <td className="px-4 py-1.5 truncate max-w-0 text-xs text-muted-foreground">
                  {source ? (source.from || source.subject) : '—'}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};

/** Unsent drafts, which are their own entity rather than mail. */
const DraftResult = ({ result }: { result: QueryResult }) => {
  const drafts = result.drafts ?? [];
  if (drafts.length === 0) return <Empty label="No drafts matched" />;

  return (
    <div className="overflow-auto h-full">
      <table className="w-full text-sm">
        <thead className="sticky top-0 bg-background border-b border-border">
          <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
            <th className="px-4 py-2 font-medium w-24">Status</th>
            <th className="px-4 py-2 font-medium w-20">By</th>
            <th className="px-4 py-2 font-medium w-56">To</th>
            <th className="px-4 py-2 font-medium">Subject</th>
          </tr>
        </thead>
        <tbody>
          {drafts.map(d => (
            <tr
              key={d.id}
              {...navRow}
              data-sel-kind="draft"
              data-sel-id={d.id}
              data-sel-field="id"
              data-sel-label={d.subject}
              className="border-b border-border/50 hover:bg-accent/40"
            >
              <td className="px-4 py-1.5 font-mono text-xs">{d.status}</td>
              <td className={`px-4 py-1.5 font-mono text-xs ${
                d.origin === 'agent' ? 'text-primary' : 'text-muted-foreground'
              }`}>
                {d.origin}
              </td>
              <td className="px-4 py-1.5 truncate max-w-0">{(d.to ?? []).join(', ')}</td>
              <td className="px-4 py-1.5 truncate max-w-0">
                {d.subject || <span className="text-muted-foreground">(no subject)</span>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};

const Empty = ({ label }: { label: string }) => (
  <div className="flex flex-col items-center justify-center h-full gap-2 py-16 text-muted-foreground">
    <Inbox size={28} className="opacity-40" />
    <p className="text-sm">{label}</p>
  </div>
);

/**
 * Contacts.
 *
 * Every message already created these — message_participants has recorded one
 * row per address since v15. What was missing was somewhere to put a name and
 * a way to look at them.
 *
 * Activating a contact narrows the query to their mail, which is what a
 * contact is *for*: the address is the least interesting thing about them.
 */
const ContactResult = ({
  contacts,
  onDrillDown,
}: {
  contacts: Contact[];
  onDrillDown: (terms: string | string[], stage?: string) => void;
}) => (
  <div className="overflow-auto h-full">
    <table className="w-full text-sm">
      <thead className="sticky top-0 bg-background border-b border-border">
        <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
          <th className="px-4 py-2 font-medium">Name</th>
          <th className="px-4 py-2 font-medium w-64">Address</th>
          <th className="px-4 py-2 font-medium w-28">Kind</th>
          <th className="px-4 py-2 font-medium w-20 text-right">Messages</th>
          <th className="px-4 py-2 font-medium w-28">Last seen</th>
        </tr>
      </thead>
      <tbody>
        {contacts.map(c => (
          <tr
            key={c.address}
            {...navRow}
            data-sel-kind="contact"
            data-sel-id={c.address}
            data-sel-field="email"
            data-sel-label={contactName(c)}
            // Two terms, not one string: the entity has to move as well.
            // `anyone:x` alone leaves `in:contacts` in place, which filters
            // the contact list to their correspondents — a real answer to a
            // question nobody asked by clicking a person's name.
            onClick={() => onDrillDown([`anyone:${c.address}`, 'in:messages'])}
            title="Show their mail"
            className="border-b border-border/50 cursor-pointer hover:bg-accent/40"
          >
            <td className="px-4 py-1.5 truncate max-w-0">{contactName(c)}</td>
            <td className="px-4 py-1.5 truncate max-w-0 font-mono text-xs text-muted-foreground">
              {c.address}
            </td>
            <td className="px-4 py-1.5">
              {/* System is the one worth seeing at a glance: it is the reason
                  a "contact" list is not just an address book. */}
              <span className={`px-1.5 py-0.5 text-[11px] font-mono ${
                c.kind === 'system'
                  ? 'bg-amber-500/10 text-amber-600 dark:text-amber-400'
                  : c.kind === 'unknown'
                    ? 'text-muted-foreground'
                    : 'bg-green-500/10 text-green-600 dark:text-green-400'
              }`}>
                {c.kind}
              </span>
            </td>
            <td className="px-4 py-1.5 text-right font-mono tabular-nums">
              {c.messages.toLocaleString()}
            </td>
            <td className="px-4 py-1.5 font-mono text-xs text-muted-foreground whitespace-nowrap">
              {c.lastSeen ? new Date(c.lastSeen).toLocaleDateString() : ''}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  </div>
);
