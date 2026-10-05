import { create } from 'zustand';

/**
 * Whether conversations in a threaded list start open or closed.
 *
 * # A default, not a state
 *
 * This decides what a conversation looks like before you touch it. Opening or
 * closing one by hand is recorded on the row and outranks this, so changing the
 * setting moves every row you have not touched and none that you have.
 *
 * # Why collapsed is the default
 *
 * It is what the list did before there was a choice, and on a real mailbox it
 * is the better one: 78% of 25,006 conversations were a single message, whose
 * sender and subject are already in the collapsed header. Expanding those adds
 * a row each and tells you nothing new.
 *
 * # Why it lives in the browser
 *
 * Beside the threading and viewer-tab preferences, which are the same kind of
 * thing: how this machine's workspace looks, not what the mailbox is.
 */
export type ThreadExpansion = 'collapsed' | 'expanded';

const EXPANSION_KEY = 'inboxql.threadExpansion';

function readExpansion(): ThreadExpansion {
  try {
    return localStorage.getItem(EXPANSION_KEY) === 'expanded' ? 'expanded' : 'collapsed';
  } catch {
    // A browser with storage blocked still gets a working app on the default.
    return 'collapsed';
  }
}

interface ThreadExpansionState {
  expansion: ThreadExpansion;
  setExpansion: (e: ThreadExpansion) => void;
}

export const useThreadExpansionStore = create<ThreadExpansionState>((set, get) => ({
  expansion: readExpansion(),
  setExpansion: (expansion) => {
    if (get().expansion === expansion) return;
    try {
      localStorage.setItem(EXPANSION_KEY, expansion);
    } catch {
      // An unwritable store costs the preference its persistence, not the
      // switch its effect.
    }
    set({ expansion });
  },
}));

/**
 * How many entries an open conversation renders before asking.
 *
 * Applied to every open conversation, not only ones the preference opened. The
 * largest thread on a real mailbox held 1,088 messages, and clicking its
 * chevron rendered all of them at once — a stall that looks like a fault. With
 * "expanded" as a default the same stall would happen on page load.
 */
export const ENTRY_CAP = 25;

/**
 * Whether a conversation is open, from the three things that decide it.
 *
 * In order: what the user did to this row, then whether it is the only
 * conversation on screen, then the preference. Kept as a function rather than
 * inline in the row so the precedence is stated once and testable.
 *
 * A single conversation opens whatever the preference says. That was never a
 * matter of taste — it is the thing the query asked to read, and a page holding
 * one closed row is a dead end.
 */
export function isOpen(override: boolean | null, alone: boolean, expansion: ThreadExpansion): boolean {
  if (override !== null) return override;
  if (alone) return true;
  return expansion === 'expanded';
}
