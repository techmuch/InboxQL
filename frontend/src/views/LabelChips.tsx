import { useCallback, useEffect, useRef, useState } from 'react';
import { Check, Tag, User, X } from 'lucide-react';
import { openQuery } from '../lib/tabs';

export interface LabelLevel {
  label: string;
  describe?: string;
}

export interface LabelVerdict {
  matched: boolean;
  level?: string;
  score?: number;
  via?: string;
}

export interface MessageLabel {
  annotator: string;
  engine: string;
  enabled: boolean;
  unit: 'message' | 'thread';
  levels?: LabelLevel[];
  machine?: LabelVerdict;
  ruling?: LabelVerdict;
}

/**
 * What a label says now: your ruling if you made one, the annotator's if not.
 *
 * A ruling outranks the machine everywhere else in InboxQL — queries, reruns,
 * the board — so the chip shows what everything else will act on.
 */
export const effective = (l: MessageLabel): LabelVerdict | undefined => l.ruling ?? l.machine;

/** The words for a verdict: "important", or the label's own name for a yes. */
const verdictText = (l: MessageLabel, v: LabelVerdict) =>
  v.level ? `${l.annotator}: ${v.level}` : v.matched ? l.annotator : `not ${l.annotator}`;

/**
 * The labels on a message, each one something you can rule on.
 *
 * # Why rulings live here
 *
 * Where you are when you notice a label is wrong — or right — is reading the
 * message it is on. Sending you to the Annotators tab to say so is how
 * corrections stop being made.
 *
 * # Why "right" is as easy as "wrong"
 *
 * A set of rulings made only of corrections says the model is never right, and
 * fitting a calibration to it squashes every score toward 50%. Agreeing has to
 * cost exactly one choice, the same as disagreeing, or it will not be done.
 *
 * Rulings made here are recorded as `inflow` — made while reading — and are
 * kept out of accuracy and calibration, which use the review queue's random
 * draws. They still outrank the machine on this message immediately.
 */
export const LabelChips = ({ messageId }: { messageId: string }) => {
  const [labels, setLabels] = useState<MessageLabel[]>([]);
  const [menu, setMenu] = useState<{ label: MessageLabel; x: number; y: number } | null>(null);
  const [showNo, setShowNo] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLabels([]);
    setMenu(null);
    fetch(`/api/messages/${encodeURIComponent(messageId)}/labels`)
      .then(r => (r.ok ? r.json() : []))
      .then(l => { if (!cancelled) setLabels(Array.isArray(l) ? l : []); })
      .catch(() => { /* a message with no labels shows none */ });
    return () => { cancelled = true; };
  }, [messageId]);

  const send = useCallback(async (annotator: string, body: object | null) => {
    const url = `/api/messages/${encodeURIComponent(messageId)}/labels/${encodeURIComponent(annotator)}`;
    const res = await fetch(url, body === null
      ? { method: 'DELETE' }
      : { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    if (res.ok) setLabels(await res.json());
    setMenu(null);
  }, [messageId]);

  if (labels.length === 0) return null;

  // "No" verdicts are real answers and can be wrong — a missed open loop is
  // exactly the mistake worth correcting — but shown by default they would
  // outnumber the yeses on every message. One chip says how many there are.
  //
  // Your own rulings are always shown, "no" included: ruling a label wrong and
  // watching it vanish into a count reads as the click having done nothing.
  const shown = labels.filter(l => showNo || effective(l)?.matched || l.ruling);
  const hidden = labels.length - shown.length;

  const openMenu = (label: MessageLabel, el: HTMLElement, x?: number, y?: number) => {
    const r = el.getBoundingClientRect();
    setMenu({ label, x: x ?? r.left, y: y ?? r.bottom + 4 });
  };

  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5" aria-label="Labels">
      {shown.map(l => {
        const v = effective(l)!;
        const ruled = Boolean(l.ruling);
        return (
          <button
            key={l.annotator}
            type="button"
            data-label-chip={l.annotator}
            // Left click narrows the Desk to this label, the way every other
            // chip in the app is a query. Ruling is the context menu — right
            // click, or the menu key / Shift+F10 on a focused chip.
            onClick={() => openQuery(v.matched ? `label:${l.annotator}` : `-label:${l.annotator}`)}
            onContextMenu={e => { e.preventDefault(); openMenu(l, e.currentTarget, e.clientX, e.clientY); }}
            onKeyDown={e => {
              if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
                e.preventDefault();
                openMenu(l, e.currentTarget);
              }
            }}
            title={chipTitle(l)}
            className={`inline-flex items-center gap-1 border px-1.5 py-0.5 text-[11px] font-mono transition-colors ${
              v.matched
                ? 'border-primary/40 bg-primary/10 text-foreground hover:bg-primary/20'
                : 'border-border text-muted-foreground line-through decoration-muted-foreground/50 hover:bg-accent'
            }`}
          >
            <Tag className="h-3 w-3 opacity-60" />
            {verdictText(l, v)}
            {!ruled && v.score !== undefined && (
              <span className="opacity-50">{Math.round(v.score * 100)}%</span>
            )}
            {/* Yours, not the model's — and it outranks the model. */}
            {ruled && <User className="h-3 w-3 text-primary" aria-label="your ruling" />}
          </button>
        );
      })}
      {hidden > 0 && !showNo && (
        <button
          type="button"
          onClick={() => setShowNo(true)}
          className="text-[11px] text-muted-foreground underline-offset-2 hover:underline"
        >
          +{hidden} said no
        </button>
      )}
      {menu && <RulingMenu menu={menu} onClose={() => setMenu(null)} onRule={send} />}
    </div>
  );
};

function chipTitle(l: MessageLabel): string {
  const parts: string[] = [];
  if (l.machine) {
    const pct = l.machine.score !== undefined ? ` (${Math.round(l.machine.score * 100)}%)` : '';
    parts.push(`${l.engine} said ${verdictText(l, l.machine)}${pct}`);
  }
  if (l.ruling) parts.push(`you said ${verdictText(l, l.ruling)}`);
  if (l.unit === 'thread') parts.push('about the whole conversation');
  parts.push('Right-click to rule on it');
  return parts.join(' · ');
}

/**
 * The menu a label opens: right, wrong, or which level it should be.
 *
 * Keyboard first: it takes focus when it opens, arrows move through it, Escape
 * closes it and returns focus to the chip. Rendered fixed at the pointer so it
 * is never clipped by the viewer's scroll container.
 */
const RulingMenu = ({ menu, onClose, onRule }: {
  menu: { label: MessageLabel; x: number; y: number };
  onClose: () => void;
  onRule: (annotator: string, body: object | null) => void;
}) => {
  const ref = useRef<HTMLDivElement>(null);
  const l = menu.label;
  const v = effective(l)!;

  useEffect(() => {
    const first = ref.current?.querySelector<HTMLElement>('[role="menuitem"]');
    first?.focus();
    const away = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    document.addEventListener('mousedown', away);
    return () => document.removeEventListener('mousedown', away);
  }, [onClose]);

  const keys = (e: React.KeyboardEvent) => {
    const items = Array.from(ref.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? []);
    const i = items.indexOf(document.activeElement as HTMLElement);
    if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
      document.querySelector<HTMLElement>(`[data-label-chip="${CSS.escape(l.annotator)}"]`)?.focus();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      items[(i + 1) % items.length]?.focus();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      items[(i - 1 + items.length) % items.length]?.focus();
    }
  };

  const item = (key: string, text: string, onClick: () => void, icon?: React.ReactNode) => (
    <button
      key={key}
      type="button"
      role="menuitem"
      onClick={onClick}
      className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs hover:bg-accent focus:bg-accent focus:outline-none"
    >
      <span className="w-3.5 shrink-0">{icon}</span>
      {text}
    </button>
  );

  const levels = l.levels ?? [];
  const items: React.ReactNode[] = [];
  if (levels.length > 0) {
    // A levelled label: "right" confirms the level it shows, and every other
    // level is one choice away. "Wrong" alone would say nothing useful.
    items.push(item('right', `Right — ${v.level}`,
      () => onRule(l.annotator, { matched: true, level: v.level }), <Check className="h-3.5 w-3.5 text-green-600" />));
    for (const lv of levels) {
      if (lv.label === v.level) continue;
      items.push(item(`level-${lv.label}`, `Should be ${lv.label}`,
        () => onRule(l.annotator, { matched: true, level: lv.label })));
    }
  } else {
    items.push(item('right', `Right — ${verdictText(l, v)}`,
      () => onRule(l.annotator, { matched: v.matched }), <Check className="h-3.5 w-3.5 text-green-600" />));
    items.push(item('wrong', `Wrong — ${v.matched ? `not ${l.annotator}` : l.annotator}`,
      () => onRule(l.annotator, { matched: !v.matched }), <X className="h-3.5 w-3.5 text-destructive" />));
  }
  if (l.ruling) {
    items.push(<div key="sep" className="my-1 border-t border-border" />);
    items.push(item('clear', `Clear my ruling — back to what ${l.engine} said`, () => onRule(l.annotator, null)));
  }

  return (
    <div
      ref={ref}
      role="menu"
      aria-label={`Rule on ${l.annotator}`}
      onKeyDown={keys}
      style={{ position: 'fixed', left: menu.x, top: menu.y }}
      className="z-50 min-w-56 border border-border bg-popover py-1 shadow-lg"
    >
      <div className="px-3 pb-1 pt-0.5 text-[10px] uppercase tracking-wider text-muted-foreground">
        {l.unit === 'thread' ? `${l.annotator} · this conversation` : l.annotator}
      </div>
      {items}
    </div>
  );
};
