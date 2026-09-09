import { useLayoutStore } from 'nexus-shell';
import { create } from 'zustand';
import { useQueryStore } from './filters';

/**
 * Opening workbench tabs from anywhere.
 *
 * This used to live inside App as a closure, which meant only App could open a
 * tab. Components deeper in the tree — the mail list wanting to show a message,
 * the importer wanting to show its errors — had no way to reach it.
 */

/** Component id of the Desk tab. */
export const DESK_TAB = 'desk';
/** Component id of the query tab, aliased to Desk. */
export const QUERY_TAB = 'desk';

const DESK_ALIASES = new Set(['desk', 'mail', 'query']);
const VIEWER_ALIASES = new Set(['viewer', 'message']);

/**
 * Open a tab, or focus it if it is already open.
 *
 * Focusing rather than duplicating is the whole point: clicking ten messages
 * should reuse one viewer, not leave ten identical tabs behind.
 */
export const openTool = (id: string, label: string): void => {
  // Normalize legacy tab IDs so old aliases ('mail', 'query') always route to 'desk'.
  if (DESK_ALIASES.has(id)) {
    id = 'desk';
    label = 'Desk';
  }
  if (VIEWER_ALIASES.has(id)) {
    id = 'viewer';
    label = 'Viewer';
  }

  const layout = useLayoutStore.getState();
  const model = layout.model;
  if (!model) return;

  let existing: string | null = null;
  const legacyToRename: string[] = [];

  model.visitNodes((node: any) => {
    if (node.getType() === 'tab') {
      const comp = node.getComponent();
      if (comp === id ||
          (id === 'desk' && DESK_ALIASES.has(comp)) ||
          (id === 'viewer' && VIEWER_ALIASES.has(comp))) {
        if (!existing) {
          const tabId = node.getId();
          existing = tabId;
          // A tab persisted under an old id keeps its old name until it is
          // renamed, so "Message" would sit in the tab bar indefinitely after
          // the rename to Viewer.
          if (comp !== id || node.getName() !== label) {
            legacyToRename.push(tabId);
          }
        }
      }
    }
  });

  for (const tabId of legacyToRename) {
    try {
      model.doAction({ type: 'FlexLayout_RenameTab', data: { node: tabId, text: label } } as any);
    } catch {}
    try {
      model.doAction({
        type: 'FlexLayout_UpdateNodeAttributes',
        data: { node: tabId, json: { component: id, name: label } }
      } as any);
    } catch {}
    const node: any = model.getNodeById(tabId);
    if (node?._attributes) {
      node._attributes.name = label;
      node._attributes.component = id;
    }
  }

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
  /**
   * The contact currently shown, if any.
   *
   * The tab holds one subject at a time and its name says which — it was
   * called "Message" when a message was all it could show. Setting either one
   * clears the other, so the tab never has two things to render and a guess to
   * make about which.
   */
  contact: string | null;
  selectedCount: number;
  setMessage: (message: any) => void;
  setContact: (address: string) => void;
  setSelectedCount: (count: number) => void;
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
  contact: null,
  selectedCount: 0,
  setMessage: (message) => set({ message, messageId: message?.id ?? null, contact: null }),
  setContact: (contact) => set({ contact, message: null, messageId: null }),
  setSelectedCount: (selectedCount) => set({ selectedCount }),
  clear: () => set({ message: null, messageId: null, contact: null, selectedCount: 0 }),
}));

/**
 * Component id of the viewer tab.
 *
 * Renamed from "message" because the tab shows a conversation's mail, a draft
 * and a ticket's evidence, not only a message. The old id stays registered as
 * an alias: layouts are persisted, and a saved layout naming a component that
 * no longer exists renders "Unknown Component" rather than failing usefully.
 */
export const MESSAGE_VIEWER_TAB = 'viewer';
/** Component id of the error log tab. */
export const ERROR_LOG_TAB = 'errors';

/** Whether a tool's tab is currently open. */
export const isToolOpen = (id: string): boolean => {
  const model = useLayoutStore.getState().model;
  if (!model) return false;
  let open = false;
  model.visitNodes((node: any) => {
    if (node.getType() === 'tab' && node.getComponent() === id) open = true;
  });
  return open;
};

/**
 * Show a message in the viewer, opening the tab when it is not already there.
 *
 * This is explicit activation — a click, or Enter on a focused row — so
 * bringing the viewer forward is what the user asked for.
 */
export const openMessage = (message: any): void => {
  useViewerStore.getState().setMessage(message);
  openTool(MESSAGE_VIEWER_TAB, 'Viewer');
};

/**
 * Point the viewer at a message without bringing it forward.
 *
 * # Why this exists
 *
 * Arrowing through the list used to call openMessage, which selects the
 * viewer's tab — so the first Down key switched away from Desk and the second
 * one went nowhere, because focus had left the list. Keyboard navigation was
 * one keystroke long.
 *
 * Preview updates an already-open viewer in place and does nothing when there
 * is none. Opening it is then Enter's job, which keeps "move" and "open" as
 * two different gestures rather than one that sometimes teleports you.
 */
export const previewMessage = (message: any): void => {
  useViewerStore.getState().setMessage(message);
};

/** Show a contact in the viewer, opening the tab when it is not already there. */
export const openContact = (address: string): void => {
  useViewerStore.getState().setContact(address);
  openTool(MESSAGE_VIEWER_TAB, 'Viewer');
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

/**
 * Open Desk on a query.
 *
 * This is what makes the dashboard's filters more than a dead end: whatever
 * narrowed a chart is a query, so it can be carried somewhere it can be read,
 * edited and saved rather than only cleared.
 */
export const openQuery = (query: string): void => {
  useQueryStore.getState().set(query);
  openTool(DESK_TAB, 'Desk');
};
