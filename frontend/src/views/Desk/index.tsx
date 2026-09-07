import { useEffect, useRef, useState } from 'react';
import {
  AlertOctagon, Bookmark, Code2, Eye, File, Inbox, Layout, Mail, MoreVertical,
  Plus, RefreshCw, Send, Star, Trash2,
} from 'lucide-react';
import { openMessage, useViewerStore } from '../../lib/tabs';
import { Editor } from './Editor';
import { Results } from './Results';
import {
  explainQuery, listSaved, runQuery, saveQuery, QueryFailed,
  type QueryResult, type SavedQuery,
} from './api';
import { useQueryStore, compose } from '../../lib/filters';

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

  const [selectedMessageIds, setSelectedMessageIds] = useState<Set<string>>(new Set());
  const [focusedIndex, setFocusedIndex] = useState<number>(-1);
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);

  const [counts, setCounts] = useState<Record<string, { total: number; unread: number }>>({});

  useEffect(() => {
    useViewerStore.getState().setSelectedCount(selectedMessageIds.size);
  }, [selectedMessageIds.size]);

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
        setFocusedIndex(-1);
        setSelectedMessageIds(new Set());
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

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (messages.length === 0) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      let newIndex = focusedIndex;
      if (e.key === 'ArrowDown') {
        newIndex = Math.min(focusedIndex + 1, messages.length - 1);
      } else {
        newIndex = Math.max(focusedIndex - 1, 0);
      }
      
      setFocusedIndex(newIndex);
      const msg = messages[newIndex];
      
      if (e.shiftKey) {
        const newSelected = new Set(selectedMessageIds);
        newSelected.add(msg.id);
        setSelectedMessageIds(newSelected);
      } else {
        setSelectedMessageIds(new Set([msg.id]));
      }
      openMessage(msg);
    }
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

      <div className="flex-1 flex flex-col min-w-0" tabIndex={0} onKeyDown={handleKeyDown}>
        <div className="border-b border-border p-2.5 flex flex-col gap-2">
          <Editor
            value={queryText}
            onChange={setQueryText}
            onRun={() => setQuery(queryText)}
            running={loading}
            errorPosition={failure?.position}
            errorMessage={failure?.message}
          />
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
            {result && result.kind !== 'messages' && (
              <span className="ml-auto text-muted-foreground font-mono">
                {result.kind === 'count'
                  ? `${(result.total ?? 0).toLocaleString()} matching`
                  : `${result.count} ${result.kind}`}
              </span>
            )}
          </div>
          {explain !== null && (
            <pre className="text-xs font-mono bg-muted/40 border border-border p-2 overflow-x-auto whitespace-pre-wrap">{explain}</pre>
          )}
        </div>

        {/* Anything that is not a list of messages renders through the shared
            result view: groups, counts, tickets, drafts. The message list below
            keeps its own rendering, and every affordance that came with it. */}
        {result && result.kind !== 'messages' ? (
          <div className="flex-1 min-h-0">
            <Results result={result} onDrillDown={async term => setQuery(await compose(query, { add: term }))} />
          </div>
        ) : (
        <>
        <div className="h-12 border-b border-border flex items-center px-4 gap-2 sticky top-0 bg-background/80 backdrop-blur-md z-10">
          <div className="p-2 flex items-center">
            <input 
              type="checkbox" 
              className="border-border cursor-pointer"
              checked={messages.length > 0 && selectedMessageIds.size === messages.length}
              ref={(el) => {
                if (el) {
                  el.indeterminate = selectedMessageIds.size > 0 && selectedMessageIds.size < messages.length;
                }
              }}
              onChange={(e) => {
                if (e.target.checked) {
                  setSelectedMessageIds(new Set(messages.map(m => m.id)));
                } else {
                  setSelectedMessageIds(new Set());
                }
              }}
              title={selectedMessageIds.size === messages.length ? "Deselect all" : "Select all"}
            />
          </div>
          {selectedMessageIds.size > 0 && (
            <div className="flex items-center gap-2 pl-1 pr-2 animate-in fade-in">
              <span className="text-xs font-semibold text-primary px-2 py-0.5 bg-primary/10 rounded">
                {selectedMessageIds.size} selected
              </span>
              <button 
                onClick={() => setSelectedMessageIds(new Set())}
                className="text-xs text-muted-foreground hover:text-foreground underline transition-colors"
              >
                Clear
              </button>
            </div>
          )}
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

        <div className="flex-1 overflow-auto" onScroll={handleScroll}>
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
          {messages.map((msg, index) => {
            const isUnread = !msg.flags?.includes('\\Seen');
            // A draft has no sender — it has not been sent by anyone yet — so
            // the column that would show From shows who it is addressed to.
            const isDraft = msg.flags?.includes('\\Draft');
            const who = isDraft
              ? (msg.to?.length ? `To: ${msg.to.join(', ')}` : 'No recipient')
              : (msg.from || '(No Sender)');
            const isOpen = msg.id === openMessageId;
            const isSelected = selectedMessageIds.has(msg.id);
            return (
              <div 
                key={msg.id}
                onClick={(e) => {
                  setFocusedIndex(index);
                  if (e.shiftKey) {
                    const newSelected = new Set(selectedMessageIds);
                    newSelected.add(msg.id);
                    setSelectedMessageIds(newSelected);
                  } else if (e.metaKey || e.ctrlKey) {
                    const newSelected = new Set(selectedMessageIds);
                    if (newSelected.has(msg.id)) newSelected.delete(msg.id);
                    else newSelected.add(msg.id);
                    setSelectedMessageIds(newSelected);
                  } else {
                    setSelectedMessageIds(new Set([msg.id]));
                  }
                  openMessage(msg);
                }}
                className={`flex items-center px-4 py-2 border-b border-border/50 cursor-pointer transition-all group relative ${
                  isOpen
                    ? 'bg-primary/15 ring-1 ring-inset ring-primary/40 border-l-4 border-l-primary z-[1]'
                    : isSelected
                    ? 'bg-primary/5 hover:bg-primary/10 ring-1 ring-inset ring-primary/20 border-l-4 border-l-transparent'
                    : isUnread
                    ? 'bg-accent/20 hover:bg-accent/40 border-l-4 border-l-transparent'
                    : 'hover:bg-accent/40 border-l-4 border-l-transparent'
                }`}
              >
                <div className="flex items-center gap-2.5 mr-3 shrink-0">
                  <input 
                    type="checkbox" 
                    checked={isSelected} 
                    onChange={(e) => {
                      const newSelected = new Set(selectedMessageIds);
                      if (e.target.checked) newSelected.add(msg.id);
                      else newSelected.delete(msg.id);
                      setSelectedMessageIds(newSelected);
                    }}
                    onClick={(e) => e.stopPropagation()} 
                    className="border-border cursor-pointer" 
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
