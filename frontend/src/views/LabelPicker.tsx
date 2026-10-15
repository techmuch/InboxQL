import { useEffect, useMemo, useRef, useState } from 'react';
import { Check, Minus, Plus, Tag } from 'lucide-react';
import { changeLabels, isSystemLabel, listLabels } from '../lib/mail';

interface LabelPickerProps {
  /** The messages being labelled. */
  ids: string[];
  /** Each message's current labels, to show which are already on. */
  current: string[][];
  /** Rendered as the button that opens it. */
  compact?: boolean;
  /** Open it from outside (the `l` key). */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

/**
 * Add or remove labels on one or more messages, as Gmail's Label menu does.
 *
 * A label on every chosen message is ticked, one on some of them shows a
 * dash, and choosing it puts it on all of them or, if all had it, takes it
 * off. Typing a name that does not exist offers to create it.
 */
export const LabelPicker = ({ ids, current, compact, open: openProp, onOpenChange }: LabelPickerProps) => {
  const [openState, setOpenState] = useState(false);
  const open = openProp ?? openState;
  const setOpen = (o: boolean) => { setOpenState(o); onOpenChange?.(o); };
  const [names, setNames] = useState<string[]>([]);
  const [filter, setFilter] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    setFilter('');
    setError(null);
    listLabels().then(ls => setNames(ls.filter(l => !l.system || l.name === 'important').map(l => l.name)));
    setTimeout(() => input.current?.focus(), 0);
    const away = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', away);
    return () => document.removeEventListener('mousedown', away);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const state = (name: string): 'all' | 'some' | 'none' => {
    const has = current.filter(ls => ls.some(l => l.toLowerCase() === name.toLowerCase())).length;
    return has === 0 ? 'none' : has === current.length ? 'all' : 'some';
  };

  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return names.filter(n => n.toLowerCase().includes(f));
  }, [names, filter]);
  const exact = names.some(n => n.toLowerCase() === filter.trim().toLowerCase());
  const canCreate = filter.trim() !== '' && !exact && !isSystemLabel(filter.trim());

  const toggle = async (name: string) => {
    setBusy(true);
    setError(null);
    try {
      if (state(name) === 'all') await changeLabels(ids, [], [name]);
      else await changeLabels(ids, [name], []);
      if (!names.includes(name)) setNames(ns => [...ns, name].sort((a, b) => a.localeCompare(b)));
      setFilter('');
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="relative" ref={box}>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        title="Label (l)"
        aria-label="Label"
        aria-expanded={open}
        className={compact
          ? 'p-2 hover:bg-accent text-muted-foreground'
          : 'flex items-center gap-1 border border-border px-2 py-0.5 hover:bg-accent/40'}
      >
        <Tag className={compact ? 'w-4 h-4' : 'h-3 w-3'} />{!compact && ' Label'}
      </button>
      {open && (
        <div
          role="dialog"
          aria-label="Label as"
          className="absolute right-0 z-50 mt-1 w-64 border border-border bg-popover text-popover-foreground shadow-lg text-xs"
          onKeyDown={e => {
            if (e.key === 'Escape') { e.stopPropagation(); setOpen(false); }
          }}
        >
          <div className="px-3 pt-2 pb-1 font-semibold text-muted-foreground">Label as</div>
          <input
            ref={input}
            value={filter}
            onChange={e => setFilter(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter') {
                e.preventDefault();
                if (canCreate) toggle(filter.trim());
                else if (shown.length === 1) toggle(shown[0]);
              }
            }}
            placeholder="Find or create a label"
            aria-label="Find or create a label"
            className="mx-3 mb-2 w-[calc(100%-1.5rem)] border border-border bg-background px-2 py-1 outline-none focus:border-primary"
          />
          <ul className="max-h-64 overflow-auto pb-1" role="listbox" aria-label="Labels">
            {shown.map(name => {
              const s = state(name);
              return (
                <li key={name}>
                  <button
                    type="button"
                    role="option"
                    aria-selected={s === 'all'}
                    disabled={busy}
                    onClick={() => toggle(name)}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-accent disabled:opacity-50"
                  >
                    <span className="flex h-3.5 w-3.5 items-center justify-center border border-border">
                      {s === 'all' && <Check className="h-3 w-3" />}
                      {s === 'some' && <Minus className="h-3 w-3" />}
                    </span>
                    <span className="truncate">{name}</span>
                  </button>
                </li>
              );
            })}
            {shown.length === 0 && !canCreate && (
              <li className="px-3 py-1.5 text-muted-foreground">No labels yet. Type a name to create one.</li>
            )}
          </ul>
          {canCreate && (
            <button
              type="button"
              disabled={busy}
              onClick={() => toggle(filter.trim())}
              className="flex w-full items-center gap-2 border-t border-border px-3 py-2 text-left hover:bg-accent"
            >
              <Plus className="h-3 w-3" /> Create “{filter.trim()}”
            </button>
          )}
          {error && <div className="px-3 pb-2 text-destructive">{error}</div>}
        </div>
      )}
    </div>
  );
};
