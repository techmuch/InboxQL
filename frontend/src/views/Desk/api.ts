/**
 * The workbench's view of the query API.
 *
 * Every shape here mirrors one the server already defines. Nothing in this file
 * knows the grammar — completions and errors are asked for, not computed. A
 * second implementation of the language in TypeScript would drift from the Go
 * one, and the failure mode is an editor that confidently offers something the
 * server then rejects.
 */

export interface Candidate {
  value: string;
  detail?: string;
  kind?: string;
}

export interface Completion {
  context: 'field' | 'value' | 'stage' | 'groupkey' | 'bucket' | 'sortkey';
  field?: string;
  valueType?: string;
  valueSource?: string;
  prefix: string;
  /** The span the candidate replaces, so insertion is a splice not a guess. */
  start: number;
  end: number;
  candidates: Candidate[];
  negated?: boolean;
}

export interface QueryMessage {
  id: string;
  from: string;
  to?: string[];
  subject: string;
  date: string;
  flags?: string[];
  size?: number;
  mailbox?: string;
}

export interface QueryGroup {
  label: string;
  value: number;
}

export interface Ticket {
  id: string;
  title: string;
  status: string;
  /** The conversation this came from, when it came from one. */
  threadKey?: string;
  priority?: string;
  dueAt?: string;
  origin: string;
  sources?: Array<{ messageId: string; subject?: string; from?: string }>;
}

export interface Draft {
  id: string;
  to?: string[];
  subject: string;
  status: string;
  origin: string;
  updatedAt?: string;
}

/**
 * One moment on a conversation's timeline.
 *
 * Exactly one payload field is set and `kind` says which. The server also sends
 * `summary` — a line already rendered — so a client can list a timeline without
 * knowing the shape of all five payload types.
 */
export interface ThreadEntry {
  kind: 'message' | 'ticket' | 'draft' | 'event' | 'annotation';
  at: string;
  actor?: string;
  summary: string;
  message?: QueryMessage;
  ticket?: Ticket;
  draft?: Draft;
  event?: { kind: string; from?: string; to?: string; actor: string; at: string };
  annotation?: { id: string; dataJson: string; confidence?: number; source: string };
}

/** A conversation and everything anchored to it. */
export interface Thread {
  key: string;
  subject?: string;
  participants?: string[];
  entries: ThreadEntry[];
  messageCount: number;
  ticketCount: number;
  draftCount: number;
  start: string;
  end: string;
}

export interface QueryResult {
  query: string;
  kind: 'messages' | 'groups' | 'count' | 'tickets' | 'drafts' | 'threads';
  count: number;
  messages?: QueryMessage[];
  groups?: QueryGroup[];
  tickets?: Ticket[];
  drafts?: Draft[];
  threads?: Thread[];
  total?: number;
  /** What an aggregate grouped by, so a chart click can build a drill-down. */
  groupField?: string;
  bucket?: string;
  sql?: string;
}

export interface SavedQuery {
  id?: string;
  name?: string;
  title: string;
  query: string;
  description?: string;
  pinned?: boolean;
}

/**
 * A query the server refused, carrying where it gave up.
 *
 * The position is the useful part: it is what puts a caret under the offending
 * character instead of making the reader diff two strings by eye.
 */
export class QueryFailed extends Error {
  position?: number;

  constructor(message: string, position?: number) {
    super(message);
    this.name = 'QueryFailed';
    this.position = position;
  }
}

async function readError(res: Response): Promise<never> {
  const text = await res.text();
  try {
    const body = JSON.parse(text);
    throw new QueryFailed(body.message ?? body.error ?? text, body.position);
  } catch (e) {
    if (e instanceof QueryFailed) throw e;
    throw new QueryFailed(text || `Request failed (${res.status})`);
  }
}

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) await readError(res);
  return res.json();
}

export async function runQuery(q: string, limit = 100, offset = 0): Promise<QueryResult> {
  const params = new URLSearchParams({ q, limit: String(limit) });
  if (offset > 0) params.set('offset', String(offset));
  return getJSON<QueryResult>(`/api/query?${params}`);
}

export async function explainQuery(q: string): Promise<QueryResult> {
  return getJSON<QueryResult>(`/api/query/explain?${new URLSearchParams({ q })}`);
}

export async function complete(q: string, pos: number): Promise<Completion> {
  const params = new URLSearchParams({ q, pos: String(pos) });
  return getJSON<Completion>(`/api/query/complete?${params}`);
}

export async function listSaved(): Promise<SavedQuery[]> {
  return getJSON<SavedQuery[]>('/api/queries');
}

export async function saveQuery(q: SavedQuery): Promise<SavedQuery> {
  // application/json is required: the server answers 415 without it, which is
  // what takes the CORS simple-request exemption away from a cross-origin page.
  const res = await fetch('/api/queries', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(q),
  });
  if (!res.ok) await readError(res);
  return res.json();
}

export async function deleteSaved(name: string): Promise<void> {
  const res = await fetch(`/api/queries?${new URLSearchParams({ name })}`, { method: 'DELETE' });
  if (!res.ok) await readError(res);
}

/**
 * Build the term that narrows a result to one group.
 *
 * A chart click cannot work this out from the label alone: "2026-03" could be a
 * month or a sender with an unusual name. The server says which, and this turns
 * that back into something the user can read and edit in the query bar.
 */
export function drillDownTerm(result: QueryResult, label: string): string | null {
  const field = result.groupField;
  if (!field || !label) return null;

  if (result.bucket) return `on:${label}`;

  switch (field) {
    case 'from':
    case 'to':
    case 'cc':
    case 'bcc':
    case 'anyone':
      return `${field}:=${label}`;
    case 'domain':
      return `from:*@${label}`;
    case 'label':
      return `label:${label}`;
    case 'account':
    case 'mailbox':
      return `${field}:${label}`;
    case 'thread':
      // thread: takes a message id, and the group key is the conversation key.
      return null;
    case 'subject':
      return `subject:"${label.replace(/"/g, '""')}"`;
    default:
      return null;
  }
}

/**
 * Mark a set of messages read, unread or starred.
 *
 * Local only: InboxQL's sync is one-way, so this changes what InboxQL knows and
 * not what the IMAP server does. The count comes back so the UI can report what
 * actually changed rather than implying the server was told.
 */
export async function setMessageFlag(
  ids: string[],
  flag: '\\Seen' | '\\Flagged',
  on: boolean,
): Promise<number> {
  const res = await fetch('/api/messages/flags', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids, flag, on }),
  });
  const body = await res.json().catch(() => null);
  if (!res.ok) throw new Error(body?.error ?? 'could not update those messages');
  return body?.changed ?? 0;
}
