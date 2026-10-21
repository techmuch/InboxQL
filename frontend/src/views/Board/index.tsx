import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  AlignLeft, Archive, CheckSquare, Loader2, Mail, MessageSquare, MoreHorizontal, Paperclip, Plus,
  RefreshCw, Workflow, X,
} from 'lucide-react';
import { Help } from '../Help';
import {
  archiveCard, createCard, createList, deleteList, editList, fetchBoards, fetchColumns, fetchLabels,
  fetchLists, moveCard,
  type BoardDef, type Column, type LabelInfo, type List, type Ticket,
} from './api';
import { CardPanel } from './CardPanel';
import { BoardPicker } from './BoardPicker';
import { ArchivedDialog, RulesDialog } from './BoardDialogs';
import { labelClass, useBoardNav } from './labels';

const BOARD_KEY = 'iql.board';

const remembered = (): string => {
  try { return localStorage.getItem(BOARD_KEY) ?? 'all'; } catch { return 'all'; }
};

/**
 * The board.
 *
 * # Filter and lists are separate
 *
 * The filter says which cards appear; the columns are the lists, which are
 * the values of one field. Keeping them apart is what gives dragging a card
 * an unambiguous meaning — it writes the card's list (and its place in it),
 * which is the inverse of the query that defined the column. If a column were
 * an arbitrary query there would be no answer to what dragging from `due:7d`
 * into `from:acme` should do.
 *
 * A board is a named filter over the same lists; the box narrows it further.
 */
export const Board = () => {
  const [boards, setBoards] = useState<BoardDef[]>([]);
  const [boardId, setBoardId] = useState(remembered);
  const [columns, setColumns] = useState<Column[]>([]);
  const [lists, setLists] = useState<List[]>([]);
  const [labels, setLabels] = useState<LabelInfo[]>([]);
  const [filter, setFilter] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [dragging, setDragging] = useState<string | null>(null);
  const [drop, setDrop] = useState<{ list: string; after: string } | null>(null);
  const [dialog, setDialog] = useState<'rules' | 'archived' | null>(null);
  const openCard = useBoardNav(s => s.openCard);
  const setOpenCard = useBoardNav(s => s.setOpenCard);

  const board = boards.find(b => b.id === boardId) ?? boards[0];
  // The board as it is now, including a move still in flight. Keys read this
  // rather than the render they were bound in, so a second Shift+→ pressed
  // before the first has redrawn starts from where the card now is.
  const columnsRef = useRef<Column[]>([]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const cols = await fetchColumns(board?.id ?? '', filter);
      columnsRef.current = cols;
      setColumns(cols);
      setError(null);
      // A rule or a harness may have made a label since the board opened;
      // without its colour it would draw grey until the next visit.
      fetchLabels().then(r => setLabels(r.labels)).catch(() => {});
    } catch (e: any) {
      setError(e.message ?? String(e));
    } finally {
      setLoading(false);
    }
  }, [board?.id, filter]);

  const loadMeta = useCallback(() => {
    fetchBoards().then(setBoards).catch(e => setError(e.message));
    fetchLists().then(setLists).catch(() => {});
    fetchLabels().then(r => setLabels(r.labels)).catch(() => {});
  }, []);

  useEffect(loadMeta, [loadMeta]);
  useEffect(() => { if (boards.length) void load(); }, [load, boards.length]);

  const chooseBoard = (id: string) => {
    setBoardId(id);
    try { localStorage.setItem(BOARD_KEY, id); } catch { /* per-viewer only */ }
  };

  const colorOf = useMemo(() => new Map(labels.map(l => [l.name, l.color])), [labels]);

  // Where a dragged card lands: after the card above the pointer, or at the
  // top. Worked out from the cards on screen, and sent as "after this card",
  // which the server resolves in the whole list — so a filtered board still
  // puts it where it was dropped.
  const place = (list: string, id: string, after: string) => {
    const moving = columnsRef.current.flatMap(c => c.tickets).find(t => t.id === id);
    if (!moving) return;
    const next = columnsRef.current.map(c => {
      let tickets = c.tickets.filter(t => t.id !== id);
      if (c.status === list) {
        const at = after === '^' ? 0 : after === '' ? tickets.length : tickets.findIndex(t => t.id === after) + 1;
        tickets = [...tickets.slice(0, at), { ...moving, status: list }, ...tickets.slice(at)];
      }
      return { ...c, tickets };
    });
    columnsRef.current = next;
    setColumns(next);
    moveCard(id, list, after)
      .catch(e => setError(`Could not move that card: ${e.message ?? e}`))
      .finally(() => { void load(); });
  };

  const onKeyCard = (e: React.KeyboardEvent, id: string) => {
    const columns = columnsRef.current;
    const ci = columns.findIndex(c => c.tickets.some(t => t.id === id));
    if (ci < 0) return;
    const col = columns[ci];
    const i = col.tickets.findIndex(t => t.id === id);
    const t = col.tickets[i];
    const focus = (status: string, index: number) => {
      requestAnimationFrame(() => {
        const cards = document.querySelectorAll<HTMLElement>(`[data-list="${CSS.escape(status)}"] [data-card]`);
        cards[Math.max(0, Math.min(index, cards.length - 1))]?.focus();
      });
    };
    switch (e.key) {
      case 'Enter':
      case ' ':
        e.preventDefault();
        setOpenCard(t.id);
        return;
      case 'ArrowDown':
      case 'ArrowUp': {
        e.preventDefault();
        const j = i + (e.key === 'ArrowDown' ? 1 : -1);
        if (j < 0 || j >= col.tickets.length) return;
        if (e.shiftKey) {
          // Reorder: after the card below, or after the one two above.
          const after = e.key === 'ArrowDown' ? col.tickets[j].id : (j === 0 ? '^' : col.tickets[j - 1].id);
          place(col.status, t.id, after);
        }
        focus(col.status, j);
        return;
      }
      case 'ArrowLeft':
      case 'ArrowRight': {
        e.preventDefault();
        const next = columns[ci + (e.key === 'ArrowRight' ? 1 : -1)];
        if (!next) return;
        if (e.shiftKey) {
          place(next.status, t.id, '');
          focus(next.status, next.tickets.length);
        } else {
          focus(next.status, i);
        }
        return;
      }
      case 'c':
        // Trello's archive key. Reversible, and the toast says how.
        e.preventDefault();
        archiveCard(t.id, true).then(() => load()).catch(err => setError(err.message));
        return;
    }
  };

  return (
    <div className="relative flex flex-col h-full bg-background text-foreground">
      <div className="border-b border-border p-3 flex flex-wrap items-center gap-2">
        <BoardPicker
          boards={boards}
          current={board}
          onChoose={chooseBoard}
          onChanged={() => { loadMeta(); void load(); }}
        />
        <input
          value={filter}
          onChange={e => setFilter(e.target.value)}
          spellCheck={false}
          placeholder="Narrow it — label:home, due:14d, from:*@acme.com, words"
          aria-label="Board filter"
          className="flex-1 min-w-48 bg-background border border-border px-3 py-1.5 font-mono text-sm
                     outline-none focus:border-primary"
        />
        <button type="button" onClick={() => setDialog('rules')} className={toolButton} title="Automation rules">
          <Workflow size={14} /> Automation
        </button>
        <button type="button" onClick={() => setDialog('archived')} className={toolButton} title="Archived cards and lists">
          <Archive size={14} /> Archived
        </button>
        <button type="button" onClick={() => { loadMeta(); void load(); }} aria-label="Reload"
          className="p-2 border border-border hover:bg-accent/40">
          <RefreshCw size={14} />
        </button>
        <Help topic="board" className="ml-1" />
      </div>

      {error && (
        <p className="px-3 py-2 text-sm text-destructive border-b border-border flex items-center gap-2">
          <span className="flex-1">{error}</span>
          <button type="button" aria-label="Dismiss" onClick={() => setError(null)}><X size={14} /></button>
        </p>
      )}

      {loading && columns.length === 0 ? (
        <div className="flex items-center justify-center flex-1 text-muted-foreground gap-2">
          <Loader2 size={16} className="animate-spin" /> Loading…
        </div>
      ) : (
        <div className="flex-1 flex items-start gap-3 p-3 overflow-x-auto">
          {columns.map((col, ci) => (
            <ColumnView
              key={col.status}
              column={col}
              first={ci === 0}
              last={ci === columns.length - 1}
              neighbours={columns}
              colorOf={colorOf}
              dragging={dragging}
              drop={drop?.list === col.status ? drop.after : null}
              boardLabels={board?.labels ?? []}
              onDragStart={setDragging}
              onDragOver={after => setDrop({ list: col.status, after })}
              onDragLeave={() => setDrop(d => (d?.list === col.status ? null : d))}
              onDrop={(id, after) => { setDragging(null); setDrop(null); place(col.status, id, after); }}
              onOpen={setOpenCard}
              onKeyCard={onKeyCard}
              onChanged={() => { loadMeta(); void load(); }}
              onError={setError}
            />
          ))}
          <AddList
            after={columns[columns.length - 1]?.status ?? ''}
            onAdded={() => { loadMeta(); void load(); }}
            onError={setError}
          />
        </div>
      )}

      {openCard && (
        <CardPanel
          id={openCard}
          lists={lists}
          labels={labels}
          onClose={() => setOpenCard(null)}
          onChanged={() => { void load(); }}
          onLabelsChanged={() => fetchLabels().then(r => setLabels(r.labels)).catch(() => {})}
        />
      )}
      {dialog === 'rules' && <RulesDialog lists={lists} onClose={() => { setDialog(null); void load(); }} />}
      {dialog === 'archived' && (
        <ArchivedDialog
          onOpen={id => { setDialog(null); setOpenCard(id); }}
          onClose={() => { setDialog(null); loadMeta(); void load(); }}
        />
      )}
    </div>
  );
};

const toolButton = 'flex items-center gap-1.5 px-2.5 py-1.5 text-sm border border-border hover:bg-accent/40';

const ColumnView = ({
  column, first, last, neighbours, colorOf, dragging, drop, boardLabels,
  onDragStart, onDragOver, onDragLeave, onDrop, onOpen, onKeyCard, onChanged, onError,
}: {
  column: Column;
  first: boolean;
  last: boolean;
  neighbours: Column[];
  colorOf: Map<string, string>;
  dragging: string | null;
  /** Where a drop would land in this list: a card id, "^" for the top. */
  drop: string | null;
  boardLabels: string[];
  onDragStart: (id: string | null) => void;
  onDragOver: (after: string) => void;
  onDragLeave: () => void;
  onDrop: (id: string, after: string) => void;
  onOpen: (id: string) => void;
  onKeyCard: (e: React.KeyboardEvent, id: string) => void;
  onChanged: () => void;
  onError: (msg: string) => void;
}) => {
  const body = useRef<HTMLDivElement>(null);

  // The card whose middle is above the pointer is the one dropped after.
  const afterAt = (y: number): string => {
    const cards = Array.from(body.current?.querySelectorAll<HTMLElement>('[data-card]') ?? []);
    let after = '^';
    for (const c of cards) {
      const r = c.getBoundingClientRect();
      if (y > r.top + r.height / 2) after = c.dataset.card ?? after;
    }
    return after;
  };

  return (
    <section
      data-list={column.status}
      aria-label={column.title}
      onDragOver={e => { e.preventDefault(); onDragOver(afterAt(e.clientY)); }}
      onDragLeave={e => { if (!e.currentTarget.contains(e.relatedTarget as Node)) onDragLeave(); }}
      onDrop={e => {
        e.preventDefault();
        const id = e.dataTransfer.getData('text/plain') || dragging;
        if (id) onDrop(id, afterAt(e.clientY));
      }}
      className="w-72 shrink-0 max-h-full flex flex-col border border-border bg-card/40"
    >
      <ListHeader column={column} first={first} last={last} neighbours={neighbours}
        onChanged={onChanged} onError={onError} />

      <div ref={body} className="flex-1 overflow-auto p-2 flex flex-col gap-2 min-h-16">
        {drop === '^' && dragging && <DropMark />}
        {column.tickets.map(t => (
          <div key={t.id} className="flex flex-col gap-2">
            <TicketCard
              ticket={t}
              colorOf={colorOf}
              closed={column.kind === 'closed'}
              dragging={dragging === t.id}
              onDragStart={onDragStart}
              onOpen={() => onOpen(t.id)}
              onKeyDown={e => onKeyCard(e, t.id)}
            />
            {drop === t.id && dragging && dragging !== t.id && <DropMark />}
          </div>
        ))}
        {column.total > column.tickets.length && (
          <p className="text-xs text-muted-foreground px-1">… {column.total - column.tickets.length} more</p>
        )}
      </div>

      <QuickAdd list={column.status} labels={boardLabels} onAdded={onChanged} onError={onError} />
    </section>
  );
};

const DropMark = () => <div className="h-1 bg-primary" aria-hidden />;

const ListHeader = ({
  column, first, last, neighbours, onChanged, onError,
}: {
  column: Column;
  first: boolean;
  last: boolean;
  neighbours: Column[];
  onChanged: () => void;
  onError: (msg: string) => void;
}) => {
  const [renaming, setRenaming] = useState(false);
  const [title, setTitle] = useState(column.title);
  const [menu, setMenu] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const others = neighbours.filter(c => c.status !== column.status);
  const [moveTo, setMoveTo] = useState(others.find(c => c.kind !== 'proposed')?.status ?? '');
  useEffect(() => setTitle(column.title), [column.title]);

  const act = (op: () => Promise<unknown>) => {
    setMenu(false);
    op().then(onChanged).catch(e => onError(e.message ?? String(e)));
  };
  const i = neighbours.findIndex(c => c.status === column.status);

  return (
    <header className="relative px-3 py-2 border-b border-border flex items-center gap-2">
      {renaming ? (
        <input
          autoFocus
          aria-label="List name"
          value={title}
          onChange={e => setTitle(e.target.value)}
          onBlur={() => {
            setRenaming(false);
            if (title.trim() && title !== column.title) act(() => editList(column.status, { title: title.trim() }));
          }}
          onKeyDown={e => {
            if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
            if (e.key === 'Escape') { setTitle(column.title); setRenaming(false); }
          }}
          className="flex-1 bg-background border border-border px-1.5 py-0.5 text-sm font-semibold"
        />
      ) : (
        <h2 className="flex-1 text-sm font-semibold truncate cursor-text" onClick={() => setRenaming(true)}
          title={`${column.title} — status:${column.status}`}>
          {column.title}
        </h2>
      )}
      {column.kind === 'closed' && <span className="text-[10px] uppercase tracking-wide text-green-700 dark:text-green-400">finished</span>}
      {column.kind === 'proposed' && <span className="text-[10px] uppercase tracking-wide text-muted-foreground">to accept</span>}
      <span className="text-xs text-muted-foreground font-mono tabular-nums">{column.total}</span>
      <button type="button" aria-label={`${column.title} list actions`} aria-expanded={menu}
        onClick={() => { setMenu(m => !m); setDeleting(false); }}
        className="p-0.5 text-muted-foreground hover:text-foreground hover:bg-accent/40">
        <MoreHorizontal size={14} />
      </button>

      {menu && (
        <div role="menu" className="absolute right-1 top-full z-30 mt-1 w-64 bg-background border border-border shadow-lg py-1 text-sm"
          onKeyDown={e => { if (e.key === 'Escape') setMenu(false); }}>
          <MenuItem onClick={() => { setMenu(false); setRenaming(true); }}>Rename list</MenuItem>
          {!first && <MenuItem onClick={() => act(() => editList(column.status, { after: i > 1 ? neighbours[i - 2].status : '^' }))}>Move left</MenuItem>}
          {!last && <MenuItem onClick={() => act(() => editList(column.status, { after: neighbours[i + 1].status }))}>Move right</MenuItem>}
          {column.kind === 'open' && (
            <MenuItem onClick={() => act(() => editList(column.status, { kind: 'closed' }))}>
              Cards here are finished
            </MenuItem>
          )}
          {column.kind === 'closed' && (
            <MenuItem onClick={() => act(() => editList(column.status, { kind: 'open' }))}>
              Cards here are still open
            </MenuItem>
          )}
          <MenuItem onClick={() => act(() => editList(column.status, { archived: true }))}>Archive this list</MenuItem>
          {column.kind !== 'proposed' && (deleting ? (
            <div className="px-3 py-2 flex flex-col gap-2 border-t border-border">
              {column.total > 0 && (
                <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                  Move its {column.total} card{column.total === 1 ? '' : 's'} to
                  <select aria-label="Move cards to" value={moveTo} onChange={e => setMoveTo(e.target.value)}
                    className="bg-background border border-border px-1.5 py-1 text-sm text-foreground">
                    {others.map(c => <option key={c.status} value={c.status}>{c.title}</option>)}
                  </select>
                </label>
              )}
              <button type="button" onClick={() => act(() => deleteList(column.status, column.total > 0 ? moveTo : ''))}
                className="px-2.5 py-1 bg-destructive text-destructive-foreground text-sm self-start">
                Delete list
              </button>
            </div>
          ) : (
            <MenuItem onClick={() => setDeleting(true)} danger>Delete list…</MenuItem>
          ))}
        </div>
      )}
    </header>
  );
};

const MenuItem = ({ onClick, children, danger }: { onClick: () => void; children: React.ReactNode; danger?: boolean }) => (
  <button type="button" role="menuitem" onClick={onClick}
    className={`w-full text-left px-3 py-1.5 hover:bg-accent/40 ${danger ? 'text-destructive' : ''}`}>
    {children}
  </button>
);

/** "+ Add a card": stays open after adding, so a run of cards is a run of Enters. */
const QuickAdd = ({
  list, labels, onAdded, onError,
}: { list: string; labels: string[]; onAdded: () => void; onError: (m: string) => void }) => {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState('');
  const add = async () => {
    const t = title.trim();
    if (!t) return;
    try {
      await createCard(t, list, labels);
      setTitle('');
      onAdded();
    } catch (e: any) {
      onError(e.message ?? String(e));
    }
  };
  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)}
        className="m-2 mt-0 flex items-center gap-1.5 px-2 py-1.5 text-sm text-muted-foreground hover:text-foreground hover:bg-accent/40">
        <Plus size={14} /> Add a card
      </button>
    );
  }
  return (
    <form className="p-2 pt-0 flex flex-col gap-1.5" onSubmit={e => { e.preventDefault(); void add(); }}>
      <textarea
        autoFocus
        aria-label="Card title"
        placeholder="Enter a title for this card…"
        value={title}
        onChange={e => setTitle(e.target.value)}
        onKeyDown={e => {
          if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void add(); }
          if (e.key === 'Escape') { setOpen(false); setTitle(''); }
        }}
        className="min-h-14 bg-background border border-border p-2 text-sm outline-none focus:border-primary"
      />
      <div className="flex items-center gap-2">
        <button type="submit" className="px-3 py-1 text-sm bg-primary text-primary-foreground">Add card</button>
        <button type="button" aria-label="Stop adding" onClick={() => { setOpen(false); setTitle(''); }}
          className="p-1 text-muted-foreground hover:text-foreground"><X size={16} /></button>
        {labels.length > 0 && <span className="text-xs text-muted-foreground truncate">with {labels.join(', ')}</span>}
      </div>
    </form>
  );
};

const AddList = ({ after, onAdded, onError }: { after: string; onAdded: () => void; onError: (m: string) => void }) => {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState('');
  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)}
        className="w-72 shrink-0 flex items-center gap-1.5 px-3 py-2 text-sm text-muted-foreground border border-dashed border-border hover:text-foreground hover:bg-accent/40">
        <Plus size={14} /> Add another list
      </button>
    );
  }
  return (
    <form
      className="w-72 shrink-0 p-2 border border-border bg-card/40 flex flex-col gap-1.5"
      onSubmit={async e => {
        e.preventDefault();
        if (!title.trim()) return;
        try {
          await createList(title.trim(), 'open', after);
          setTitle('');
          setOpen(false);
          onAdded();
        } catch (err: any) {
          onError(err.message ?? String(err));
        }
      }}
    >
      <input autoFocus aria-label="List name" placeholder="Enter list title…" value={title}
        onChange={e => setTitle(e.target.value)}
        onKeyDown={e => { if (e.key === 'Escape') setOpen(false); }}
        className="bg-background border border-border px-2 py-1 text-sm" />
      <div className="flex items-center gap-2">
        <button type="submit" className="px-3 py-1 text-sm bg-primary text-primary-foreground">Add list</button>
        <button type="button" aria-label="Stop adding" onClick={() => setOpen(false)} className="p-1 text-muted-foreground"><X size={16} /></button>
      </div>
    </form>
  );
};

const TicketCard = ({
  ticket, colorOf, closed, dragging, onDragStart, onOpen, onKeyDown,
}: {
  ticket: Ticket;
  colorOf: Map<string, string>;
  closed: boolean;
  dragging: boolean;
  onDragStart: (id: string | null) => void;
  onOpen: () => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
}) => {
  const source = ticket.sources?.[0];
  // Across every source, not just the one shown: a card raised from three
  // messages carries the files from all three.
  const attachments = (ticket.sources ?? []).reduce((n, s) => n + (s.attachments ?? 0), 0);
  const due = ticket.dueAt ? new Date(ticket.dueAt) : null;
  const dueClass = !due ? '' : closed ? 'bg-green-600 text-white'
    : due.getTime() < Date.now() ? 'bg-red-600 text-white'
    : due.getTime() - Date.now() < 24 * 3600_000 ? 'bg-yellow-400 text-black' : '';
  const checklistDone = (ticket.checklistTotal ?? 0) > 0 && ticket.checklistDone === ticket.checklistTotal;

  return (
    <article
      data-card={ticket.id}
      tabIndex={0}
      role="button"
      aria-label={ticket.title}
      draggable
      onDragStart={e => {
        e.dataTransfer.setData('text/plain', ticket.id);
        e.dataTransfer.effectAllowed = 'move';
        onDragStart(ticket.id);
      }}
      onDragEnd={() => onDragStart(null)}
      onClick={onOpen}
      onKeyDown={onKeyDown}
      className={`bg-background border border-border p-2 cursor-pointer hover:border-primary/50
                  focus:outline-none focus:ring-2 focus:ring-primary flex flex-col gap-1.5
                  ${dragging ? 'opacity-40' : ''}`}
    >
      {(ticket.labels?.length ?? 0) > 0 && (
        <div className="flex flex-wrap gap-1">
          {ticket.labels!.map(l => (
            <span key={l} className={`px-1.5 text-[10px] font-medium leading-4 ${labelClass(colorOf.get(l))}`}>{l}</span>
          ))}
        </div>
      )}

      <p className="text-sm leading-snug">{ticket.title}</p>

      <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
        {due && (
          <span className={`px-1 ${dueClass}`} title={due.toLocaleString()}>
            {due.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}
          </span>
        )}
        {ticket.priority && <span className="uppercase tracking-wide">{ticket.priority}</span>}
        {ticket.body && <span title="This card has a description"><AlignLeft size={11} /></span>}
        {(ticket.comments ?? 0) > 0 && (
          <span className="flex items-center gap-0.5" title="Comments"><MessageSquare size={11} />{ticket.comments}</span>
        )}
        {(ticket.checklistTotal ?? 0) > 0 && (
          <span className={`flex items-center gap-0.5 px-1 ${checklistDone ? 'bg-green-600 text-white' : ''}`} title="Checklist">
            <CheckSquare size={11} />{ticket.checklistDone ?? 0}/{ticket.checklistTotal}
          </span>
        )}
        {ticket.origin !== 'human' && ticket.confidence !== undefined && ticket.confidence !== null && (
          <span className="font-mono">{Math.round(ticket.confidence * 100)}%</span>
        )}
        {/* Provenance, on the card itself. A card that cannot be traced back
            to the mail that made it is a task in a worse task manager. */}
        {source && (
          <span className="flex items-center gap-1 min-w-0" title={source.subject}>
            <Mail size={10} className="shrink-0" />
            <span className="truncate max-w-32">{source.from || source.subject}</span>
          </span>
        )}
        {attachments > 0 && (
          <span className="flex items-center gap-0.5" title={`${attachments} file${attachments === 1 ? '' : 's'} on this card's mail`}>
            <Paperclip size={10} />{attachments}
          </span>
        )}
      </div>
    </article>
  );
};
