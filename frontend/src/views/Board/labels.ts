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

export const labelClass = (color?: string): string =>
  (color && LABEL_CLASSES[color]) || 'bg-muted text-foreground';

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
