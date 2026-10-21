import { useEffect, useRef, useState } from 'react';
import { ChevronDown, Pencil, Plus } from 'lucide-react';
import { createBoard, deleteBoard, editBoard, type BoardDef } from './api';

/**
 * Which board, and the boards themselves. A board is a named filter over the
 * same lists: "Home" is `label:home`, "This week" is `due:7d`.
 */
export const BoardPicker = ({
  boards, current, onChoose, onChanged,
}: {
  boards: BoardDef[];
  current?: BoardDef;
  onChoose: (id: string) => void;
  onChanged: () => void;
}) => {
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<BoardDef | 'new' | null>(null);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) { setOpen(false); setEditing(null); }
    };
    document.addEventListener('mousedown', away);
    return () => document.removeEventListener('mousedown', away);
  }, [open]);

  return (
    <div className="relative" ref={box}>
      <button type="button" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(o => !o)}
        className="flex items-center gap-1.5 px-2.5 py-1.5 text-sm font-semibold border border-border hover:bg-accent/40">
        {current?.title ?? 'Board'} <ChevronDown size={14} />
      </button>
      {open && (
        <div role="menu" className="absolute left-0 top-full mt-1 z-30 w-80 bg-background border border-border shadow-lg py-1 text-sm"
          onKeyDown={e => { if (e.key === 'Escape') { setOpen(false); setEditing(null); } }}>
          {editing ? (
            <BoardForm
              board={editing === 'new' ? undefined : editing}
              canDelete={boards.length > 1}
              onDone={id => {
                setEditing(null);
                setOpen(false);
                onChanged();
                if (id) onChoose(id);
              }}
              onCancel={() => setEditing(null)}
            />
          ) : (
            <>
              {boards.map(b => (
                <div key={b.id} className="flex items-center group">
                  <button type="button" role="menuitemradio" aria-checked={b.id === current?.id}
                    onClick={() => { onChoose(b.id); setOpen(false); }}
                    className={`flex-1 text-left px-3 py-1.5 hover:bg-accent/40 ${b.id === current?.id ? 'font-semibold' : ''}`}>
                    {b.title}
                    {b.filter && <span className="ml-2 font-mono text-xs text-muted-foreground">{b.filter}</span>}
                  </button>
                  <button type="button" aria-label={`Edit ${b.title}`} onClick={() => setEditing(b)}
                    className="p-1.5 mr-1 text-muted-foreground hover:text-foreground opacity-60 group-hover:opacity-100">
                    <Pencil size={12} />
                  </button>
                </div>
              ))}
              <div className="border-t border-border mt-1 pt-1">
                <button type="button" onClick={() => setEditing('new')}
                  className="w-full text-left px-3 py-1.5 hover:bg-accent/40 flex items-center gap-1.5">
                  <Plus size={14} /> New board
                </button>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
};

const BoardForm = ({
  board, canDelete, onDone, onCancel,
}: {
  board?: BoardDef;
  canDelete: boolean;
  onDone: (id?: string) => void;
  onCancel: () => void;
}) => {
  const [title, setTitle] = useState(board?.title ?? '');
  const [filter, setFilter] = useState(board?.filter ?? '');
  const [error, setError] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);

  const save = async () => {
    try {
      if (board) {
        await editBoard(board.id, { title, filter });
        onDone(board.id);
      } else {
        const b = await createBoard(title, filter);
        onDone(b.id);
      }
    } catch (e: any) {
      setError(e.message ?? String(e));
    }
  };

  return (
    <form className="p-3 flex flex-col gap-2" onSubmit={e => { e.preventDefault(); void save(); }}>
      <label className="flex flex-col gap-1 text-xs text-muted-foreground">
        Title
        <input autoFocus aria-label="Board title" value={title} onChange={e => setTitle(e.target.value)}
          className="bg-background border border-border px-2 py-1 text-sm text-foreground" />
      </label>
      <label className="flex flex-col gap-1 text-xs text-muted-foreground">
        Which cards — a query, empty for all
        <input aria-label="Board filter" value={filter} onChange={e => setFilter(e.target.value)} spellCheck={false}
          placeholder="label:home"
          className="bg-background border border-border px-2 py-1 text-sm font-mono text-foreground" />
      </label>
      <p className="text-xs text-muted-foreground">
        Cards added on this board get the labels its filter names.
      </p>
      {error && <p className="text-xs text-destructive">{error}</p>}
      <div className="flex items-center gap-2">
        <button type="submit" className="px-3 py-1 text-sm bg-primary text-primary-foreground">
          {board ? 'Save' : 'Make board'}
        </button>
        <button type="button" onClick={onCancel} className="px-2 py-1 text-sm hover:bg-accent/40">Cancel</button>
        {board && canDelete && (confirm ? (
          <button type="button" className="ml-auto px-2 py-1 text-sm bg-destructive text-destructive-foreground"
            onClick={async () => {
              try { await deleteBoard(board.id); onDone(); } catch (e: any) { setError(e.message); }
            }}>
            Delete board
          </button>
        ) : (
          <button type="button" onClick={() => setConfirm(true)} className="ml-auto text-xs text-destructive underline">
            Delete…
          </button>
        ))}
      </div>
    </form>
  );
};
