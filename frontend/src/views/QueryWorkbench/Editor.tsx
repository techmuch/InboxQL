import { useCallback, useEffect, useRef, useState } from 'react';
import { Play, Loader2 } from 'lucide-react';
import { complete, type Candidate, type Completion } from './api';

interface EditorProps {
  value: string;
  onChange: (value: string) => void;
  onRun: () => void;
  running: boolean;
  /** Where the server gave up, if the last run was refused. */
  errorPosition?: number;
  errorMessage?: string;
}

/**
 * The query editor.
 *
 * Two things carry most of the weight: completions that know the field's type,
 * and a caret under the character the parser stopped at. Both come from the
 * server — the editor never decides what is valid, it only shows the answer.
 */
export const Editor = ({
  value,
  onChange,
  onRun,
  running,
  errorPosition,
  errorMessage,
}: EditorProps) => {
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const [completion, setCompletion] = useState<Completion | null>(null);
  const [highlighted, setHighlighted] = useState(0);
  const [open, setOpen] = useState(false);
  // Completions belong to an editor someone is typing in. Without this the
  // list opens on mount — the effect below runs for the initial empty value —
  // and covers the empty state before anyone has clicked anything.
  const [focused, setFocused] = useState(false);

  // The request that is in flight. A slower earlier response must not overwrite
  // a faster later one, which is what makes the list flicker back to a stale
  // prefix as you type.
  const requestSeq = useRef(0);

  // The value the list was last dismissed at, either by accepting a candidate
  // or by pressing Escape. Without this the debounced effect re-fires on the
  // text that accepting just produced, and the list reopens over the result to
  // offer the value already sitting in the box.
  const dismissedAt = useRef<string | null>(null);

  const ask = useCallback(async (text: string, pos: number) => {
    const seq = ++requestSeq.current;
    try {
      const c = await complete(text, pos);
      if (seq !== requestSeq.current) return;
      setCompletion(c);
      setHighlighted(0);
      // Re-checked here rather than trusting the value captured when the
      // request went out: focus can be lost while it is in flight.
      setOpen(c.candidates.length > 0 && document.activeElement === inputRef.current);
    } catch {
      // A completion that fails is not worth interrupting typing for.
      if (seq === requestSeq.current) setOpen(false);
    }
  }, []);

  // Debounced: a request per keystroke is wasteful, and the list is unreadable
  // while it changes faster than the eye tracks.
  useEffect(() => {
    const el = inputRef.current;
    if (!el || !focused) return;
    if (dismissedAt.current === value) return;
    dismissedAt.current = null;

    const handle = window.setTimeout(() => ask(value, el.selectionStart ?? value.length), 120);
    return () => window.clearTimeout(handle);
  }, [value, focused, ask]);

  const accept = (candidate: Candidate) => {
    if (!completion) return;
    const before = value.slice(0, completion.start);
    const after = value.slice(completion.end);
    const next = before + candidate.value + after;
    dismissedAt.current = next;
    onChange(next);
    setOpen(false);

    // Put the cursor after what was inserted. A field completion ends in a
    // colon, so the next thing typed is its value and the caret belongs there.
    const caret = before.length + candidate.value.length;
    requestAnimationFrame(() => {
      const el = inputRef.current;
      if (el) {
        el.focus();
        el.setSelectionRange(caret, caret);
      }
    });
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const candidates = completion?.candidates ?? [];

    if (open && candidates.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setHighlighted(h => (h + 1) % candidates.length);
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setHighlighted(h => (h - 1 + candidates.length) % candidates.length);
        return;
      }
      if (e.key === 'Tab' || (e.key === 'Enter' && !e.metaKey && !e.ctrlKey)) {
        e.preventDefault();
        accept(candidates[highlighted]);
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        dismissedAt.current = value;
        setOpen(false);
        return;
      }
    }

    // Cmd+Enter runs whatever is written, whether or not a list is showing.
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      setOpen(false);
      onRun();
      return;
    }
    // A bare Enter runs it too, since a query is one line far more often than
    // it is several.
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      onRun();
    }
  };

  const candidates = completion?.candidates ?? [];

  return (
    <div className="relative">
      <div className="flex items-start gap-2">
        <div className="flex-1 relative">
          <textarea
            ref={inputRef}
            value={value}
            onChange={e => onChange(e.target.value)}
            onKeyDown={onKeyDown}
            onFocus={() => setFocused(true)}
            onBlur={() => {
              setFocused(false);
              // Delayed so a click on a candidate lands before the list goes.
              window.setTimeout(() => setOpen(false), 120);
            }}
            rows={2}
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            placeholder="from:stripe after:2026-01-01   ·   is:unread -from:*@acme.com   ·   | count by week"
            aria-label="Query"
            className="w-full resize-y bg-background border border-border px-3 py-2 font-mono text-sm
                       outline-none focus:border-primary focus:ring-1 focus:ring-primary/40"
          />

          {open && candidates.length > 0 && (
            <ul
              role="listbox"
              className="absolute z-20 left-0 right-0 mt-1 max-h-72 overflow-auto
                         bg-popover border border-border shadow-lg"
            >
              {candidates.map((c, i) => (
                <li
                  key={`${c.kind}-${c.value}`}
                  role="option"
                  aria-selected={i === highlighted}
                  onMouseDown={e => { e.preventDefault(); accept(c); }}
                  onMouseEnter={() => setHighlighted(i)}
                  className={`flex items-baseline gap-3 px-3 py-1.5 cursor-pointer text-sm ${
                    i === highlighted ? 'bg-primary/10' : 'hover:bg-accent/40'
                  }`}
                >
                  <span className="font-mono">{c.value}</span>
                  {c.detail && (
                    <span className="text-xs text-muted-foreground truncate ml-auto">{c.detail}</span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>

        <button
          type="button"
          onClick={onRun}
          disabled={running}
          className="flex items-center gap-1.5 px-3 py-2 bg-primary text-primary-foreground
                     text-sm font-medium disabled:opacity-60"
        >
          {running ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />}
          Run
        </button>
      </div>

      {errorMessage && <QueryCaret query={value} position={errorPosition} message={errorMessage} />}
    </div>
  );
};

/**
 * The parser's complaint, with a caret under where it stopped.
 *
 * The position is the whole reason this is not a plain error string: pointing
 * at the character beats making someone compare their query to a description
 * of what is wrong with it.
 */
const QueryCaret = ({
  query,
  position,
  message,
}: {
  query: string;
  position?: number;
  message: string;
}) => (
  <div className="mt-2 border-l-2 border-destructive/70 pl-3 py-1">
    {position !== undefined && position >= 0 && (
      <pre className="font-mono text-xs text-muted-foreground whitespace-pre overflow-x-auto">
        {query}
        {'\n'}
        {' '.repeat(Math.min(position, query.length))}^
      </pre>
    )}
    <p className="text-sm text-destructive mt-1">{message}</p>
  </div>
);
