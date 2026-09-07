import { create } from 'zustand';

/**
 * The one query every surface shares.
 *
 * # Why there is only one
 *
 * There used to be two channels: a query string in Desk's bar, and a separate
 * list of cross-filter terms the dashboard wrote into. What ran was the two
 * concatenated, so clicking a top sender changed the results without appearing
 * in the bar — the bar showed `folder:inbox` while the server was being asked
 * `folder:inbox from:"alice@acme.com"`.
 *
 * That is the same hidden-filter-struct that three separate implementations of
 * in Go, and it came back one layer up. One string, read and written by every
 * surface, is the only shape where what is shown and what runs cannot disagree.
 *
 * # Composition is not done here
 *
 * Adding and removing terms goes through the server, because it needs the
 * lexer: a quoted value containing a space is one term, and every frontend
 * attempt at that has split on whitespace and been wrong.
 */
interface QueryState {
  /** The query, exactly as it will be sent. */
  text: string;
  set: (query: string) => void;
  clear: () => void;
}

export const useQueryStore = create<QueryState>((set) => ({
  text: 'folder:inbox',
  set: (text) => set({ text }),
  clear: () => set({ text: '' }),
}));

export interface QueryTerm {
  text: string;
  field?: string;
  negated: boolean;
  start: number;
  end: number;
}

/** Split a query into its top-level terms, for rendering pills. */
export async function queryTerms(q: string): Promise<QueryTerm[]> {
  if (!q.trim()) return [];
  const res = await fetch(`/api/query/terms?${new URLSearchParams({ q })}`);
  if (!res.ok) return [];
  return (await res.json()).terms ?? [];
}

export interface QueryStage {
  verb: string;
  text: string;
  start: number;
  end: number;
}

/** The stages of a query's pipeline, for showing which view is active. */
export async function queryStages(q: string): Promise<QueryStage[]> {
  if (!q.trim()) return [];
  const res = await fetch(`/api/query/terms?${new URLSearchParams({ q })}`);
  if (!res.ok) return [];
  return (await res.json()).stages ?? [];
}

type Compose =
  | { add: string }
  | { remove: string }
  | { field: string; term: string }
  /** Narrow to a hand-picked set: one term naming several identities. */
  | { field: string; values: string[]; negated?: boolean }
  /** Add a pipeline stage. A terminal one replaces whichever terminal is on. */
  | { stage: string }
  /** Remove every stage with this verb. */
  | { dropStage: string }
  /** Replace the term at an offset, assembling it from its parts server-side. */
  | { at: number; field: string; value: string; negated: boolean }
  /** Replace the term at an offset with a literal term, or remove it if empty. */
  | { at: number; term: string };

/**
 * Add, remove or replace a term, server-side.
 *
 * A click is about to re-run the query anyway, so the round trip costs nothing
 * and it buys one implementation of the grammar instead of two.
 */
export async function compose(q: string, op: Compose): Promise<string> {
  const params = new URLSearchParams({ q });
  if ('at' in op) {
    params.set('at', String(op.at));
    if ('value' in op) {
      // The parts go over raw; the server decides the quoting, because that is
      // grammar and every frontend attempt at it has been wrong.
      params.set('field', op.field);
      params.set('value', op.value);
      params.set('negated', String(op.negated));
    } else {
      params.set('term', op.term);
    }
  } else if ('values' in op) {
    // Repeated params, and the server assembles the term. The parens, the OR
    // and the fact that one value needs neither are grammar.
    params.set('field', op.field);
    for (const v of op.values) params.append('values', v);
    if (op.negated) params.set('negated', 'true');
  } else if ('add' in op) params.set('add', op.add);
  else if ('remove' in op) params.set('remove', op.remove);
  else if ('stage' in op) params.set('stage', op.stage);
  else if ('dropStage' in op) params.set('dropStage', op.dropStage);
  else {
    params.set('field', op.field);
    params.set('term', op.term);
  }

  const res = await fetch(`/api/query/compose?${params}`);
  if (!res.ok) return q;
  return (await res.json()).query ?? q;
}

/** Quote a value so a chart label containing spaces or a colon stays one term. */
export const asTerm = (field: string, value: string): string =>
  `${field}:"${String(value).replace(/"/g, '""')}"`;
