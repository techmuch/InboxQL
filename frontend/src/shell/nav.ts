/**
 * The left-hand pane: the places, in the order the redesign gives them.
 *
 * An item either goes to a place, or goes to the mail list with a query —
 * Inbox, Everything, Files and People are all the mail list asking a
 * different question, which is the point of the query language. An item
 * whose place is not built yet is left out rather than drawn dead.
 */
export interface NavItem {
  key: string;
  label: string;
  place: string;
  /** The query the mail list shows, for items that are a question. */
  query?: string;
  /** The number beside it: a count query, or several added up (a count is about one kind). */
  count?: string | string[];
  /** Spoken after the number: "4 unread". */
  countNoun?: string;
}

export interface NavSection {
  key: string;
  label: string;
  items: NavItem[];
}

export const NAV: NavSection[] = [
  {
    key: 'needs', label: 'Needs me', items: [
      { key: 'today', label: 'Today', place: 'today' },
      { key: 'ahead', label: 'Ahead', place: 'ahead' },
      { key: 'replies', label: 'Replies I owe', place: 'replies', count: 'awaiting:me', countNoun: 'owed' },
      { key: 'review', label: 'To review', place: 'review', count: ['in:tickets state:proposed', 'in:events is:proposed', 'in:drafts status:queued'], countNoun: 'to review' },
      { key: 'outbox', label: 'Drafts and outbox', place: 'outbox', count: 'in:drafts status:queued', countNoun: 'waiting to send' },
    ],
  },
  {
    key: 'mail', label: 'Mail', items: [
      { key: 'inbox', label: 'Inbox', place: 'desk', query: 'folder:inbox', count: 'folder:inbox is:unread', countNoun: 'unread' },
      { key: 'everything', label: 'Everything', place: 'desk', query: 'folder:allmail' },
      { key: 'ask', label: 'Ask in words', place: 'ask' },
    ],
  },
  {
    key: 'places', label: 'Places', items: [
      { key: 'calendar', label: 'Calendar', place: 'calendar' },
      { key: 'board', label: 'Board', place: 'board' },
      { key: 'files', label: 'Files', place: 'desk', query: 'in:attachments' },
      { key: 'people', label: 'People', place: 'desk', query: 'in:contacts kind:person' },
      { key: 'activity', label: 'Agent activity', place: 'activity' },
      { key: 'settings', label: 'Settings', place: 'settings' },
      { key: 'help', label: 'Help', place: 'help' },
    ],
  },
];

/** Whether an item is the one on screen. */
export function isCurrent(item: NavItem, place: string, query: string): boolean {
  if (item.place !== place) return false;
  if (!item.query) return true;
  const q = query.trim();
  return q === item.query || q.startsWith(item.query + ' ');
}

/** Every item, flat, for the phone bar and the palette. */
export const allNavItems = (): NavItem[] => NAV.flatMap(s => s.items);
