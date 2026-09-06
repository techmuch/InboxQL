import { useCallback, useEffect, useState } from 'react';
import { Loader2, Mail, RefreshCw } from 'lucide-react';
import { openQuery } from '../../lib/tabs';

interface TicketSource {
  messageId: string;
  subject?: string;
  from?: string;
  date?: string;
}

interface Ticket {
  id: string;
  title: string;
  status: string;
  priority?: string;
  dueAt?: string;
  origin: string;
  confidence?: number;
  sources?: TicketSource[];
}

interface Column {
  status: string;
  total: number;
  tickets: Ticket[];
}

/**
 * The board.
 *
 * # Filter and columns are separate
 *
 * The filter says which tickets appear; the columns are the values of one
 * designated field. Keeping them apart is what gives dragging a card an
 * unambiguous meaning — it writes that field, which is the inverse of the query
 * that defined the column. If a column were an arbitrary query there would be
 * no answer to what dragging from `due:7d` into `from:acme` should do.
 */
export const Board = () => {
  const [columns, setColumns] = useState<Column[]>([]);
  const [filter, setFilter] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [dragging, setDragging] = useState<string | null>(null);

  const load = useCallback(async (q: string) => {
    setLoading(true);
    setError(null);
    try {
      const params = q ? `?${new URLSearchParams({ q })}` : '';
      const res = await fetch(`/api/tickets/board${params}`);
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.message ?? body.error ?? `${res.status}`);
      }
      setColumns(await res.json());
    } catch (e: any) {
      setError(e.message ?? String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(filter); }, [load, filter]);

  // Dropping a card writes the status. That is the whole interaction: the
  // column is a value of that field, so moving between columns is setting it.
  const move = async (id: string, status: string) => {
    const ticket = columns.flatMap(c => c.tickets).find(t => t.id === id);
    if (!ticket || ticket.status === status) return;

    // Move it locally first so the card does not visibly snap back while the
    // request is in flight; the reload below is the source of truth.
    setColumns(cols => cols.map(c => ({
      ...c,
      tickets: c.status === status
        ? [...c.tickets, { ...ticket, status }]
        : c.tickets.filter(t => t.id !== id),
    })));

    try {
      const res = await fetch('/api/tickets', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...ticket, status }),
      });
      if (!res.ok) throw new Error(`${res.status}`);
    } catch (e: any) {
      setError(`Could not move that ticket: ${e.message ?? e}`);
    } finally {
      load(filter);
    }
  };

  return (
    <div className="flex flex-col h-full bg-background text-foreground">
      <div className="border-b border-border p-3 flex items-center gap-2">
        <input
          value={filter}
          onChange={e => setFilter(e.target.value)}
          spellCheck={false}
          placeholder="Scope the board — priority:high, from:*@acme.com, due:14d"
          aria-label="Board filter"
          className="flex-1 bg-background border border-border px-3 py-1.5 font-mono text-sm
                     outline-none focus:border-primary"
        />
        <button
          type="button"
          onClick={() => load(filter)}
          aria-label="Reload"
          className="p-2 border border-border hover:bg-accent/40"
        >
          <RefreshCw size={14} />
        </button>
      </div>

      {error && <p className="px-3 py-2 text-sm text-destructive border-b border-border">{error}</p>}

      {loading && columns.length === 0 ? (
        <div className="flex items-center justify-center flex-1 text-muted-foreground gap-2">
          <Loader2 size={16} className="animate-spin" /> Loading…
        </div>
      ) : (
        <div className="flex-1 flex gap-3 p-3 overflow-x-auto">
          {columns.map(col => (
            <ColumnView
              key={col.status}
              column={col}
              dragging={dragging}
              onDragStart={setDragging}
              onDrop={id => { move(id, col.status); setDragging(null); }}
            />
          ))}
        </div>
      )}
    </div>
  );
};

const ColumnView = ({
  column,
  dragging,
  onDragStart,
  onDrop,
}: {
  column: Column;
  dragging: string | null;
  onDragStart: (id: string | null) => void;
  onDrop: (id: string) => void;
}) => (
  <section
    onDragOver={e => e.preventDefault()}
    onDrop={e => {
      e.preventDefault();
      const id = e.dataTransfer.getData('text/plain') || dragging;
      if (id) onDrop(id);
    }}
    className="w-72 shrink-0 flex flex-col border border-border bg-card/40"
  >
    <header className="px-3 py-2 border-b border-border flex items-baseline gap-2">
      <h2 className="text-xs font-semibold uppercase tracking-wide">{column.status}</h2>
      <span className="text-xs text-muted-foreground font-mono tabular-nums">{column.total}</span>
    </header>

    <div className="flex-1 overflow-auto p-2 flex flex-col gap-2 min-h-24">
      {column.tickets.length === 0 ? (
        <p className="text-xs text-muted-foreground px-1 py-2">Nothing here.</p>
      ) : (
        column.tickets.map(t => (
          <TicketCard key={t.id} ticket={t} onDragStart={onDragStart} />
        ))
      )}
      {column.total > column.tickets.length && (
        <p className="text-xs text-muted-foreground px-1">
          … {column.total - column.tickets.length} more
        </p>
      )}
    </div>
  </section>
);

const TicketCard = ({
  ticket,
  onDragStart,
}: {
  ticket: Ticket;
  onDragStart: (id: string | null) => void;
}) => {
  const source = ticket.sources?.[0];
  const overdue = ticket.dueAt ? new Date(ticket.dueAt) < new Date() : false;

  return (
    <article
      draggable
      onDragStart={e => {
        e.dataTransfer.setData('text/plain', ticket.id);
        onDragStart(ticket.id);
      }}
      onDragEnd={() => onDragStart(null)}
      className="bg-background border border-border p-2 cursor-grab active:cursor-grabbing
                 hover:border-primary/50 flex flex-col gap-1.5"
    >
      <p className="text-sm leading-snug">{ticket.title}</p>

      <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
        {ticket.dueAt && (
          <span className={overdue ? 'text-destructive font-medium' : ''}>
            {overdue ? 'overdue ' : 'due '}
            {new Date(ticket.dueAt).toLocaleDateString()}
          </span>
        )}
        {ticket.priority && <span className="uppercase tracking-wide">{ticket.priority}</span>}
        {ticket.origin === 'annotator' && ticket.confidence !== undefined && (
          <span className="font-mono">{Math.round(ticket.confidence * 100)}%</span>
        )}
      </div>

      {/* Provenance, on the card itself. A ticket that cannot be traced back to
          the mail that made it is a task in a worse task manager. */}
      {source && (
        <button
          type="button"
          onClick={() => openQuery(`thread:${source.messageId}`)}
          title={source.subject}
          className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-primary text-left"
        >
          <Mail size={10} className="shrink-0" />
          <span className="truncate">{source.from || source.subject}</span>
        </button>
      )}
    </article>
  );
};
