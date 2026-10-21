import type { QueryTerm } from './filters';

/**
 * The folder a query is inside, and what was added to it.
 *
 * The query stays one string — everything that opens a query from elsewhere
 * keeps writing one — and the frame is read out of it: its top-level
 * `folder:` term, or the `in:` term that says which kind of thing it is about.
 * The Desk shows that as a fixed chip in front of the box, and the box holds
 * the rest. Putting them back together is the same string with the term at
 * the front, so the chip and the box cannot disagree with what runs.
 */
export interface Scope {
  /** A rail id: a folder (inbox, allmail, …), or a kind (files, cards, …). */
  id: string;
  label: string;
  /** The term as it stands in the query; empty when the folder is implied. */
  term: string;
  /** Mail, or another kind of thing. */
  mail: boolean;
}

export const FOLDER_LABELS: Record<string, string> = {
  inbox: 'Inbox',
  starred: 'Starred',
  sent: 'Sent',
  drafts: 'Drafts',
  allmail: 'All Mail',
  archive: 'Archive',
  important: 'Important',
  spam: 'Spam',
  trash: 'Trash',
  all: 'Everything',
};

/** The kinds a query can be about, by `in:` value, with the name people use. */
export const KIND_SCOPES: Record<string, { id: string; label: string }> = {
  attachments: { id: 'files', label: 'Files' },
  files: { id: 'files', label: 'Files' },
  tickets: { id: 'cards', label: 'Cards' },
  ticket: { id: 'cards', label: 'Cards' },
  contacts: { id: 'people', label: 'People' },
  events: { id: 'events', label: 'Events' },
  drafts: { id: 'drafts', label: 'Drafts' },
  logs: { id: 'log', label: 'Log' },
};

/** The `in:` value for a kind scope's id. */
export const KIND_TERMS: Record<string, string> = {
  files: 'in:attachments',
  cards: 'in:tickets',
  people: 'in:contacts',
  events: 'in:events',
  log: 'in:logs',
};

/** What the result list is, when nothing in the query names it. */
const RESULT_KINDS: Record<string, { id: string; label: string }> = {
  tickets: { id: 'cards', label: 'Cards' },
  contacts: { id: 'people', label: 'People' },
  attachments: { id: 'files', label: 'Files' },
  events: { id: 'events', label: 'Events' },
  drafts: { id: 'drafts', label: 'Drafts' },
  logs: { id: 'log', label: 'Log' },
};

const ALL_MAIL: Scope = { id: 'allmail', label: 'All Mail', term: '', mail: true };

/**
 * Split a query into its frame and the rest.
 *
 * Only a top-level, positive term frames a query: `-folder:spam` is a filter,
 * and a folder inside parentheses is part of an expression somebody wrote.
 * `resultKind` names the kind when no term does — `status:todo` is about
 * cards without saying `in:tickets`.
 */
export function splitScope(query: string, terms: QueryTerm[], resultKind?: string): { scope: Scope; rest: string } {
  const frame = terms.find(t => !t.negated && (t.field === 'folder' || t.field === 'in'));
  if (!frame) {
    const kind = resultKind ? RESULT_KINDS[resultKind] : undefined;
    const scope = kind ? { ...kind, term: '', mail: false } : ALL_MAIL;
    return { scope, rest: query.trim() };
  }
  const value = frame.text.slice(frame.text.indexOf(':') + 1).toLowerCase();
  const rest = (query.slice(0, frame.start) + query.slice(frame.end)).replace(/\s{2,}/g, ' ').trim();
  if (frame.field === 'folder') {
    if (value === 'drafts') return { scope: { id: 'drafts', label: 'Drafts', term: frame.text, mail: false }, rest };
    return { scope: { id: value, label: FOLDER_LABELS[value] ?? value, term: frame.text, mail: true }, rest };
  }
  if (value === 'mail') return { scope: { ...ALL_MAIL, term: frame.text }, rest };
  const kind = KIND_SCOPES[value] ?? { id: value, label: value };
  return { scope: { ...kind, term: frame.text, mail: false }, rest };
}

/** The frame and the rest, back into one query, the frame first. */
export const joinScope = (term: string, rest: string): string =>
  [term.trim(), rest.trim()].filter(Boolean).join(' ');

/**
 * Whether a query names its own frame — a saved query like `in:contacts …`
 * keeps it; one like `awaiting:me | timeline` runs inside the current folder.
 */
export const namesItsFrame = (terms: QueryTerm[]): boolean =>
  terms.some(t => !t.negated && (t.field === 'folder' || t.field === 'in'));

/** Whether a term is in the query, at the top level, as written. */
export const hasTerm = (terms: QueryTerm[], term: string): boolean =>
  terms.some(t => t.text.toLowerCase() === term.toLowerCase());
