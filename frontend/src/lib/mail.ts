import { useViewerStore } from './tabs';

/**
 * Changing messages: archive, trash, labels, read, starred.
 *
 * Every change is made in InboxQL at once and queued for the account's
 * server, which the server's sync worker applies within seconds. The Desk,
 * the viewer and anything else showing mail listen for MAIL_CHANGED to
 * refresh, which is also what the server sends when new mail arrives.
 */

/** The system labels, in the order Gmail shows them. */
export const SYSTEM_LABELS = ['inbox', 'sent', 'drafts', 'important', 'spam', 'trash'] as const;

export const isSystemLabel = (name: string) =>
  (SYSTEM_LABELS as readonly string[]).includes(name.toLowerCase());

/**
 * The labels worth showing as chips: the user's own. Important is left off —
 * Gmail marks so much mail important that a chip on every other row is noise;
 * the rail's Important row is where it is useful.
 */
export const shownLabels = (labels?: string[]) => (labels ?? []).filter(l => !isSystemLabel(l));

/** `label:x`, quoted when the name needs it. */
export const labelQuery = (name: string) =>
  /^[\w./-]+$/.test(name) ? `label:${name}` : `label:"${name.replace(/"/g, '""')}"`;

export type Move = 'archive' | 'inbox' | 'trash' | 'untrash' | 'spam' | 'notspam';

export interface LabelCount {
  name: string;
  system: boolean;
  total: number;
  unread: number;
}

/** Fired after mail changes, here or on the server. */
export const MAIL_CHANGED = 'iql:mail-changed';

/**
 * Who changed it. `local` is a change made in this window, which has to show
 * at once; `server` is the sync worker reporting new mail or another device,
 * which can arrive every few seconds during a first sync.
 */
export type MailChangeSource = 'local' | 'server';

export const mailChanged = (source: MailChangeSource = 'local') =>
  window.dispatchEvent(new CustomEvent(MAIL_CHANGED, { detail: { source } }));

/** Run f whenever mail changes; returns the unsubscribe. */
export const onMailChanged = (f: (source: MailChangeSource) => void) => {
  const h = (e: Event) => f((e as CustomEvent).detail?.source ?? 'local');
  window.addEventListener(MAIL_CHANGED, h);
  return () => window.removeEventListener(MAIL_CHANGED, h);
};

async function post(url: string, body: unknown): Promise<any> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new Error(data?.error ?? `${url} failed`);
  return data;
}

/** The labels a move leaves on a message, so the viewer updates at once. */
const afterMove = (labels: string[], to: Move): string[] => {
  const set = new Set(labels);
  const add = (l: string) => set.add(l);
  const del = (l: string) => set.delete(l);
  switch (to) {
    case 'archive': del('inbox'); break;
    case 'inbox': add('inbox'); del('trash'); del('spam'); break;
    case 'trash': add('trash'); del('inbox'); del('spam'); break;
    case 'spam': add('spam'); del('inbox'); del('trash'); break;
    case 'untrash': add('inbox'); del('trash'); break;
    case 'notspam': add('inbox'); del('spam'); break;
  }
  return [...set];
};

/** Patch the message the viewer is showing, if it is one of these. */
const patchViewer = (ids: string[], f: (m: any) => any) => {
  const { message } = useViewerStore.getState();
  if (message && ids.includes(message.id)) {
    useViewerStore.setState({ message: f(message) });
  }
};

export async function moveMessages(ids: string[], to: Move): Promise<number> {
  const body = await post('/api/messages/move', { ids, to });
  patchViewer(ids, m => ({ ...m, labels: afterMove(m.labels ?? [], to) }));
  mailChanged();
  return body?.changed ?? 0;
}

export async function changeLabels(ids: string[], add: string[], remove: string[]): Promise<number> {
  const body = await post('/api/messages/labels', { ids, add, remove });
  patchViewer(ids, m => {
    const set = new Set<string>(m.labels ?? []);
    add.forEach(l => set.add(l));
    remove.forEach(l => {
      for (const x of set) if (x.toLowerCase() === l.toLowerCase()) set.delete(x);
    });
    return { ...m, labels: [...set] };
  });
  mailChanged();
  return body?.changed ?? 0;
}

export async function setFlag(ids: string[], flag: '\\Seen' | '\\Flagged', on: boolean): Promise<number> {
  const body = await post('/api/messages/flags', { ids, flag, on });
  patchViewer(ids, m => {
    const flags = (m.flags ?? []).filter((f: string) => f !== flag);
    return { ...m, flags: on ? [...flags, flag] : flags };
  });
  mailChanged();
  return body?.changed ?? 0;
}

export async function listLabels(): Promise<LabelCount[]> {
  const res = await fetch('/api/labels');
  if (!res.ok) return [];
  const body = await res.json().catch(() => null);
  return body?.labels ?? [];
}

export async function createLabel(name: string): Promise<void> {
  await post('/api/labels', { name });
  mailChanged();
}

export async function deleteLabel(name: string): Promise<void> {
  const res = await fetch(`/api/labels/${encodeURIComponent(name)}`, { method: 'DELETE' });
  const body = await res.json().catch(() => null);
  if (!res.ok) throw new Error(body?.error ?? 'could not delete the label');
  mailChanged();
}
