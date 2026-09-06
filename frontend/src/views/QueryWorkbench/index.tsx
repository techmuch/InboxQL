import { useCallback, useEffect, useState } from 'react';
import { Bookmark, Code2, Loader2, Pin, Plus, Tag, Trash2 } from 'lucide-react';
import { useWorkbenchStore } from '../../lib/tabs';
import { Editor } from './Editor';
import { Results } from './Results';
import {
  deleteSaved,
  explainQuery,
  listSaved,
  runQuery,
  saveQuery,
  withTerm,
  QueryFailed,
  type QueryResult,
  type SavedQuery,
} from './api';

/**
 * The query workbench.
 *
 * # Why the text is the state
 *
 * Everything that narrows the result writes into the query box: clicking a row
 * in an aggregate appends a term rather than mutating a hidden filter. That is
 * what makes a filter shareable, saveable and editable, and it is how anyone
 * learns the language — by watching their clicks turn into it.
 */
export const QueryWorkbench = () => {
  const [text, setText] = useState('');
  const [result, setResult] = useState<QueryResult | null>(null);
  const [failure, setFailure] = useState<QueryFailed | null>(null);
  const [running, setRunning] = useState(false);

  const [saved, setSaved] = useState<SavedQuery[]>([]);
  const [explain, setExplain] = useState<string | null>(null);
  const [naming, setNaming] = useState(false);

  const refreshSaved = useCallback(async () => {
    try {
      setSaved(await listSaved());
    } catch {
      // The rail is a convenience; failing to load it should not stop anyone
      // running a query.
    }
  }, []);

  useEffect(() => { refreshSaved(); }, [refreshSaved]);

  const run = useCallback(async (query?: string) => {
    const q = query ?? text;
    setRunning(true);
    setFailure(null);
    setExplain(null);
    try {
      setResult(await runQuery(q));
    } catch (e) {
      setFailure(e instanceof QueryFailed ? e : new QueryFailed(String(e)));
      setResult(null);
    } finally {
      setRunning(false);
    }
  }, [text]);

  // A query handed over from elsewhere — the dashboard's filters, most often —
  // is consumed once, so reopening the tab does not silently re-run it.
  const pending = useWorkbenchStore(s => s.pending);
  const setPending = useWorkbenchStore(s => s.setPending);
  useEffect(() => {
    if (pending === null) return;
    setPending(null);
    setText(pending);
    run(pending);
  }, [pending, setPending, run]);

  // Clicking a group narrows the query by writing the term into the box, where
  // it can be read and edited, rather than applying it invisibly.
  const drillDown = (term: string) => {
    const next = withTerm(text, term);
    setText(next);
    run(next);
  };

  const showExplain = async () => {
    if (explain) { setExplain(null); return; }
    try {
      const res = await explainQuery(text);
      setExplain(res.sql ?? '');
    } catch (e) {
      setFailure(e instanceof QueryFailed ? e : new QueryFailed(String(e)));
    }
  };

  const load = (q: SavedQuery) => {
    setText(q.query);
    run(q.query);
  };

  return (
    <div className="flex h-full bg-background text-foreground">
      <SavedRail
        queries={saved}
        onLoad={load}
        onDelete={async name => { await deleteSaved(name); refreshSaved(); }}
      />

      <div className="flex-1 flex flex-col min-w-0">
        <div className="border-b border-border p-3 flex flex-col gap-2">
          <Editor
            value={text}
            onChange={setText}
            onRun={() => run()}
            running={running}
            errorPosition={failure?.position}
            errorMessage={failure?.message}
          />

          <div className="flex items-center gap-2 text-xs">
            <button
              type="button"
              onClick={() => setNaming(true)}
              disabled={!text.trim()}
              className="flex items-center gap-1 px-2 py-1 border border-border hover:bg-accent/40 disabled:opacity-40"
            >
              <Bookmark size={12} /> Save
            </button>
            <button
              type="button"
              onClick={showExplain}
              disabled={!text.trim()}
              className="flex items-center gap-1 px-2 py-1 border border-border hover:bg-accent/40 disabled:opacity-40"
            >
              <Code2 size={12} /> {explain ? 'Hide SQL' : 'Explain'}
            </button>

            {result && (
              <span className="ml-auto text-muted-foreground font-mono">
                {result.kind === 'count'
                  ? `${(result.total ?? 0).toLocaleString()} matching`
                  : `${result.count} ${result.kind === 'groups' ? 'groups' : 'messages'}`}
              </span>
            )}
          </div>

          {explain !== null && (
            <pre className="text-xs font-mono bg-muted/40 border border-border p-2 overflow-x-auto whitespace-pre-wrap">
              {explain}
            </pre>
          )}
        </div>

        <div className="flex-1 min-h-0">
          {running && !result ? (
            <div className="flex items-center justify-center h-full text-muted-foreground gap-2">
              <Loader2 size={16} className="animate-spin" /> Running…
            </div>
          ) : result ? (
            <Results result={result} onDrillDown={drillDown} />
          ) : (
            <StartHere onPick={q => { setText(q); run(q); }} />
          )}
        </div>
      </div>

      {naming && (
        <SaveDialog
          query={text}
          onCancel={() => setNaming(false)}
          onSaved={() => { setNaming(false); refreshSaved(); }}
        />
      )}
    </div>
  );
};

const SavedRail = ({
  queries,
  onLoad,
  onDelete,
}: {
  queries: SavedQuery[];
  onLoad: (q: SavedQuery) => void;
  onDelete: (name: string) => void;
}) => (
  <aside className="w-56 shrink-0 border-r border-border flex flex-col">
    <div className="px-3 py-2 border-b border-border text-xs uppercase tracking-wide text-muted-foreground">
      Saved
    </div>
    <div className="flex-1 overflow-auto">
      {queries.length === 0 ? (
        <p className="px-3 py-3 text-xs text-muted-foreground">
          Nothing saved yet. A saved query is reusable as <code className="font-mono">saved:name</code>.
        </p>
      ) : (
        queries.map(q => (
          <div key={q.name} className="group flex items-center gap-1 px-2 py-1.5 hover:bg-accent/40">
            <button
              type="button"
              onClick={() => onLoad(q)}
              className="flex-1 min-w-0 text-left"
              title={q.query}
            >
              <div className="flex items-center gap-1 text-sm truncate">
                {q.pinned && <Pin size={10} className="shrink-0 text-primary" />}
                {q.title}
              </div>
              <div className="font-mono text-[10px] text-muted-foreground truncate">
                saved:{q.name}
              </div>
            </button>
            <button
              type="button"
              onClick={() => q.name && onDelete(q.name)}
              aria-label={`Delete ${q.title}`}
              className="opacity-0 group-hover:opacity-100 p-1 text-muted-foreground hover:text-destructive"
            >
              <Trash2 size={12} />
            </button>
          </div>
        ))
      )}
    </div>
  </aside>
);

/**
 * The empty state.
 *
 * Real queries rather than a description of the syntax: the fastest way to
 * learn what the language does is to run something and edit it.
 */
const StartHere = ({ onPick }: { onPick: (q: string) => void }) => {
  const examples: Array<[string, string]> = [
    ['is:unread', 'everything unread'],
    ['to:me() after:7d', 'sent to you this week'],
    ['| count by week', 'volume over time'],
    ['| top domain 10', 'who writes to you most'],
    ['has:attachment larger:5mb', 'the big attachments'],
    ['-from:me() is:unread', 'unread, not from you'],
  ];

  return (
    <div className="p-6 max-w-2xl">
      <h2 className="text-sm font-medium mb-1">Write a query</h2>
      <p className="text-xs text-muted-foreground mb-4">
        A filter, then optional <code className="font-mono">|</code> stages. Press Enter to run.
      </p>
      <div className="grid gap-1">
        {examples.map(([q, why]) => (
          <button
            key={q}
            type="button"
            onClick={() => onPick(q)}
            className="flex items-baseline gap-3 text-left px-3 py-2 border border-border hover:bg-accent/40"
          >
            <code className="font-mono text-sm">{q}</code>
            <span className="text-xs text-muted-foreground ml-auto">{why}</span>
          </button>
        ))}
      </div>
    </div>
  );
};

/**
 * Naming a query, or promoting it to a label.
 *
 * Both are offered here because they are the same string: a rule annotator's
 * instruction is a query expression, so "save this" and "label everything this
 * matches" differ only in what is done with the text afterwards.
 */
const SaveDialog = ({
  query,
  onCancel,
  onSaved,
}: {
  query: string;
  onCancel: () => void;
  onSaved: () => void;
}) => {
  const [title, setTitle] = useState('');
  const [pinned, setPinned] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      await saveQuery({ title, query, pinned });
      onSaved();
    } catch (e: any) {
      setError(e.message ?? String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center bg-black/40" onClick={onCancel}>
      <div
        className="bg-background border border-border p-4 w-96 flex flex-col gap-3"
        onClick={e => e.stopPropagation()}
      >
        <h3 className="text-sm font-medium">Save this query</h3>
        <code className="font-mono text-xs bg-muted/40 border border-border p-2 break-all">
          {query}
        </code>

        <label className="flex flex-col gap-1 text-xs">
          Name
          <input
            autoFocus
            value={title}
            onChange={e => setTitle(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && title.trim()) submit(); }}
            placeholder="Acme invoices"
            className="bg-background border border-border px-2 py-1.5 text-sm outline-none focus:border-primary"
          />
        </label>

        <label className="flex items-center gap-2 text-xs">
          <input type="checkbox" checked={pinned} onChange={e => setPinned(e.target.checked)} />
          Pin to the top
        </label>

        <p className="text-[11px] text-muted-foreground flex items-start gap-1">
          <Tag size={11} className="mt-0.5 shrink-0" />
          Reusable as <code className="font-mono">saved:{'{name}'}</code> inside other queries, and
          usable as a rule label with <code className="font-mono">iql annotate create</code>.
        </p>

        {error && <p className="text-xs text-destructive">{error}</p>}

        <div className="flex justify-end gap-2">
          <button type="button" onClick={onCancel} className="px-3 py-1.5 text-sm border border-border">
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={!title.trim() || busy}
            className="flex items-center gap-1 px-3 py-1.5 text-sm bg-primary text-primary-foreground disabled:opacity-50"
          >
            {busy ? <Loader2 size={12} className="animate-spin" /> : <Plus size={12} />}
            Save
          </button>
        </div>
      </div>
    </div>
  );
};
