import { create } from 'zustand';
import type { MailtoDraft } from './emailLinks';

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
  /** What New message starts with, from a mailto link; empty otherwise. */
  composeDraft: MailtoDraft | null;
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
/** New message, filled in from a mailto link. */
export const openComposeWith = (draft: MailtoDraft) => useAppUi.setState({ compose: true, composeDraft: draft });
export const openNewTask = () => useAppUi.getState().set('task', true);
export const openPalette = () => useAppUi.getState().set('palette', true);
export const openKeys = () => useAppUi.getState().set('keys', true);
