import { useEffect, useMemo, useRef, useState } from 'react';
import {
  AlertOctagon, Bookmark, Code2, Eye, File, Inbox, Mail, Mails, MoreVertical,
  MessagesSquare, RefreshCw, Search, Send, SlidersHorizontal, Sparkles, Star, Trash2, X,
} from 'lucide-react';
import {
  bulkApply, bulkCount, bulkUndo, labelQuery, listLabels, moveMessages, onMailChanged, setFlag, shownLabels,
  type BulkResult, type LabelCount, type Move,
} from '../../lib/mail';
import { openMessage, openTool, previewMessage, useViewerStore } from '../../lib/tabs';
import { movedByKeyboard, navRow, useRovingFocus } from '../../lib/rovingFocus';
import { sequencePending } from '../../lib/keySequences';
import { refKey, refOf, useSelectionStore } from '../../lib/selection';
import { ariaCurrent } from '../../lib/rowState';
import { HiddenNote } from './HiddenNote';
import { SelectionBar } from './SelectionBar';
import { BulkBar } from './BulkBar';
import { GroupView, GROUPINGS } from './GroupView';
import { AllItems } from './AllItems';
import { Editor } from './Editor';
import { Results } from './Results';
import { Pills } from './Pills';
import { QuickFilters } from './QuickFilters';
import { Rail } from './Rail';
import {
  explainQuery, listSaved, runQuery, saveQuery, QueryFailed,
  deleteSaved, listRailFolders, moveSaved, resetFolders, setFolderHidden,
  type QueryResult, type SavedQuery,
} from './api';
import { listAnnotators, type Annotator } from '../ai/api';
import { useQueryStore, compose, queryStages, queryTerms, type QueryTerm } from '../../lib/filters';
import { hasTerm, joinScope, namesItsFrame, splitScope, type Scope } from '../../lib/scope';
import {
  canThread, isMailQuery, openingFolderQuery, THREAD_STAGE, useThreadingStore,
} from '../../lib/threading';

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
/**
 * The query that shows an annotator's mail.
 *
 * A label and an extractor answer different questions, so they get different
 * terms rather than one spelling that is wrong for half of them:
 *
 *   label:money        it said yes to this message
 *   extract:receipts   it pulled at least one record out of this message
 *
 * Both are ordinary filter terms, so the entry teaches the language the same
 * way `folder:inbox` does — clicking it puts something you could have typed
 * into the bar.
 */
function annotationQuery(a: Annotator): string {
  return a.kind === 'extract' ? `extract:${a.name}` : `label:${a.name}`;
}

/**
 * What to call a row of each result kind.
 *
 * The entity's own name is right for most of them, but two read badly out
 * loud: "12 threads" is a data structure where the user sees conversations,
 * and "12 attachments" counts arrivals where the rows are files.
 */
const resultNoun = (kind: string): string => {
  switch (kind) {
    case 'threads': return 'conversations';
    case 'attachments': return 'files';
    default: return kind;
  }
};

/**
 * Which field holds a result's rows, for the kinds that page.
 *
 * A kind absent from this map does not page, and that is a statement about the
 * kind rather than an omission: an aggregate has no stable next page, because
 * paging a GROUP BY by row offset returns a different answer every time the
 * underlying data moves. `Options.Offset` in the planner says the same thing
 * from the other side.
 */
const PAGED_ROWS: Record<string, string | undefined> = {
  messages: 'messages',
  threads: 'threads',
  contacts: 'contacts',
  attachments: 'attachments',
  tickets: 'tickets',
  drafts: 'drafts',
  logs: 'logs',
  events: 'events',
};

/**
 * The folder in front of the query box: always there, never typed over, and
 * not removable — a different folder is chosen in the rail.
 */
const ScopeChip = ({ label, icon: Icon }: { label: string; icon?: React.ComponentType<{ size?: number; className?: string }> }) => (
  <span
    className="shrink-0 flex items-center gap-1 px-2 py-1 text-xs leading-4 font-semibold border border-border bg-muted/50"
    title="The folder this query is inside. Choose another in the rail."
    aria-label={`Inside ${label}`}
  >
    {Icon && <Icon size={12} className="text-muted-foreground" />}
    {label} <span aria-hidden className="text-muted-foreground font-normal">›</span>
  </span>
);

export const Desk = () => {
  const [messages, setMessages] = useState<any[]>([]);
  const openMessageId = useViewerStore(s => s.messageId);
  const [loading, setLoading] = useState(true);
  const query = useQueryStore(s => s.text);
  const setQuery = useQueryStore(s => s.set);

  // The query's top-level terms, from the server's lexer: what the folder is,
  // which labels are on, what the box should hold. Kept with the query they
  // describe, so terms for an older query are never read as this one's.
  const [termsFor, setTermsFor] = useState<{ q: string; terms: QueryTerm[] } | null>(null);
  useEffect(() => {
    let cancelled = false;
    queryTerms(query).then(terms => { if (!cancelled) setTermsFor({ q: query, terms }); })
      .catch(() => { if (!cancelled) setTermsFor({ q: query, terms: [] }); });
    return () => { cancelled = true; };
  }, [query]);
  const terms = termsFor?.q === query ? termsFor.terms : null;

  // All Items is not a query about one kind of thing: it searches them all,
  // so it is a mode of the Desk rather than a folder in the query.
  const [allItems, setAllItems] = useState(false);
  const [allItemsText, setAllItemsText] = useState('');
  // What All Items last searched for, as distinct from what is being typed.
  const [allItemsQuery, setAllItemsQuery] = useState('');

  // What is being typed: the query without its folder. The folder is a chip in
  // front of the box, so it is always there and never typed over; Run puts it
  // back in front. Seeded from the shared query each time that changes, so the
  // box and the results can never describe different things.
  const [queryText, setQueryText] = useState('');
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
  const threaded = stages.includes(THREAD_STAGE);

  // Following the preference when it changes.
  //
  // The opening query is already seeded from it, so this is only for the
  // moment somebody flips the setting with the Desk open — without it the
  // switch does nothing until the next reload, which reads as broken.
  //
  // It runs on the preference, not on the query: reacting to the query too
  // would put the stage back the instant the Threads button removed it, and
  // the button would stop working.
  const threading = useThreadingStore(s => s.threading);
  const appliedThreading = useRef(threading);
  useEffect(() => {
    if (appliedThreading.current === threading) return;
    appliedThreading.current = threading;

    const wanted = threading === 'threaded';
    if (wanted === threaded) return;
    // Only where the stage means something. A query that counts, or that is
    // about contacts, is left exactly as it was.
    if (wanted && !canThread(query, stages)) return;

    let cancelled = false;
    compose(query, wanted ? { stage: THREAD_STAGE } : { dropStage: THREAD_STAGE })
      .then(next => { if (!cancelled) setQuery(next); })
      .catch(() => {
        // The preference is still saved; the query in the bar simply keeps the
        // shape it had, and the next one opens threaded.
      });
    return () => { cancelled = true; };
  }, [threading, threaded, query, stages, setQuery]);

  const [result, setResult] = useState<QueryResult | null>(null);
  const [failure, setFailure] = useState<QueryFailed | null>(null);

  // The folder, and the rest. The last known split stands while the terms for
  // a new query are on their way, so the chip does not flicker to All Mail.
  const lastSplit = useRef<{ scope: Scope; rest: string }>({
    scope: { id: 'inbox', label: 'Inbox', term: 'folder:inbox', mail: true }, rest: '',
  });
  const split = useMemo(() => {
    if (!terms) return lastSplit.current;
    const next = splitScope(query, terms, result?.kind);
    lastSplit.current = next;
    return next;
  }, [terms, query, result?.kind]);
  const scope = split.scope;
  const folder = scope.id;
  useEffect(() => {
    if (terms) setQueryText(splitScope(query, terms).rest);
    // Only when a new query's terms arrive: typing must not be overwritten.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [termsFor]);

  // Run: the folder in front of what was typed — unless what was typed names a
  // folder or kind of its own, which then wins rather than being ANDed with
  // the chip into nothing.
  const runTyped = async () => {
    const typed = queryText.trim();
    const own = typed ? await queryTerms(typed).catch(() => [] as QueryTerm[]) : [];
    setQuery(namesItsFrame(own) ? typed : joinScope(scope.term, typed));
  };
  const clearAll = () => setQuery(joinScope(scope.term, ''));

  // The builder is for composing by hand; most of the time it is in the way.
  const [builder, setBuilder] = useState(() => {
    try { return localStorage.getItem('iql.desk.builder') === 'on'; } catch { return false; }
  });
  const toggleBuilder = () => setBuilder(b => {
    try { localStorage.setItem('iql.desk.builder', b ? 'off' : 'on'); } catch { /* per viewer */ }
    return !b;
  });
  const [saved, setSaved] = useState<SavedQuery[]>([]);
  // The mailbox rows somebody has turned off. Hidden, never deleted: these are
  // the mailbox itself, and losing Inbox with no way back is a bad afternoon.
  const [hiddenFolders, setHiddenFolders] = useState<string[]>([]);
  const [explain, setExplain] = useState<string | null>(null);

  useEffect(() => {
    listRailFolders()
      .then(b => setHiddenFolders(b.hidden ?? []))
      .catch(() => {
        // A rail that cannot read its own preferences shows everything, which
        // is the safe direction.
      });
  }, []);

  useEffect(() => {
    listSaved().then(setSaved).catch(() => {
      // The rail's saved section is a convenience; losing it should not stop
      // anyone reading their mail.
    });
  }, []);

  // What the annotators have found, as rail entries.
  //
  // An annotator's results were reachable only by knowing to type
  // `label:money` or `extract:receipts` — the same gap the Other block was
  // written to close for contacts and files. The counts come from the
  // annotator listing rather than a query apiece: `progress.matched` is
  // exactly what `label:x` returns, which is checkable (money 34, receipts
  // 31) and one request instead of a dozen.
  const [annotators, setAnnotators] = useState<Annotator[]>([]);
  useEffect(() => {
    listAnnotators()
      .then(list => setAnnotators(Array.isArray(list) ? list : []))
      .catch(() => {
        // Same as above: a missing section is better than a blank mailbox.
      });
  }, []);

  // Only the ones holding something.
  //
  // The starter pack is eleven, and on a mailbox where nothing has run yet
  // that is eleven rows of "0" burying the entries that would actually show
  // you mail. An annotator with no results is not a place to go; it is a job
  // to run, and the Annotators panel is where that is done.
  const annotated = annotators.filter(a => (a.progress?.matched ?? 0) > 0);

  // The mail labels — yours, and Gmail's — for the rail. The system ones are
  // the rows above; Important is shown here only when something carries it.
  const [mailLabels, setMailLabels] = useState<LabelCount[]>([]);
  const fetchLabels = () => {
    listLabels()
      .then(ls => setMailLabels(ls.filter(l => !l.system || (l.name === 'important' && l.total > 0))))
      .catch(() => {});
  };
  useEffect(fetchLabels, []);

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
      // Every list kind pages, not just messages.
      //
      // This used to return early for anything but a message list, with
      // hasMore false — which was not "no infinite scroll", it was silent
      // truncation. `in:contacts` on a real mailbox showed 50 of 57, with
      // nothing on screen saying so and no way to reach the other seven. The
      // planner has had OFFSET for every kind all along; only this stopped
      // asking for it.
      const rowsField = PAGED_ROWS[res.kind];
      if (!rowsField) {
        // An aggregate or a count, which genuinely has no next page: paging a
        // GROUP BY by row offset gives a different answer each time the data
        // moves, which is worse than no paging. The scroll state resets rather
        // than pointing at an offset nothing will use.
        setResult(res);
        setMessages([]);
        setHasMore(false);
        setOffset(0);
        return;
      }

      const incoming: any[] = (res as any)[rowsField] ?? [];
      if (isLoadMore) {
        // Merged into the result, because the lists read their rows from it.
        setResult(prev => {
          const previous: any[] = prev ? (prev as any)[rowsField] ?? [] : [];
          return { ...res, [rowsField]: [...previous, ...incoming] } as QueryResult;
        });
        setMessages(prev => (res.kind === 'messages' ? [...prev, ...incoming] : prev));
      } else {
        setResult(res);
        setMessages(res.kind === 'messages' ? incoming : []);
      }
      setOffset(currentOffset + incoming.length);
      setHasMore(incoming.length === 50);
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

  // Mail changed — a move made here or in the viewer, or new mail the server
  // reported. The list, the counts and the rail's labels all follow.
  //
  // Through a ref, because the handler is registered once and the query it
  // must re-run changes.
  //
  // The server's reports are gathered for a moment and refresh the list only
  // when it is scrolled to the first page: a first sync reports every few
  // seconds for hours, and reloading under somebody reading page four would
  // throw them back to the top each time. The counts always follow.
  const refreshRef = useRef<(source: 'local' | 'server') => void>(() => {});
  const serverTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  refreshRef.current = (source) => {
    if (source === 'local') {
      fetchMessages(0); fetchCounts(); fetchLabels();
      return;
    }
    if (serverTimer.current) return;
    serverTimer.current = setTimeout(() => {
      serverTimer.current = null;
      if (offset <= 50) fetchMessages(0);
      fetchCounts(); fetchLabels();
    }, 1500);
  };
  useEffect(() => {
    const off = onMailChanged(source => refreshRef.current(source));
    return () => { off(); if (serverTimer.current) clearTimeout(serverTimer.current); };
  }, []);

  // What a mail shortcut acts on: the selection, or else the row with focus.
  const targetIds = (): string[] => {
    if (selectedMessageIds.length > 0) return selectedMessageIds;
    const ref = refOf(document.activeElement);
    return ref?.kind === 'message' ? [ref.id] : [];
  };
  const labelsOf = (ids: string[]) =>
    ids.map(id => messages.find(m => m.id === id)?.labels ?? []);
  const [labelOpen, setLabelOpen] = useState(false);

  // Every match selected, not just the loaded page: the actions then go to
  // the server as the query. Offered once the whole page is selected and
  // there is more than the page; dropped when the query or the selection
  // changes, because it describes this query and nothing else.
  const [allMatching, setAllMatching] = useState<{ query: string; count: number } | null>(null);
  const [offerCount, setOfferCount] = useState<number | null>(null);
  // How many the query matches, for "1–50 of 1,234" and "Select all 1,234".
  // The folder's own total is the wrong number once anything is added to it.
  const [matchCount, setMatchCount] = useState<number | null>(null);
  useEffect(() => {
    setMatchCount(null);
    if (result?.kind !== 'messages') return;
    if (!hasMore) { setMatchCount(messages.length); return; }
    let cancelled = false;
    bulkCount(query).then(n => { if (!cancelled) setMatchCount(n); }).catch(() => {});
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, result?.kind, hasMore && offset <= 50]);
  const pageSelected = result?.kind === 'messages' && messages.length > 0 && messages.every(isSelected);
  const offerAll = pageSelected && hasMore && !allMatching;
  useEffect(() => { setAllMatching(null); setOfferCount(null); }, [query]);
  useEffect(() => { if (selectedCount === 0) setAllMatching(null); }, [selectedCount]);
  useEffect(() => {
    if (!offerAll || offerCount !== null) return;
    if (matchCount !== null) { setOfferCount(matchCount); return; }
    bulkCount(query).then(setOfferCount).catch(() => setOfferCount(null));
  }, [offerAll, offerCount, query, matchCount]);
  const where = split.rest ? `${scope.label} › ${split.rest}` : scope.label;

  // Grouping the folder — by sender, domain, importance — for processing a
  // kind of mail at once. Only for mail; another kind has its own views.
  const [groupBy, setGroupBy] = useState<string | null>(null);
  const grouping = groupBy !== null && scope.mail && !allItems;

  // The last bulk change, with its undo, until the next one or a while passes.
  const [lastBulk, setLastBulk] = useState<{ said: string; undo: unknown } | null>(null);
  useEffect(() => {
    if (!lastBulk) return;
    const t = window.setTimeout(() => setLastBulk(null), 20_000);
    return () => window.clearTimeout(t);
  }, [lastBulk]);
  const bulkDone = (said: string, out: BulkResult) => {
    setLastBulk({ said, undo: out.undo });
    setAllMatching(null);
    clearSelection();
  };

  const move = async (to: Move, ids = targetIds()) => {
    if (allMatching) {
      const said = { archive: 'Archived', trash: 'Trashed', spam: 'Marked as spam', inbox: 'Moved to the inbox',
        untrash: 'Restored', notspam: 'Restored' }[to];
      const out = await bulkApply(allMatching.query, to);
      bulkDone(`${said} ${out.changed.toLocaleString()} of ${out.matched.toLocaleString()}.`, out);
      return;
    }
    if (ids.length === 0) return;
    await moveMessages(ids, to);
    clearSelection();
  };

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
    archive: { label: 'Archive', icon: Mails,        empty: 'Nothing archived. Archiving takes a message out of the inbox; it stays in All Mail.' },
    drafts:  { label: 'Drafts',  icon: File,         empty: 'No drafts. Messages you start and have not sent wait here.' },
    allmail: { label: 'All Mail', icon: Mails,       empty: 'No mail yet. Add an account, or import mail, to begin.' },
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
    // No badge: All Mail is everything, and its size is not a thing you act on.
    { id: 'allmail', label: 'All Mail', icon: Mails },
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
  // `*` then a letter, as in Gmail: `* a` selects the page, `* n` nothing.
  const starPending = useRef<number | null>(null);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    const moving = ['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key);
    const typingHere = (e.target as HTMLElement).closest?.('input, textarea, [contenteditable="true"], [role="dialog"]');
    if (!typingHere && !e.metaKey && !e.ctrlKey && !e.altKey) {
      if (starPending.current !== null) {
        window.clearTimeout(starPending.current);
        starPending.current = null;
        if (e.key === 'a') { e.preventDefault(); selectReplaceAll(messages.map(messageRef)); return; }
        if (e.key === 'n') { e.preventDefault(); setAllMatching(null); clearSelection(); return; }
      }
      if (e.key === '*') {
        e.preventDefault();
        starPending.current = window.setTimeout(() => { starPending.current = null; }, 1500);
        return;
      }
      if (e.key === 'x') {
        // Toggle the focused row in or out of the selection, without moving.
        const ref = refOf(document.activeElement);
        if (ref) { e.preventDefault(); selectToggle(ref); return; }
      }
    }
    // Read before the hook moves focus: extending a selection has to include
    // the row you extended *from*, and after the move there is no way to know
    // what that was.
    const from = refOf(document.activeElement);

    // Gmail's keys, on the selection or the focused row. Never while typing:
    // the query editor sits inside this pane.
    const typing = (e.target as HTMLElement).closest?.('input, textarea, [contenteditable="true"], [role="dialog"]');
    if (!typing && !e.metaKey && !e.ctrlKey && !e.altKey && !sequencePending()) {
      const ids = targetIds();
      const shortcut: Record<string, () => void> = {
        e: () => move('archive', ids),
        '#': () => move('trash', ids),
        '!': () => move('spam', ids),
        l: () => setLabelOpen(true),
        s: () => {
          const starred = ids.every(id => messages.find(m => m.id === id)?.flags?.includes('\\Flagged'));
          setFlag(ids, '\\Flagged', !starred);
        },
        I: () => setFlag(ids, '\\Seen', true),
        U: () => setFlag(ids, '\\Seen', false),
      };
      // Every match selected: the same keys act on all of them, on the server.
      if (allMatching) {
        const bulk = (action: 'read' | 'unread' | 'star', said: string) => async () => {
          const out = await bulkApply(allMatching.query, action);
          bulkDone(`${said} ${out.changed.toLocaleString()} of ${out.matched.toLocaleString()}.`, out);
        };
        shortcut.I = bulk('read', 'Marked read');
        shortcut.U = bulk('unread', 'Marked unread');
        shortcut.s = bulk('star', 'Starred');
        delete shortcut.l;
      }
      if (ids.length > 0 && shortcut[e.key]) {
        e.preventDefault();
        if (e.key === 'l' && selectedMessageIds.length === 0) selectOnly(refOf(document.activeElement)!);
        shortcut[e.key]();
        return;
      }
    }

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
      <Rail
        folders={navItems}
        hiddenFolders={hiddenFolders}
        saved={saved}
        annotated={annotated}
        labels={mailLabels}
        labelQuery={labelQuery}
        queryText={queryText}
        activeFolder={allItems ? 'allitems' : scope.id}
        isActive={q => !allItems && Boolean(terms) && (hasTerm(terms!, q) || split.rest === q.trim())}
        onAllItems={() => {
          // Whatever was being searched for is searched for everywhere.
          const words = split.rest.replace(/\|.*$/, '').trim();
          setAllItems(true); setAllItemsText(words); setAllItemsQuery(words);
        }}
        onLabel={async q => {
          setAllItems(false);
          // A label is mail. Inside a folder it is added, or taken off if it is
          // on; from cards or people it opens that label across all mail.
          if (!scope.mail) { setQuery(q); return; }
          if (terms && hasTerm(terms, q)) setQuery(await compose(query, { remove: q }));
          else setQuery(await compose(query, { add: q }));
        }}
        onFolder={async id => {
          setAllItems(false);
          // Coming back from contacts, files or tickets, the query is about a
          // different kind of thing and adding a folder to it produces
          // `in:contacts kind:person folder:inbox` — still contacts, now with
          // a term that means nothing there.
          if (!isMailQuery(query)) {
            setQuery(openingFolderQuery(`folder:${id}`));
            return;
          }
          setQuery(await compose(query, { field: 'folder', term: `folder:${id}` }));
        }}
        onQuery={async q => {
          setAllItems(false);
          // A saved query runs inside the folder, unless it names its own
          // folder or kind — "People" is about contacts wherever you are.
          const own = await queryTerms(q).catch(() => [] as QueryTerm[]);
          setQuery(namesItsFrame(own) || !scope.mail ? q : joinScope(scope.term, q));
        }}
        annotationQuery={annotationQuery}
        onMove={async (name, to) => {
          try {
            const body = await moveSaved(name, to);
            setSaved(body.queries ?? []);
          } catch {
            // The rail keeps the order it had; the next load corrects it.
          }
        }}
        onRemove={async name => {
          if (!window.confirm(`Remove "${name}" from the rail? The query is deleted.`)) return;
          try {
            await deleteSaved(name);
            setSaved(await listSaved());
          } catch {
            // Same: nothing moves rather than the rail lying about what is there.
          }
        }}
        onEdit={q => setQuery(q.query)}
        onToggleFolder={async (id, hidden) => {
          try {
            const body = await setFolderHidden(id, hidden);
            setHiddenFolders(body.hidden ?? []);
          } catch {
            // Unchanged rather than half-applied.
          }
        }}
        onResetFolders={async () => {
          try {
            await resetFolders();
            setHiddenFolders([]);
          } catch {}
        }}
        onAddDefaults={() => openTool('settings', 'Settings')}
      />

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
            value={allItems ? allItemsText : queryText}
            onChange={allItems ? setAllItemsText : setQueryText}
            onRun={allItems ? () => setAllItemsQuery(allItemsText) : runTyped}
            running={loading}
            errorPosition={allItems ? undefined : failure ? Math.max(0, (failure.position ?? 0) - (scope.term ? scope.term.length + 1 : 0)) : undefined}
            errorMessage={allItems ? undefined : failure?.message}
            scope={<ScopeChip label={allItems ? 'All Items' : scope.label} icon={allItems ? Search : undefined} />}
            placeholder={allItems ? 'Search mail, files, cards, people and events' : undefined}
          />
          {/* The same query, as pills — the builder. Not a second copy: every
              edit is written back through the composer. Off by default; most
              work is done with the rail and the filters. */}
          {builder && !allItems && <Pills query={query} onChange={setQuery} />}
          {!allItems && (
            <QuickFilters
              scope={scope}
              terms={terms}
              onToggle={async (term, on, remove) => {
                let next = query;
                for (const r of remove) next = await compose(next, { remove: r });
                next = await compose(next, on ? { add: term } : { remove: term });
                setQuery(next);
              }}
            />
          )}

          {!allItems && <div className="flex items-center gap-2 text-xs">
            <button
              type="button"
              aria-pressed={builder}
              onClick={toggleBuilder}
              title={builder ? 'Hide the query builder' : 'Show the query as pills you can edit'}
              className={`flex items-center gap-1 px-2 py-1 border ${
                builder ? 'border-primary bg-primary/10 text-primary' : 'border-border hover:bg-accent/40'
              }`}
            >
              <SlidersHorizontal size={12} /> Builder
            </button>
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
            {scope.mail && !allItems && (
              <label className="flex items-center gap-1 text-muted-foreground">
                Group by
                <select aria-label="Group by" value={groupBy ?? ''}
                  onChange={e => setGroupBy(e.target.value || null)}
                  className={`bg-background border px-1 py-0.5 text-xs ${groupBy ? 'border-primary text-primary' : 'border-border text-foreground'}`}>
                  <option value="">—</option>
                  {GROUPINGS.map(g => <option key={g.id} value={g.id}>{g.label}</option>)}
                </select>
              </label>
            )}
            {result && result.kind !== 'messages' && (
              <span className="ml-auto text-muted-foreground font-mono">
                {result.kind === 'count'
                  ? `${(result.total ?? 0).toLocaleString()} matching`
                  : `${result.count} ${resultNoun(result.kind)}`}
              </span>
            )}
          </div>}
          {explain !== null && (
            <pre className="text-xs font-mono bg-muted/40 border border-border p-2 overflow-x-auto whitespace-pre-wrap">{explain}</pre>
          )}
        </div>

        {/* Above the results rather than inside the message list, because
            selection is a property of Desk and not of messages — it used to
            exist in exactly one of the five modes this pane can be in. */}
        {allMatching ? (
          <BulkBar
            count={allMatching.count}
            where={where}
            query={allMatching.query}
            labels={mailLabels.map(l => l.name)}
            onDone={bulkDone}
            onClear={() => { setAllMatching(null); clearSelection(); }}
          />
        ) : (
        <SelectionBar
          onNarrow={async (field, values) => setQuery(await compose(query, { field, values }))}
          // Offered only when everything selected is a message, because that
          // is the only kind this action means anything for. A conversation
          // and a ticket do not have a read flag.
          messageIds={selectedMessageIds}
          onSetFlag={async (flag, on) => {
            await setFlag(selectedMessageIds, flag, on);
          }}
          onMove={to => move(to, selectedMessageIds)}
          messageLabels={labelsOf(selectedMessageIds)}
          labelOpen={labelOpen}
          onLabelOpenChange={setLabelOpen}
        />
        )}
        {offerAll && offerCount !== null && offerCount > messages.length && (
          <div className="flex shrink-0 items-center justify-center gap-2 border-b border-border bg-muted/40 px-3 py-1.5 text-xs">
            <span>All {messages.length} on this page are selected.</span>
            <button type="button" onClick={() => setAllMatching({ query, count: offerCount })}
              className="font-semibold text-primary hover:underline">
              Select all {offerCount.toLocaleString()} in {where}
            </button>
          </div>
        )}
        {lastBulk && (
          <div role="status" className="flex shrink-0 items-center gap-3 border-b border-border bg-green-600/10 px-3 py-1.5 text-xs">
            <span className="flex-1">{lastBulk.said}</span>
            <button type="button" className="font-semibold text-primary hover:underline"
              onClick={async () => {
                const undo = lastBulk.undo;
                setLastBulk(null);
                const n = await bulkUndo(undo);
                setLastBulk({ said: `Undone: ${n.toLocaleString()} back as they were.`, undo: null });
              }}
              hidden={lastBulk.undo === null}>
              Undo
            </button>
            <button type="button" aria-label="Dismiss" onClick={() => setLastBulk(null)} className="text-muted-foreground"><X size={12} /></button>
          </div>
        )}

        <HiddenNote
          result={result}
          onShow={async () => setQuery(await compose(query, { field: 'folder', term: 'folder:all' }))}
        />

        {/* Anything that is not a list of messages renders through the shared
            result view: groups, counts, tickets, drafts. The message list below
            keeps its own rendering, and every affordance that came with it. */}
        {allItems ? (
          <AllItems
            query={allItemsQuery}
            onSeeAll={q => { setAllItems(false); setQuery(q); }}
          />
        ) : grouping ? (
          <GroupView
            query={query}
            by={groupBy!}
            labels={mailLabels.map(l => l.name)}
            onShow={async term => { setGroupBy(null); setQuery(await compose(query, { add: term })); }}
            onDone={(said, out) => setLastBulk({ said, undo: out.undo })}
          />
        ) : result && result.kind !== 'messages' ? (
          <div className="flex-1 min-h-0">
            <Results
              // Handed to the list rather than wrapped around it: each one owns
              // its own `overflow-auto` container, and the scroll event does not
              // bubble, so a handler on this div would never fire.
              onScroll={handleScroll}
              result={result}
              onDrillDown={async (terms, stage) => {
                // One composer call per part rather than one assembled string:
                // the server owns where a term goes and where a stage goes,
                // and it already knows that a second term for the same field
                // replaces the first and that a terminal stage replaces the
                // terminal that is there.
                let next = query;
                for (const term of Array.isArray(terms) ? terms : [terms]) {
                  next = await compose(next, { add: term });
                }
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
            {messages.length === 0 ? '0' : `1-${messages.length}`} of {(matchCount ?? messages.length).toLocaleString()}
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
                {/* The folder's own empty message is about the folder; with
                    something added to it, what is empty is the match. */}
                <span className="italic">{split.rest
                  ? `Nothing in ${scope.label} matches ${split.rest}.`
                  : meta?.empty ?? 'Nothing here.'}</span>
                {split.rest !== '' && (
                  <button onClick={clearAll} className="text-xs font-semibold text-primary hover:underline not-italic">
                    Show all of {scope.label}
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
                // What the viewer is showing, which outlives focus — this list
                // has drawn it as a left bar since before `rowState` named the
                // idea, and keeps its own denser styling because it also has
                // unread and selected states to show in the same row.
                aria-current={ariaCurrent(isOpen)}
                // Preview follows an arrow key, so arrowing updates an
                // already-open viewer. It deliberately does not bring the
                // viewer forward: doing that on every keypress is what made the
                // first Down key switch tabs and the second one do nothing.
                //
                // Gated on the key, not on focus arriving. Tabbing back into
                // this list focuses the row the roving tab stop remembers, and
                // previewing there would pull the viewer off whatever the user
                // had just opened from another pane.
                onFocus={() => { if (movedByKeyboard()) previewMessage(msg); }}
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
                  selected && isOpen
                    ? 'bg-primary/20 ring-1 ring-inset ring-primary/50 border-l-4 border-l-primary z-[1]'
                    : selected
                    ? 'bg-primary/10 hover:bg-primary/15 dark:bg-primary/20 dark:hover:bg-primary/25 ring-1 ring-inset ring-primary/30 border-l-4 border-l-primary/60 z-[1]'
                    : isOpen
                    ? 'bg-primary/15 ring-1 ring-inset ring-primary/40 border-l-4 border-l-primary z-[1]'
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
                  {(() => {
                    const starred = msg.flags?.includes('\\Flagged');
                    return (
                      <button
                        type="button"
                        aria-label={starred ? 'Unstar' : 'Star'}
                        aria-pressed={starred}
                        title={starred ? 'Unstar (s)' : 'Star (s)'}
                        onClick={(e) => { e.stopPropagation(); setFlag([msg.id], '\\Flagged', !starred); }}
                        className="shrink-0"
                      >
                        <Star className={`w-4 h-4 transition-colors ${starred ? 'fill-yellow-400 text-yellow-500' : 'text-muted-foreground/40 hover:text-yellow-500'}`} />
                      </button>
                    );
                  })()}
                  <div className="w-4 flex items-center justify-center" title={isOpen ? "Currently viewing this message" : undefined}>
                    {isOpen ? (
                      <Eye 
                        className="w-3.5 h-3.5 text-primary shrink-0 animate-in fade-in" 
                      />
                    ) : null}
                  </div>
                </div>
                <div className={`w-48 truncate mr-4 text-sm flex items-center gap-2 ${isOpen ? 'font-bold text-foreground' : isUnread ? 'font-bold' : selected ? 'font-semibold text-foreground' : 'text-foreground/70'}`}>
                  {isDraft && (
                    <span className="shrink-0 px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider bg-amber-500/15 text-amber-700 dark:text-amber-400">
                      Draft
                    </span>
                  )}
                  <span className={`truncate ${isDraft && !msg.to?.length ? 'italic text-muted-foreground' : ''}`}>{who}</span>
                </div>
                <div className="flex-1 truncate flex items-center gap-2 text-foreground/90">
                  {shownLabels(msg.labels).map((l: string) => (
                    <span key={l} className="shrink-0 px-1.5 py-0.5 text-[10px] font-medium bg-muted text-muted-foreground border border-border/60">
                      {l}
                    </span>
                  ))}
                  <span className={`text-sm ${isOpen ? 'font-semibold text-foreground' : isUnread ? 'font-bold' : 'font-medium'}`}>{msg.subject || '(No Subject)'}</span>
                  <span className="text-sm text-muted-foreground opacity-60">— {msg.body?.substring(0, 100).replace(/\n/g, ' ')}</span>
                </div>
                {/* In the flow rather than floated over the date, and
                    `invisible` rather than removed, so the space is reserved
                    whether or not the cursor is here. Overlaying it hid the
                    date on hover; adding it on hover made the row twitch. */}
                <button
                  type="button"
                  title="Find messages like this one"
                  aria-label="Find messages like this one"
                  onClick={e => {
                    e.stopPropagation();
                    setQuery(`similar:${msg.id}`);
                  }}
                  // opacity rather than visibility: `group-hover:visible` does
                  // not take effect in this build while the opacity variant
                  // does, and an opacity-0 element still occupies its space,
                  // which is the property that matters here.
                  className="ml-2 shrink-0 border border-border p-1 opacity-0 transition-opacity
                             hover:bg-accent group-hover:opacity-100 focus:opacity-100"
                >
                  <Sparkles className="h-3 w-3 text-muted-foreground" />
                </button>

                <div className={`ml-3 text-xs tabular-nums whitespace-nowrap ${isOpen ? 'font-bold text-primary' : isUnread ? 'font-bold text-primary' : 'text-muted-foreground'}`}>
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
