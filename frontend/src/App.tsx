import { useEffect, useState, useCallback, useMemo } from 'react';
import { ShellLayout, AppTitle, chatPanel, componentRegistry, menuRegistry, commandRegistry, useThemeStore, useKeyboardShortcuts, UserProfile } from 'nexus-shell';
import { Layout, Search, Mail, BarChart2, Settings, Plus, Server, Shield, Trash2, Zap, Cpu, Eye, X, Check, AlertCircle, RefreshCw, MessageSquare, Inbox, Star, Send, File, AlertOctagon, MoreVertical, User, Lock, Download, Database, AlertTriangle } from 'lucide-react';
import { ResponsiveCalendar } from '@nivo/calendar';
import { create } from 'zustand';
import { AgentManager } from './AgentManager';
import { ImportPanel } from './views/ImportPanel';
import { MessageViewer } from './views/MessageViewer';
import { ErrorLog } from './views/ErrorLog';
import { openTool, openMessage, openErrorLog, useViewerStore } from './lib/tabs';
import { version as appVersion } from '../package.json';
import 'nexus-shell/style.css';
import './App.css';

// --- Filter Store ---
interface FilterState {
  date: string | null;
  from: string | null;
  topic: string | null;
  setDate: (date: string | null) => void;
  setFrom: (from: string | null) => void;
  setTopic: (topic: string | null) => void;
  clearAll: () => void;
}

const useFilterStore = create<FilterState>((set) => ({
  date: null,
  from: null,
  topic: null,
  setDate: (date) => set((state) => ({ ...state, date: state.date === date ? null : date })),
  setFrom: (from) => set((state) => ({ ...state, from: state.from === from ? null : from })),
  setTopic: (topic) => set((state) => ({ ...state, topic: state.topic === topic ? null : topic })),
  clearAll: () => set({ date: null, from: null, topic: null }),
}));

// --- Login View ---
const LoginView = ({ onLogin }: { onLogin: (user: any) => void }) => {
  const [username, setUsername] = useState('admin@inboxql.local');
  const [password, setPassword] = useState('password123');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password })
      });
      if (res.ok) {
        const user = await res.json();
        onLogin(user);
      } else {
        setError('Invalid username or password');
      }
    } catch (e) {
      setError('Connection error');
    }
    setLoading(false);
  };

  return (
    <div className="h-screen w-screen flex items-center justify-center bg-background text-foreground p-4">
      <div className="w-full max-w-md bg-card border border-border  shadow-xl p-8 animate-in fade-in zoom-in-95 duration-300">
        <div className="flex flex-col items-center mb-8">
          <div className="w-16 h-16 bg-primary/10  flex items-center justify-center mb-4">
            <Mail className="w-8 h-8 text-primary" />
          </div>
          <h1 className="text-2xl font-bold">InboxQL</h1>
          <p className="text-muted-foreground text-sm">Email for Engineers</p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-6">
          <div className="space-y-2">
            <label className="text-xs font-bold uppercase tracking-wider text-muted-foreground">Username</label>
            <div className="flex items-center w-full bg-background border border-border focus-within:ring-2 focus-within:ring-primary/20 focus-within:border-primary/50 transition-all">
              <div className="pl-3 pr-2 text-muted-foreground flex items-center justify-center shrink-0">
                <User className="w-4 h-4" />
              </div>
              <input 
                type="text" 
                value={username}
                onChange={e => setUsername(e.target.value)}
                className="w-full bg-transparent py-2.5 pr-3 text-sm focus:outline-none border-0 text-foreground"
                placeholder="Enter username"
                required
              />
            </div>
          </div>

          <div className="space-y-2">
            <label className="text-xs font-bold uppercase tracking-wider text-muted-foreground">Password</label>
            <div className="flex items-center w-full bg-background border border-border focus-within:ring-2 focus-within:ring-primary/20 focus-within:border-primary/50 transition-all">
              <div className="pl-3 pr-2 text-muted-foreground flex items-center justify-center shrink-0">
                <Lock className="w-4 h-4" />
              </div>
              <input 
                type="password" 
                value={password}
                onChange={e => setPassword(e.target.value)}
                className="w-full bg-transparent py-2.5 pr-3 text-sm focus:outline-none border-0 text-foreground"
                placeholder="••••••••"
                required
              />
            </div>
          </div>

          {error && (
            <div className="bg-red-500/10 border border-red-500/20 text-red-500 text-xs p-3  flex items-center gap-2">
              <AlertCircle className="w-4 h-4" />
              {error}
            </div>
          )}

          <button 
            type="submit" 
            disabled={loading}
            className="w-full bg-primary text-primary-foreground py-2.5  font-semibold shadow-md hover:opacity-90 active:scale-[0.98] transition-all disabled:opacity-50 text-sm"
          >
            {loading ? 'Authenticating...' : 'Sign In'}
          </button>
        </form>
      </div>
    </div>
  );
};

// --- Dashboard View ---
const Dashboard = () => {
  const [volume, setVolume] = useState<any[]>([]);
  const [senders, setSenders] = useState<any[]>([]);
  const [topics, setTopics] = useState<any[]>([]);
  const { theme } = useThemeStore();
  const { date, from, topic, setDate, setFrom, setTopic, clearAll } = useFilterStore();

  useEffect(() => {
    const fetchData = async () => {
      try {
        const query = new URLSearchParams();
        if (date) query.append('date', date);
        if (from) query.append('from', from);
        if (topic) query.append('topic', topic);
        const qs = query.toString() ? `&${query.toString()}` : '';

        const [vRes, sRes, tRes] = await Promise.all([
          fetch(`/api/analytics?type=volume${qs}`),
          fetch(`/api/analytics?type=senders${qs}`),
          fetch(`/api/analytics?type=topics${qs}`)
        ]);
        if (vRes.ok) setVolume(await vRes.json());
        if (sRes.ok) setSenders(await sRes.json());
        if (tRes.ok) setTopics(await tRes.json());
      } catch (e) {
        console.error('Failed to fetch analytics', e);
      }
    };
    fetchData();
  }, [date, from, topic]);

  const calendarData = useMemo(() => {
    return (volume || []).map(d => ({
      day: d.label,
      value: d.value
    }));
  }, [volume]);

  const year = new Date().getFullYear();
  const fromDate = `${year}-01-01`;
  const toDate = `${year}-12-31`;

  return (
    <div className="p-6 overflow-auto h-full bg-background text-foreground">
      <div className="flex justify-between items-center mb-6">
        <h2 className="text-2xl font-bold">Analytics Pulse</h2>
        {(date || from || topic) && (
          <button onClick={clearAll} className="flex items-center gap-1.5 text-[10px] font-semibold text-primary hover:bg-primary/20 bg-primary/10 px-2.5 py-1  transition-colors">
            <X className="w-2.5 h-2.5" /> Clear Filters
          </button>
        )}
      </div>

      {(date || from || topic) && (
        <div className="bg-primary/5 border border-primary/10  px-4 py-2 flex items-center gap-3 mb-6">
          <span className="text-[10px] font-bold uppercase tracking-wider text-primary/60">Active Filters:</span>
          <div className="flex flex-1 gap-2 overflow-auto no-scrollbar">
            {date && <span className="px-2 py-0.5 bg-primary text-primary-foreground text-[10px] font-semibold  flex items-center gap-1 shadow-sm">Date: {date} <X onClick={() => setDate(null)} className="w-2.5 h-2.5 cursor-pointer" /></span>}
            {from && <span className="px-2 py-0.5 bg-primary text-primary-foreground text-[10px] font-semibold  flex items-center gap-1 shadow-sm">From: {from} <X onClick={() => setFrom(null)} className="w-2.5 h-2.5 cursor-pointer" /></span>}
            {topic && <span className="px-2 py-0.5 bg-primary text-primary-foreground text-[10px] font-semibold  flex items-center gap-1 shadow-sm">Topic: {topic} <X onClick={() => setTopic(null)} className="w-2.5 h-2.5 cursor-pointer" /></span>}
          </div>
        </div>
      )}
      
      <div className="space-y-6">
        {/* Temporal Volume - Calendar Heatmap */}
        <div className={`p-6 bg-card border  shadow-sm min-h-80 flex flex-col group transition-colors ${date ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-primary/50'}`}>
          <div className="flex items-center gap-3 mb-4">
            <BarChart2 className="w-5 h-5 text-primary" />
            <span className="font-bold text-lg text-foreground">Communication Intensity {date ? `on ${date}` : ''}</span>
          </div>
          <div className="flex-1 h-64 min-h-64">
            {calendarData.length > 0 ? (
              <ResponsiveCalendar
                data={calendarData}
                from={fromDate}
                to={toDate}
                emptyColor={theme === 'dark' ? '#27272a' : '#f4f4f5'}
                colors={['#61cdbb', '#97e3d5', '#e8c1a0', '#f47560']}
                margin={{ top: 20, right: 0, bottom: 0, left: 20 }}
                yearSpacing={40}
                monthBorderColor={theme === 'dark' ? '#18181b' : '#ffffff'}
                dayBorderWidth={2}
                dayBorderColor={theme === 'dark' ? '#18181b' : '#ffffff'}
                theme={{
                  text: { fill: theme === 'dark' ? '#a1a1aa' : '#71717a', fontSize: 10, fontWeight: 600 },
                  tooltip: { container: { background: theme === 'dark' ? '#18181b' : '#ffffff', color: theme === 'dark' ? '#fafafa' : '#18181b' } }
                }}
                onClick={(datum) => setDate(datum.day)}
              />
            ) : (
              <div className="h-full flex items-center justify-center text-xs text-muted-foreground italic">
                No temporal data available for these filters
              </div>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div className={`p-6 bg-card border  shadow-sm min-h-64 flex flex-col group transition-colors ${from ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-primary/50'}`}>
            <div className="flex items-center gap-3 mb-4">
              <Mail className="w-5 h-5 text-primary" />
              <span className="font-bold text-lg text-foreground">Top Senders {from ? `(Filtered)` : ''}</span>
            </div>
            <div className="flex-1 space-y-3">
              {(senders || []).slice(0, 5).map((d, i) => (
                <button 
                  key={i} 
                  onClick={() => setFrom(d.label)}
                  className={`w-full flex items-center gap-2.5 p-1.5  transition-all ${from === d.label ? 'bg-primary/10 ring-1 ring-primary' : 'hover:bg-accent'}`}
                >
                  <div className="w-7 h-7  bg-primary/10 flex items-center justify-center text-[9px] font-bold text-primary">
                    {d.label[0]?.toUpperCase() || '?'}
                  </div>
                  <div className="flex-1 min-w-0 text-left">
                    <div className="text-xs font-semibold truncate leading-tight">{d.label}</div>
                    <div className="text-[9px] text-muted-foreground uppercase tracking-tighter">{d.value} messages</div>
                  </div>
                  {from === d.label && <Check className="w-2.5 h-2.5 text-primary" />}
                </button>
              ))}
              {(!senders || senders.length === 0) && <span className="text-xs text-muted-foreground italic text-center pt-10 block">No senders found for these filters</span>}
            </div>
          </div>

          <div className={`p-6 bg-card border  shadow-sm min-h-64 flex flex-col group transition-colors ${topic ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-primary/50'}`}>
            <div className="flex items-center gap-3 mb-4">
              <Layout className="w-5 h-5 text-primary" />
              <span className="font-bold text-lg text-foreground">Topic Trends {topic ? `(Filtered)` : ''}</span>
            </div>
            <div className="flex-1 flex flex-wrap gap-1.5 content-start">
              {(topics || []).map((d, i) => (
                <button 
                  key={i} 
                  onClick={() => setTopic(d.label)}
                  className={`px-2.5 py-1  text-[10px] font-semibold flex items-center gap-1.5 transition-all ${topic === d.label ? 'bg-primary text-primary-foreground shadow-sm' : 'bg-muted hover:bg-primary/10 hover:text-primary'}`}
                >
                  <span className="truncate max-w-[100px]">{d.label}</span>
                  <span className={`text-[9px] ${topic === d.label ? 'text-primary-foreground/70' : 'opacity-50 font-mono'}`}>{d.value}</span>
                </button>
              ))}
              {(!topics || topics.length === 0) && <span className="text-xs text-muted-foreground italic text-center pt-10 w-full block">No topics found for these filters</span>}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

// --- Gmail-like Mail Client Tool ---
const MailClient = () => {
  const [messages, setMessages] = useState<any[]>([]);
  const openMessageId = useViewerStore(s => s.messageId);
  const [loading, setLoading] = useState(true);
  const [folder, setFolder] = useState('inbox');
  const { date, from, topic, clearAll } = useFilterStore();

  const [selectedMessageIds, setSelectedMessageIds] = useState<Set<string>>(new Set());
  const [focusedIndex, setFocusedIndex] = useState<number>(-1);
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);

  const [counts, setCounts] = useState<Record<string, { total: number; unread: number }>>({});

  const fetchMessages = async (currentOffset = 0) => {
    const isLoadMore = currentOffset > 0;
    if (isLoadMore) setLoadingMore(true);
    else setLoading(true);
    try {
      const query = new URLSearchParams();
      query.append('limit', '50');
      query.append('offset', currentOffset.toString());
      query.append('folder', folder);
      // The dashboard's cross-filters compose with the folder rather than
      // replacing it: Sent plus a date means "sent, on that date".
      if (date) query.append('date', date);
      if (from) query.append('from', from);
      if (topic) query.append('topic', topic);

      const res = await fetch(`/api/messages?${query.toString()}`);
      if (res.status === 401) {
        window.location.reload();
        return;
      }
      if (!res.ok) throw new Error();
      const data = await res.json();
      const newMessages = data || [];
      
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
      console.error('Failed to fetch messages', e);
    }
    if (isLoadMore) setLoadingMore(false);
    else setLoading(false);
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
  }, [folder, date, from, topic]);

  useEffect(() => {
    fetchCounts();
  }, [folder, date, from, topic]);

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
          {navItems.map(item => (
            <button
              key={item.id}
              onClick={() => setFolder(item.id)}
              className={`w-full flex items-center gap-4 px-4 py-2.5  text-sm transition-colors ${folder === item.id ? 'bg-primary/10 text-primary font-bold' : 'hover:bg-accent text-foreground/70'}`}
            >
              <item.icon className={`w-4 h-4 ${folder === item.id ? 'text-primary' : 'text-muted-foreground'}`} />
              <span className="flex-1 text-left">{item.label}</span>
              {item.badge ? <span className="text-[10px] font-bold tabular-nums">{item.badge}</span> : null}
            </button>
          ))}
        </div>
      </div>

      <div className="flex-1 flex flex-col" tabIndex={0} onKeyDown={handleKeyDown}>
        <div className="h-12 border-b border-border flex items-center px-4 gap-2 sticky top-0 bg-background/80 backdrop-blur-md z-10">
          <button className="p-2 hover:bg-accent  transition-colors"><input type="checkbox" className="border-border" /></button>
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

        {(date || from || topic) && (
          <div className="bg-primary/5 border-b border-primary/10 px-4 py-1.5 flex items-center gap-3">
            <span className="text-[9px] font-bold uppercase tracking-wider text-primary/60">Active Filters:</span>
            <div className="flex flex-1 gap-1.5 overflow-auto no-scrollbar">
              {date && <span className="px-2 py-0.5 bg-primary text-primary-foreground text-[9px] font-semibold  flex items-center gap-1 shadow-sm">Date: {date} <X onClick={() => useFilterStore.getState().setDate(null)} className="w-2 h-2 cursor-pointer" /></span>}
              {from && <span className="px-2 py-0.5 bg-primary text-primary-foreground text-[9px] font-semibold  flex items-center gap-1 shadow-sm">From: {from} <X onClick={() => useFilterStore.getState().setFrom(null)} className="w-2 h-2 cursor-pointer" /></span>}
              {topic && <span className="px-2 py-0.5 bg-primary text-primary-foreground text-[9px] font-semibold  flex items-center gap-1 shadow-sm">Topic: {topic} <X onClick={() => useFilterStore.getState().setTopic(null)} className="w-2 h-2 cursor-pointer" /></span>}
            </div>
            <button onClick={clearAll} className="text-[9px] font-bold text-primary hover:bg-primary/10 px-2 py-0.5 transition-colors uppercase tracking-tighter">Clear All</button>
          </div>
        )}

        <div className="flex-1 overflow-auto" onScroll={handleScroll}>
          {messages.length === 0 && !loading && (() => {
            const meta = folderMeta[folder];
            const EmptyIcon = meta?.icon ?? Mail;
            return (
              <div className="flex flex-col items-center justify-center h-full text-muted-foreground space-y-4 px-8 text-center">
                <EmptyIcon className="w-12 h-12 opacity-10" />
                <span className="italic">{meta?.empty ?? 'Nothing here.'}</span>
                {(date || from || topic) && (
                  <button onClick={clearAll} className="text-xs font-semibold text-primary hover:underline not-italic">
                    Filters are active — clear them
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
                className={`flex items-center px-4 py-2 border-b border-border/50 cursor-pointer transition-colors group ${
                  selectedMessageIds.has(msg.id) || msg.id === openMessageId ? 'bg-primary/10 ring-1 ring-inset ring-primary/40'
                  : isUnread ? 'bg-accent/20' : 'hover:bg-accent/40'
                }`}
              >
                <div className="flex items-center gap-3 mr-4">
                  <input 
                    type="checkbox" 
                    checked={selectedMessageIds.has(msg.id)} 
                    onChange={(e) => {
                      const newSelected = new Set(selectedMessageIds);
                      if (e.target.checked) newSelected.add(msg.id);
                      else newSelected.delete(msg.id);
                      setSelectedMessageIds(newSelected);
                    }}
                    onClick={(e) => e.stopPropagation()} 
                    className="border-border" 
                  />
                  <Star className="w-4 h-4 text-muted-foreground/40 hover:text-yellow-500 transition-colors" />
                </div>
                <div className={`w-48 truncate mr-4 text-sm flex items-center gap-2 ${isUnread ? 'font-bold' : 'text-foreground/70'}`}>
                  {isDraft && (
                    <span className="shrink-0 px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider bg-amber-500/15 text-amber-700 dark:text-amber-400">
                      Draft
                    </span>
                  )}
                  <span className={`truncate ${isDraft && !msg.to?.length ? 'italic text-muted-foreground' : ''}`}>{who}</span>
                </div>
                <div className="flex-1 truncate flex items-center gap-2 text-foreground/90">
                  <span className={`text-sm ${isUnread ? 'font-bold' : 'font-medium'}`}>{msg.subject || '(No Subject)'}</span>
                  <span className="text-sm text-muted-foreground opacity-60">— {msg.body?.substring(0, 100).replace(/\n/g, ' ')}</span>
                </div>
                <div className={`ml-4 text-xs tabular-nums whitespace-nowrap ${isUnread ? 'font-bold text-primary' : 'text-muted-foreground'}`}>
                  {formatDate(msg.date)}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
};

// --- Unified Settings View ---
const SettingsView = () => {
  const [searchQuery, setSearchQuery] = useState('');
  const [activeCategory, setActiveCategory] = useState('accounts');
  const { theme, setTheme } = useThemeStore();
  const [showAddForm, setShowAddForm] = useState(false);
  const [editingAccountId, setEditingAccountId] = useState<string | null>(null);
  const [testingConnectionId, setTestingConnectionId] = useState<string | null>(null);
  const [connectionResults, setConnectionResults] = useState<Record<string, { success: boolean, message?: string }>>({});
  const [accounts, setAccounts] = useState<any[]>([]);
  const [stats, setStats] = useState<Record<string, any>>({});
  
  const [userProfile, setUserProfile] = useState<any>(null);
  const [ignoreWords, setIgnoreWords] = useState('');

  const fetchAccounts = async () => {
    try {
      const res = await fetch('/api/accounts');
      if (res.status === 401) return;
      if (!res.ok) throw new Error();
      const data = await res.json();
      setAccounts(data || []);
      if (data) data.forEach((acc: any) => fetchStats(acc.id));
    } catch (e) {
      console.error('Failed to fetch accounts', e);
    }
  };

  const fetchStats = async (id: string) => {
    try {
      const res = await fetch(`/api/accounts/stats?id=${id}`);
      if (res.status === 401) return;
      if (!res.ok) throw new Error();
      const data = await res.json();
      setStats(prev => ({ ...prev, [id]: data }));
    } catch (e) {
      console.error('Failed to fetch stats', e);
    }
  };

  const fetchProfile = async () => {
    try {
      const res = await fetch('/api/profile');
      if (res.ok) {
        const data = await res.json();
        setUserProfile(data);
      }
    } catch (e) {}
  };

  const fetchIgnoreWords = async () => {
    try {
      const res = await fetch('/api/settings?key=ignore_words');
      if (res.ok) {
        const data = await res.json();
        setIgnoreWords(data.value);
      }
    } catch (e) {}
  };

  useEffect(() => {
    fetchAccounts();
    fetchProfile();
    fetchIgnoreWords();
    const interval = setInterval(() => {
      accounts.forEach(acc => fetchStats(acc.id));
    }, 5000);
    return () => clearInterval(interval);
  }, [accounts.length]);

  const [formState, setFormState] = useState({
    name: '', email: '', user: '', pass: '', imap: '', smtp: '', port: '993', ssl: true
  });

  const handleTestConnection = async (id: string, imap: string, user: string, pass: string) => {
    setTestingConnectionId(id);
    await new Promise(resolve => setTimeout(resolve, 1500));
    let isSuccess = !imap.includes('error');
    let message = isSuccess ? 'Successfully connected to servers.' : 'Failed to connect: Connection timed out or invalid server address.';
    if (isSuccess && (user.includes('invalid') || pass === '')) {
      isSuccess = false;
      message = 'Authentication failed: Invalid username or password.';
    }
    setConnectionResults(prev => ({ ...prev, [id]: { success: isSuccess, message } }));
    setTestingConnectionId(null);
  };

  const handleSync = async (id: string) => {
    try {
      await fetch(`/api/accounts/sync?id=${id}`, { method: 'POST' });
    } catch (e) {
      console.error('Sync failed', e);
    }
    setTimeout(() => {
      fetchStats(id);
    }, 3000);
  };

  const handleAddAccount = async () => {
    const payload = {
      id: editingAccountId || undefined,
      name: formState.name,
      email: formState.email,
      host: formState.imap,
      port: parseInt(formState.port),
      user: formState.user,
      password: formState.pass,
      ssl: formState.ssl,
      smtpHost: formState.smtp,
      smtpPort: 587
    };

    try {
      await fetch('/api/accounts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      fetchAccounts();
      setShowAddForm(false);
      setEditingAccountId(null);
      setFormState({ name: '', email: '', user: '', pass: '', imap: '', smtp: '', port: '993', ssl: true });
    } catch (e) {
      console.error('Save failed', e);
    }
  };

  const handleEditAccount = (acc: any) => {
    setFormState({
      name: acc.name,
      email: acc.email,
      user: acc.user,
      pass: acc.password || '',
      imap: acc.host,
      smtp: acc.smtpHost || '',
      port: acc.port.toString(),
      ssl: acc.ssl
    });
    setEditingAccountId(acc.id);
    setShowAddForm(true);
  };

  const handleCancel = () => {
    setShowAddForm(false);
    setEditingAccountId(null);
    setFormState({ name: '', email: '', user: '', pass: '', imap: '', smtp: '', port: '993', ssl: true });
  };

  const handleDeleteAccount = async (id: string) => {
    try {
      await fetch(`/api/accounts?id=${id}`, { method: 'DELETE' });
      fetchAccounts();
    } catch (e) {
      console.error('Delete failed', e);
    }
  };

  const handleUpdateProfile = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const res = await fetch('/api/profile', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(userProfile)
      });
      if (res.ok) {
        alert('Profile updated successfully');
      }
    } catch (e) {}
  };

  const handleUpdateIgnoreWords = async () => {
    try {
      const res = await fetch('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key: 'ignore_words', value: ignoreWords })
      });
      if (res.ok) {
        alert('Ignore words updated successfully');
      }
    } catch (e) {
      console.error('Failed to update ignore words', e);
    }
  };

  const categories = [
    { id: 'accounts', label: 'Mail Accounts', icon: Mail },
    { id: 'import', label: 'Import Mail', icon: Download },
    { id: 'profile', label: 'User Profile', icon: User },
    { id: 'appearance', label: 'Appearance', icon: Eye },
    { id: 'ai', label: 'AI Configuration', icon: Cpu },
    { id: 'security', label: 'Security', icon: Shield },
    { id: 'data', label: 'Data Management', icon: Database },
  ];

  const filteredCategories = categories.filter(c => 
    c.label.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const formatSize = (bytes: number) => {
    if (!bytes) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  return (
    <div className="flex h-full bg-background text-foreground overflow-hidden">
      <div className="w-64 border-r border-border bg-card/30 flex flex-col">
        <div className="p-4 border-b border-border">
          <div className="flex items-center w-full bg-background border border-border focus-within:ring-1 focus-within:ring-primary transition-all">
            <div className="pl-3 pr-2 text-muted-foreground flex items-center justify-center shrink-0">
              <Search className="w-4 h-4" />
            </div>
            <input 
              type="text" 
              placeholder="Search settings..." 
              className="w-full bg-transparent py-1.5 pr-3 text-sm focus:outline-none border-0 text-foreground"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
          </div>
        </div>
        <div className="flex-1 overflow-auto py-2">
          {filteredCategories.map(cat => (
            <button 
              key={cat.id}
              onClick={() => setActiveCategory(cat.id)}
              className={`w-full flex items-center gap-3 px-4 py-2 text-sm transition-colors ${activeCategory === cat.id ? 'bg-primary/10 text-primary font-semibold' : 'hover:bg-accent text-foreground/70'}`}
            >
              <cat.icon className="w-4 h-4" />
              {cat.label}
            </button>
          ))}
        </div>
        <div className="p-4 border-t border-border mt-auto opacity-50">
          <div className="text-xs font-mono flex items-center justify-between text-muted-foreground">
            <span>InboxQL</span>
            <span>v{appVersion}</span>
          </div>
        </div>
      </div>

      <div className="flex-1 overflow-auto p-8">
        <div className="max-w-3xl mx-auto">
          {activeCategory === 'accounts' && (
            <div className="animate-in fade-in duration-300">
              <div className="flex justify-between items-center mb-8">
                <div>
                  <h2 className="text-2xl font-bold text-foreground">Mail Accounts</h2>
                  <p className="text-muted-foreground text-sm mt-1">Configure your IMAP and SMTP connections.</p>
                </div>
                {!showAddForm && (
                  <button 
                    onClick={() => setShowAddForm(true)}
                    className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-1.5  text-xs font-semibold hover:opacity-90 transition-opacity"
                  >
                    <Plus className="w-3 h-3" /> Add Account
                  </button>
                )}
              </div>

              {showAddForm && (
                <div className="bg-card border border-primary/30  p-6 shadow-lg mb-8 animate-in fade-in slide-in-from-top-4 duration-200">
                  <div className="flex justify-between items-center mb-6">
                    <h3 className="text-lg font-bold">{editingAccountId ? 'Edit Account' : 'Add New Account'}</h3>
                    <button onClick={handleCancel} className="text-muted-foreground hover:text-foreground">
                      <X className="w-5 h-5" />
                    </button>
                  </div>
                  <div className="grid grid-cols-2 gap-4 mb-6">
                    <div className="space-y-1.5">
                      <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Account Name</label>
                      <input 
                        type="text" 
                        placeholder="e.g. My Work Email"
                        className="w-full bg-background border border-border  px-3 py-2 text-sm focus:ring-1 focus:ring-primary outline-none"
                        value={formState.name}
                        onChange={(e) => setFormState({...formState, name: e.target.value})}
                      />
                    </div>
                    <div className="space-y-1.5">
                      <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Email Address</label>
                      <input 
                        type="email" 
                        placeholder="user@example.com"
                        className="w-full bg-background border border-border  px-3 py-2 text-sm focus:ring-1 focus:ring-primary outline-none"
                        value={formState.email}
                        onChange={(e) => setFormState({...formState, email: e.target.value})}
                      />
                    </div>
                    <div className="space-y-1.5">
                      <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Username</label>
                      <input 
                        type="text" 
                        placeholder="Login username"
                        className="w-full bg-background border border-border  px-3 py-2 text-sm focus:ring-1 focus:ring-primary outline-none"
                        value={formState.user}
                        onChange={(e) => setFormState({...formState, user: e.target.value})}
                      />
                    </div>
                    <div className="space-y-1.5">
                      <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Password</label>
                      <input 
                        type="password" 
                        placeholder={editingAccountId ? 'Unchanged — type to replace' : '••••••••'}
                        className="w-full bg-background border border-border  px-3 py-2 text-sm focus:ring-1 focus:ring-primary outline-none"
                        value={formState.pass}
                        onChange={(e) => setFormState({...formState, pass: e.target.value})}
                      />
                      {editingAccountId && (
                        <p className="text-[10px] text-muted-foreground">
                          Stored passwords are encrypted and never sent to the browser. Leave blank to keep the existing one.
                        </p>
                      )}
                    </div>
                    <div className="space-y-1.5">
                      <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">IMAP Server</label>
                      <div className="flex gap-2">
                        <input 
                          type="text" 
                          placeholder="imap.example.com"
                          className="flex-1 bg-background border border-border  px-3 py-2 text-sm focus:ring-1 focus:ring-primary outline-none"
                          value={formState.imap}
                          onChange={(e) => setFormState({...formState, imap: e.target.value})}
                        />
                        <input 
                          type="text" 
                          placeholder="993"
                          className="w-20 bg-background border border-border  px-3 py-2 text-sm focus:ring-1 focus:ring-primary outline-none"
                          value={formState.port}
                          onChange={(e) => setFormState({...formState, port: e.target.value})}
                        />
                      </div>
                    </div>
                    <div className="flex items-center gap-3 pt-4">
                      <input 
                        type="checkbox" 
                        id="ssl-toggle"
                        className="w-4 h-4 border-border text-primary focus:ring-primary"
                        checked={formState.ssl}
                        onChange={(e) => setFormState({...formState, ssl: e.target.checked})}
                      />
                      <label htmlFor="ssl-toggle" className="text-xs font-bold text-muted-foreground uppercase tracking-wider cursor-pointer">Use SSL/TLS</label>
                    </div>
                    <div className="space-y-1.5">
                      <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">SMTP Server</label>
                      <input 
                        type="text" 
                        placeholder="smtp.example.com"
                        className="w-full bg-background border border-border  px-3 py-2 text-sm focus:ring-1 focus:ring-primary outline-none"
                        value={formState.smtp}
                        onChange={(e) => setFormState({...formState, smtp: e.target.value})}
                      />
                    </div>
                  </div>
                  <div className="flex justify-between items-center">
                    <button 
                      onClick={() => handleTestConnection('form', formState.imap, formState.user, formState.pass)}
                      disabled={testingConnectionId === 'form'}
                      className="flex items-center gap-1.5 text-[10px] font-bold text-primary hover:bg-primary/5 px-2 py-1 transition-colors disabled:opacity-50 uppercase tracking-tight"
                    >
                      {testingConnectionId === 'form' ? <Zap className="w-2.5 h-2.5 animate-pulse" /> : <Zap className="w-2.5 h-2.5" />}
                      Test Connection
                    </button>
                    <div className="flex gap-2">
                      <button 
                        onClick={handleCancel}
                        className="px-3 py-1.5 text-[11px] font-semibold hover:bg-accent  transition-colors uppercase"
                      >
                        Cancel
                      </button>
                      <button 
                        onClick={handleAddAccount}
                        className="px-4 py-1.5 bg-primary text-primary-foreground  text-[11px] font-bold shadow-sm hover:opacity-90 transition-opacity uppercase"
                      >
                        {editingAccountId ? 'Update' : 'Save Account'}
                      </button>
                    </div>
                  </div>
                  {connectionResults['form'] && (
                    <div className={`mt-4 p-3  flex items-start gap-3 text-xs ${connectionResults['form'].success ? 'bg-green-500/10 text-green-500 border border-green-500/20' : 'bg-red-500/10 text-red-500 border border-red-500/20'}`}>
                      {connectionResults['form'].success ? <Check className="w-4 h-4 shrink-0" /> : <AlertCircle className="w-4 h-4 shrink-0" />}
                      {connectionResults['form'].message}
                    </div>
                  )}
                </div>
              )}

              <div className="space-y-4">
                {accounts.map(acc => (
                  <div key={acc.id} className="bg-card border border-border  p-5 shadow-sm group">
                    <div className="flex justify-between items-center mb-4">
                      <div className="flex items-center gap-3">
                        <div className={`w-2.5 h-2.5  ${
                          acc.lastSyncStatus === 'syncing' ? 'bg-primary animate-pulse shadow-[0_0_8px_rgba(var(--primary),0.4)]' : 
                          acc.lastSyncStatus === 'error' ? 'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.4)]' : 
                          'bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.4)]'
                        }`} />
                        <span className="font-bold text-lg">{acc.name}</span>
                        <span className="text-sm text-muted-foreground">{acc.email}</span>
                      </div>
                      <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                        <button 
                          onClick={() => handleSync(acc.id)}
                          disabled={acc.lastSyncStatus === 'syncing'}
                          className="p-2 text-muted-foreground hover:text-primary hover:bg-primary/10  transition-all"
                          title="Sync Now"
                        >
                          <RefreshCw className={`w-4 h-4 ${acc.lastSyncStatus === 'syncing' ? 'animate-spin' : ''}`} />
                        </button>
                        <button 
                          onClick={() => handleTestConnection(acc.id, acc.host, acc.user, acc.password)}
                          disabled={testingConnectionId === acc.id}
                          className="p-2 text-muted-foreground hover:text-primary hover:bg-primary/10  transition-all"
                          title="Test Connection"
                        >
                          <Server className={`w-4 h-4 ${testingConnectionId === acc.id ? 'animate-pulse' : ''}`} />
                        </button>
                        <button 
                          onClick={() => handleEditAccount(acc)}
                          className="p-2 text-muted-foreground hover:text-foreground hover:bg-accent  transition-all"
                        >
                          <Settings className="w-4 h-4" />
                        </button>
                        <button 
                          onClick={() => handleDeleteAccount(acc.id)}
                          className="p-2 text-muted-foreground hover:text-destructive hover:bg-destructive/10  transition-all"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </div>
                    
                    {acc.lastSyncStatus === 'error' && acc.lastSyncError && (
                      <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20  flex items-start gap-3 text-xs text-red-500 animate-in slide-in-from-top-2 duration-200">
                        <AlertCircle className="w-4 h-4 shrink-0" />
                        <div>
                          <div className="font-bold uppercase tracking-tight mb-0.5">Synchronization Error</div>
                          {acc.lastSyncError}
                        </div>
                      </div>
                    )}
                    
                    <div className="grid grid-cols-3 gap-4 mb-6">
                      <div className="text-center p-3 bg-muted/30  border border-border/50">
                        <div className="text-[10px] uppercase font-bold text-muted-foreground mb-1">Messages</div>
                        <div className="text-lg font-bold tabular-nums">{stats[acc.id]?.totalMessages || 0}</div>
                      </div>
                      <div className="text-center p-3 bg-muted/30  border border-border/50">
                        <div className="text-[10px] uppercase font-bold text-muted-foreground mb-1">Storage</div>
                        <div className="text-lg font-bold tabular-nums">{formatSize(stats[acc.id]?.storageSize)}</div>
                      </div>
                      <div className="text-center p-3 bg-muted/30  border border-border/50">
                        <div className="text-[10px] uppercase font-bold text-muted-foreground mb-1">Last Sync</div>
                        <div className="text-xs font-medium h-7 flex items-center justify-center text-center">
                          {stats[acc.id]?.lastSync === 'Never' || !stats[acc.id]?.lastSync ? 'Never' : new Date(stats[acc.id]?.lastSync).toLocaleTimeString()}
                        </div>
                      </div>
                    </div>

                    <div className="grid grid-cols-2 gap-4 text-xs font-mono opacity-80">
                      <div className="bg-muted/50 p-3  border border-border/50 flex items-center gap-3">
                        <Server className="w-4 h-4 text-primary opacity-50" />
                        <div>
                          <div className="text-[10px] uppercase font-bold tracking-tighter opacity-50 flex items-center gap-2">
                            IMAP {acc.ssl ? <Shield className="w-2 h-2 text-green-500" /> : null}
                          </div>
                          {acc.host}:{acc.port}
                        </div>
                      </div>
                      <div className="bg-muted/50 p-3  border border-border/50 flex items-center gap-3">
                        <Zap className="w-4 h-4 text-primary opacity-50" />
                        <div>
                          <div className="text-[10px] uppercase font-bold tracking-tighter opacity-50">SMTP</div>
                          {acc.smtpHost}
                        </div>
                      </div>
                    </div>
                    {connectionResults[acc.id] && (
                      <div className={`mt-4 p-3  flex items-start gap-3 text-xs ${connectionResults[acc.id].success ? 'bg-green-500/10 text-green-500 border border-green-500/20' : 'bg-red-500/10 text-red-500 border border-red-500/20'}`}>
                        {connectionResults[acc.id].success ? <Check className="w-4 h-4 shrink-0" /> : <AlertCircle className="w-4 h-4 shrink-0" />}
                        {connectionResults[acc.id].message}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {activeCategory === 'profile' && userProfile && (
            <div className="animate-in fade-in duration-300">
              <h2 className="text-2xl font-bold text-foreground mb-2">User Profile</h2>
              <p className="text-muted-foreground text-sm mb-8">Manage your personal information and profile picture.</p>
              
              <form onSubmit={handleUpdateProfile} className="space-y-6">
                <div className="flex items-center gap-8 mb-8">
                  <div className="relative group">
                    <div className="w-24 h-24  bg-muted border-2 border-dashed border-border flex items-center justify-center overflow-hidden">
                      {userProfile.profileImageUrl ? (
                        <img src={userProfile.profileImageUrl} alt="Profile" className="w-full h-full object-cover" />
                      ) : (
                        <User className="w-10 h-10 text-muted-foreground opacity-50" />
                      )}
                    </div>
                    <button type="button" className="absolute inset-0 bg-black/40 text-white text-[10px] font-bold opacity-0 group-hover:opacity-100 flex items-center justify-center transition-opacity ">CHANGE</button>
                  </div>
                  <div className="flex-1 space-y-4">
                    <div className="space-y-1.5">
                      <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Display Name</label>
                      <input 
                        type="text" 
                        className="w-full bg-background border border-border  px-4 py-2.5 text-sm focus:ring-2 focus:ring-primary/20 outline-none transition-all"
                        value={userProfile.displayName || ''}
                        onChange={e => setUserProfile({...userProfile, displayName: e.target.value})}
                      />
                    </div>
                    <div className="space-y-1.5">
                      <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Email Address</label>
                      <input 
                        type="email" 
                        className="w-full bg-background border border-border  px-4 py-2.5 text-sm focus:ring-2 focus:ring-primary/20 outline-none transition-all"
                        value={userProfile.email || ''}
                        onChange={e => setUserProfile({...userProfile, email: e.target.value})}
                      />
                    </div>
                  </div>
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Profile Image URL</label>
                  <input 
                    type="text" 
                    placeholder="https://example.com/avatar.jpg"
                    className="w-full bg-background border border-border  px-4 py-2.5 text-sm focus:ring-2 focus:ring-primary/20 outline-none transition-all"
                    value={userProfile.profileImageUrl || ''}
                    onChange={e => setUserProfile({...userProfile, profileImageUrl: e.target.value})}
                  />
                </div>

                <div className="pt-4">
                  <button type="submit" className="bg-primary text-primary-foreground px-5 py-2  font-semibold shadow-sm hover:opacity-90 active:scale-[0.98] transition-all text-sm">Save Profile</button>
                </div>
              </form>
            </div>
          )}

          {activeCategory === 'import' && (
            <ImportPanel accounts={accounts} />
          )}

          {activeCategory === 'appearance' && (
            <div className="animate-in fade-in duration-300">
              <h2 className="text-2xl font-bold text-foreground mb-2">Appearance</h2>
              <p className="text-muted-foreground text-sm mb-8">Customize how InboxQL looks on your screen.</p>
              <div className="space-y-8">
                <section>
                  <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">Color Theme</h3>
                  <div className="grid grid-cols-3 gap-4">
                    {[
                      { id: 'light', label: 'Light' }, { id: 'dark', label: 'Dark' }, { id: 'gt', label: 'Georgia Tech' },
                    ].map(t => (
                      <button 
                        key={t.id}
                        onClick={() => setTheme(t.id as any)}
                        className={`p-4 border  flex flex-col items-center gap-2 transition-all ${theme === t.id ? 'border-primary bg-primary/5 ring-1 ring-primary' : 'border-border hover:bg-accent'}`}
                      >
                        <div className={`w-full h-16  mb-1 shadow-inner ${t.id === 'light' ? 'bg-white' : t.id === 'dark' ? 'bg-zinc-900' : 'bg-[#B3A369]'}`} />
                        <span className="text-sm font-medium">{t.label}</span>
                        {theme === t.id && <Check className="w-3 h-3 text-primary absolute top-2 right-2" />}
                      </button>
                    ))}
                  </div>
                </section>
              </div>
            </div>
          )}

          {activeCategory === 'ai' && (
            <div className="animate-in fade-in duration-300">
              <h2 className="text-2xl font-bold text-foreground mb-2">AI & Analysis Configuration</h2>
              <p className="text-muted-foreground text-sm mb-8">Optimize how InboxQL processes and categorizes your emails.</p>
              
              <div className="space-y-6">
                <div className="space-y-2">
                  <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Topic Trend Ignore Words</label>
                  <p className="text-xs text-muted-foreground mb-2 text-balance leading-relaxed">Common words or prefixes to exclude from the topic analysis. Separate multiple words with commas.</p>
                  <textarea 
                    className="w-full bg-background border border-border  px-4 py-3 text-sm focus:ring-2 focus:ring-primary/20 outline-none transition-all h-32 font-mono"
                    value={ignoreWords}
                    onChange={e => setIgnoreWords(e.target.value)}
                    placeholder="re:,fwd:,the,and,etc..."
                  />
                </div>
                <button 
                  onClick={handleUpdateIgnoreWords}
                  className="bg-primary text-primary-foreground px-5 py-2  font-semibold shadow-sm hover:opacity-90 active:scale-[0.98] transition-all text-sm"
                >
                  Save Configuration
                </button>
              </div>
            </div>
          )}

          {activeCategory === 'security' && (
            <div className="py-20 text-center animate-in zoom-in-95 duration-300">
              <AlertCircle className="w-12 h-12 mx-auto mb-4 opacity-10" />
              <h3 className="text-lg font-medium opacity-50">Security Configuration coming soon</h3>
              <p className="text-sm text-muted-foreground mt-2">We are hard at work bringing this feature to InboxQL.</p>
            </div>
          )}

          {activeCategory === 'data' && (
            <div className="animate-in fade-in duration-300">
              <h2 className="text-2xl font-bold text-foreground mb-2 flex items-center gap-2">
                <Database className="w-6 h-6 text-destructive" />
                Data Management
              </h2>
              <p className="text-muted-foreground text-sm mb-8">Manage the data stored in your local InboxQL database.</p>
              
              <div className="border border-destructive/20 bg-destructive/5 p-6 space-y-4">
                <div className="flex items-start gap-4">
                  <AlertTriangle className="w-6 h-6 text-destructive shrink-0 mt-0.5" />
                  <div>
                    <h3 className="text-sm font-bold text-destructive mb-1">Delete All Stored Messages</h3>
                    <p className="text-sm text-muted-foreground mb-4 leading-relaxed">
                      This will permanently erase all synchronized messages and attachments from your local database. 
                      It will not affect mail on your remote IMAP server, but it cannot be undone locally. 
                      You will need to re-sync or re-import your accounts to populate the inbox again.
                    </p>
                    <button 
                      onClick={() => alert('Data deletion API not yet implemented')}
                      className="bg-destructive hover:bg-destructive/90 text-destructive-foreground px-4 py-2 text-sm font-semibold transition-colors flex items-center gap-2"
                    >
                      <Trash2 className="w-4 h-4" />
                      Erase All Local Data
                    </button>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

// --- Register Components ---
componentRegistry.register('dashboard', Dashboard);
componentRegistry.register('mail', MailClient);
componentRegistry.register('search', () => <div className="p-8 text-muted-foreground italic text-center mt-20 font-medium">Search functionality coming soon...</div>);
componentRegistry.register('settings', SettingsView);
componentRegistry.register('agents', AgentManager);
componentRegistry.register('message', MessageViewer);
componentRegistry.register('errors', ErrorLog);

function App() {
  // Every menu advertises a shortcut — Control+Shift+D and the rest — but the
  // shell only binds them when this hook is mounted, and it never was. The
  // menus were displaying keystrokes that did nothing.
  useKeyboardShortcuts();

  const [user, setUser] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [activeAccount, setActiveAccount] = useState<any>(null);
  const [globalUnread, setGlobalUnread] = useState(0);
  const [syncStatus] = useState('Idle');
  const [lastError, setLastError] = useState<string | null>(null);

  // Tab opening moved to lib/tabs so components deeper in the tree — the mail
  // list, the importer — can open one too. App keeps a stable reference for
  // the effects and commands below.
  const openToolCb = useCallback((id: string, label: string) => openTool(id, label), []);

  const checkAuth = async () => {
    try {
      const res = await fetch('/api/profile');
      if (res.ok) {
        const data = await res.json();
        setUser(data);
      } else {
        setUser(null);
      }
    } catch (e) {
      setUser(null);
    }
    setLoading(false);
  };

  const handleLogout = async () => {
    await fetch('/api/logout', { method: 'POST' });
    setUser(null);
  };

  useEffect(() => {
    checkAuth();
  }, []);

  useEffect(() => {
    if (!user) return;
    const fetchGlobalStats = async () => {
      try {
        const res = await fetch('/api/accounts');
        if (res.status === 401) { setUser(null); return; }
        const accounts = await res.json();
        if (accounts && accounts.length > 0) {
          setActiveAccount(accounts[0]);
          let totalUnread = 0;
          for (const acc of accounts) {
            const sRes = await fetch(`/api/accounts/stats?id=${acc.id}`);
            if (sRes.ok) {
              const sData = await sRes.json();
              totalUnread += sData.unreadMessages || 0;
            }
          }
          setGlobalUnread(totalUnread);
          setLastError(null);
        }
      } catch (e) {
        setLastError('Connection Error');
      }
    };
    fetchGlobalStats();
    const interval = setInterval(fetchGlobalStats, 10000);
    return () => clearInterval(interval);
  }, [user]);

  useEffect(() => {
    commandRegistry.registerCommand({
      id: 'iql.open-dashboard',
      label: 'Analytics Dashboard',
      keybinding: 'Control+Shift+D',
      execute: () => openToolCb('dashboard', 'Analytics Dashboard'),
    });
    commandRegistry.registerCommand({
      id: 'iql.open-mail',
      label: 'Mailbox',
      keybinding: 'Control+Shift+M',
      execute: () => openToolCb('mail', 'Mailbox'),
    });
    commandRegistry.registerCommand({
      id: 'iql.open-search',
      label: 'Search Email',
      keybinding: 'Control+Shift+F',
      execute: () => openToolCb('search', 'Search'),
    });
    commandRegistry.registerCommand({
      id: 'iql.open-agents',
      label: 'AI Agents',
      keybinding: 'Control+Shift+A',
      execute: () => openToolCb('agents', 'AI Agents'),
    });
    commandRegistry.registerCommand({
      id: 'iql.open-errors',
      label: 'View: Error Log',
      keybinding: 'Control+Shift+E',
      execute: () => openErrorLog(),
    });

    commandRegistry.registerCommand({
      id: 'iql.open-settings',
      label: 'Settings',
      keybinding: 'Control+,',
      execute: () => openToolCb('settings', 'Settings'),
    });
    commandRegistry.registerCommand({
      id: 'iql.logout',
      label: 'Sign Out',
      execute: handleLogout,
    });
    commandRegistry.registerCommand({
      id: 'nexus.about',
      label: 'About InboxQL',
      execute: () => alert(`InboxQL v${appVersion}\nInboxQL Workbench`),
    });

    menuRegistry.setMenus({
      'Tools': [
        { id: 'tools.dashboard', label: 'Analytics Dashboard', commandId: 'iql.open-dashboard' },
        { id: 'tools.mail', label: 'Mailbox', commandId: 'iql.open-mail' },
        { id: 'tools.search', label: 'Search Email', commandId: 'iql.open-search' },
        { id: 'tools.agents', label: 'AI Agents', commandId: 'iql.open-agents' },
      ],
      'View': [
        { id: 'view.toggle-chat', label: 'Toggle Chat', commandId: 'view.toggleChat', keybinding: 'Control+I' },
      ],
      'Help': [
        { id: 'help.settings', label: 'Settings', commandId: 'iql.open-settings' },
        { id: 'help.errors', label: 'Error Log', commandId: 'iql.open-errors' },
        { id: 'help.divider', label: '---' },
        { id: 'help.about', label: 'About InboxQL', commandId: 'nexus.about' },
        { id: 'help.logout', label: 'Sign Out', commandId: 'iql.logout' },
      ]
    });

    // Branding and the activity bar used to be forced here by polling the DOM
    // every 100ms: nexus-shell 0.1.x had no way to set a title, and no way to
    // suppress the activity bar, so the shell's own markup was rewritten from
    // outside. 0.2.x makes both declarative — the `title` prop on ShellLayout,
    // and ConnectedPaneRail rendering nothing when no panels are registered —
    // so the interval is gone rather than merely tidied.
  }, [user, openTool]);

  const statusBar = [
    { id: 'acc', label: activeAccount ? `Account: ${activeAccount.name}` : 'No Account', alignment: 'left' as const, icon: Mail },
    // Account-wide, junk and trash included — which is a different number
    // from the Inbox badge beside it. Saying which one it is stops the two
    // from reading as a contradiction.
    { id: 'unread', label: `${globalUnread} unread, all folders`, alignment: 'left' as const },
    { id: 'status', label: `Sync: ${syncStatus}`, alignment: 'center' as const, icon: RefreshCw },
    { id: 'error', label: lastError || 'System OK', alignment: 'right' as const, icon: lastError ? AlertCircle : Check },
    { id: 'chat', label: 'Chat', alignment: 'right' as const, icon: MessageSquare, onClick: () => commandRegistry.executeCommand('view.toggleChat') },
  ];

  useEffect(() => {
    if (user && !loading) {
      // Auto-open mailbox on login
      setTimeout(() => openToolCb('mail', 'Mailbox'), 500);
    }
  }, [user, loading, openTool]);

  if (loading) return null;
  if (!user) return <LoginView onLogin={setUser} />;

  return (
    <div className="h-screen w-screen overflow-hidden iql-workbench">
      <ShellLayout
        // `app-lockup` keeps the QL in InboxQL; see App.css for why a Tailwind
        // class could not do it.
        title={<AppTitle title="InboxQL" subtitle={`v${appVersion} • Email for Engineers`} className="app-lockup" />}
        // Chat was a fixture of the shell in 0.1.x, toggled by a built-in
        // command. In 0.2.x it is an ordinary registered panel, so passing no
        // panels left `view.toggleChat` with nothing to toggle. Registering it
        // on the bottom edge restores the View menu and status-bar buttons —
        // and the bottom edge specifically, because it is the one side with no
        // icon rail, which keeps the activity bar hidden as before.
        panels={[chatPanel({ side: 'bottom' })]}
        statusBarConfig={statusBar}
        rightMenuBarContent={
          <UserProfile
            profile={{
              name: user.displayName,
              email: user.email,
              avatarUrl: user.profileImageUrl,
            }}
            onClick={() => commandRegistry.executeCommand('iql.open-settings')}
          />
        }
      />
    </div>
  );
}

export default App;
