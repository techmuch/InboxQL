/**
 * Help for the screen you are on: what it is for, what is on it, and the keys
 * that work there. `?` shows it above the full list of keys.
 */
export interface PlaceHelp {
  title: string;
  purpose: string;
  sections?: [string, string][];
  keys?: [string, string][];
  /** The guide that explains it. */
  guide?: string;
}

export const PLACE_HELP: Record<string, PlaceHelp> = {
  today: {
    title: 'Today', purpose: 'What needs you, before what arrived. Every section is a query you can open as a list.',
    sections: [
      ['Replies you owe', 'Conversations the agent judged waiting on you. “Not waiting on me” records your ruling.'],
      ['To review', 'Mail to send, calendar proposals, cards and agent answers to settle, counted.'],
      ['Arrived since yesterday', 'New mail from people, then everything else by sender; archive a sender’s at once, with Undo.'],
      ['Ahead', 'The dates you marked important, counted down.'],
      ['Next 48 hours', 'The calendar.'],
    ],
    keys: [['g t', 'Come back to Today'], ['g r', 'To review'], ['g a', 'Ahead']],
    guide: 'getting-started',
  },
  ahead: {
    title: 'Ahead', purpose: 'What is coming, and is any of it a problem: the calendar, card due dates and important dates in one place.',
    sections: [['Important dates', 'Counted down; amber inside a lead time or the last two weeks.'], ['Overdue', 'Cards past their due date.'], ['Two weeks / Two months', 'Day by day, or as month grids with what matters listed beneath.']],
    keys: [['w', 'Two weeks'], ['m', 'Two months'], ['i', 'Mark a date as important']],
    guide: 'calendar',
  },
  replies: {
    title: 'Replies I owe', purpose: 'Every conversation waiting on you — or, on the other tab, on someone else.',
    sections: [['Last two weeks', 'Reply, open, or rule who it is waiting on.'], ['Older than two weeks', 'Still owed? Reply, or say it is not.']],
    guide: 'contacts',
  },
  review: {
    title: 'To review', purpose: 'One queue for everything proposed and waiting on a person. Nothing an agent proposes takes effect until you approve it.',
    sections: [['Cards', 'Accept or reject.'], ['Calendar proposals', 'Approve or reject.'], ['Mail', 'Send, or return to drafts.'], ['Agent answers', 'Settle what a second reading disagreed with.']],
    guide: 'tickets',
  },
  outbox: { title: 'Drafts and outbox', purpose: 'Outgoing mail that has not gone: waiting for your Send, failed with the reason, and drafts.', guide: 'reading' },
  desk: {
    title: 'Mail', purpose: 'The mail list, asking whatever query is in its bar. The message under the cursor reads in the panel on the right.',
    keys: [['↑ ↓', 'Move; the message reads on the right'], ['Enter', 'Open it on its own'], ['/', 'Edit the query'], ['.', 'Show or hide the reading panel']],
    guide: 'desk',
  },
  viewer: { title: 'Reading', purpose: 'One message, contact or file on its own.', keys: [['Esc', 'Back to where you were']], guide: 'reading' },
  person: { title: 'Person', purpose: 'Who someone is, what is open between you, then everything with them.', guide: 'contacts' },
  topic: { title: 'Topic', purpose: 'Everything about one topic: open loops, money, cards and people, then its mail.', guide: 'labels' },
  'topic-index': { title: 'Topics', purpose: 'What your mail is about, as the agent tags it. Each opens as a page.', guide: 'labels' },
  calendar: { title: 'Calendar', purpose: 'Your calendars and invitations; agent proposals are marked until you approve them.', keys: [['t', 'Today'], ['← →', 'Previous and next'], ['d w m a', 'Day, week, month, agenda'], ['n', 'New event']], guide: 'calendar' },
  board: { title: 'Board', purpose: 'Your cards and lists. Proposed cards wait for you to accept them.', guide: 'tickets' },
  activity: { title: 'Agent activity', purpose: 'What agents wrote and proposed, and how often they are right on your checks.', guide: 'command-line' },
  settings: { title: 'Settings', purpose: 'What needs attention first, then every setting, grouped by what you are doing.', guide: 'settings' },
  help: { title: 'Help', purpose: 'Every guide, built in and matching your version.', keys: [['/', 'Search the guides']] },
};
