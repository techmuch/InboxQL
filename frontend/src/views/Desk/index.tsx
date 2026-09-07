import { useEffect, useRef, useState } from 'react';
import {
  AlertOctagon, Bookmark, Code2, Eye, File, Inbox, Layout, Mail, MoreVertical,
  MessagesSquare, Plus, RefreshCw, Send, Star, Trash2,
} from 'lucide-react';
import { openMessage, previewMessage, useViewerStore } from '../../lib/tabs';
import { navRow, useRovingFocus } from '../../lib/rovingFocus';
import { refKey, refOf, useSelectionStore } from '../../lib/selection';
import { SelectionBar } from './SelectionBar';
import { Editor } from './Editor';
import { Results } from './Results';
import { Pills } from './Pills';
import {
  explainQuery, listSaved, runQuery, saveQuery, setMessageFlag, QueryFailed,
  type QueryResult, type SavedQuery,
} from './api';
import { useQueryStore, compose, queryStages } from '../../lib/filters';

/**
 * Desk — one surface over mail, drafts and tickets.
 *
 * # Folders were always queries
 *
 * `folderClause()` has always returned a SQL predicate per folder; folders.go
 * says so itself. So the rail is a list of queries, the built-in entries are
 * `folder:inbox` and friends, and a saved query sits beside them
 * indistinguishably. Clicking one puts its query in the bar, which is how the
 * language gets taught without anyone reading documentation.
 *
 * # Why the merge went this way round
 *
 * The mailbox already had infinite scroll, keyboard navigation, shift
 * multi-select and the viewer; the workbench had a query bar and a result-kind
 * switch. Growing the mailbox is two additions. Growing the workbench meant
 * rebuilding a mail client, so the message list below is the original one,
 * wrapped rather than rewritten.
 */
export const Desk = () => {
  const [messages, setMessages] = useState<any[]>([]);
  const openMessageId = useViewerStore(s => s.messageId);
  const [loading, setLoading] = useState(true);
  const [folder, setFolder] = useState('inbox');
  const query = useQueryStore(s => s.text);
  const setQuery = useQueryStore(s => s.set);
  const clearAll = () => setQuery('');

  // What is being typed. It is seeded from the shared query and commits back
  // to it, so the bar and the results can never describe different things —
  // which they did while a separate list of cross-filter terms was silently
  // concatenated onto whatever the bar showed.
  const [queryText, setQueryText] = useState(query);
  useEffect(() => { setQueryText(query); }, [query]);
  // What actually ran, as distinct from what is being typed.
  //
  // Deriving the fetch from the live text meant a query per keystroke: typing
  // `| top domain 5` ran `| top domai` on the way past, and that failure
  // landed after the good result and painted an error over it. A query runs
  // when it is submitted.

  // Which pipeline stages the committed query carries, asked for rather than
  // pattern-matched out of the string: `| timeline` inside a quoted value is
  // not a stage, and only the lexer knows that.
  const [stages, setStages] = useState<string[]>([]);
  useEffect(() => {
    let cancelled = false;
    queryStages(query)
      .then(s => { if (!cancelled) setStages(s.map(x => x.verb)); })
      .catch(() => { if (!cancelled) setStages([]); });
    return () => { cancelled = true; };
  }, [query]);
  const threaded = stages.includes('timeline');

  const [result, setResult] = useState<QueryResult | null>(null);
  const [failure, setFailure] = useState<QueryFailed | null>(null);
  const [saved, setSaved] = useState<SavedQuery[]>([]);
  const [explain, setExplain] = useState<string | null>(null);

  useEffect(() => {
    listSaved().then(setSaved).catch(() => {
      // The rail's saved section is a convenience; losing it should not stop
      // anyone reading their mail.
    });
  }, []);

  // Selection lives outside Desk and is typed, so it survives a mode change
  // and can describe a conversation without pretending to be a message.
  const selection = useSelectionStore(s => s.refs);
  const selectOnly = useSelectionStore(s => s.only);
  const selectToggle = useSelectionStore(s => s.toggle);
  const selectAdd = useSelectionStore(s => s.add);
  const selectReplaceAll = useSelectionStore(s => s.replaceAll);
  const clearSelection = useSelectionStore(s => s.clear);
  const selectedCount = Object.keys(selection).length;

  /** A message, as something selectable. `id:` is how a chosen set is written. */
  const messageRef = (m: any) => ({
    kind: 'message' as const, id: m.id, label: m.subject, field: 'id',
  });
  const isSelected = (m: any) => Boolean(selection[refKey(messageRef(m))]);
  // The message ids in the selection, for actions that only apply to messages.
  const selectedMessageIds = Object.values(selection)
    .filter(r => r.kind === 'message')
    .map(r => r.id);

  // Arrow keys walk whatever is on screen, in the order it is painted. There
  // is deliberately no focused-row state here: the DOM already knows which
  // element has focus, and a second copy of that would be the thing that
  // disagreed with it.
  const { ref: navRef, onKeyDown: navKeyDown } = useRovingFocus<HTMLDivElement>();
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);

  const [counts, setCounts] = useState<Record<string, { total: number; unread: number }>>({});

  useEffect(() => {
    useViewerStore.getState().setSelectedCount(selectedCount);
  }, [selectedCount]);

  // The query the rail and the cross-filters compose to.
  //
  // Terms concatenate because they are terms in one expression rather than
  // fields in a struct — which is what let the three separate filter
  // implementations behind this collapse into one.
  const effectiveQuery = query;

  // The request in flight. A slower earlier response must not overwrite a
  // faster later one — the same guard the editor's completions need.
  const runSeq = useRef(0);

  const fetchMessages = async (currentOffset = 0) => {
    const isLoadMore = currentOffset > 0;
    if (isLoadMore) setLoadingMore(true);
    else setLoading(true);
    setFailure(null);
    const seq = ++runSeq.current;
    try {
      const res = await runQuery(effectiveQuery, 50, currentOffset);
      if (seq !== runSeq.current) return;
      if (res.kind !== 'messages') {
        // An aggregate, a ticket list, a draft list. None of them page, so the
        // scroll state is reset rather than left pointing at a message offset.
        setResult(res);
        setMessages([]);
        setHasMore(false);
        setOffset(0);
        return;
      }

      const newMessages = res.messages ?? [];
      setResult(res);
      if (isLoadMore) {
        setMessages(prev => [...prev, ...newMessages]);
      } else {
        setMessages(newMessages);
      }
      setOffset(currentOffset + newMessages.length);
      setHasMore(newMessages.length === 50);
    } catch (e) {
      if (seq !== runSeq.current) return;
      if (e instanceof QueryFailed) {
        setFailure(e);
        setResult(null);
        setMessages([]);
        setHasMore(false);
      } else {
        console.error('Failed to run the query', e);
      }
    } finally {
      if (seq === runSeq.current) {
        if (isLoadMore) setLoadingMore(false);
        else setLoading(false);
      }
    }
  };

  /**
   * Counts come from the server, across every folder at once.
   *
   * They used to be derived from the loaded page, which meant the sidebar
   * reported "how many of the fifty messages on screen are unread" while
   * looking like a mailbox total.
   */
  const fetchCounts = async () => {
    try {
      const res = await fetch('/api/messages/counts');
      if (!res.ok) return;
      const rows = await res.json();
      const byFolder: Record<string, { total: number; unread: number }> = {};
      for (const r of rows) byFolder[r.folder] = { total: r.total, unread: r.unread };
      setCounts(byFolder);
    } catch { /* the sidebar renders without counts */ }
  };

  useEffect(() => {
    fetchMessages(0);
  }, [effectiveQuery]);

  useEffect(() => {
    fetchCounts();
  }, [effectiveQuery]);

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    const now = new Date();
    if (date.toDateString() === now.toDateString()) {
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' });
  };

  /**
   * What each folder is, in one place.
   *
   * The empty state matters more here than it looks: "your inbox is empty"
   * shown under Trash reads as a bug. Each folder says what being empty
   * actually means for it, and Sent and Drafts say it without implying the
   * user should go and sync something.
   */
  const folderMeta: Record<string, { label: string; icon: any; empty: string }> = {
    inbox:   { label: 'Inbox',   icon: Inbox,        empty: 'Nothing in the inbox. Sync an account or import mail to begin.' },
    starred: { label: 'Starred', icon: Star,         empty: 'No starred messages. Flagged mail collects here.' },
    sent:    { label: 'Sent',    icon: Send,         empty: 'No sent mail. Messages you sent — or imported from a Sent folder — appear here.' },
    drafts:  { label: 'Drafts',  icon: File,         empty: 'No drafts. Composed but unsent messages wait here for approval.' },
    spam:    { label: 'Spam',    icon: AlertOctagon, empty: 'No spam. Mail flagged as junk lands here.' },
    trash:   { label: 'Trash',   icon: Trash2,       empty: 'Trash is empty. Deleted mail is kept here, not removed.' },
  };

  const navItems = [
    // Inbox and Spam show unread, because that is the number you act on.
    // The rest show totals: an unread count on Sent or Drafts is meaningless.
    { id: 'inbox',   label: 'Inbox',   icon: Inbox,        badge: counts.inbox?.unread },
    { id: 'starred', label: 'Starred', icon: Star,         badge: counts.starred?.total },
    { id: 'sent',    label: 'Sent',    icon: Send,         badge: counts.sent?.total },
    { id: 'drafts',  label: 'Drafts',  icon: File,         badge: counts.drafts?.total },
    { id: 'spam',    label: 'Spam',    icon: AlertOctagon, badge: counts.spam?.unread },
    { id: 'trash',   label: 'Trash',   icon: Trash2,       badge: counts.trash?.total },
  ];

  // The detail view used to live here, replacing the list and requiring a
  // back button to escape. It is a tab of its own now, so the list stays put
  // and reading the next message does not mean navigating backwards first.

  // Movement is the hook's job and knows nothing about messages, so it works
  // in every mode. Selection is layered on afterwards, and only for rows that
  // are messages — a Set of message ids does not describe a conversation or an
  // aggregate bucket, and pretending it does would be a worse lie than not
  // supporting multi-select there.
  const handleKeyDown = (e: React.KeyboardEvent) => {
    const moving = ['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key);
    // Read before the hook moves focus: extending a selection has to include
    // the row you extended *from*, and after the move there is no way to know
    // what that was.
    const from = refOf(document.activeElement);

    navKeyDown(e);
    if (!moving) return;

    // Whatever kind of row it is. The handler asks the row rather than knowing
    // — which is what makes selection work in every mode without a branch per
    // mode.
    const ref = refOf(document.activeElement);
    if (!ref) return;

    if (!e.shiftKey) {
      selectOnly(ref);
      return;
    }
    // Extending includes the row extended *from*, which a plain add would drop.
    if (from) selectAdd(from);
    selectAdd(ref);
  };

  // Clicking selects, in every mode, for the same reason arrowing does: the
  // handler asks the row what it is instead of knowing. Wiring this per row
  // meant only the message list had it — every other mode could be navigated
  // to but not selected.
  const handleClick = (e: React.MouseEvent) => {
    const ref = refOf(e.target as HTMLElement);
    if (!ref) return;
    if (e.shiftKey) selectAdd(ref);
    else if (e.metaKey || e.ctrlKey) selectToggle(ref);
    else selectOnly(ref);
  };

  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const { scrollTop, scrollHeight, clientHeight } = e.currentTarget;
    if (scrollHeight - scrollTop <= clientHeight * 1.5) {
      if (hasMore && !loadingMore && !loading) {
        fetchMessages(offset);
      }
    }
  };

  return (
    <div className="flex h-full bg-background text-foreground overflow-hidden">
      <div className="w-64 flex flex-col pt-4 border-r border-border/50 bg-card/20">
        <div className="px-4 mb-6">
          <button className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2  shadow-sm hover:shadow-md transition-all font-semibold text-[13px]">
            <Plus className="w-4 h-4" /> Compose
          </button>
        </div>
        <div className="flex-1 overflow-auto px-2 space-y-0.5">
          {navItems.map(item => {
            // Every rail entry is a query. The built-ins are folder:inbox and
            // friends; a saved query below is the same kind of thing.
            const entryQuery = `folder:${item.id}`;
            const active = queryText.trim() === entryQuery;
            return (
              <button
                key={item.id}
                onClick={async () => {
                  // Replace the folder facet, keep whatever else narrows the
                  // view. Replacing the whole query would silently discard a
                  // cross-filter someone had just applied.
                  setFolder(item.id);
                  setQuery(await compose(query, { field: 'folder', term: entryQuery }));
                }}
                title={entryQuery}
                className={`w-full flex items-center gap-4 px-4 py-2.5  text-sm transition-colors ${active ? 'bg-primary/10 text-primary font-bold' : 'hover:bg-accent text-foreground/70'}`}
              >
                <item.icon className={`w-4 h-4 ${active ? 'text-primary' : 'text-muted-foreground'}`} />
                <span className="flex-1 text-left">{item.label}</span>
                {item.badge ? <span className="text-[10px] font-bold tabular-nums">{item.badge}</span> : null}
              </button>
            );
          })}

          {saved.length > 0 && (
            <>
              <div className="px-4 pt-4 pb-1 text-[10px] font-bold uppercase tracking-wider text-muted-foreground">Saved</div>
              {saved.map(q => (
                <button
                  key={q.name}
                  // A saved query is a complete expression, not a facet, so it
                  // replaces rather than merges.
                  onClick={() => setQuery(q.query)}
                  title={q.query}
                  className={`w-full flex items-center gap-4 px-4 py-2  text-sm transition-colors ${queryText.trim() === q.query.trim() ? 'bg-primary/10 text-primary font-bold' : 'hover:bg-accent text-foreground/70'}`}
                >
                  <Bookmark className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
                  <span className="flex-1 text-left truncate">{q.title}</span>
                </button>
              ))}
            </>
          )}

          <div className="px-4 pt-4 pb-1 text-[10px] font-bold uppercase tracking-wider text-muted-foreground">Other</div>
          {[
            { label: 'Tickets', q: 'in:tickets -status:done -status:rejected' },
            { label: 'Proposed', q: 'in:tickets status:proposed' },
          ].map(entry => (
            <button
              key={entry.label}
              onClick={() => setQuery(entry.q)}
              title={entry.q}
              className={`w-full flex items-center gap-4 px-4 py-2  text-sm transition-colors ${queryText.trim() === entry.q ? 'bg-primary/10 text-primary font-bold' : 'hover:bg-accent text-foreground/70'}`}
            >
              <Layout className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
              <span className="flex-1 text-left truncate">{entry.label}</span>
            </button>
          ))}
        </div>
      </div>

      {/* The ref sits on the whole pane rather than on the list, so pressing
          Down anywhere in Desk that is not a text field moves into the
          results. Focus previously had to land on this div by tabbing, and
          clicking a row did not put it there — which is why the keyboard
          worked only by accident. */}
      <div
        ref={navRef}
        className="flex-1 flex flex-col min-w-0"
        tabIndex={0}
        onKeyDown={handleKeyDown}
        onClick={handleClick}
      >
        <div className="border-b border-border p-2.5 flex flex-col gap-2">
          <Editor
            value={queryText}
            onChange={setQueryText}
            onRun={() => setQuery(queryText)}
            running={loading}
            errorPosition={failure?.position}
            errorMessage={failure?.message}
          />
          {/* The same query, as pills. Not a second copy — every edit is
              written back through the composer, so the box and the pills can
              never describe different things. */}
          <Pills query={query} onChange={setQuery} />

          <div className="flex items-center gap-2 text-xs">
            <button
              type="button"
              onClick={async () => {
                const title = window.prompt('Name this query', '');
                if (!title) return;
                try {
                  await saveQuery({ title, query: effectiveQuery });
                  setSaved(await listSaved());
                } catch (e: any) {
                  setFailure(new QueryFailed(e.message ?? String(e)));
                }
              }}
              className="flex items-center gap-1 px-2 py-1 border border-border hover:bg-accent/40"
            >
              <Bookmark size={12} /> Save
            </button>
            <button
              type="button"
              onClick={async () => {
                if (explain) { setExplain(null); return; }
                try {
                  const res = await explainQuery(effectiveQuery);
                  setExplain(res.sql ?? '');
                } catch (e: any) {
                  setFailure(e instanceof QueryFailed ? e : new QueryFailed(String(e)));
                }
              }}
              className="flex items-center gap-1 px-2 py-1 border border-border hover:bg-accent/40"
            >
              <Code2 size={12} /> {explain ? 'Hide SQL' : 'Explain'}
            </button>
            {/* Grouping by conversation is not a display mode hiding beside
                the query — it is a stage in it. So the button writes the
                query, the query bar shows what changed, and there is no second
                piece of state that can disagree with the results. */}
            <button
              type="button"
              aria-pressed={threaded}
              title={threaded
                ? 'Back to a flat list'
                : 'Group into conversations, with the tickets and drafts they produced'}
              onClick={async () =>
                setQuery(await compose(query,
                  threaded ? { dropStage: 'timeline' } : { stage: 'timeline' }))
              }
              className={`flex items-center gap-1 px-2 py-1 border ${
                threaded
                  ? 'border-primary bg-primary/10 text-primary'
                  : 'border-border hover:bg-accent/40'
              }`}
            >
              <MessagesSquare size={12} /> Threads
            </button>
            {result && result.kind !== 'messages' && (
              <span className="ml-auto text-muted-foreground font-mono">
                {result.kind === 'count'
                  ? `${(result.total ?? 0).toLocaleString()} matching`
                  : `${result.count} ${result.kind === 'threads' ? 'conversations' : result.kind}`}
              </span>
            )}
          </div>
          {explain !== null && (
            <pre className="text-xs font-mono bg-muted/40 border border-border p-2 overflow-x-auto whitespace-pre-wrap">{explain}</pre>
          )}
        </div>

        {/* Above the results rather than inside the message list, because
            selection is a property of Desk and not of messages — it used to
            exist in exactly one of the five modes this pane can be in. */}
        <SelectionBar
          onNarrow={async (field, values) => setQuery(await compose(query, { field, values }))}
          // Offered only when everything selected is a message, because that
          // is the only kind this action means anything for. A conversation
          // and a ticket do not have a read flag.
          messageIds={selectedMessageIds}
          onSetFlag={async (flag, on) => {
            await setMessageFlag(selectedMessageIds, flag, on);
            fetchMessages(0);
            fetchCounts();
          }}
        />

        {/* Anything that is not a list of messages renders through the shared
            result view: groups, counts, tickets, drafts. The message list below
            keeps its own rendering, and every affordance that came with it. */}
        {result && result.kind !== 'messages' ? (
          <div className="flex-1 min-h-0">
            <Results
              result={result}
              onDrillDown={async (term, stage) => {
                // Two composer calls rather than one string: the server owns
                // where a term goes and where a stage goes, and it already
                // knows that adding a terminal stage replaces the terminal
                // that is there.
                let next = await compose(query, { add: term });
                if (stage) next = await compose(next, { stage });
                setQuery(next);
              }}
            />
          </div>
        ) : (
        <>
        <div className="h-12 border-b border-border flex items-center px-4 gap-2 sticky top-0 bg-background/80 backdrop-blur-md z-10">
          <div className="p-2 flex items-center">
            <input 
              type="checkbox" 
              className="border-border cursor-pointer"
              checked={messages.length > 0 && messages.every(isSelected)}
              ref={(el) => {
                if (el) {
                  el.indeterminate = selectedCount > 0 && !messages.every(isSelected);
                }
              }}
              onChange={(e) => {
                if (e.target.checked) selectReplaceAll(messages.map(messageRef));
                else clearSelection();
              }}
              title={messages.length > 0 && messages.every(isSelected) ? 'Deselect all' : 'Select all'}
            />
          </div>
          <button onClick={() => { fetchMessages(0); fetchCounts(); }} className={`p-2 hover:bg-accent  transition-colors ${loading ? 'animate-spin' : ''}`}>
            <RefreshCw className="w-4 h-4 text-muted-foreground" />
          </button>
          <button className="p-2 hover:bg-accent  transition-colors"><MoreVertical className="w-4 h-4 text-muted-foreground" /></button>
          <div className="flex-1" />
          {/* The page shows at most 50; the total comes from the server so
              the two numbers are not the same number twice. */}
          <div className="text-xs text-muted-foreground font-medium tabular-nums px-4">
            {messages.length === 0 ? '0' : `1-${messages.length}`} of {counts[folder]?.total ?? messages.length}
          </div>
        </div>

        {/* No pill row. The bar above shows the query itself, so a second
            rendering of the same terms directly beneath it is noise — and a
            second rendering is precisely how the two came to disagree. */}

        {/* Its rows are options now, so the list says what it is. Before this
            the message list was a stack of plain divs: unreachable by Tab and
            silent to a screen reader. */}
        <div
          className="flex-1 overflow-auto"
          role="listbox"
          aria-label="Messages"
          aria-multiselectable="true"
          onScroll={handleScroll}
        >
          {messages.length === 0 && !loading && (() => {
            const meta = folderMeta[folder];
            const EmptyIcon = meta?.icon ?? Mail;
            return (
              <div className="flex flex-col items-center justify-center h-full text-muted-foreground space-y-4 px-8 text-center">
                <EmptyIcon className="w-12 h-12 opacity-10" />
                <span className="italic">{meta?.empty ?? 'Nothing here.'}</span>
                {query.trim() !== '' && (
                  <button onClick={clearAll} className="text-xs font-semibold text-primary hover:underline not-italic">
                    <code className="font-mono">{query}</code> matched nothing — clear it
                  </button>
                )}
              </div>
            );
          })()}
          {messages.map(msg => {
            const isUnread = !msg.flags?.includes('\\Seen');
            // A draft has no sender — it has not been sent by anyone yet — so
            // the column that would show From shows who it is addressed to.
            const isDraft = msg.flags?.includes('\\Draft');
            const who = isDraft
              ? (msg.to?.length ? `To: ${msg.to.join(', ')}` : 'No recipient')
              : (msg.from || '(No Sender)');
            const isOpen = msg.id === openMessageId;
            const selected = isSelected(msg);
            return (
              <div
                key={msg.id}
                {...navRow}
                // Read by the keyboard handler to extend selection, and the
                // only thing that tells it this row is a message at all.
                // What this row is, read by the click handlers, the keyboard
                // handler and the toolbar alike. None of them branch on mode.
                data-sel-kind="message"
                data-sel-id={msg.id}
                data-sel-field="id"
                data-sel-label={msg.subject}
                role="option"
                aria-selected={selected}
                // Preview follows focus, so arrowing updates an already-open
                // viewer. It deliberately does not bring the viewer forward:
                // doing that on every keypress is what made the first Down key
                // switch tabs and the second one do nothing.
                onFocus={() => previewMessage(msg)}
                // Selection is handled by the pane, which does it the same way
                // for every kind of row. This only has to say what a plain
                // click on a message means, which is "read it".
                //
                // A modified click is a selection gesture, not a reading one,
                // and must not bring the viewer forward — the viewer is a tab,
                // so opening it hides the list you are selecting in.
                onClick={(e) => {
                  if (e.shiftKey || e.metaKey || e.ctrlKey) return;
                  openMessage(msg);
                }}
                className={`flex items-center px-4 py-2 border-b border-border/50 cursor-pointer transition-all group relative ${
                  isOpen
                    ? 'bg-primary/15 ring-1 ring-inset ring-primary/40 border-l-4 border-l-primary z-[1]'
                    : selected
                    ? 'bg-primary/5 hover:bg-primary/10 ring-1 ring-inset ring-primary/20 border-l-4 border-l-transparent'
                    : isUnread
                    ? 'bg-accent/20 hover:bg-accent/40 border-l-4 border-l-transparent'
                    : 'hover:bg-accent/40 border-l-4 border-l-transparent'
                }`}
              >
                <div className="flex items-center gap-2.5 mr-3 shrink-0">
                  <input
                    type="checkbox"
                    checked={selected}
                    onChange={() => selectToggle(messageRef(msg))}
                    onClick={(e) => e.stopPropagation()}
                    aria-label={`Select ${msg.subject || 'message'}`}
                  />
                  <Star className="w-4 h-4 text-muted-foreground/40 hover:text-yellow-500 transition-colors" />
                  <div className="w-4 flex items-center justify-center" title={isOpen ? "Currently viewing this message" : undefined}>
                    {isOpen ? (
                      <Eye 
                        className="w-3.5 h-3.5 text-primary shrink-0 animate-in fade-in" 
                      />
                    ) : null}
                  </div>
                </div>
                <div className={`w-48 truncate mr-4 text-sm flex items-center gap-2 ${isOpen ? 'font-bold text-foreground' : isUnread ? 'font-bold' : 'text-foreground/70'}`}>
                  {isDraft && (
                    <span className="shrink-0 px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider bg-amber-500/15 text-amber-700 dark:text-amber-400">
                      Draft
                    </span>
                  )}
                  <span className={`truncate ${isDraft && !msg.to?.length ? 'italic text-muted-foreground' : ''}`}>{who}</span>
                </div>
                <div className="flex-1 truncate flex items-center gap-2 text-foreground/90">
                  <span className={`text-sm ${isOpen ? 'font-semibold text-foreground' : isUnread ? 'font-bold' : 'font-medium'}`}>{msg.subject || '(No Subject)'}</span>
                  <span className="text-sm text-muted-foreground opacity-60">— {msg.body?.substring(0, 100).replace(/\n/g, ' ')}</span>
                </div>
                <div className={`ml-4 text-xs tabular-nums whitespace-nowrap ${isOpen ? 'font-bold text-primary' : isUnread ? 'font-bold text-primary' : 'text-muted-foreground'}`}>
                  {formatDate(msg.date)}
                </div>
              </div>
            );
          })}
        </div>
        </>
        )}
      </div>
    </div>
  );
};
