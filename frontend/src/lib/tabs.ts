import { useLayoutStore } from 'nexus-shell';
import { create } from 'zustand';

/**
 * Opening workbench tabs from anywhere.
 *
 * This used to live inside App as a closure, which meant only App could open a
 * tab. Components deeper in the tree — the mail list wanting to show a message,
 * the importer wanting to show its errors — had no way to reach it.
 */

/**
 * Open a tab, or focus it if it is already open.
 *
 * Focusing rather than duplicating is the whole point: clicking ten messages
 * should reuse one viewer, not leave ten identical tabs behind.
 */
export const openTool = (id: string, label: string): void => {
  const layout = useLayoutStore.getState();
  const model = layout.model;
  if (!model) return;

  let existing: string | null = null;
  model.visitNodes((node: any) => {
    if (node.getType() === 'tab' && node.getComponent() === id) {
      existing = node.getId();
    }
  });

  if (!existing) {
    layout.addTab(id, label);
    return;
  }

  // FlexLayout's Actions are not exported through nexus-shell, so the action
  // is dispatched by its wire format. The payload key is `tabNode`, not
  // `tabId` — with the wrong key the model looks up `undefined`, finds
  // nothing, and returns normally, so the tab quietly never came forward and
  // no error was raised to say so. Hence the check afterwards rather than a
  // bare try/catch: silence is the failure mode here, not an exception.
  try {
    model.doAction({ type: 'FlexLayout_SelectTab', data: { tabNode: existing } } as any);
  } catch (e) {
    console.error('[InboxQL] could not focus the existing tab', id, e);
    return;
  }

  const node: any = model.getNodeById(existing);
  const parent = node?.getParent?.();
  const selected = parent?.getChildren?.()[parent.getSelected?.()];
  if (selected?.getId?.() !== existing) {
    console.error('[InboxQL] tab did not come forward', id);
  }
};

interface ViewerState {
  /** The message currently shown in the viewer tab, if any. */
  messageId: string | null;
  message: any | null;
  setMessage: (message: any) => void;
  clear: () => void;
}

/**
 * The message the viewer tab is showing.
 *
 * Held outside the tab because the list and the viewer are now separate
 * components in separate tabs, with no parent between them to hold it.
 */
export const useViewerStore = create<ViewerState>((set) => ({
  messageId: null,
  message: null,
  setMessage: (message) => set({ message, messageId: message?.id ?? null }),
  clear: () => set({ message: null, messageId: null }),
}));

/** Component id of the message viewer tab. */
export const MESSAGE_VIEWER_TAB = 'message';
/** Component id of the error log tab. */
export const ERROR_LOG_TAB = 'errors';

/**
 * Show a message in the viewer, opening the tab when it is not already there.
 */
export const openMessage = (message: any): void => {
  useViewerStore.getState().setMessage(message);
  openTool(MESSAGE_VIEWER_TAB, 'Message');
};

interface ErrorLogState {
  /** When set, the log shows only this import job's failures. */
  jobFilter: string | null;
  setJobFilter: (jobId: string | null) => void;
}

export const useErrorLogStore = create<ErrorLogState>((set) => ({
  jobFilter: null,
  setJobFilter: (jobFilter) => set({ jobFilter }),
}));

/** Open the error log, optionally scoped to one import job. */
export const openErrorLog = (jobId?: string): void => {
  useErrorLogStore.getState().setJobFilter(jobId ?? null);
  openTool(ERROR_LOG_TAB, 'Error Log');
};
