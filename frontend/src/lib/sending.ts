import { create } from 'zustand';
import { mailChanged } from './mail';

/**
 * Sending, with Undo.
 *
 * Send does not send: the server holds the message for a few seconds (its
 * Undo window) and then delivers it. The toast counts that down, offers Undo,
 * and then says whether it went.
 */
export interface Sending {
  draftId: string;
  subject: string;
  /** When the server sends it. */
  until: number;
  state: 'waiting' | 'sent' | 'failed' | 'undone';
  error?: string;
}

interface SendingStore {
  current: Sending | null;
  set: (s: Sending | null) => void;
}

export const useSendingStore = create<SendingStore>(set => ({
  current: null,
  set: current => set({ current }),
}));

async function post(url: string, body?: unknown): Promise<any> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new Error(data?.error ?? 'could not send');
  return data;
}

/** Start the toast for a draft the server has scheduled. */
function track(draft: { id: string; subject: string }, undoMs: number) {
  const s: Sending = { draftId: draft.id, subject: draft.subject, until: Date.now() + undoMs, state: 'waiting' };
  useSendingStore.getState().set(s);
  // Ask how it went once the window has passed and the server has had a
  // moment to deliver.
  const check = async (tries: number) => {
    const cur = useSendingStore.getState().current;
    if (!cur || cur.draftId !== draft.id || cur.state !== 'waiting') return;
    const r = await fetch(`/api/drafts/${encodeURIComponent(draft.id)}`).catch(() => null);
    const d = r?.ok ? await r.json().catch(() => null) : null;
    if (d?.status === 'sent') {
      useSendingStore.getState().set({ ...cur, state: 'sent' });
      mailChanged();
    } else if (d?.status === 'failed') {
      useSendingStore.getState().set({ ...cur, state: 'failed', error: d.lastError });
      mailChanged();
    } else if (tries > 0) {
      setTimeout(() => void check(tries - 1), 1500);
    }
  };
  setTimeout(() => void check(20), undoMs + 1200);
}

/** Send a new message: create it and start its Undo window. */
export async function sendNew(fields: {
  accountId: string; to: string[]; cc: string[]; subject: string; body: string; inReplyTo?: string;
}): Promise<void> {
  const data = await post('/api/drafts', { ...fields, send: true });
  track(data.draft, data.undoMs ?? 10000);
}

/** Send a draft already saved or queued in the outbox. */
export async function sendDraft(id: string): Promise<void> {
  const data = await post(`/api/drafts/${encodeURIComponent(id)}/send`);
  track(data.draft, data.undoMs ?? 10000);
  mailChanged();
}

/** Take it back while its Undo window lasts. */
export async function undoSend(): Promise<void> {
  const cur = useSendingStore.getState().current;
  if (!cur) return;
  try {
    await post(`/api/drafts/${encodeURIComponent(cur.draftId)}/undo`);
    useSendingStore.getState().set({ ...cur, state: 'undone' });
  } catch (e) {
    useSendingStore.getState().set({ ...cur, state: 'failed', error: e instanceof Error ? e.message : String(e) });
  }
  mailChanged();
}
