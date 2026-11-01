import { create } from 'zustand';
import type { ComposeStart } from './replies';

/**
 * The dialogs any part of the application can open: compose, new task,
 * Find anything, and the keyboard page. Held here so a menu item, a key
 * sequence and a button in a tab all open the same one.
 */
interface AppUi {
  compose: boolean;
  task: boolean;
  palette: boolean;
  keys: boolean;
  /** What New message starts with — a mailto link, a reply, a forward — or empty. */
  composeDraft: ComposeStart | null;
  set: (k: 'compose' | 'task' | 'palette' | 'keys', open: boolean) => void;
}

export const useAppUi = create<AppUi>((set) => ({
  compose: false,
  task: false,
  palette: false,
  keys: false,
  composeDraft: null,
  set: (k, open) => set({ [k]: open, ...(k === 'compose' ? { composeDraft: null } : {}) } as Partial<AppUi>),
}));

export const openCompose = () => useAppUi.getState().set('compose', true);
/** New message, filled in: from a mailto link, or as a reply or forward. */
export const openComposeWith = (draft: ComposeStart) => useAppUi.setState({ compose: true, composeDraft: draft });
export const openNewTask = () => useAppUi.getState().set('task', true);
export const openPalette = () => useAppUi.getState().set('palette', true);
export const openKeys = () => useAppUi.getState().set('keys', true);
