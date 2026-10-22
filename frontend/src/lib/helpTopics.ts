/**
 * Every place in the application that has help, and the guide section that
 * gives it. This is the inventory: a screen that should explain itself is
 * listed here, and a "?" on it is `<Help topic="…" />`.
 *
 * Two tests keep it true. internal/docs reads this file and renders every
 * guide named here, so a topic pointing at a heading that was renamed fails
 * the build rather than landing at the top of the page. helpTopics.test.ts
 * checks the other side: every topic is used somewhere, and nothing asks for
 * a topic that is not here.
 *
 * Keep each entry on one line, in the form below — the Go test reads it.
 */
import { openDoc } from './docsNav';

export interface HelpTopic {
  id: string;
  /** Where it is in the application, for a person reading this list. */
  where: string;
  /** The guide's slug, and the heading's id within it. */
  doc: string;
  anchor?: string;
  /** What the guide section is about, shown on the "?". */
  label: string;
}

export const HELP_TOPICS: HelpTopic[] = [
  { id: 'rail', where: 'Desk, the rail', doc: 'desk', anchor: 'the-rail', label: 'The rail' },
  { id: 'trash-and-spam', where: 'Desk, the note of what Trash and Spam held back', doc: 'query-language', anchor: 'trash-and-spam-stay-out-unless-you-ask', label: 'Trash and Spam stay out unless you ask' },
  { id: 'query-bar', where: 'Desk, the query bar', doc: 'query-language', label: 'The query language' },
  { id: 'load-images', where: 'Message viewer, the Load images bar', doc: 'desk', anchor: 'links-and-images', label: 'Links and images' },
  { id: 'message-labels', where: 'Message viewer, labels from annotators', doc: 'labels', anchor: 'reading-the-answers', label: 'Reading the answers' },
  { id: 'work-loop', where: 'Work loop', doc: 'reading', anchor: 'measuring-the-loop', label: 'Measuring the loop' },
  { id: 'approvals', where: 'Waiting for approval', doc: 'desk', anchor: 'waiting-for-approval', label: 'Waiting for approval' },
  { id: 'contact-card', where: 'Contact card', doc: 'contacts', anchor: 'what-a-contact-card-shows', label: 'What a contact card shows' },
  { id: 'file-tab', where: 'File viewer', doc: 'files', anchor: 'the-file-tab', label: 'The File tab' },
  { id: 'calendar', where: 'Calendar', doc: 'calendar', anchor: 'the-views', label: 'The calendar' },
  { id: 'event-card', where: 'Calendar, an event', doc: 'calendar', anchor: 'an-events-card', label: "An event's card" },
  { id: 'board', where: 'Board', doc: 'tickets', anchor: 'the-board', label: 'The board' },
  { id: 'annotators', where: 'Annotators', doc: 'labels', label: 'Labels and extractors' },
  { id: 'log', where: 'Log', doc: 'desk', anchor: 'the-log', label: 'The Log' },
  { id: 'windows', where: 'Windows', doc: 'desk', anchor: 'windows', label: 'Windows' },
  { id: 'dashboard', where: 'Dashboard', doc: 'desk', anchor: 'the-dashboard', label: 'The Dashboard' },
  { id: 'settings-profile', where: 'Settings, User Profile', doc: 'settings', anchor: 'user-profile', label: 'User Profile' },
  { id: 'settings-general', where: 'Settings, General', doc: 'settings', anchor: 'general', label: 'General settings' },
  { id: 'settings-system', where: 'Settings, System', doc: 'settings', anchor: 'system', label: 'The System section' },
  { id: 'settings-accounts', where: 'Settings, Mail Accounts', doc: 'accounts-and-import', anchor: 'adding-an-account', label: 'Mail accounts' },
  { id: 'settings-calendars', where: 'Settings, Calendars', doc: 'calendar', anchor: 'connecting-google-calendar', label: 'Connecting a calendar' },
  { id: 'settings-import', where: 'Settings, Import Mail', doc: 'accounts-and-import', anchor: 'importing-from-apple-mail', label: 'Importing mail' },
  { id: 'settings-ai', where: 'Settings, AI Configuration', doc: 'settings', anchor: 'ai-configuration', label: 'AI Configuration' },
  { id: 'settings-logging', where: 'Settings, Logging', doc: 'settings', anchor: 'logging', label: 'Logging' },
  { id: 'settings-maintenance', where: 'Settings, Maintenance', doc: 'settings', anchor: 'maintenance', label: 'Maintenance' },
  { id: 'settings-data', where: 'Settings, Data Management', doc: 'settings', anchor: 'data-management', label: 'Data Management' },
  { id: 'settings-storage', where: 'Settings, Browser Storage', doc: 'settings', anchor: 'browser-storage', label: 'Browser Storage' },
  { id: 'gmail-app-password', where: 'Settings, Mail Accounts, adding Gmail', doc: 'accounts-and-import', anchor: 'gmail', label: 'Gmail' },
  { id: 'ai-starters', where: 'Settings, AI Configuration, Starters', doc: 'labels', anchor: 'somewhere-to-start', label: 'Somewhere to start' },
];

export const helpTopic = (id: string): HelpTopic | undefined => HELP_TOPICS.find(t => t.id === id);

/** Open the guide section for a topic. */
export function openHelp(id: string) {
  const t = helpTopic(id);
  if (t) openDoc(t.doc, t.anchor);
}
