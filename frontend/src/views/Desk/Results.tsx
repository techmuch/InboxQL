import { Inbox } from 'lucide-react';
import { openAttachment, openContact, openMessage, previewMessage } from '../../lib/tabs';
import { navRow } from '../../lib/rovingFocus';
import { refKey, useSelectionStore } from '../../lib/selection';
import { contactName, drillDownTerm, type AttachmentFile, type Contact, type QueryResult } from './api';
import { fileTypeLabel, formatBytes } from '../AttachmentPreview';
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
      return <ContactResult contacts={contacts} />;
    }
    case 'threads': {
      const threads = result.threads ?? [];
      if (threads.length === 0) return <Empty label="No conversations matched" />;
      return <ThreadResult threads={threads} onDrillDown={onDrillDown} />;
    }
    case 'attachments': {
      const files = result.attachments ?? [];
      if (files.length === 0) return <Empty label="No files matched" />;
      return <AttachmentResult files={files} onDrillDown={onDrillDown} />;
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
  const selection = useSelectionStore(s => s.refs);
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
            const selected = Boolean(selection[refKey({ kind: 'group', id: g.label })]);
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
                aria-selected={selected}
                onClick={() => term && onDrillDown(term)}
                title={term ? `Narrow to ${term}` : undefined}
                className={`border-b border-border/50 transition-colors ${
                  selected
                    ? 'bg-primary/10 hover:bg-primary/15 dark:bg-primary/20 dark:hover:bg-primary/25 ring-1 ring-inset ring-primary/30'
                    : term
                    ? 'cursor-pointer hover:bg-accent/40'
                    : ''
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
  const selection = useSelectionStore(s => s.refs);
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
            const selected = Boolean(selection[refKey({ kind: 'message', id: m.id })]);
            return (
              <tr
                key={m.id}
                {...navRow}
                data-sel-kind="message"
                data-sel-id={m.id}
                data-sel-field="id"
                data-sel-label={m.subject}
                aria-selected={selected}
                onFocus={() => previewMessage(m)}
                onClick={(e) => {
                  if (e.shiftKey || e.metaKey || e.ctrlKey) return;
                  openMessage(m);
                }}
                className={`border-b border-border/50 cursor-pointer transition-colors ${
                  selected
                    ? 'bg-primary/10 hover:bg-primary/15 dark:bg-primary/20 dark:hover:bg-primary/25 ring-1 ring-inset ring-primary/30'
                    : 'hover:bg-accent/40'
                }`}
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
  const selection = useSelectionStore(s => s.refs);
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
            const selected = Boolean(selection[refKey({ kind: 'ticket', id: t.id })]);
            return (
              <tr
                key={t.id}
                {...navRow}
                data-sel-kind="ticket"
                data-sel-id={t.id}
                data-sel-field="id"
                data-sel-label={t.title}
                aria-selected={selected}
                // Activating a ticket opens the conversation it came from,
                // which is the whole reason provenance is recorded.
                onClick={() => t.threadKey && onDrillDown(`thread:${t.threadKey}`, 'timeline')}
                title={t.threadKey ? 'Show the conversation this came from' : undefined}
                className={`border-b border-border/50 transition-colors ${
                  selected
                    ? 'bg-primary/10 hover:bg-primary/15 dark:bg-primary/20 dark:hover:bg-primary/25 ring-1 ring-inset ring-primary/30'
                    : 'hover:bg-accent/40'
                } ${t.threadKey ? 'cursor-pointer' : ''}`}
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
  const selection = useSelectionStore(s => s.refs);
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
          {drafts.map(d => {
            const selected = Boolean(selection[refKey({ kind: 'draft', id: d.id })]);
            return (
              <tr
                key={d.id}
                {...navRow}
                data-sel-kind="draft"
                data-sel-id={d.id}
                data-sel-field="id"
                data-sel-label={d.subject}
                aria-selected={selected}
                className={`border-b border-border/50 transition-colors ${
                  selected
                    ? 'bg-primary/10 hover:bg-primary/15 dark:bg-primary/20 dark:hover:bg-primary/25 ring-1 ring-inset ring-primary/30'
                    : 'hover:bg-accent/40'
                }`}
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
            );
          })}
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
/**
 * Files, one row per file rather than per arrival.
 *
 * # Why the reach column is mostly blank
 *
 * Most files arrived once, and "1 message" on every row would be a column of
 * noise that hides the handful of rows where the number is the point. A blank
 * says "once", which is the unremarkable case, and anything written there is
 * worth reading.
 *
 * Clicking a row opens the preview in the viewer, the way clicking a message
 * opens the message. Clicking the type narrows to that kind of file, which is
 * the same drill-down an aggregate row offers.
 */
const AttachmentResult = ({ files, onDrillDown }: {
  files: AttachmentFile[];
  onDrillDown?: (term: string) => void;
}) => (
  <div className="overflow-auto h-full">
    <table className="w-full text-sm">
      <thead className="sticky top-0 bg-background border-b border-border">
        <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
          <th className="px-4 py-2 font-medium">Name</th>
          <th className="px-4 py-2 font-medium w-24">Type</th>
          <th className="px-4 py-2 font-medium w-24 text-right">Size</th>
          <th className="px-4 py-2 font-medium w-40">Reach</th>
          <th className="px-4 py-2 font-medium w-28">Last seen</th>
        </tr>
      </thead>
      <tbody>
        {files.map(f => (
          <tr
            key={f.key}
            {...navRow}
            onClick={() => openAttachment(f)}
            title={f.storagePath ? `Open ${f.filename}` : f.skipped || 'The bytes were not kept'}
            className="border-b border-border/50 hover:bg-accent/40 cursor-pointer"
          >
            <td className="px-4 py-2">
              <span className={`truncate block ${f.storagePath ? '' : 'text-muted-foreground italic'}`}>
                {f.filename}
              </span>
              {/* The mail it last came on, so a filename like "scan.pdf" is
                  identifiable without opening it. */}
              {f.subject && (
                <span className="truncate block text-xs text-muted-foreground">{f.subject}</span>
              )}
            </td>
            <td className="px-4 py-2">
              <button
                type="button"
                onClick={e => { e.stopPropagation(); onDrillDown?.(`type:${fileTypeLabel(f.mimeType)}`); }}
                className="text-muted-foreground hover:text-primary"
              >
                {fileTypeLabel(f.mimeType)}
              </button>
            </td>
            <td className="px-4 py-2 text-right font-mono text-xs tabular-nums text-muted-foreground">
              {formatBytes(f.size)}
            </td>
            <td className="px-4 py-2 text-xs text-muted-foreground">
              {f.messages > 1 && (
                <>
                  {f.messages} messages
                  {f.threads > 1 && <> · {f.threads} threads</>}
                </>
              )}
              {!f.storagePath && <span className="italic">not stored</span>}
              {f.storagePath && f.textStatus === 'empty' && (
                // Worth a word: without it a scan looks like a file whose
                // contents did not match, rather than one nothing has read.
                <span className="italic">scanned</span>
              )}
            </td>
            <td className="px-4 py-2 text-xs text-muted-foreground">
              {f.lastSeen ? new Date(f.lastSeen).toLocaleDateString() : ''}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  </div>
);

const ContactResult = ({ contacts }: { contacts: Contact[] }) => {
  const selection = useSelectionStore(s => s.refs);
  return (
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
          {contacts.map(c => {
            const selected = Boolean(selection[refKey({ kind: 'contact', id: c.address })]);
            return (
              <tr
                key={c.address}
                {...navRow}
                data-sel-kind="contact"
                data-sel-id={c.address}
                data-sel-field="email"
                data-sel-label={contactName(c)}
                aria-selected={selected}
                // Opens the card, the way clicking a message opens the message.
                // Narrowing to their mail is an explicit action on the card —
                // clicking a person's name should show you the person.
                onClick={() => openContact(c.address)}
                title="Open this contact"
                className={`border-b border-border/50 cursor-pointer transition-colors ${
                  selected
                    ? 'bg-primary/10 hover:bg-primary/15 dark:bg-primary/20 dark:hover:bg-primary/25 ring-1 ring-inset ring-primary/30'
                    : 'hover:bg-accent/40'
                }`}
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
        );
      })}
    </tbody>
  </table>
</div>
);
};
