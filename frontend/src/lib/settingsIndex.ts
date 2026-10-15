/**
 * Every setting, so Find anything can jump straight to one.
 *
 * Settings are spread over a dozen sections and most are not mounted until
 * their section is open, so they cannot be found by reading the page. Each
 * entry names its section and an anchor; the element carrying the anchor is
 * marked with `settingAnchor(id)`, and settingsIndex.test.ts checks that every
 * entry here has one, so the list cannot quietly drift from the page.
 */
import { requestSettingsSection, requestSettingsTab, revealSetting } from './settingsNav';
import { openTool } from './tabs';

export interface SettingEntry {
  id: string;
  label: string;
  /** The Settings section it is in, as settingsNav names them. */
  section: string;
  /** For a section with tabs of its own (AI Configuration). */
  tab?: string;
  /** Other words someone might type for it. */
  keywords?: string;
  /**
   * Where to land when the setting itself is not drawn — a machine setting
   * before settings.json exists, the conversation option while conversations
   * are off. Another entry's id.
   */
  within?: string;
}

export const SECTION_LABEL: Record<string, string> = {
  profile: 'User Profile',
  general: 'General',
  system: 'System',
  accounts: 'Mail Accounts',
  calendars: 'Calendars',
  import: 'Import Mail',
  ai: 'AI Configuration',
  logging: 'Logging',
  maintenance: 'Maintenance',
  data: 'Data Management',
  storage: 'Browser Storage',
};

export const SETTINGS: SettingEntry[] = [
  { id: 'display-name', label: 'Display name', section: 'profile', keywords: 'name me' },
  { id: 'profile-email', label: 'Your email address', section: 'profile', keywords: 'address me' },
  { id: 'profile-image', label: 'Profile picture', section: 'profile', keywords: 'avatar image photo gravatar' },

  { id: 'theme', label: 'Color theme', section: 'general', keywords: 'dark mode light georgia tech appearance colour' },
  { id: 'inbox-layout', label: 'Inbox layout', section: 'general', keywords: 'conversations threads threaded flat grouped' },
  { id: 'conversations-open', label: 'Conversations start open or closed', section: 'general', keywords: 'expand collapse threads', within: 'inbox-layout' },
  { id: 'attachment-previews', label: 'Attachment previews', section: 'general', keywords: 'files open where preview' },
  { id: 'viewer-tabs', label: 'Viewer tabs', section: 'general', keywords: 'split reuse message tab' },
  { id: 'rail-entries', label: 'Rail entries', section: 'general', keywords: 'sidebar saved queries folders hide reorder' },

  { id: 'this-server', label: 'Version and server', section: 'system', keywords: 'version about mode dev service uptime restart' },
  { id: 'mailbox-location', label: 'Where the mailbox is', section: 'system', keywords: 'data directory folder database path' },
  { id: 'server-address', label: 'Server address', section: 'system', keywords: 'url port password auth' },
  { id: 'login-service', label: 'Login service', section: 'system', keywords: 'background service launch at login start' },
  { id: 'power', label: 'Power', section: 'system', keywords: 'battery mains' },
  { id: 'ai-and-models', label: 'AI and models status', section: 'system', keywords: 'provider remote local' },
  { id: 'updates', label: 'Updates', section: 'system', keywords: 'check for updates upgrade new version release' },
  { id: 'listen-address', label: 'Listen address', section: 'system', keywords: 'addr port settings.json machine', within: 'machine-settings' },
  { id: 'models-folder', label: 'Models folder', section: 'system', keywords: 'weights gliner laya settings.json machine', within: 'machine-settings' },
  { id: 'site-name', label: 'Site name', section: 'system', keywords: 'hostname hosts localhost settings.json machine', within: 'machine-settings' },
  { id: 'heavy-work-on-battery', label: 'Heavy work on battery', section: 'system', keywords: 'annotation sweep power settings.json machine', within: 'machine-settings' },
  { id: 'machine-settings', label: 'Machine settings (settings.json)', section: 'system', keywords: 'settings.json machine file create' },
  { id: 'ai-assistants', label: 'AI assistants', section: 'system', keywords: 'skills claude code codex gemini agents install' },

  { id: 'mail-accounts', label: 'Mail accounts', section: 'accounts', keywords: 'add account imap smtp server password sync gmail' },

  { id: 'calendars-connected', label: 'Connected calendars', section: 'calendars', keywords: 'sync calendar accounts' },
  { id: 'calendar-invitations', label: 'Invitations in mail', section: 'calendars', keywords: 'invites ics meeting' },
  { id: 'google-calendar', label: 'Connect Google Calendar', section: 'calendars', keywords: 'gmail oauth google' },
  { id: 'exchange-server', label: 'Connect Exchange Server', section: 'calendars', keywords: 'ews outlook microsoft ntlm' },

  { id: 'import-source', label: 'Import mail from', section: 'import', keywords: 'apple mail eml mbox full disk access' },
  { id: 'recent-imports', label: 'Recent imports', section: 'import', keywords: 'history import runs', within: 'import-source' },

  { id: 'ai-models', label: 'AI models and providers', section: 'ai', tab: 'models', keywords: 'llm ollama openai provider api key model' },
  { id: 'ai-starters', label: 'Starter labels and extractors', section: 'ai', tab: 'starters', keywords: 'annotators pack install' },
  { id: 'ai-analysis', label: 'Analysis', section: 'ai', tab: 'analysis', keywords: 'llm prompt summarise' },

  { id: 'log-slow-queries', label: 'Slow query threshold', section: 'logging', keywords: 'milliseconds performance' },
  { id: 'log-categories', label: 'Log categories', section: 'logging', keywords: 'http debug noisy also record' },
  { id: 'log-lines', label: 'Log lines to keep', section: 'logging', keywords: 'size cap prune' },
  { id: 'log-query-text', label: 'Record the text of queries', section: 'logging', keywords: 'privacy query log' },

  { id: 'health-checks', label: 'Health checks and maintenance', section: 'maintenance', keywords: 'doctor reindex attachments text extraction repair' },
  { id: 'erase-data', label: 'Delete all stored messages', section: 'data', keywords: 'erase wipe reset database' },
  { id: 'browser-storage', label: 'Browser storage', section: 'storage', keywords: 'local storage cache clear' },
];

/** Spread onto the element a setting lives in: `<section {...settingAnchor('theme')}>`. */
export const settingAnchor = (id: string) => ({ id: `setting-${id}`, 'data-setting': id });

/**
 * Every word typed must begin a word of the text, in any order: "dark mode"
 * finds the theme, "calen" finds Calendars, and "port" finds the listen
 * address without also finding Import.
 */
export function matchesWords(text: string, query: string): boolean {
  const words = text.toLowerCase().split(/[^a-z0-9.]+/).filter(Boolean);
  return query.toLowerCase().split(/\s+/).filter(Boolean)
    .every(q => words.some(w => w.startsWith(q)));
}

export function matchesSetting(s: SettingEntry, query: string): boolean {
  return matchesWords(`${s.label} ${SECTION_LABEL[s.section] ?? s.section} ${s.keywords ?? ''} settings`, query);
}

/** Open Settings at one setting: its section, its tab, scrolled to and flashed. */
export function openSetting(id: string) {
  const s = SETTINGS.find(x => x.id === id);
  if (!s) return;
  requestSettingsSection(s.section);
  if (s.tab) requestSettingsTab(s.tab);
  openTool('settings', 'Settings');
  revealSetting(id, s.within);
}
