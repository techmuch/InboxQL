/** The board's data, and every change the page makes to it. */

export interface TicketSource {
  messageId: string;
  subject?: string;
  from?: string;
  date?: string;
  /** How many distinct files this evidence carried. */
  attachments?: number;
}

export interface Ticket {
  id: string;
  threadKey?: string;
  title: string;
  body?: string;
  status: string;
  priority?: string;
  dueAt?: string;
  startAt?: string;
  archivedAt?: string;
  closedAt?: string;
  origin: string;
  confidence?: number;
  position: number;
  labels?: string[];
  checklistDone?: number;
  checklistTotal?: number;
  comments?: number;
  sources?: TicketSource[];
  createdAt?: string;
}

export interface ChecklistItem {
  id: string;
  ticketId: string;
  checklist: string;
  text: string;
  done: boolean;
  doneAt?: string;
}

export interface Comment {
  id: string;
  body: string;
  bodyHtml?: string;
  actor: string;
  createdAt: string;
  updatedAt: string;
}

export interface HistoryEvent {
  id: string;
  kind: string;
  from?: string;
  to?: string;
  actor: string;
  at: string;
}

export interface CardDetail extends Ticket {
  bodyHtml?: string;
  listTitle?: string;
  checklist: ChecklistItem[];
  commentList: Comment[];
  history: HistoryEvent[];
}

export interface Column {
  status: string;
  title: string;
  kind: 'proposed' | 'open' | 'closed';
  position: number;
  total: number;
  tickets: Ticket[];
}

export interface List {
  id: string;
  title: string;
  kind: 'proposed' | 'open' | 'closed';
  position: number;
  archivedAt?: string;
  cards: number;
}

export interface BoardDef {
  id: string;
  title: string;
  filter: string;
  position: number;
  labels?: string[];
}

export interface LabelInfo {
  name: string;
  color: string;
  chosen?: boolean;
  cards: number;
  messages: number;
}

export interface RuleAction { do: string; value?: string }

export interface Rule {
  id: string;
  title: string;
  trigger: 'enter-list' | 'mail';
  list: string;
  query?: string;
  actions: RuleAction[];
  enabled: boolean;
  lastRunAt?: string;
}

async function call<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    const text = await res.text();
    let msg = text || `${res.status}`;
    try {
      const j = JSON.parse(text);
      msg = j.message ?? j.error ?? msg;
    } catch { /* plain text */ }
    throw new Error(msg);
  }
  return res.json() as Promise<T>;
}

export const fetchColumns = (board: string, q: string) => {
  const params = new URLSearchParams();
  if (board) params.set('board', board);
  if (q) params.set('q', q);
  params.set('limit', '200');
  return call<Column[]>('GET', `/api/tickets/board?${params}`);
};

export const fetchCard = (id: string) => call<CardDetail>('GET', `/api/tickets?id=${encodeURIComponent(id)}`);

export const createCard = (title: string, list: string, labels: string[]) =>
  call<CardDetail>('POST', '/api/tickets', { title, status: list, labels, origin: 'human' });

export type CardPatch = Partial<Pick<Ticket, 'title' | 'body' | 'priority' | 'status'>> & {
  dueAt?: string | null;
  startAt?: string | null;
};

export const patchCard = (id: string, patch: CardPatch) =>
  call<CardDetail>('PATCH', `/api/tickets?id=${encodeURIComponent(id)}`, patch);

/** after: a card id, "^" for the top, "" for the bottom. */
export const moveCard = (id: string, list: string, after: string) =>
  call<CardDetail>('POST', '/api/board/move', { id, list, after });

export const archiveCard = (id: string, archived: boolean) =>
  call<CardDetail>('POST', '/api/board/archive', { id, archived });

export const deleteCard = async (id: string) => {
  const res = await fetch(`/api/tickets?id=${encodeURIComponent(id)}`, { method: 'DELETE' });
  if (!res.ok) throw new Error(await res.text());
};

export const setCardLabels = (id: string, change: { add?: string[]; remove?: string[] }) =>
  call<CardDetail>('POST', '/api/board/labels', { id, ...change });

export const fetchLabels = () => call<{ labels: LabelInfo[]; colors: string[] }>('GET', '/api/board/labels');
export const setLabelColor = (label: string, color: string) =>
  call('POST', '/api/board/labels/color', { label, color });

export const addChecklistItem = (ticketId: string, text: string, checklist = '') =>
  call<CardDetail>('POST', '/api/board/checklist', { ticketId, text, checklist });
export const editChecklistItem = (id: string, change: { done?: boolean; text?: string }) =>
  call('PATCH', '/api/board/checklist', { id, ...change });
export const deleteChecklistItem = (id: string) =>
  call('DELETE', `/api/board/checklist?id=${encodeURIComponent(id)}`);
export const renameChecklist = (ticketId: string, from: string, to: string) =>
  call<CardDetail>('POST', '/api/board/checklist/rename', { ticketId, from, to });
export const deleteChecklist = (ticketId: string, checklist: string) =>
  call('DELETE', `/api/board/checklist?ticketId=${encodeURIComponent(ticketId)}&checklist=${encodeURIComponent(checklist)}`);

export const addComment = (ticketId: string, body: string) =>
  call<CardDetail>('POST', '/api/board/comments', { ticketId, body });
export const editComment = (id: string, body: string) => call('PATCH', '/api/board/comments', { id, body });
export const deleteComment = (id: string) => call('DELETE', `/api/board/comments?id=${encodeURIComponent(id)}`);

export const fetchLists = (archived = false) => call<List[]>('GET', `/api/board/lists${archived ? '?archived=1' : ''}`);
export const createList = (title: string, kind: string, after: string) =>
  call<List>('POST', '/api/board/lists', { title, kind, after });
export const editList = (id: string, change: { title?: string; kind?: string; after?: string; archived?: boolean }) =>
  call('PATCH', '/api/board/lists', { id, ...change });
export const deleteList = (id: string, moveTo: string) =>
  call('DELETE', `/api/board/lists?id=${encodeURIComponent(id)}&moveTo=${encodeURIComponent(moveTo)}`);

export const fetchBoards = () => call<BoardDef[]>('GET', '/api/board/boards');
export const createBoard = (title: string, filter: string) => call<BoardDef>('POST', '/api/board/boards', { title, filter });
export const editBoard = (id: string, change: { title?: string; filter?: string; after?: string }) =>
  call('PATCH', '/api/board/boards', { id, ...change });
export const deleteBoard = (id: string) => call('DELETE', `/api/board/boards?id=${encodeURIComponent(id)}`);

export const fetchRules = () => call<{ rules: Rule[]; actions: string[] }>('GET', '/api/board/rules');
export const createRule = (r: Partial<Rule>) => call<Rule>('POST', '/api/board/rules', r);
export const editRule = (r: Rule) => call<Rule>('PATCH', '/api/board/rules', r);
export const deleteRule = (id: string) => call('DELETE', `/api/board/rules?id=${encodeURIComponent(id)}`);
export const runRules = (id = '', backfill = false) =>
  call<{ rules: number; matched: number; created: number; attached: number; errors?: string[] }>(
    'POST', '/api/board/rules/run', { id, backfill });

export const fetchDue = (from: Date, to: Date) =>
  call<Ticket[]>('GET', `/api/board/due?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`);

/** A day as the date input wants it, in local time. */
export const toDay = (iso?: string): string => {
  if (!iso) return '';
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
};

/** A day from a date input, as the end of the working day in local time. */
export const fromDay = (day: string): string | null =>
  day ? new Date(`${day}T17:00`).toISOString() : null;
