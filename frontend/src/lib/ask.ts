import { create } from 'zustand';
import { goTo } from './places';

/**
 * Ask in words: a question answered by Claude Code on this computer, with
 * read-only iql commands. See internal/ask.
 */
export interface AskCommand { command: string; refused?: boolean }
export interface AskAnswer {
  question: string; text: string; commands: AskCommand[]; session?: string;
  seconds: number; costUsd?: number; cli: string; error?: string;
}
export interface AskStatus { cli: string; enabled: boolean; available: { claude: string }; sends: string }

interface AskState {
  /** The conversation so far, oldest first. */
  turns: (AskAnswer & { pending?: boolean })[];
  ask: (question: string) => Promise<void>;
  reset: () => void;
}

export const useAsk = create<AskState>((set, get) => ({
  turns: [],
  reset: () => set({ turns: [] }),
  ask: async (question) => {
    const q = question.trim();
    if (!q) return;
    const last = get().turns.at(-1);
    const session = last && !last.error ? last.session : undefined;
    set(s => ({ turns: [...s.turns, { question: q, text: '', commands: [], seconds: 0, cli: 'Claude Code', pending: true }] }));
    let a: AskAnswer;
    try {
      const r = await fetch('/api/ask', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ question: q, session }) });
      const body = await r.json().catch(() => ({}));
      a = r.ok ? body : { question: q, text: '', commands: [], seconds: 0, cli: 'Claude Code', error: body.error ?? `the server answered ${r.status}` };
    } catch (e) {
      a = { question: q, text: '', commands: [], seconds: 0, cli: 'Claude Code', error: (e as Error).message };
    }
    set(s => ({ turns: [...s.turns.slice(0, -1), a] }));
  },
}));

/** Ask from anywhere: go to the Ask page and start the question. */
export const askInWords = (question: string) => {
  useAsk.getState().reset();
  goTo('ask');
  void useAsk.getState().ask(question);
};

/**
 * Whether the words typed in the query bar read as a question rather than a
 * query: a question mark, or a question word first, and no field:value terms.
 */
export function looksLikeQuestion(text: string): boolean {
  const t = text.trim();
  if (!t || /\b[a-z]+:\S/i.test(t) || t.includes('|')) return false;
  if (t.endsWith('?')) return true;
  return /^(how|what|who|whom|when|where|which|why|did|do|does|is|are|was|were|can|could|should|have|has|show me|tell me|list|find|summari[sz]e)\b/i.test(t) && t.split(/\s+/).length >= 3;
}

export async function askStatus(): Promise<AskStatus | null> {
  try { const r = await fetch('/api/ask'); return r.ok ? await r.json() : null; } catch { return null; }
}

export async function setAskCLI(cli: '' | 'claude'): Promise<AskStatus> {
  const r = await fetch('/api/ask', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ cli }) });
  const body = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(body.error ?? 'could not change it');
  return body;
}
