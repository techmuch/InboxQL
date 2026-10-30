import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * What the Desk is showing, as far as going back to it is concerned.
 *
 * The query alone is not enough: "Inbox grouped by sender" and "Inbox" are the
 * same query, and the grouped one is the view you choose the next thing from.
 */
export interface View {
  query: string;
  groupBy: string | null;
  allItems: boolean;
  allItemsQuery: string;
}

export interface Visited extends View {
  /** How it read when you left it, for the Back button's name. */
  label: string;
  /** How far down the list you were. */
  scroll: number;
}

const same = (a: View, b: View) =>
  a.query === b.query && a.groupBy === b.groupBy && a.allItems === b.allItems && a.allItemsQuery === b.allItemsQuery;

/** Enough to get back to where somebody started an afternoon's sorting. */
const LIMIT = 50;

/**
 * The views you have been in, newest last, and a way back to each.
 *
 * # Why not the browser's history
 *
 * The Desk is one tab among several in a page that never navigates, and the
 * browser's Back leaves the application. Pushing entries into it would make
 * the browser's button sometimes mean "previous query" and sometimes "leave",
 * depending on which tab was last used — so this is the Desk's own, with its
 * own button and keys.
 *
 * # What counts as a step
 *
 * Every change of view, however it was made: the rail, a filter, a typed
 * query, a group's Show. Going back is not a step, or Back twice would return
 * to where you started. Repeats are not stored twice.
 */
export function useViewHistory(
  current: View,
  describe: () => string,
  scrollOf: () => number,
  restore: (v: Visited) => void,
) {
  const stack = useRef<Visited[]>([]);
  const [size, setSize] = useState(0);
  // The view as it was last seen, kept up to date while it is the current one.
  const shown = useRef<Visited | null>(null);
  // A view being returned to, so arriving at it is not recorded as a step.
  const returning = useRef<View | null>(null);

  useEffect(() => {
    const before = shown.current;
    shown.current = { ...current, label: describe(), scroll: 0 };
    if (!before || same(before, current)) return;
    if (returning.current && same(returning.current, current)) {
      returning.current = null;
      return;
    }
    returning.current = null;
    const top = stack.current[stack.current.length - 1];
    if (!top || !same(top, before)) {
      stack.current = [...stack.current, { ...before, scroll: scrollOf() }].slice(-LIMIT);
      setSize(stack.current.length);
    }
    // Only a change of view is a step.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current.query, current.groupBy, current.allItems, current.allItemsQuery]);

  // The name settles once the new query's terms arrive, and the scroll
  // position moves as you read, so both follow the view while it is current.
  useEffect(() => {
    if (shown.current && same(shown.current, current)) shown.current.label = describe();
  });

  const back = useCallback(() => {
    const to = stack.current[stack.current.length - 1];
    if (!to) return;
    stack.current = stack.current.slice(0, -1);
    setSize(stack.current.length);
    if (shown.current && same(shown.current, to)) return;
    returning.current = to;
    restore(to);
  }, [restore]);

  return {
    back,
    /** Where Back goes, or null when there is nowhere. */
    previous: size > 0 ? stack.current[stack.current.length - 1] : null,
  };
}
