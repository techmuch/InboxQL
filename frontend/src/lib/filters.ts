import { create } from 'zustand';

/**
 * The dashboard's cross-filters, as query terms.
 *
 * These used to be three named fields — date, from, topic — so a fourth kind of
 * filter needed a field here, a parameter on the API, and a branch in three
 * separate SQL builders. They are terms in one language now, so the store holds
 * strings and the server is sent an expression.
 *
 * Lifted out of App.tsx because two surfaces read it: the dashboard writes the
 * terms, and Desk composes them with whatever query the rail selected.
 */
interface FilterState {
  terms: string[];
  /** Add a term, or remove it if it is already applied. */
  toggle: (term: string) => void;
  remove: (term: string) => void;
  clearAll: () => void;
}

/**
 * The field a term constrains, for the one-per-field rule.
 *
 * Split on the first colon, after any leading dash: `-from:x` constrains
 * `from`, not `-from`. Getting that wrong meant a negated term never replaced
 * its positive twin.
 */
const fieldOf = (term: string): string => {
  const bare = term.startsWith('-') ? term.slice(1) : term;
  const colon = bare.indexOf(':');
  return colon < 0 ? bare : bare.slice(0, colon);
};

export const useFilterStore = create<FilterState>((set) => ({
  terms: [],
  toggle: (term) => set((state) => ({
    terms: state.terms.includes(term)
      ? state.terms.filter(t => t !== term)
      : [...state.terms.filter(t => fieldOf(t) !== fieldOf(term)), term],
  })),
  remove: (term) => set((state) => ({ terms: state.terms.filter(t => t !== term) })),
  clearAll: () => set({ terms: [] }),
}));

/** Quote a value so a chart label containing spaces or a colon stays one term. */
export const asTerm = (field: string, value: string): string =>
  `${field}:"${String(value).replace(/"/g, '""')}"`;
