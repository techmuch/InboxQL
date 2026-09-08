import { create } from 'zustand';

/**
 * What kind of thing a selected row stands for.
 *
 * Selection used to be a Set of message ids, which worked only because the
 * only navigable list was messages. A conversation is not a message and an
 * aggregate bucket is neither, so the set has to say what it holds — otherwise
 * every action downstream has to guess, and "archive this bucket" is a
 * question with no answer.
 */
export type SelectionKind = 'message' | 'thread' | 'ticket' | 'draft' | 'group' | 'contact';

export interface SelectionRef {
  kind: SelectionKind;
  id: string;
  /** What to call it in the toolbar. Falls back to the id. */
  label?: string;
  /**
   * The query field this kind is named by, so a selection can be turned back
   * into a query. Absent for kinds that cannot be — an aggregate bucket
   * carries its own field, which is why it is stored rather than derived.
   */
  field?: string;
}

/** The key a ref is stored under: unique per kind, so ids may collide safely. */
export const refKey = (r: Pick<SelectionRef, 'kind' | 'id'>) => `${r.kind}:${r.id}`;

interface SelectionState {
  refs: Record<string, SelectionRef>;
  /** Replace the selection with exactly this row. */
  only: (ref: SelectionRef) => void;
  /** Add or remove one row, leaving the rest alone. */
  toggle: (ref: SelectionRef) => void;
  /** Add a row without removing anything, for Shift-extend. */
  add: (ref: SelectionRef) => void;
  replaceAll: (refs: SelectionRef[]) => void;
  clear: () => void;
}

/**
 * The selection, shared by every mode Desk can be in.
 *
 * Outside Desk rather than inside it because the toolbar, the rows and the
 * keyboard handler all need it, and threading it through five renderers is how
 * the last piece of shared state in this app ended up with three copies that
 * disagreed.
 */
export const useSelectionStore = create<SelectionState>((set) => ({
  refs: {},
  only: (ref) => set({ refs: { [refKey(ref)]: ref } }),
  toggle: (ref) => set((s) => {
    const key = refKey(ref);
    if (s.refs[key]) {
      const { [key]: _removed, ...rest } = s.refs;
      return { refs: rest };
    }
    return { refs: { ...s.refs, [key]: ref } };
  }),
  add: (ref) => set((s) => ({ refs: { ...s.refs, [refKey(ref)]: ref } })),
  replaceAll: (refs) => set({
    refs: Object.fromEntries(refs.map(r => [refKey(r), r])),
  }),
  clear: () => set({ refs: {} }),
}));

/** The selection as a list, in insertion order. */
export const selectedRefs = (refs: Record<string, SelectionRef>): SelectionRef[] =>
  Object.values(refs);

/**
 * Read a row's identity off its DOM attributes.
 *
 * Rows declare what they are — `data-sel-kind`, `data-sel-id` — and both the
 * click handlers and the keyboard handler read it the same way. That is what
 * keeps selection working in every mode without a branch per mode: the
 * handlers never learn what a row is, they ask it.
 */
export function refOf(el: Element | null | undefined): SelectionRef | null {
  const row = (el as HTMLElement | null)?.closest?.('[data-sel-id]') as HTMLElement | null;
  if (!row) return null;
  const kind = row.dataset.selKind as SelectionKind | undefined;
  const id = row.dataset.selId;
  if (!kind || !id) return null;
  return { kind, id, label: row.dataset.selLabel, field: row.dataset.selField };
}

/** The plural noun for a kind, for the toolbar. */
export function nounFor(kind: SelectionKind, n: number): string {
  const nouns: Record<SelectionKind, [string, string]> = {
    message: ['message', 'messages'],
    thread: ['conversation', 'conversations'],
    ticket: ['ticket', 'tickets'],
    draft: ['draft', 'drafts'],
    group: ['group', 'groups'],
    contact: ['contact', 'contacts'],
  };
  const [one, many] = nouns[kind];
  return `${n} ${n === 1 ? one : many}`;
}

/**
 * Describe a mixed selection, grouped by kind.
 *
 * Mixed selections are possible — a timeline holds conversations and the
 * messages inside them — so the toolbar counts each kind rather than
 * flattening them into one number that describes nothing.
 */
export function describeSelection(refs: SelectionRef[]): string {
  const counts = new Map<SelectionKind, number>();
  for (const r of refs) counts.set(r.kind, (counts.get(r.kind) ?? 0) + 1);
  return [...counts.entries()].map(([kind, n]) => nounFor(kind, n)).join(', ');
}

/**
 * The field and values a selection narrows by, or null when it cannot.
 *
 * A selection can only become a query if everything in it is named by the same
 * field. A mixed selection of conversations and tickets is two queries, and
 * pretending otherwise would produce one that matches neither.
 */
export function narrowable(refs: SelectionRef[]): { field: string; values: string[] } | null {
  if (refs.length === 0) return null;
  const fields = new Set(refs.map(r => r.field).filter(Boolean));
  if (fields.size !== 1) return null;
  const field = [...fields][0]!;
  const values = refs.filter(r => r.field === field).map(r => r.id);
  if (values.length !== refs.length) return null;
  return { field, values };
}
