import { useEffect, useRef, useState } from 'react';
import { Ban, Plus, X } from 'lucide-react';
import { complete, type Candidate } from './api';
import { compose, queryTerms, type QueryTerm } from '../../lib/filters';

interface PillsProps {
  /** The query these pills describe. They never hold state of their own. */
  query: string;
  onChange: (query: string) => void;
}

/**
 * The query, as editable pills.
 *
 * # A projection, never a second copy
 *
 * Pills are derived from the query and every edit is written back through the
 * server's composer. They hold no state between renders. That matters more here
 * than anywhere else in this app: a list of terms kept *alongside* the query is
 * exactly what made the bar show one thing while the server was asked another,
 * and it is the third time that shape has caused a bug.
 *
 * So: the text box and the pills are two views of one string. Type in the box
 * or click a pill; both end up in the same place.
 *
 * # Why editing goes through the server
 *
 * Quoting is grammar. `subject:quarterly report` is two terms and
 * `subject:"quarterly report"` is one, and deciding which to write is exactly
 * the judgement three previous frontend attempts got wrong. The pill sends the
 * field, the value and whether it is negated; the server assembles the term.
 */
export const Pills = ({ query, onChange }: PillsProps) => {
  const [terms, setTerms] = useState<QueryTerm[]>([]);
  const [editing, setEditing] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    queryTerms(query)
      .then(t => { if (!cancelled) setTerms(t); })
      .catch(() => { if (!cancelled) setTerms([]); });
    return () => { cancelled = true; };
  }, [query]);

  const edit = async (start: number, field: string, value: string, negated: boolean) => {
    setEditing(null);
    if (!value.trim()) {
      onChange(await compose(query, { at: start, term: '' }));
      return;
    }
    onChange(await compose(query, { at: start, field, value, negated }));
  };

  if (terms.length === 0) return null;

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {terms.map(term => {
        const value = valueOf(term);
        return editing === term.start ? (
          <PillEditor
            key={term.start}
            field={term.field ?? ''}
            value={value}
            onCommit={v => edit(term.start, term.field ?? '', v, term.negated)}
            onCancel={() => setEditing(null)}
          />
        ) : (
          <span
            key={term.start}
            className={`group flex items-center text-xs font-mono border ${
              term.negated
                ? 'border-destructive/40 bg-destructive/5 text-destructive'
                : 'border-border bg-muted/50'
            }`}
          >
            {/* Negation is a property of the term, so it toggles in place
                rather than making you retype the whole thing with a dash. */}
            <button
              type="button"
              onClick={() => edit(term.start, term.field ?? '', value, !term.negated)}
              title={term.negated ? 'Include these instead' : 'Exclude these instead'}
              className="px-1.5 py-0.5 hover:bg-accent/60 border-r border-border/50"
            >
              {term.negated ? <Ban size={10} /> : <Plus size={10} className="opacity-40" />}
            </button>

            <button
              type="button"
              onClick={() => setEditing(term.start)}
              title="Edit"
              className="px-2 py-0.5 hover:bg-accent/60"
            >
              {term.field && <span className="opacity-60">{term.field}:</span>}
              {value || <span className="opacity-40">…</span>}
            </button>

            <button
              type="button"
              onClick={() => edit(term.start, '', '', false)}
              aria-label={`Remove ${term.text}`}
              className="px-1.5 py-0.5 hover:bg-destructive/15 border-l border-border/50"
            >
              <X size={10} />
            </button>
          </span>
        );
      })}
    </div>
  );
};

/**
 * A pill's value, with the field prefix and any quoting removed.
 *
 * Display only — the value goes back to the server raw and is re-quoted there,
 * so nothing here has to decide what quoting means.
 */
function valueOf(term: QueryTerm): string {
  let text = term.text.replace(/^-/, '');
  if (term.field) {
    const colon = text.indexOf(':');
    if (colon >= 0) text = text.slice(colon + 1);
  }
  if (text.startsWith('"') && text.endsWith('"') && text.length > 1) {
    text = text.slice(1, -1).replace(/""/g, '"');
  }
  return text;
}

/**
 * Editing one pill's value, with the same completions the query bar offers.
 *
 * Asking for completions on `field:value` rather than on the bare value is what
 * makes them type-aware for free: the server already knows that `from:` wants
 * addresses and `is:` wants a fixed set.
 */
const PillEditor = ({
  field,
  value,
  onCommit,
  onCancel,
}: {
  field: string;
  value: string;
  onCommit: (value: string) => void;
  onCancel: () => void;
}) => {
  const [draft, setDraft] = useState(value);
  const [options, setOptions] = useState<Candidate[]>([]);
  const [highlighted, setHighlighted] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const seq = useRef(0);

  useEffect(() => { inputRef.current?.select(); }, []);

  useEffect(() => {
    if (!field) return;
    const mine = ++seq.current;
    const probe = `${field}:${draft}`;
    const handle = window.setTimeout(() => {
      complete(probe, probe.length)
        .then(c => { if (mine === seq.current) { setOptions(c.candidates); setHighlighted(0); } })
        .catch(() => { if (mine === seq.current) setOptions([]); });
    }, 120);
    return () => window.clearTimeout(handle);
  }, [field, draft]);

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (options.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setHighlighted(h => (h + 1) % options.length);
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setHighlighted(h => (h - 1 + options.length) % options.length);
        return;
      }
      if (e.key === 'Tab') {
        e.preventDefault();
        setDraft(options[highlighted].value);
        return;
      }
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      onCommit(draft);
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      onCancel();
    }
  };

  return (
    <span className="relative flex items-center text-xs font-mono border border-primary bg-background">
      {field && <span className="pl-2 opacity-60">{field}:</span>}
      <input
        ref={inputRef}
        value={draft}
        onChange={e => setDraft(e.target.value)}
        onKeyDown={onKeyDown}
        onBlur={() => window.setTimeout(onCancel, 120)}
        aria-label={field ? `${field} value` : 'search text'}
        style={{ width: `${Math.max(draft.length + 1, 6)}ch` }}
        className="bg-transparent px-1 py-0.5 outline-none"
      />

      {options.length > 0 && (
        <ul
          role="listbox"
          className="absolute z-30 top-full left-0 mt-0.5 min-w-56 max-h-56 overflow-auto
                     bg-popover border border-border shadow-lg"
        >
          {options.map((o, i) => (
            <li
              key={`${o.kind}-${o.value}`}
              role="option"
              aria-selected={i === highlighted}
              onMouseDown={e => { e.preventDefault(); onCommit(o.value); }}
              onMouseEnter={() => setHighlighted(i)}
              className={`flex items-baseline gap-3 px-2 py-1 cursor-pointer ${
                i === highlighted ? 'bg-primary/10' : 'hover:bg-accent/40'
              }`}
            >
              <span>{o.value}</span>
              {o.detail && (
                <span className="ml-auto text-[10px] text-muted-foreground truncate">{o.detail}</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </span>
  );
};
