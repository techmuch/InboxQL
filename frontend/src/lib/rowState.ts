import { useViewerStore, type ViewerKind } from './tabs';

/**
 * How a result row shows what it is.
 *
 * # Three states, not two
 *
 * A row used to have two: ticked for a bulk action, or not. That is `selection`
 * below, and it is a set — many rows at once, surviving anything.
 *
 * Browser focus is the second, and it is not stored anywhere: exactly one
 * element in the document has it, and it is lost the moment the user clicks
 * into another pane. That made it useless as a record of *what is on screen* —
 * arrow to a message, click into the viewer to read it, and the list no longer
 * showed which row you were looking at.
 *
 * `current` is the third: the row whose subject the viewer is showing. It
 * outlives focus, because it is a fact about the viewer rather than about the
 * keyboard.
 */
export interface RowState {
  /** Ticked for a bulk action. */
  selected: boolean;
  /** The subject the viewer is showing. */
  current: boolean;
}

/**
 * Whether this row is the subject the viewer is showing.
 *
 * # Why this is derived rather than stored
 *
 * `useViewerStore` already knows: it holds the message, the contact and the
 * file it is displaying. A parallel `currentRowId` would be a second answer to
 * a question that already has one, and the two would diverge the first time
 * something set the viewer without going through a row — a link out of a
 * ticket, a `iql ui open` command from the CLI.
 *
 * # Why the slots are per-kind, and why that matters here
 *
 * One global "current row" would un-highlight a file the moment a message
 * inside it was previewed — while the file is still on screen in its own tab.
 * The store's separate slots are what let a file stay outlined in the Desk list
 * while its messages are arrowed through in the File tab, which is the whole
 * point of the cascade.
 */
export const useIsCurrent = (kind: ViewerKind, id: string | null | undefined): boolean =>
  useViewerStore(s => {
    if (!id) return false;
    switch (kind) {
      case 'message':
        return s.messageId === id;
      case 'contact':
        return s.contact === id;
      case 'file':
        // Files are identified by their content address, so the same bytes
        // under two names are one row and one highlight.
        return s.file?.key === id;
    }
  });

/**
 * The background and outline for a row in a given state.
 *
 * # Why one function rather than appended class strings
 *
 * Selected and current both want a ring, and `ring-1 ring-2` in one attribute
 * does not resolve by the order they are written — Tailwind emits both rules
 * and the later one in the stylesheet wins, whichever way round the caller put
 * them. Computing the ring exactly once is the only way to be sure which is
 * drawn.
 *
 * # Why current has no fill
 *
 * The two states co-occur: a row can be ticked for a bulk action *and* be the
 * one on screen. If both drew a fill, a selection of exactly one row would be
 * indistinguishable from a preview, and the user would read "I have selected
 * this" as "this is what I am looking at". So selection fills, current
 * outlines, and a row that is both does both.
 */
export function rowStateClasses({ selected, current }: RowState): string {
  if (current && selected) {
    return 'bg-primary/15 dark:bg-primary/25 ring-2 ring-inset ring-primary';
  }
  if (current) {
    return 'ring-2 ring-inset ring-primary hover:bg-accent/40';
  }
  if (selected) {
    return 'bg-primary/10 hover:bg-primary/15 dark:bg-primary/20 dark:hover:bg-primary/25 ring-1 ring-inset ring-primary/30';
  }
  return 'hover:bg-accent/40';
}

/**
 * `aria-current` for a row, or undefined when it is not the one.
 *
 * Undefined rather than `"false"`, because `aria-current="false"` is a
 * meaningful value a screen reader will announce, and fifty rows announcing
 * that they are not the current one is worse than silence.
 */
export const ariaCurrent = (current: boolean): 'true' | undefined => (current ? 'true' : undefined);
