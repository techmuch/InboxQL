import { useEffect, useState, useCallback, useMemo } from 'react';
import { ShellLayout, AppTitle, chatPanel, componentRegistry, menuRegistry, commandRegistry, useThemeStore, useKeyboardShortcuts, UserProfile, BUNDLED_THEMES } from 'nexus-shell';
import { Layout, Search, Mail, BarChart2, Server, Shield, Trash2, Cpu, Eye, X, Check, AlertCircle, RefreshCw, MessageSquare, User, Lock, Download, Database, AlertTriangle, HardDrive, Wrench } from 'lucide-react';
import { ResponsiveCalendar } from '@nivo/calendar';
import { AgentManager } from './AgentManager';
import { ImportPanel } from './views/ImportPanel';
import { BrowserStoragePanel } from './views/BrowserStoragePanel';
import { AISettingsPanel } from './views/AISettingsPanel';
import { MaintenancePanel } from './views/MaintenancePanel';
import { Annotators } from './views/Annotators';
import { MessageViewer } from './views/MessageViewer';
import { ErrorLog } from './views/ErrorLog';
import { Desk } from './views/Desk';
import { Board } from './views/Board';
import { openTool, openErrorLog, openQuery, useViewerStore, type ViewerMode } from './lib/tabs';
import { useAttachmentTargetStore, type AttachmentTarget } from './lib/attachmentTarget';
import { CalendarDays, Radio, RadioTower, ScrollText } from 'lucide-react';
import { useThreadingStore, type Threading } from './lib/threading';
import { ENTRY_CAP, useThreadExpansionStore, type ThreadExpansion } from './lib/threadExpansion';
import { startWindowSession, useWindowsStore, WINDOWS_TAB } from './lib/windows';
import { Windows } from './views/Windows';
import { WindowNote } from './views/WindowNote';
import { RailDefaults } from './views/RailDefaults';
import { LogSettings } from './views/LogSettings';
import { useQueryStore, queryTerms, compose, asTerm, type QueryTerm } from './lib/filters';
import { useDevReload } from './lib/devReload';
import { requestSettingsSection, takeSettingsSection, SETTINGS_SECTION_EVENT } from './lib/settingsNav';
import { settingAnchor, openSetting, SETTINGS, SECTION_LABEL, matchesSetting, matchesWords } from './lib/settingsIndex';
import { SystemPanel } from './views/SystemPanel';
import { fetchGravatar } from './lib/avatars';
import { Docs } from './views/Docs';
import { Calendar } from './views/Calendar';
import { MailAccounts } from './views/MailAccounts';
import { CalendarSettings } from './views/CalendarSettings';
import { ReminderToasts } from './views/ReminderToasts';
import { CALENDAR_TAB, newCalendarEvent } from './lib/calendarNav';
import { AppDialogs } from './views/AppDialogs';
import { Approvals } from './views/Approvals';
import { openCompose, openKeys, openNewTask, openPalette } from './lib/appUi';
import { installKeySequences } from './lib/keySequences';
import { openDoc, DOCS_TAB } from './lib/docsNav';
import { version as appVersion } from '../package.json';
import 'nexus-shell/style.css';
import './App.css';
import { Help } from './views/Help';

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
  const query = useQueryStore(s => s.text);
  const setQuery = useQueryStore(s => s.set);
  const [terms, setTerms] = useState<QueryTerm[]>([]);

  // The pills are the query's own terms, fetched rather than kept alongside it.
  // A parallel list is how the bar and the results came to disagree.
  useEffect(() => { queryTerms(query).then(setTerms).catch(() => setTerms([])); }, [query]);

  // A facet click replaces that facet and leaves everything else alone.
  const toggleTerm = async (term: string) => {
    const already = terms.some(t => t.text === term);
    setQuery(await compose(query, already ? { remove: term } : { add: term }));
  };
  const removeTerm = async (term: string) => setQuery(await compose(query, { remove: term }));
  const clearAll = () => setQuery('');
  const filter = query;
  // Each widget highlights when a term for its own field is applied. The value
  // is read back out of the term so the heading can name it.
  const applied = (field: string) => terms.find(t => t.field === field);
  const appliedValue = (field: string) => {
    const term = applied(field);
    if (!term) return null;
    const value = term.text.slice(term.text.indexOf(':') + 1);
    return value.replace(/^"|"$/g, '').replace(/""/g, '"');
  };

  // Why the failure is rendered rather than logged: every widget appending its
  // own aggregate to a shared query used to 400 whenever Desk was aggregating,
  // and because this only reached the console the dashboard simply showed
  // yesterday's numbers. A chart that is quietly stale is worse than one that
  // says it could not load.
  const [analyticsError, setAnalyticsError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const fetchData = async () => {
      const qs = filter ? `&${new URLSearchParams({ q: filter })}` : '';
      try {
        const [vRes, sRes, tRes] = await Promise.all([
          fetch(`/api/analytics?type=volume${qs}`),
          fetch(`/api/analytics?type=senders${qs}`),
          fetch(`/api/analytics?type=topics${qs}`)
        ]);
        if (cancelled) return;

        const failed = [vRes, sRes, tRes].find(r => !r.ok);
        if (failed) {
          const body = await failed.json().catch(() => null);
          setAnalyticsError(body?.error ?? `Analytics failed (${failed.status}).`);
          // The old numbers are cleared with the error. Leaving them on screen
          // beside a warning invites reading them as current.
          setVolume([]); setSenders([]); setTopics([]);
          return;
        }

        setAnalyticsError(null);
        setVolume(await vRes.json());
        setSenders(await sRes.json());
        setTopics(await tRes.json());
      } catch (e) {
        if (cancelled) return;
        setAnalyticsError(e instanceof Error ? e.message : String(e));
      }
    };
    fetchData();
    return () => { cancelled = true; };
  }, [filter]);

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
        <h2 className="flex items-center gap-2 text-2xl font-bold">Analytics Pulse <Help topic="dashboard" /></h2>
        {terms.length > 0 && (
          <button onClick={clearAll} className="flex items-center gap-1.5 text-[10px] font-semibold text-primary hover:bg-primary/20 bg-primary/10 px-2.5 py-1  transition-colors">
            <X className="w-2.5 h-2.5" /> Clear Filters
          </button>
        )}
      </div>

      {terms.length > 0 && (
        <div className="bg-primary/5 border border-primary/10  px-4 py-2 flex items-center gap-3 mb-6">
          <span className="text-[10px] font-bold uppercase tracking-wider text-primary/60">Query:</span>
          <div className="flex flex-1 gap-2 overflow-auto no-scrollbar">
            {terms.map(t => (
              <span key={t.start} className="px-2 py-0.5 bg-primary text-primary-foreground text-[10px] font-mono flex items-center gap-1 shadow-sm whitespace-nowrap">
                {t.text} <X onClick={() => removeTerm(t.text)} className="w-2.5 h-2.5 cursor-pointer" />
              </span>
            ))}
          </div>
          {/* The filters are a query, so they can go somewhere they can be
              edited and saved rather than only cleared. */}
          <button
            onClick={() => openQuery(filter)}
            className="text-[10px] font-semibold text-primary hover:bg-primary/20 bg-primary/10 px-2.5 py-1 whitespace-nowrap transition-colors"
          >
            Open in Desk
          </button>
        </div>
      )}
      
      {analyticsError && (
        <div className="mb-6 flex items-start gap-2 border border-destructive/20 bg-destructive/10 p-3 text-xs text-destructive">
          <AlertCircle className="mt-px h-3.5 w-3.5 shrink-0" />
          <span>{analyticsError}</span>
        </div>
      )}

      <div className="space-y-6">
        {/* Temporal Volume - Calendar Heatmap */}
        <div className={`p-6 bg-card border  shadow-sm min-h-80 flex flex-col group transition-colors ${applied('on') ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-primary/50'}`}>
          <div className="flex items-center gap-3 mb-4">
            <BarChart2 className="w-5 h-5 text-primary" />
            <span className="font-bold text-lg text-foreground">Communication Intensity {appliedValue('on') ? `on ${appliedValue('on')}` : ''}</span>
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
                onClick={(datum) => toggleTerm(`on:${datum.day}`)}
              />
            ) : (
              <div className="h-full flex items-center justify-center text-xs text-muted-foreground italic">
                No temporal data available for these filters
              </div>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div className={`p-6 bg-card border  shadow-sm min-h-64 flex flex-col group transition-colors ${applied('from') ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-primary/50'}`}>
            <div className="flex items-center gap-3 mb-4">
              <Mail className="w-5 h-5 text-primary" />
              <span className="font-bold text-lg text-foreground">Top Senders {applied('from') ? `(Filtered)` : ''}</span>
            </div>
            <div className="flex-1 space-y-3">
              {(senders || []).slice(0, 5).map((d, i) => (
                <button 
                  key={i} 
                  onClick={() => toggleTerm(asTerm('from', d.label))}
                  className={`w-full flex items-center gap-2.5 p-1.5  transition-all ${appliedValue('from') === d.label ? 'bg-primary/10 ring-1 ring-primary' : 'hover:bg-accent'}`}
                >
                  <div className="w-7 h-7  bg-primary/10 flex items-center justify-center text-[9px] font-bold text-primary">
                    {d.label[0]?.toUpperCase() || '?'}
                  </div>
                  <div className="flex-1 min-w-0 text-left">
                    <div className="text-xs font-semibold truncate leading-tight">{d.label}</div>
                    <div className="text-[9px] text-muted-foreground uppercase tracking-tighter">{d.value} messages</div>
                  </div>
                  {appliedValue('from') === d.label && <Check className="w-2.5 h-2.5 text-primary" />}
                </button>
              ))}
              {(!senders || senders.length === 0) && <span className="text-xs text-muted-foreground italic text-center pt-10 block">No senders found for these filters</span>}
            </div>
          </div>

          <div className={`p-6 bg-card border  shadow-sm min-h-64 flex flex-col group transition-colors ${applied('subject') ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-primary/50'}`}>
            <div className="flex items-center gap-3 mb-4">
              <Layout className="w-5 h-5 text-primary" />
              <span className="font-bold text-lg text-foreground">Topic Trends {applied('subject') ? `(Filtered)` : ''}</span>
            </div>
            <div className="flex-1 flex flex-wrap gap-1.5 content-start">
              {(topics || []).map((d, i) => (
                <button 
                  key={i} 
                  onClick={() => toggleTerm(asTerm('subject', d.label))}
                  className={`px-2.5 py-1  text-[10px] font-semibold flex items-center gap-1.5 transition-all ${appliedValue('subject') === d.label ? 'bg-primary text-primary-foreground shadow-sm' : 'bg-muted hover:bg-primary/10 hover:text-primary'}`}
                >
                  <span className="truncate max-w-[100px]">{d.label}</span>
                  <span className={`text-[9px] ${appliedValue('subject') === d.label ? 'text-primary-foreground/70' : 'opacity-50 font-mono'}`}>{d.value}</span>
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
// --- Unified Settings View ---
/**
 * The viewer reuse preference.
 *
 * Two options spelled out rather than a checkbox, because "Reuse viewer tab ☐"
 * tells you what is being turned off and not what happens instead — and the
 * alternative here is a different arrangement of the workspace, which is worth
 * a sentence.
 */
const ViewerModeSetting = () => {
  const mode = useViewerStore(s => s.mode);
  const setMode = useViewerStore(s => s.setMode);

  const options: { id: ViewerMode; label: string; detail: string }[] = [
    {
      id: 'reuse',
      label: 'Reuse one Viewer tab',
      detail: 'Messages, contacts and files share a single tab. Opening one replaces the last.',
    },
    {
      id: 'split',
      label: 'A tab for each kind',
      detail: 'Message, Contact and File get their own tabs, so opening a file leaves the message you were reading where it was.',
    },
  ];

  return (
    <section {...settingAnchor('viewer-tabs')}>
      <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">Viewer Tabs</h3>
      <div className="grid grid-cols-2 gap-4">
        {options.map(o => (
          <button
            key={o.id}
            onClick={() => setMode(o.id)}
            aria-pressed={mode === o.id}
            className={`relative p-4 border text-left flex flex-col gap-1.5 transition-all ${
              mode === o.id
                ? 'border-primary bg-primary/5 ring-1 ring-primary'
                : 'border-border hover:bg-accent'
            }`}
          >
            <span className="text-sm font-medium">{o.label}</span>
            <span className="text-xs text-muted-foreground leading-snug">{o.detail}</span>
            {mode === o.id && <Check className="w-3 h-3 text-primary absolute top-2 right-2" />}
          </button>
        ))}
      </div>
      <p className="text-[11px] text-muted-foreground mt-3">
        Switching to a single tab closes the Contact and File tabs; switching back reopens
        the ones that still have something in them.
      </p>
    </section>
  );
};

/**
 * Whether the Desk opens threaded.
 *
 * Phrased as what you see rather than as the stage that produces it. The query
 * bar shows `| timeline` either way, which is where somebody learns the
 * language; a settings page is not the place to teach it.
 */
const ThreadingSetting = () => {
  const threading = useThreadingStore(s => s.threading);
  const setThreading = useThreadingStore(s => s.setThreading);

  const options: { id: Threading; label: string; detail: string }[] = [
    {
      id: 'flat',
      label: 'One row per message',
      detail: 'Every message on its own line, newest first.',
    },
    {
      id: 'threaded',
      label: 'One row per conversation',
      detail: 'Messages grouped into the conversation they belong to, with the tickets and drafts it produced.',
    },
  ];

  return (
    <section {...settingAnchor('inbox-layout')}>
      <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">Inbox Layout</h3>
      <div className="grid grid-cols-2 gap-4">
        {options.map(o => (
          <button
            key={o.id}
            onClick={() => setThreading(o.id)}
            aria-pressed={threading === o.id}
            className={`relative p-4 border text-left flex flex-col gap-1.5 transition-all ${
              threading === o.id
                ? 'border-primary bg-primary/5 ring-1 ring-primary'
                : 'border-border hover:bg-accent'
            }`}
          >
            <span className="text-sm font-medium">{o.label}</span>
            <span className="text-xs text-muted-foreground leading-snug">{o.detail}</span>
            {threading === o.id && <Check className="w-3 h-3 text-primary absolute top-2 right-2" />}
          </button>
        ))}
      </div>
      <p className="text-[11px] text-muted-foreground mt-3">
        Where the Desk starts. The Threads button still changes any one query, and a query
        that counts something, or that is about contacts or files, is left alone.
      </p>
    </section>
  );
};

/**
 * Whether conversations start open or closed.
 *
 * Shown under Inbox Layout because it only means anything when the inbox is one
 * row per conversation — and it says so when it is not, rather than sitting
 * there looking like it does something.
 */
const ThreadExpansionSetting = () => {
  const expansion = useThreadExpansionStore(s => s.expansion);
  const setExpansion = useThreadExpansionStore(s => s.setExpansion);
  const threaded = useThreadingStore(s => s.threading) === 'threaded';

  const options: { id: ThreadExpansion; label: string; detail: string }[] = [
    {
      id: 'collapsed',
      label: 'Closed',
      detail: 'One line each. Arrow keys move between conversations, which is what makes the list quick to scan.',
    },
    {
      id: 'expanded',
      label: 'Open',
      detail: 'Every message, ticket and draft under its conversation. Arrow keys walk through all of them.',
    },
  ];

  return (
    <section {...settingAnchor('conversations-open')}>
      <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">
        Conversations
      </h3>
      <div className={`grid grid-cols-2 gap-4 ${threaded ? '' : 'opacity-60'}`}>
        {options.map(o => (
          <button
            key={o.id}
            onClick={() => setExpansion(o.id)}
            aria-pressed={expansion === o.id}
            className={`relative p-4 border text-left flex flex-col gap-1.5 transition-all ${
              expansion === o.id
                ? 'border-primary bg-primary/5 ring-1 ring-primary'
                : 'border-border hover:bg-accent'
            }`}
          >
            <span className="text-sm font-medium">{o.label}</span>
            <span className="text-xs text-muted-foreground leading-snug">{o.detail}</span>
            {expansion === o.id && <Check className="w-3 h-3 text-primary absolute top-2 right-2" />}
          </button>
        ))}
      </div>
      <p className="text-[11px] text-muted-foreground mt-3">
        {threaded
          ? `A conversation you open or close by hand stays that way. A search that finds a single conversation always opens it, and a long one shows its first ${ENTRY_CAP} entries until you ask for the rest.`
          : 'Applies when the inbox shows one row per conversation, above.'}
      </p>
    </section>
  );
};

/**
 * Where an attachment chip puts its preview.
 *
 * A file has two homes and the application used to answer for you: a chip on a
 * message opened inline, the Desk's file list opened the viewer, and neither
 * could be asked for from the other place.
 *
 * Three options rather than a checkbox pair, because "both" is a real answer
 * and not the absence of a choice — and because what it means depends on the
 * setting below it, which the note says out loud rather than leaving to be
 * discovered.
 */
const AttachmentTargetSetting = () => {
  const target = useAttachmentTargetStore(s => s.target);
  const setTarget = useAttachmentTargetStore(s => s.setTarget);
  const mode = useViewerStore(s => s.mode);

  const options: { id: AttachmentTarget; label: string; detail: string }[] = [
    {
      id: 'message',
      label: 'In the message',
      detail: 'Below the attachment chips, with the mail it arrived on still around it.',
    },
    {
      id: 'file',
      label: 'In the File tab',
      detail: 'The file on its own, with its size, every message it came on, and what is inside it.',
    },
    {
      id: 'both',
      label: 'Both at once',
      detail: 'Inline for the context, and the File tab for the history, without clicking twice.',
    },
  ];

  return (
    <section {...settingAnchor('attachment-previews')}>
      <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">
        Attachment Previews
      </h3>
      <div className="grid grid-cols-3 gap-4">
        {options.map(o => (
          <button
            key={o.id}
            onClick={() => setTarget(o.id)}
            aria-pressed={target === o.id}
            className={`relative p-4 border text-left flex flex-col gap-1.5 transition-all ${
              target === o.id
                ? 'border-primary bg-primary/5 ring-1 ring-primary'
                : 'border-border hover:bg-accent'
            }`}
          >
            <span className="text-sm font-medium">{o.label}</span>
            <span className="text-xs text-muted-foreground leading-snug">{o.detail}</span>
            {target === o.id && <Check className="w-3 h-3 text-primary absolute top-2 right-2" />}
          </button>
        ))}
      </div>
      {/* The two settings interact, and silently doing something else would be
          worse than saying so here. With one shared tab the file and the
          message cannot both be in it. */}
      {mode === 'reuse' && target !== 'message' && (
        <p className="text-[11px] text-muted-foreground mt-3">
          {target === 'file'
            ? 'With one shared Viewer tab, opening a file replaces the message you were reading. Choose a tab for each kind below to keep both.'
            : 'With one shared Viewer tab there is nowhere to put the second copy, so this behaves as “In the message”. Choose a tab for each kind below for both at once.'}
        </p>
      )}
    </section>
  );
};

const SettingsView = () => {
  const [searchQuery, setSearchQuery] = useState('');
  const [activeCategory, setActiveCategory] = useState(() => takeSettingsSection() ?? 'profile');
  useEffect(() => {
    const go = (e: Event) => {
      const id = (e as CustomEvent<string>).detail;
      if (!id) return;
      takeSettingsSection();
      setActiveCategory(id);
      // A jump from Find anything to a section the sidebar's filter is
      // hiding clears the filter, so the sidebar shows where you are.
      setSearchQuery(q => !q.trim() || matchesWords(SECTION_LABEL[id] ?? id, q)
        || SETTINGS.some(st => st.section === id && matchesSetting(st, q)) ? q : '');
    };
    window.addEventListener(SETTINGS_SECTION_EVENT, go);
    return () => window.removeEventListener(SETTINGS_SECTION_EVENT, go);
  }, []);
  const { theme, setTheme } = useThemeStore();
  const [userProfile, setUserProfile] = useState<any>(null);

  const fetchProfile = async () => {
    try {
      const res = await fetch('/api/profile');
      if (res.ok) {
        const data = await res.json();
        setUserProfile(data);
      }
    } catch (e) {}
  };

  const handleEraseData = async () => {
    if (!confirm('Are you sure you want to permanently erase all synchronized data from the local database? This cannot be undone.')) return;
    try {
      const res = await fetch('/api/data', { method: 'DELETE' });
      if (res.ok) {
        alert('All synchronized data has been erased from the local database.');
        window.location.reload();
      } else {
        alert('Failed to erase data. Check logs for details.');
      }
    } catch (e) {
      alert('Network error while erasing data.');
    }
  };

  // The importer asks which account imported mail belongs to.
  const [accounts, setAccounts] = useState<any[]>([]);
  useEffect(() => {
    fetchProfile();
    fetch('/api/accounts').then(r => (r.ok ? r.json() : [])).then(d => setAccounts(d ?? [])).catch(() => {});
  }, [activeCategory]);

  const [profileNote, setProfileNote] = useState<{ ok: boolean; text: string } | null>(null);
  const saveProfile = async (profile: any): Promise<boolean> => {
    try {
      const res = await fetch('/api/profile', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(profile)
      });
      setProfileNote(res.ok ? { ok: true, text: 'Saved.' } : { ok: false, text: (await res.text()) || 'Could not save.' });
      return res.ok;
    } catch {
      setProfileNote({ ok: false, text: 'Could not reach the server.' });
      return false;
    }
  };
  const handleUpdateProfile = async (e: React.FormEvent) => {
    e.preventDefault();
    await saveProfile(userProfile);
  };
  // Asks Gravatar once, for this address, keeps the picture in the mailbox,
  // and points the profile at the stored copy — so no page ever loads it from
  // Gravatar.
  const fetchMyGravatar = async () => {
    const email = (userProfile?.email ?? '').trim();
    if (!email) { setProfileNote({ ok: false, text: 'Add your email address first: Gravatar looks pictures up by it.' }); return; }
    setProfileNote(null);
    try {
      const m = await fetchGravatar(email);
      if (m.status !== 'stored') {
        setProfileNote({ ok: false, text: `Gravatar has no picture for ${email}.` });
        return;
      }
      const next = { ...userProfile, profileImageUrl: `/api/avatars/image?address=${encodeURIComponent(email.toLowerCase())}&v=${encodeURIComponent(m.fetchedAt ?? '')}` };
      setUserProfile(next);
      if (await saveProfile(next)) setProfileNote({ ok: true, text: 'Saved your Gravatar picture. It is kept here and loaded from here.' });
    } catch (e: any) {
      setProfileNote({ ok: false, text: e?.message ?? 'Could not reach Gravatar.' });
    }
  };

  const categories = [
    { id: 'profile', label: 'User Profile', icon: User },
    // General absorbed Appearance rather than sitting beside it. Appearance
    // held one thing — the theme — and a category per preference is how a
    // settings page ends up with eight sections nobody can find anything in.
    { id: 'general', label: 'General', icon: Eye },
    // The machine rather than the mailbox: where the mail is, how the server
    // runs, the settings file, restart and update.
    { id: 'system', label: 'System', icon: Server },
    { id: 'accounts', label: 'Mail Accounts', icon: Mail },
    { id: 'calendars', label: 'Calendars', icon: CalendarDays },
    { id: 'import', label: 'Import Mail', icon: Download },
    { id: 'ai', label: 'AI Configuration', icon: Cpu },
    { id: 'security', label: 'Security', icon: Shield },
    // Its own section rather than inside General, which is per-browser by its
    // own description, or Maintenance, which is a page of verbs and would
    // become a junk drawer.
    { id: 'logging', label: 'Logging', icon: ScrollText },
    { id: 'maintenance', label: 'Maintenance', icon: Wrench },
    { id: 'data', label: 'Data Management', icon: Database },
    { id: 'storage', label: 'Browser Storage', icon: HardDrive },
  ];

  // The same index Find anything uses, so the two searches agree: a section
  // stays listed when its name matches or anything in it does, and the
  // settings that match are listed under it to jump to.
  const matchingSettings = searchQuery.trim() ? SETTINGS.filter(st => matchesSetting(st, searchQuery)) : [];
  const filteredCategories = categories.filter(c =>
    matchesWords(c.label, searchQuery) || matchingSettings.some(st => st.section === c.id)
  );


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
            <div key={cat.id} className={`flex items-center pr-3 ${activeCategory === cat.id ? 'bg-primary/10' : ''}`}>
              <button
                onClick={() => setActiveCategory(cat.id)}
                className={`flex-1 flex items-center gap-3 px-4 py-2 text-sm transition-colors ${activeCategory === cat.id ? 'text-primary font-semibold' : 'hover:bg-accent text-foreground/70'}`}
              >
                <cat.icon className="w-4 h-4" />
                {cat.label}
              </button>
              {/* The open section's guide, beside its name rather than over its
                  page, where it would sit on the section's own buttons. */}
              {activeCategory === cat.id && cat.id !== 'security' && <Help topic={`settings-${cat.id}`} />}
            </div>
          ))}
          {matchingSettings.length > 0 && (
            <div className="mt-3 border-t border-border pt-2">
              <p className="px-4 pb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Settings</p>
              {matchingSettings.map(st => (
                <button
                  key={st.id}
                  onClick={() => openSetting(st.id)}
                  className="w-full px-4 py-1.5 text-left text-xs hover:bg-accent"
                >
                  <span className="text-foreground">{st.label}</span>
                  <span className="block text-[10px] text-muted-foreground">{SECTION_LABEL[st.section]}</span>
                </button>
              ))}
            </div>
          )}
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
          {activeCategory === 'accounts' && <MailAccounts />}

          {activeCategory === 'profile' && (
            userProfile ? (
              <div className="animate-in fade-in duration-300">
                <h2 className="text-2xl font-bold text-foreground mb-2">User Profile</h2>
                <p className="text-muted-foreground text-sm mb-8">Manage your personal information and profile picture.</p>
                
                <form onSubmit={handleUpdateProfile} className="space-y-6">
                  <div className="flex items-center gap-8 mb-8">
                    <div className="w-24 h-24 shrink-0 bg-muted border-2 border-dashed border-border flex items-center justify-center overflow-hidden">
                      {userProfile.profileImageUrl ? (
                        <img src={userProfile.profileImageUrl} alt="Profile" className="w-full h-full object-cover" />
                      ) : (
                        <User className="w-10 h-10 text-muted-foreground opacity-50" />
                      )}
                    </div>
                    <div className="flex-1 space-y-4">
                      <div className="space-y-1.5" {...settingAnchor('display-name')}>
                        <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Display Name</label>
                        <input 
                          type="text" 
                          className="w-full bg-background border border-border  px-4 py-2.5 text-sm focus:ring-2 focus:ring-primary/20 outline-none transition-all"
                          value={userProfile.displayName || ''}
                          onChange={e => setUserProfile({...userProfile, displayName: e.target.value})}
                        />
                      </div>
                      <div className="space-y-1.5" {...settingAnchor('profile-email')}>
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

                  <div className="space-y-1.5" {...settingAnchor('profile-image')}>
                    <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Profile Image URL</label>
                    <input 
                      type="text" 
                      placeholder="https://example.com/avatar.jpg"
                      className="w-full bg-background border border-border  px-4 py-2.5 text-sm focus:ring-2 focus:ring-primary/20 outline-none transition-all"
                      value={userProfile.profileImageUrl || ''}
                      onChange={e => setUserProfile({...userProfile, profileImageUrl: e.target.value})}
                    />
                    <div className="flex flex-wrap items-center gap-3 pt-1">
                      <button type="button" onClick={fetchMyGravatar}
                        className="border border-border px-3 py-1.5 text-xs font-semibold hover:bg-accent">
                        Use my Gravatar
                      </button>
                      <span className="text-[11px] text-muted-foreground">
                        Asks Gravatar once for the picture of the email address above, and keeps it here. Your
                        pages load it from InboxQL, never from Gravatar.
                      </span>
                    </div>
                  </div>

                  <div className="pt-4 flex items-center gap-4">
                    <button type="submit" className="bg-primary text-primary-foreground px-5 py-2  font-semibold shadow-sm hover:opacity-90 active:scale-[0.98] transition-all text-sm">Save Profile</button>
                    {profileNote && (
                      <span className={`text-xs ${profileNote.ok ? 'text-emerald-600 dark:text-emerald-400' : 'text-destructive'}`}>{profileNote.text}</span>
                    )}
                  </div>
                </form>
              </div>
            ) : (
              <div className="p-8 text-muted-foreground text-sm">Loading profile...</div>
            )
          )}

          {activeCategory === 'import' && (
            <ImportPanel accounts={accounts} />
          )}

          {activeCategory === 'general' && (
            <div className="animate-in fade-in duration-300">
              <h2 className="text-2xl font-bold text-foreground mb-2">General</h2>
              <p className="text-muted-foreground text-sm mb-8">
                How InboxQL looks and behaves in this browser. These settings are stored
                here rather than on the server, so each machine you use can differ.
              </p>
              <div className="space-y-8">
                <section {...settingAnchor('theme')}>
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

                <ThreadingSetting />
                <ThreadExpansionSetting />
                <AttachmentTargetSetting />
                <ViewerModeSetting />
                <RailDefaults />
              </div>
            </div>
          )}

          {activeCategory === 'ai' && (
            <AISettingsPanel />
          )}

          {activeCategory === 'security' && (
            <div className="py-20 text-center animate-in zoom-in-95 duration-300">
              <AlertCircle className="w-12 h-12 mx-auto mb-4 opacity-10" />
              <h3 className="text-lg font-medium opacity-50">Security Configuration coming soon</h3>
              <p className="text-sm text-muted-foreground mt-2">We are hard at work bringing this feature to InboxQL.</p>
            </div>
          )}

          {activeCategory === 'logging' && (
            <div className="animate-in fade-in duration-300">
              <h2 className="text-2xl font-bold text-foreground mb-2">Logging</h2>
              <p className="text-muted-foreground text-sm mb-8">
                What InboxQL writes down about what it is doing. Unlike General, these are stored
                on the server and apply to every window.
              </p>
              <LogSettings />
            </div>
          )}

          {activeCategory === 'system' && <SystemPanel />}

          {activeCategory === 'calendars' && <CalendarSettings />}

          {activeCategory === 'maintenance' && <MaintenancePanel />}

          {activeCategory === 'data' && (
            <div className="animate-in fade-in duration-300">
              <h2 className="text-2xl font-bold text-foreground mb-2 flex items-center gap-2">
                <Database className="w-6 h-6 text-destructive" />
                Data Management
              </h2>
              <p className="text-muted-foreground text-sm mb-8">Manage the data stored in your local InboxQL database.</p>
              
              <div className="border border-destructive/20 bg-destructive/5 p-6 space-y-4" {...settingAnchor('erase-data')}>
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
                      onClick={handleEraseData}
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

          {activeCategory === 'storage' && (
            <BrowserStoragePanel />
          )}
        </div>
      </div>
    </div>
  );
};

// --- Register Components ---
componentRegistry.register('dashboard', Dashboard);
componentRegistry.register('desk', Desk);
// The layout is persisted, so a tab someone had open before the merge still
// names the component by its old id. Without these the saved layout renders
// "Unknown Component" and the mailbox looks deleted rather than renamed.
componentRegistry.register('mail', Desk);
componentRegistry.register('query', Desk);
componentRegistry.register('search', Desk);
componentRegistry.register('settings', SettingsView);
componentRegistry.register(DOCS_TAB, Docs);
componentRegistry.register(CALENDAR_TAB, Calendar);
componentRegistry.register('agents', AgentManager);
componentRegistry.register('viewer', () => <MessageViewer only="message" />);
// The old id, so a layout persisted before the rename still resolves.
componentRegistry.register('message', () => <MessageViewer only="message" />);
// The per-kind viewers split mode opens. Registered unconditionally and
// permanently: layouts are persisted, so somebody who used split mode once has
// these in their saved layout forever, and an unregistered component renders
// "Unknown Component" — which is what the alias above already exists to avoid.
componentRegistry.register('viewer-contact', () => <MessageViewer only="contact" />);
componentRegistry.register('viewer-file', () => <MessageViewer only="file" />);
componentRegistry.register('errors', ErrorLog);
componentRegistry.register('windows', Windows);

componentRegistry.register('board', Board);
componentRegistry.register('annotators', Annotators);
componentRegistry.register('approvals', Approvals);

/** File → Import calendar: pick an .ics file and add it to your own calendar. */
function importCalendarFile() {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.ics,text/calendar';
  input.onchange = async () => {
    const file = input.files?.[0];
    if (!file) return;
    const r = await fetch('/api/calendar/import?calendar=local', {
      method: 'POST', headers: { 'Content-Type': 'text/calendar' }, body: await file.text(),
    }).catch(() => null);
    const d = r ? await r.json().catch(() => ({})) : {};
    const note = r?.ok
      ? `Imported ${d.imported} event${d.imported === 1 ? '' : 's'} from ${file.name} into your calendar.`
      : `Could not import ${file.name}: ${d.error ?? 'the server refused it'}.`;
    useWindowsStore.getState().note(note);
    if (r?.ok) openTool(CALENDAR_TAB, 'Calendar');
  };
  input.click();
}

function App() {
  // Every menu advertises a shortcut — Control+Shift+D and the rest — but the
  // shell only binds them when this hook is mounted, and it never was. The
  // menus were displaying keystrokes that did nothing.
  useKeyboardShortcuts();
  useDevReload();
  // g d, c, ? and the rest, and Ctrl/⌘+K for Find anything.
  useEffect(() => installKeySequences(id => commandRegistry.executeCommand(id), openPalette), []);

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

  // Registering this window with the backend, so something outside the browser
  // can answer "what is on screen" and "put this in front of them". Started
  // once, after sign-in: an unauthenticated window has nothing to report and
  // could not be commanded anyway.
  const windowName = useWindowsStore(s => s.name);
  const windowLive = useWindowsStore(s => s.connected);
  useEffect(() => {
    if (user) startWindowSession();
  }, [user]);

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

  // The AI settings page links across to the annotator surface, which is a
  // tab rather than a pane. An event rather than a prop, because the settings
  // tree is several components deep and threading a callback through it would
  // couple every level to something none of them use.
  useEffect(() => {
    const open = () => openToolCb('annotators', 'Annotators');
    window.addEventListener('iql:open-annotators', open);
    return () => window.removeEventListener('iql:open-annotators', open);
  }, [openToolCb]);

  useEffect(() => {
    // Labels are what Find anything shows, so they name the thing rather than
    // the menu it sits in. The Ctrl+Shift bindings predate the menus' current
    // shape; they keep working for the people who learned them, but the menus
    // no longer advertise them — several are browser shortcuts on Windows and
    // Linux. The g-sequences in lib/keySequences are what the menus teach.
    const cmd = (id: string, label: string, execute: () => void, keybinding?: string) =>
      commandRegistry.registerCommand({ id, label, execute, keybinding });

    // Places.
    cmd('iql.open-desk', 'Go to Desk', () => openToolCb('desk', 'Desk'), 'Control+Shift+M');
    cmd('iql.open-calendar', 'Go to Calendar', () => openToolCb(CALENDAR_TAB, 'Calendar'), 'Control+Shift+Y');
    // Not Control+Shift+B: Chrome binds that to the bookmarks bar.
    cmd('iql.open-board', 'Go to Board', () => openToolCb('board', 'Board'), 'Control+Shift+K');
    // Desk on a contact query rather than a tab of its own: contacts are an
    // entity in the query language, and a separate surface would be a second
    // place that filters, sorts and pages them.
    cmd('iql.open-contacts', 'Go to Contacts', () => { useQueryStore.getState().set('in:contacts'); openToolCb('desk', 'Desk'); }, 'Control+Shift+C');
    cmd('iql.open-files', 'Go to Files', () => { useQueryStore.getState().set('in:attachments'); openToolCb('desk', 'Desk'); });
    cmd('iql.open-dashboard', 'Go to Dashboard', () => openToolCb('dashboard', 'Analytics Dashboard'), 'Control+Shift+D');
    cmd('iql.open-search', 'Search mail', () => {
      openToolCb('desk', 'Desk');
      setTimeout(() => {
        const el = document.querySelector<HTMLTextAreaElement>('textarea[aria-label="Query"]');
        el?.focus();
        el?.select();
      }, 50);
    }, 'Control+Shift+F');
    cmd('iql.find-anything', 'Find anything…', openPalette);

    // Creating.
    cmd('iql.new-message', 'New message', openCompose);
    cmd('iql.new-event', 'New event', newCalendarEvent);
    cmd('iql.new-task', 'New task', openNewTask);
    const openSettingsAt = (section: string) => {
      requestSettingsSection(section);
      openToolCb('settings', 'Settings');
    };
    cmd('iql.import-mail', 'Import mail…', () => openSettingsAt('import'));
    cmd('iql.import-calendar', 'Import calendar (.ics)…', importCalendarFile);
    cmd('iql.export-calendar', 'Export calendar (.ics)', () => {
      const a = document.createElement('a');
      a.href = '/api/calendar/export?calendar=local';
      a.download = 'inboxql-calendar.ics';
      a.click();
    });
    cmd('iql.open-settings', 'Settings', () => openToolCb('settings', 'Settings'), 'Control+,');
    cmd('iql.open-profile', 'Profile', () => openSettingsAt('profile'));
    cmd('iql.logout', 'Sign out', handleLogout);

    // View.
    // Themes are the shell's own commands (theme.<id>), one per bundled theme.
    cmd('iql.threading-threaded', 'Conversations: grouped', () => useThreadingStore.getState().setThreading('threaded'));
    cmd('iql.threading-flat', 'Conversations: one row per message', () => useThreadingStore.getState().setThreading('flat'));
    cmd('iql.viewer-reuse', 'Viewer tabs: reuse one', () => useViewerStore.getState().setMode('reuse'));
    cmd('iql.viewer-split', 'Viewer tabs: one per kind', () => useViewerStore.getState().setMode('split'));
    cmd('iql.reset-layout', 'Reset layout', () => {
      try { localStorage.removeItem('nexus-shell-layout'); } catch { /* storage unavailable */ }
      window.location.reload();
    });
    cmd('iql.open-windows', 'Windows', () => openToolCb(WINDOWS_TAB, 'Windows'));
    cmd('iql.open-errors', 'Log', () => openErrorLog(), 'Control+Shift+E');

    // Intelligence.
    cmd('iql.open-approvals', 'Waiting for approval', () => openToolCb('approvals', 'Waiting for approval'));
    cmd('iql.open-annotators', 'Labels & extractors', () => openToolCb('annotators', 'Labels & extractors'), 'Control+Shift+L');
    cmd('iql.ai-assistants', 'AI assistants', () => openSetting('ai-assistants'));
    cmd('iql.open-agents', 'AI Agents (Preview)', () => openToolCb('agents', 'AI Agents'), 'Control+Shift+A');

    // Help.
    cmd('iql.open-docs', 'Documentation', () => openDoc(), 'F1');
    cmd('iql.keyboard', 'Keyboard shortcuts', openKeys);
    cmd('iql.for-agents', 'For agents', () => openDoc('agents'));
    cmd('iql.check-updates', 'Check for updates', () => openSetting('updates'));
    // The version on its own answers little; the System section says which
    // version, where the mailbox is and how the server is running.
    cmd('nexus.about', 'About InboxQL', () => openSettingsAt('system'));

    const item = (id: string, label: string, keybinding?: string) => ({ id, label, commandId: id, keybinding });
    // No dividers: the shell's menu bar has no separator and draws "---" as
    // an item. Grouping is by order instead.
    menuRegistry.setMenus({
      'File': [
        item('iql.new-message', 'New message', 'C'),
        item('iql.new-event', 'New event'),
        item('iql.new-task', 'New task', 'T'),
        item('iql.import-mail', 'Import mail…'),
        item('iql.import-calendar', 'Import calendar (.ics)…'),
        item('iql.export-calendar', 'Export calendar (.ics)'),
        item('iql.open-settings', 'Settings…', 'Ctrl+,'),
      ],
      'Go': [
        item('iql.open-desk', 'Desk', 'G D'),
        item('iql.open-calendar', 'Calendar', 'G C'),
        item('iql.open-board', 'Board', 'G B'),
        item('iql.open-contacts', 'Contacts', 'G P'),
        item('iql.open-files', 'Files', 'G F'),
        item('iql.open-dashboard', 'Dashboard'),
        item('iql.find-anything', 'Find anything…', 'Ctrl+K'),
      ],
      'View': [
        { id: 'view.toggle-chat', label: 'Chat', commandId: 'view.toggleChat', keybinding: 'Ctrl+I' },
        { id: 'view.threading', label: 'Conversations', submenu: [
          item('iql.threading-threaded', 'Grouped'),
          item('iql.threading-flat', 'One row per message'),
        ] },
        { id: 'view.viewer', label: 'Viewer tabs', submenu: [
          item('iql.viewer-reuse', 'Reuse one'),
          item('iql.viewer-split', 'One per kind'),
        ] },
        { id: 'view.theme', label: 'Theme', submenu: BUNDLED_THEMES.map(t => ({ id: `view.theme.${t.id}`, label: t.label, commandId: `theme.${t.id}` })) },
        item('iql.reset-layout', 'Reset layout'),
        item('iql.open-windows', 'Windows'),
        item('iql.open-errors', 'Log'),
      ],
      'Intelligence': [
        item('iql.open-approvals', 'Waiting for approval', 'G A'),
        item('iql.open-annotators', 'Labels & extractors'),
        item('iql.ai-assistants', 'AI assistants'),
        item('iql.open-agents', 'AI Agents (Preview)'),
      ],
      'Help': [
        item('iql.open-docs', 'Documentation', 'F1'),
        item('iql.keyboard', 'Keyboard shortcuts', '?'),
        item('iql.for-agents', 'For agents'),
        item('iql.check-updates', 'Check for updates'),
        item('nexus.about', 'About InboxQL'),
      ],
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
    // Which window this is, and whether the backend can reach it. Clicking it
    // opens the list, because a name on its own is a label nobody can map to a
    // screen until something shows them the mapping.
    {
      id: 'window',
      label: windowName ? `Window ${windowName}${windowLive ? '' : ' (no channel)'}` : 'Window —',
      alignment: 'right' as const,
      icon: windowLive ? RadioTower : Radio,
      onClick: () => openToolCb(WINDOWS_TAB, 'Windows'),
    },
    { id: 'chat', label: 'Chat', alignment: 'right' as const, icon: MessageSquare, onClick: () => commandRegistry.executeCommand('view.toggleChat') },
  ];

  useEffect(() => {
    if (user && !loading) {
      // Auto-open Desk on login
      setTimeout(() => openToolCb('desk', 'Desk'), 500);
    }
  }, [user, loading, openTool]);

  if (loading) return null;
  if (!user) return <LoginView onLogin={setUser} />;

  return (
    <div className="h-screen w-screen overflow-hidden iql-workbench">
      {/* Outside the shell, because it explains something that happened to the
          whole window rather than to any one panel. */}
      <WindowNote />
      <ReminderToasts />
      <AppDialogs />
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
            actions={[
              { id: 'profile', label: 'Profile', onSelect: () => commandRegistry.executeCommand('iql.open-profile') },
              { id: 'settings', label: 'Settings', onSelect: () => commandRegistry.executeCommand('iql.open-settings') },
              { id: 'logout', label: 'Sign out', onSelect: () => commandRegistry.executeCommand('iql.logout'), destructive: true, divider: true },
            ]}
          />
        }
      />
    </div>
  );
}

export default App;
