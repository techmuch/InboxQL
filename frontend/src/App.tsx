import { useEffect, useState, useCallback } from 'react';
import { commandRegistry, useThemeStore, useKeyboardShortcuts } from 'nexus-shell';
import { Search, Mail, Server, Shield, Cpu, Eye, Check, AlertCircle, User, Lock, Download, Database, HardDrive, Wrench } from 'lucide-react';
import { AgentManager } from './AgentManager';
import { ImportPanel } from './views/ImportPanel';
import { BrowserStoragePanel } from './views/BrowserStoragePanel';
import { AISettingsPanel } from './views/AISettingsPanel';
import { MaintenancePanel } from './views/MaintenancePanel';
import { Annotators } from './views/Annotators';
import { MessageViewer } from './views/MessageViewer';
import { ErrorLog } from './views/ErrorLog';
import { Desk } from './views/Desk';
import { Dashboard } from './views/Dashboard';
import { Board } from './views/Board';
import { DataManagement } from './views/DataManagement';
import { openTool, openErrorLog, openQuery, useViewerStore } from './lib/tabs';
import { useAttachmentTargetStore, type AttachmentTarget } from './lib/attachmentTarget';
import { CalendarDays, ScrollText } from 'lucide-react';
import { useThreadingStore, type Threading } from './lib/threading';
import { ENTRY_CAP, useThreadExpansionStore, type ThreadExpansion } from './lib/threadExpansion';
import { startWindowSession, useWindowsStore, WINDOWS_TAB } from './lib/windows';
import { Windows } from './views/Windows';
import { WindowNote } from './views/WindowNote';
import { RailDefaults } from './views/RailDefaults';
import { LogSettings } from './views/LogSettings';
import { useQueryStore } from './lib/filters';
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
import { WorkLoop } from './views/WorkLoop';
import { openCompose, openKeys, openNewTask, openPalette } from './lib/appUi';
import { installKeySequences } from './lib/keySequences';
import { openDoc } from './lib/docsNav';
import { appVersion, notesAnchor } from './lib/version';
import { AppShell } from './shell/AppShell';
import { Evidence } from './shell/Evidence';
import { followAddress, usePlaceStore } from './lib/places';
import 'nexus-shell/style.css';
import './App.css';
import { Help } from './views/Help';
import { Today } from './views/Today';
import { SettingsOverview, type SettingsGroup } from './views/Settings/Overview';
import { Ahead } from './views/Ahead';
import { Review } from './views/NeedsMe/Review';
import { Replies } from './views/NeedsMe/Replies';
import { Outbox } from './views/NeedsMe/Outbox';
import { Activity } from './views/NeedsMe/Activity';
import { Person } from './views/Subjects/Person';
import { Ask } from './views/Ask';
import { AskSetting } from './views/Ask/AskSetting';
import { askInWords, looksLikeQuestion } from './lib/ask';
import { Topic, Topics } from './views/Subjects/Topic';

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

// --- Gmail-like Mail Client Tool ---
// --- Unified Settings View ---
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
  const [activeCategory, setActiveCategory] = useState(() => takeSettingsSection() ?? 'overview');
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

  // Sections, grouped by what you are doing rather than by the code behind
  // them: who you are and your accounts, agents, how things look, your data,
  // and this computer. Each section id is unchanged, so every link into
  // Settings (the settings index, the guides) still lands where it did.
  const categories = [
    { id: 'overview', label: 'Overview', icon: Check, group: '' },
    { id: 'profile', label: 'Profile', icon: User, group: 'you' },
    { id: 'accounts', label: 'Mail accounts', icon: Mail, group: 'you' },
    { id: 'calendars', label: 'Calendars', icon: CalendarDays, group: 'you' },
    { id: 'ai', label: 'AI and agents', icon: Cpu, group: 'agents' },
    { id: 'general', label: 'Look and reading', icon: Eye, group: 'reading' },
    { id: 'import', label: 'Import mail', icon: Download, group: 'data' },
    { id: 'data', label: 'Backups and data', icon: Database, group: 'data' },
    { id: 'maintenance', label: 'Health and maintenance', icon: Wrench, group: 'data' },
    { id: 'storage', label: 'Browser storage', icon: HardDrive, group: 'data' },
    { id: 'system', label: 'System and updates', icon: Server, group: 'computer' },
    { id: 'logging', label: 'Logging', icon: ScrollText, group: 'computer' },
    { id: 'security', label: 'Security', icon: Shield, group: 'computer' },
  ];
  const GROUPS: SettingsGroup[] = [
    { id: 'you', label: 'You and your accounts', about: 'Your name and picture, the mail accounts InboxQL reads, and connected calendars.', sections: [] },
    { id: 'agents', label: 'Agents and AI', about: 'Where summaries and drafts are written, local models, and the starters.', sections: [] },
    { id: 'reading', label: 'Reading and appearance', about: 'Theme, how the inbox is laid out, conversations, attachment previews.', sections: [] },
    { id: 'data', label: 'Your data', about: 'Importing, backups, health checks and maintenance, and what this browser stores.', sections: [] },
    { id: 'computer', label: 'This computer', about: 'Version and updates, the background service, logging, security.', sections: [] },
  ].map(g => ({ ...g, sections: categories.filter(c => c.group === g.id).map(c => ({ id: c.id, label: c.label })) }));

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
          {filteredCategories.map((cat, i) => (
            <div key={cat.id}>
            {cat.group && cat.group !== filteredCategories[i - 1]?.group && (
              <h2 className="px-4 pb-1 pt-3 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">{GROUPS.find(g => g.id === cat.group)?.label}</h2>
            )}
            <div className={`flex items-center pr-3 ${activeCategory === cat.id ? 'bg-primary/10' : ''}`}>
              <button
                onClick={() => setActiveCategory(cat.id)}
                aria-current={activeCategory === cat.id ? 'page' : undefined}
                className={`flex-1 flex items-center gap-3 px-4 py-2 text-sm transition-colors ${activeCategory === cat.id ? 'text-primary font-semibold' : 'hover:bg-accent text-foreground/70'}`}
              >
                <cat.icon className="w-4 h-4" />
                {cat.label}
              </button>
              {/* The open section's guide, beside its name rather than over its
                  page, where it would sit on the section's own buttons. */}
              {activeCategory === cat.id && cat.id !== 'security' && cat.id !== 'overview' && <Help topic={`settings-${cat.id}`} />}
            </div>
            </div>
          ))}
          {matchingSettings.length > 0 && (
            <div className="mt-3 border-t border-border pt-2">
              <p className="px-4 pb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">Settings</p>
              {matchingSettings.map(st => (
                <button
                  key={st.id}
                  onClick={() => openSetting(st.id)}
                  className="w-full px-4 py-1.5 text-left text-xs hover:bg-accent"
                >
                  <span className="text-foreground">{st.label}</span>
                  <span className="block text-[11px] text-muted-foreground">{SECTION_LABEL[st.section]}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="flex-1 overflow-auto p-8">
        <div className="max-w-3xl mx-auto">
          {activeCategory === 'overview' && <SettingsOverview groups={GROUPS} open={setActiveCategory} />}
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
                        <label htmlFor="profile-name" className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Display Name</label>
                        <input id="profile-name" 
                          type="text" 
                          className="w-full bg-background border border-border  px-4 py-2.5 text-sm focus:ring-2 focus:ring-primary/20 outline-none transition-all"
                          value={userProfile.displayName || ''}
                          onChange={e => setUserProfile({...userProfile, displayName: e.target.value})}
                        />
                      </div>
                      <div className="space-y-1.5" {...settingAnchor('profile-email')}>
                        <label htmlFor="profile-email-input" className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Email Address</label>
                        <input id="profile-email-input" 
                          type="email" 
                          className="w-full bg-background border border-border  px-4 py-2.5 text-sm focus:ring-2 focus:ring-primary/20 outline-none transition-all"
                          value={userProfile.email || ''}
                          onChange={e => setUserProfile({...userProfile, email: e.target.value})}
                        />
                      </div>
                    </div>
                  </div>

                  <div className="space-y-1.5" {...settingAnchor('profile-image')}>
                    <label htmlFor="profile-image-url" className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Profile Image URL</label>
                    <input id="profile-image-url" 
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
                <RailDefaults />
              </div>
            </div>
          )}

          {activeCategory === 'ai' && (
            <>
            <AskSetting />
            <AISettingsPanel />
            </>
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

          {activeCategory === 'data' && <DataManagement />}

          {activeCategory === 'storage' && (
            <BrowserStoragePanel />
          )}
        </div>
      </div>
    </div>
  );
};

// --- Places ---
/**
 * What each place shows. A place is one screen in the middle of the frame
 * (see lib/places.ts); its id is the old tab component id, so everything that
 * opened a tab now goes to the place instead.
 */
const VIEWS: Record<string, { label: string; view: React.ComponentType }> = {
  today: { label: 'Today', view: Today },
  ahead: { label: 'Ahead', view: Ahead },
  desk: { label: 'Mail', view: Desk },
  dashboard: { label: 'Dashboard', view: Dashboard },
  settings: { label: 'Settings', view: SettingsView },
  help: { label: 'Help', view: Docs },
  calendar: { label: 'Calendar', view: Calendar },
  agents: { label: 'AI Agents (preview)', view: AgentManager },
  viewer: { label: 'Reading', view: ViewerPlace },
  errors: { label: 'Log', view: ErrorLog },
  windows: { label: 'Windows', view: Windows },
  board: { label: 'Board', view: Board },
  annotators: { label: 'Labels and extractors', view: Annotators },
  approvals: { label: 'Waiting for approval', view: Approvals },
  review: { label: 'To review', view: Review },
  replies: { label: 'Replies I owe', view: Replies },
  outbox: { label: 'Drafts and outbox', view: Outbox },
  person: { label: 'Person', view: Person },
  ask: { label: 'Ask in words', view: Ask },
  topic: { label: 'Topic', view: Topic },
  'topic-index': { label: 'Topics', view: Topics },
  'work-loop': { label: 'Work loop', view: WorkLoop },
  activity: { label: 'Agent activity', view: Activity },
};

/** The viewer as a place: a way back, then the message, contact or file. */
function ViewerPlace() {
  return (
    <div className="flex h-full flex-col">
      <div className="iql-crumbs">
        <button type="button" className="iql-btn" onClick={() => window.history.back()}>← Back</button>
        <span className="text-muted-foreground">Esc also goes back</span>
      </div>
      <div className="min-h-0 flex-1"><MessageViewer only="message" /></div>
    </div>
  );
}

const labelOf = (place: string) => VIEWS[place]?.label ?? 'InboxQL';
/** Saved queries the pane already has as places of their own. */
const PLACE_QUERIES = new Set(['inbox', 'tickets', 'proposed', 'files', 'people']);
const AVAILABLE = new Set(Object.keys(VIEWS));

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
  const [globalUnread, setGlobalUnread] = useState(0);
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
    cmd('iql.open-desk', 'Go to mail, where you left it', () => openToolCb('desk', 'Desk'), 'Control+Shift+M');
    cmd('iql.open-today', 'Go to Today', () => openToolCb('today', 'Today'));
    cmd('iql.open-ahead', 'Go to Ahead', () => openToolCb('ahead', 'Ahead'));
    cmd('iql.open-replies', 'Go to Replies I owe', () => openToolCb('replies', 'Replies I owe'));
    cmd('iql.open-outbox', 'Go to Drafts and outbox', () => openToolCb('outbox', 'Drafts and outbox'));
    cmd('iql.open-activity', 'Go to Agent activity', () => openToolCb('activity', 'Agent activity'));
    cmd('iql.open-inbox', 'Go to Inbox', () => openQuery('folder:inbox'));
    cmd('iql.open-everything', 'Go to Everything', () => openQuery('folder:allmail'));
    cmd('iql.open-review', 'Go to To review', () => openToolCb('review', 'To review'));
    cmd('iql.open-calendar', 'Go to Calendar', () => openToolCb(CALENDAR_TAB, 'Calendar'), 'Control+Shift+Y');
    // Not Control+Shift+B: Chrome binds that to the bookmarks bar.
    cmd('iql.open-board', 'Go to Board', () => openToolCb('board', 'Board'), 'Control+Shift+K');
    // Desk on a contact query rather than a tab of its own: contacts are an
    // entity in the query language, and a separate surface would be a second
    // place that filters, sorts and pages them.
    cmd('iql.open-contacts', 'Go to People', () => { useQueryStore.getState().set('in:contacts kind:person'); openToolCb('desk', 'Desk'); }, 'Control+Shift+C');
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
    cmd('iql.open-windows', 'Windows', () => openToolCb(WINDOWS_TAB, 'Windows'));
    cmd('iql.open-errors', 'Log', () => openErrorLog(), 'Control+Shift+E');

    // Intelligence.
    cmd('iql.open-approvals', 'Waiting for approval', () => openToolCb('approvals', 'Waiting for approval'));
    cmd('iql.open-work-loop', 'Work loop', () => openToolCb('work-loop', 'Work loop'));
    cmd('iql.open-annotators', 'Labels & extractors', () => openToolCb('annotators', 'Labels & extractors'), 'Control+Shift+L');
    cmd('iql.ai-assistants', 'AI assistants', () => openSetting('ai-assistants'));
    cmd('iql.open-agents', 'AI Agents (Preview)', () => openToolCb('agents', 'AI Agents'), 'Control+Shift+A');

    // Help.
    cmd('iql.open-docs', 'Documentation', () => openDoc(), 'F1');
    cmd('iql.keyboard', 'Keyboard shortcuts', openKeys);
    cmd('iql.for-agents', 'For agents', () => openDoc('agents'));
    cmd('iql.release-notes', 'Release notes', () => openDoc('release-notes', notesAnchor(appVersion)));
    cmd('iql.check-updates', 'Check for updates', () => openSetting('updates'));
    // The version on its own answers little; the System section says which
    // version, where the mailbox is and how the server is running.
    cmd('nexus.about', 'About InboxQL', () => openSettingsAt('system'));

    // Branding and the activity bar used to be forced here by polling the DOM
    // every 100ms: nexus-shell 0.1.x had no way to set a title, and no way to
    // suppress the activity bar, so the shell's own markup was rewritten from
    // outside. 0.2.x makes both declarative — the `title` prop on ShellLayout,
    // and ConnectedPaneRail rendering nothing when no panels are registered —
    // so the interval is gone rather than merely tidied.
  }, [user, openTool]);

  useEffect(() => followAddress(), []);
  const place = usePlaceStore(s => s.id);
  // Esc on the reading place goes back, as the crumb says.
  useEffect(() => {
    if (place !== 'viewer') return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      const t = e.target as HTMLElement | null;
      if (t?.closest?.('input, textarea, select, [role="dialog"]')) return;
      window.history.back();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [place]);

  // Subjects in the left-hand pane: your saved queries, in your order, less the
  // ones that are places of their own now, then Topics.
  const [saved, setSaved] = useState<{ name: string; title?: string; query: string }[]>([]);
  useEffect(() => {
    if (!user) return;
    const read = () => fetch('/api/queries').then(r => (r.ok ? r.json() : [])).then(d => setSaved(Array.isArray(d) ? d : [])).catch(() => {});
    read();
    window.addEventListener('iql:saved-changed', read);
    return () => window.removeEventListener('iql:saved-changed', read);
  }, [user]);
  const subjects = {
    key: 'subjects', label: 'Subjects',
    items: [
      ...saved.filter(q => !PLACE_QUERIES.has(q.name)).map(q => ({ key: `q-${q.name}`, label: q.title || q.name, place: 'desk', query: q.query })),
      { key: 'topics', label: 'Topics', place: 'topic-index' },
    ],
  };

  if (loading) return null;
  if (!user) return <LoginView onLogin={setUser} />;

  const View = (VIEWS[place] ?? VIEWS.today).view;
  const onDesk = place === 'desk';

  return (
    <>
      {/* Outside the frame, because it explains something that happened to the
          whole window rather than to any one screen. */}
      <WindowNote />
      <ReminderToasts />
      <AppDialogs />
      <AppShell
        available={AVAILABLE}
        subjects={subjects}
        labelOf={labelOf}
        onQuery={q => (looksLikeQuestion(q) ? askInWords(q) : openQuery(q))}
        onAsk={q => askInWords(q)}
        hideQuery={onDesk}
        actions={
          <>
            <button type="button" className="iql-btn" onClick={openPalette} aria-keyshortcuts="Meta+K Control+K">Go to… <kbd>⌘K</kbd></button>
            <button type="button" className="iql-btn primary" onClick={openCompose} aria-keyshortcuts="c">Write <kbd>c</kbd></button>
          </>
        }
        status={
          <>
            <span aria-hidden="true" className={`inline-block h-2 w-2 rounded-full ${lastError ? 'bg-destructive' : 'bg-green-600'}`} />
            <span>{lastError ? lastError : `${globalUnread} unread`}</span>
            <button type="button" onClick={() => openToolCb(WINDOWS_TAB, 'Windows')}
              title="Open windows, and what each is showing">
              {windowName ? `Window ${windowName}${windowLive ? '' : ' (no channel)'}` : 'Window —'}
            </button>
          </>
        }
        person={
          <>
            <span className="truncate" title={user.email}>{user.displayName || user.email}</span>
            <button type="button" onClick={() => commandRegistry.executeCommand('iql.open-profile')}>Profile</button>
            <button type="button" onClick={handleLogout}>Sign out</button>
            <button type="button" onClick={() => openDoc('release-notes', notesAnchor(appVersion))}>v{appVersion}</button>
          </>
        }
        evidenceDefault={
          onDesk
            ? <><h2>Reading</h2><p>Move to a message to read it here. Enter opens it on its own.</p></>
            : <><h2>Evidence</h2><p>What this screen rests on appears here. Drag the edge or press <kbd className="iql-kbd">.</kbd> to hide it.</p></>
        }
      >
        <View />
      </AppShell>
      {onDesk && <DeskReading />}
    </>
  );
}

/**
 * The message under the cursor in the mail list, read in the evidence panel.
 *
 * Arrowing the list already points the viewer at the row (previewMessage);
 * the viewer used to be a tab beside the list, and now it is the panel.
 */
function DeskReading() {
  const has = useViewerStore(s => Boolean(s.message || s.contact || s.file));
  if (!has) return null;
  return <Evidence><MessageViewer /></Evidence>;
}

export default App;
