import { create } from 'zustand';

/**
 * Places: what the middle of the window is showing.
 *
 * # Why places replaced tabs
 *
 * The workspace used to be a docking layout of tabs. Every surface was a tab,
 * opening anything meant finding or adding one, and the person decided where
 * everything went. The redesign has one screen at a time in a fixed frame —
 * the places on the left, the screen, the evidence on the right — so "open
 * the calendar" now means *go to* the calendar, and Back comes back.
 *
 * A place is in the address (`#/calendar`), so Back and Forward, reloading
 * and a bookmark all work, and `iql ui list` can say what a window shows in
 * the same words `iql ui open` takes.
 *
 * Everything that used to open a tab calls `openTool(id)`, which now lands
 * here. The ids are the old component ids, so nothing that names one had to
 * change: `desk`, `calendar`, `viewer`, `settings`.
 */

/** Old ids that mean the same place. */
const ALIASES: Record<string, string> = {
  mail: 'desk',
  query: 'desk',
  search: 'desk',
  message: 'viewer',
  'viewer-contact': 'viewer',
  'viewer-file': 'viewer',
  maintenance: 'settings',
  docs: 'help',
  documentation: 'help',
};

export const HOME_PLACE = 'today';

export const normalisePlace = (id: string): string => ALIASES[id] ?? id;

export interface PlaceState {
  /** The place on screen. */
  id: string;
  /** Places visited in this window, newest last, for the palette and for `iql ui list`. */
  recent: string[];
  go: (id: string) => void;
}

const readHash = (): string | null => {
  try {
    const m = /^#\/([a-z0-9-]+)/i.exec(window.location.hash);
    return m ? normalisePlace(m[1]) : null;
  } catch {
    return null;
  }
};

export const usePlaceStore = create<PlaceState>((set, get) => ({
  id: readHash() ?? HOME_PLACE,
  recent: [],
  go: (raw) => {
    const id = normalisePlace(raw);
    if (get().id === id) return;
    set(s => ({ id, recent: [...s.recent.filter(r => r !== id), id].slice(-12) }));
    try {
      if (readHash() !== id) window.history.pushState(null, '', `#/${id}`);
    } catch {
      // An address that cannot change (a sandboxed frame) costs Back, not the
      // navigation itself.
    }
  },
}));

/** Go to a place. Going to the place you are on does nothing. */
export const goTo = (id: string) => usePlaceStore.getState().go(id);

let listening = false;

/**
 * Follow Back and Forward.
 *
 * The browser's history is the history: a place pushes an entry when you go
 * there, and this follows the address when the browser moves through them.
 */
export function followAddress(): () => void {
  if (listening) return () => {};
  listening = true;
  const onPop = () => {
    const id = readHash() ?? HOME_PLACE;
    if (usePlaceStore.getState().id !== id) usePlaceStore.setState(s => ({ id, recent: [...s.recent.filter(r => r !== id), id].slice(-12) }));
  };
  window.addEventListener('popstate', onPop);
  window.addEventListener('hashchange', onPop);
  // The first visit has no address yet; give it one without adding a step.
  try {
    if (!readHash()) window.history.replaceState(null, '', `#/${usePlaceStore.getState().id}`);
  } catch { /* see go() */ }
  return () => {
    listening = false;
    window.removeEventListener('popstate', onPop);
    window.removeEventListener('hashchange', onPop);
  };
}
