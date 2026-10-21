import { useEffect, useMemo, useRef, useState } from 'react';
import { Check, Palette, Plus } from 'lucide-react';
import { setLabelColor, type LabelInfo } from './api';
import { LABEL_CLASSES, labelClass } from './labels';

/**
 * Labels for a card. The list is the labels on cards and the topics on mail —
 * one vocabulary — so a card can be labelled with what its mail is about
 * without inventing a near-synonym. Typing a name nobody has used makes it.
 */
export const LabelPicker = ({
  labels,
  selected,
  onToggle,
  onColors,
}: {
  labels: LabelInfo[];
  selected: string[];
  onToggle: (name: string, on: boolean) => void;
  /** A colour changed; the caller reloads the labels. */
  onColors: () => void;
}) => {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const [painting, setPainting] = useState<string | null>(null);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', away);
    return () => document.removeEventListener('mousedown', away);
  }, [open]);

  const norm = q.trim().toLowerCase().replace(/\s+/g, ' ');
  const shown = useMemo(
    () => labels.filter(l => !norm || l.name.includes(norm)).slice(0, 40),
    [labels, norm],
  );
  const exists = labels.some(l => l.name === norm);

  return (
    <div className="relative" ref={box}>
      <button
        type="button"
        aria-label="Edit labels"
        aria-expanded={open}
        onClick={() => setOpen(o => !o)}
        className="p-1 border border-dashed border-border text-muted-foreground hover:text-foreground hover:bg-accent/40"
      >
        <Plus size={12} />
      </button>
      {open && (
        <div
          role="dialog"
          aria-label="Labels"
          className="absolute left-0 top-full mt-1 z-30 w-64 bg-background border border-border shadow-lg p-2 flex flex-col gap-2"
          onKeyDown={e => { if (e.key === 'Escape') { e.stopPropagation(); setOpen(false); } }}
        >
          <input
            autoFocus
            aria-label="Search or make a label"
            placeholder="Search or make a label"
            value={q}
            onChange={e => setQ(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter' && norm) {
                e.preventDefault();
                onToggle(norm, !selected.includes(norm));
                setQ('');
              }
            }}
            className="bg-background border border-border px-2 py-1 text-sm"
          />
          <ul className="max-h-64 overflow-auto flex flex-col gap-1">
            {shown.map(l => {
              const on = selected.includes(l.name);
              return (
                <li key={l.name} className="flex flex-col">
                  <div className="flex items-center gap-1">
                    <button
                      type="button"
                      role="checkbox"
                      aria-checked={on}
                      onClick={() => onToggle(l.name, !on)}
                      className={`flex-1 flex items-center justify-between px-2 py-1 text-xs font-medium text-left ${labelClass(l.color)}`}
                    >
                      <span className="truncate">{l.name}</span>
                      {on && <Check size={12} />}
                    </button>
                    <button
                      type="button"
                      aria-label={`Colour of ${l.name}`}
                      onClick={() => setPainting(p => (p === l.name ? null : l.name))}
                      className="p-1 text-muted-foreground hover:text-foreground"
                    >
                      <Palette size={12} />
                    </button>
                  </div>
                  {painting === l.name && (
                    <div className="flex flex-wrap gap-1 py-1" role="radiogroup" aria-label={`Colour of ${l.name}`}>
                      {Object.keys(LABEL_CLASSES).map(c => (
                        <button
                          key={c}
                          type="button"
                          role="radio"
                          aria-checked={l.color === c}
                          aria-label={c}
                          title={c}
                          onClick={async () => { await setLabelColor(l.name, c); setPainting(null); onColors(); }}
                          className={`w-6 h-4 ${LABEL_CLASSES[c]} ${l.color === c ? 'ring-2 ring-offset-1 ring-primary' : ''}`}
                        />
                      ))}
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
          {norm && !exists && (
            <button
              type="button"
              onClick={() => { onToggle(norm, true); setQ(''); }}
              className="text-left text-sm px-2 py-1 hover:bg-accent/40"
            >
              Make “{norm}”
            </button>
          )}
        </div>
      )}
    </div>
  );
};
