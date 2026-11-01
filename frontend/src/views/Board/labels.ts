import { create } from 'zustand';
import { openTool } from '../../lib/tabs';

/**
 * Label colours, as Trello has them. Written out in full so Tailwind finds
 * every class; a name it does not know falls back to the neutral chip.
 */
export const LABEL_CLASSES: Record<string, string> = {
  green: 'bg-green-600 text-white',
  yellow: 'bg-yellow-400 text-black',
  orange: 'bg-orange-500 text-white',
  red: 'bg-red-600 text-white',
  purple: 'bg-purple-600 text-white',
  blue: 'bg-blue-600 text-white',
  sky: 'bg-sky-500 text-white',
  lime: 'bg-lime-500 text-black',
  pink: 'bg-pink-500 text-white',
  black: 'bg-neutral-800 text-white',
};

/**
 * The same colours as a card wears them: a tint with a bar of the full
 * colour at its edge, and text dark enough to read on the tint.
 *
 * The solid fills above are right for a swatch you pick from and too loud on
 * a board, where every card carries one or two and the board turns into a
 * wall of saturated blocks that the titles compete with. A tint keeps the
 * colour findable without making it the loudest thing on the card, and the
 * text clears 4.5:1 in both themes because it is the colour's dark (or, in
 * the dark theme, light) end rather than white on a mid-tone.
 */
export const LABEL_CHIPS: Record<string, string> = {
  green: 'bg-green-600/15 text-green-800 dark:text-green-300 border-l-2 border-green-600',
  yellow: 'bg-yellow-400/25 text-yellow-900 dark:text-yellow-200 border-l-2 border-yellow-500',
  orange: 'bg-orange-500/15 text-orange-800 dark:text-orange-300 border-l-2 border-orange-500',
  red: 'bg-red-600/15 text-red-800 dark:text-red-300 border-l-2 border-red-600',
  purple: 'bg-purple-600/15 text-purple-800 dark:text-purple-300 border-l-2 border-purple-600',
  blue: 'bg-blue-600/15 text-blue-800 dark:text-blue-300 border-l-2 border-blue-600',
  sky: 'bg-sky-500/15 text-sky-800 dark:text-sky-300 border-l-2 border-sky-500',
  lime: 'bg-lime-500/20 text-lime-900 dark:text-lime-300 border-l-2 border-lime-500',
  pink: 'bg-pink-500/15 text-pink-800 dark:text-pink-300 border-l-2 border-pink-500',
  black: 'bg-neutral-800/10 text-neutral-900 dark:bg-neutral-400/15 dark:text-neutral-200 border-l-2 border-neutral-800 dark:border-neutral-400',
};

export const labelClass = (color?: string): string =>
  (color && LABEL_CHIPS[color]) || 'bg-muted text-foreground';

/**
 * Which card is open, so somewhere other than the board — a due date on the
 * calendar — can open one: it sets the card, and the board opens it.
 */
interface BoardNav {
  openCard: string | null;
  setOpenCard: (id: string | null) => void;
}

export const useBoardNav = create<BoardNav>(set => ({
  openCard: null,
  setOpenCard: id => set({ openCard: id }),
}));

/** Show the board with one card open. */
export const openCardOnBoard = (id: string): void => {
  useBoardNav.getState().setOpenCard(id);
  openTool('board', 'Board');
};
