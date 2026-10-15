import { create } from 'zustand';

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
  set: (k: 'compose' | 'task' | 'palette' | 'keys', open: boolean) => void;
}

export const useAppUi = create<AppUi>((set) => ({
  compose: false,
  task: false,
  palette: false,
  keys: false,
  set: (k, open) => set({ [k]: open } as Partial<AppUi>),
}));

export const openCompose = () => useAppUi.getState().set('compose', true);
export const openNewTask = () => useAppUi.getState().set('task', true);
export const openPalette = () => useAppUi.getState().set('palette', true);
export const openKeys = () => useAppUi.getState().set('keys', true);
