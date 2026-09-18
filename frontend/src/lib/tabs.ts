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
  // The id is normalised and the label is not. A layout persisted under the
  // old 'message' component must still resolve, but the caller's label is a
  // real choice now: the same tab is "Viewer" when it holds all three kinds
  // and "Message" when split mode gives contacts and files their own.
  if (VIEWER_ALIASES.has(id)) {
    id = 'viewer';
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

/**
 * How the viewer behaves when you open a second kind of thing.
 *
 * `reuse` — one Viewer tab holds whichever of a message, a contact or a file
 * you looked at last. `split` — each kind gets its own tab, so opening a file
 * leaves the message you were reading where it was.
 *
 * A browser-local preference, deliberately, and the same class of thing as the
 * theme and the saved layout: it describes how this window arranges itself.
 * Storing it on the server would make a laptop and a desktop have to agree
 * about tab behaviour, which is not a thing they have any reason to agree on.
 */
export type ViewerMode = 'reuse' | 'split';

const VIEWER_MODE_KEY = 'inboxql.viewerMode';

/** The kinds of subject the viewer can hold. Each is a tab in split mode. */
export type ViewerKind = 'message' | 'contact' | 'file';

/**
 * Component ids for the per-kind viewer tabs.
 *
 * # Why these can never be unregistered
 *
 * Layouts are persisted. Once somebody has used split mode, their saved layout
 * names `viewer-contact` and `viewer-file` — and a layout naming a component
 * that no longer exists renders "Unknown Component", which is what the
 * `register('message', MessageViewer)` alias already exists to prevent. They
 * stay registered whatever the preference says.
 */
export const VIEWER_TABS: Record<ViewerKind, { id: string; label: string }> = {
  message: { id: 'viewer', label: 'Message' },
  contact: { id: 'viewer-contact', label: 'Contact' },
  file: { id: 'viewer-file', label: 'File' },
};

function readViewerMode(): ViewerMode {
  try {
    return localStorage.getItem(VIEWER_MODE_KEY) === 'split' ? 'split' : 'reuse';
  } catch {
    // A browser with storage blocked still gets a working app on the default.
    return 'reuse';
  }
}

interface ViewerState {
  /** The message currently shown in the viewer tab, if any. */
  messageId: string | null;
  message: any | null;
  /**
   * The contact currently shown, if any.
   *
   * In reuse mode the tab holds one subject at a time and its name says which
   * — it was called "Message" when a message was all it could show. Setting
   * one clears the others, so the tab never has two things to render and a
   * guess to make about which.
   *
   * In split mode the slots are independent, because each has its own tab and
   * clearing one would blank a tab the user is still looking at.
   */
  contact: string | null;
  /**
   * The file currently shown, if any.
   *
   * A third subject alongside a message and a contact, because a file is an
   * entity in its own right now: it can be arrived at from a search that
   * returned files rather than mail, where there is no one message to open
   * instead — the whole point of the file being the row is that it belongs to
   * several.
   */
  file: any | null;
  /** The message viewed before navigating to a contact, if any. */
  previousMessage: any | null;
  selectedCount: number;
  /** How opening a second kind of subject behaves. */
  mode: ViewerMode;
  setMessage: (message: any) => void;
  setContact: (address: string) => void;
  setFile: (file: any) => void;
  setSelectedCount: (count: number) => void;
  setMode: (mode: ViewerMode) => void;
  clear: () => void;
}

/**
 * clearsOthers decides whether setting one subject blanks the rest.
 *
 * The whole behavioural difference between the two modes lives in this one
 * question, which is why it is a function rather than a condition repeated in
 * each setter.
 */
const clearsOthers = (mode: ViewerMode) => mode === 'reuse';

/**
 * The message the viewer tab is showing.
 *
 * Held outside the tab because the list and the viewer are now separate
 * components in separate tabs, with no parent between them to hold it.
 */
export const useViewerStore = create<ViewerState>((set, get) => ({
  messageId: null,
  message: null,
  contact: null,
  file: null,
  previousMessage: null,
  selectedCount: 0,
  mode: readViewerMode(),
  setMessage: (message) => set((s) => ({
    message,
    messageId: message?.id ?? null,
    ...(clearsOthers(s.mode) ? { contact: null, file: null } : {}),
    previousMessage: null,
  })),
  setContact: (contact) => {
    const { message: currentMsg, mode } = get();
    set((s) => ({
      contact,
      ...(clearsOthers(mode) ? { message: null, messageId: null, file: null } : {}),
      // Only meaningful in reuse mode, where the contact replaced the message.
      // In split mode the message is still in its own tab, so there is nothing
      // to go back to and ContactCard hides the button.
      previousMessage: currentMsg ?? s.previousMessage,
    }));
  },
  setFile: (file) => {
    const { message: currentMsg, mode } = get();
    set((s) => ({
      file,
      ...(clearsOthers(mode) ? { message: null, messageId: null, contact: null } : {}),
      previousMessage: currentMsg ?? s.previousMessage,
    }));
  },
  setSelectedCount: (selectedCount) => set({ selectedCount }),
  setMode: (mode) => {
    if (get().mode === mode) return;
    try {
      localStorage.setItem(VIEWER_MODE_KEY, mode);
    } catch {
      // An unwritable store costs the preference its persistence, not the
      // switch its effect.
    }
    set({ mode });
    applyViewerMode(mode);
  },
  clear: () => set({
    message: null, messageId: null, contact: null, file: null,
    previousMessage: null, selectedCount: 0,
  }),
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
 * Where a subject of this kind should open.
 *
 * In reuse mode every kind routes to the one Viewer tab, which is why the
 * label changes too: three kinds sharing a tab cannot call it "Message".
 */
const viewerTarget = (kind: ViewerKind): { id: string; label: string } =>
  useViewerStore.getState().mode === 'split'
    ? VIEWER_TABS[kind]
    : { id: VIEWER_TABS.message.id, label: 'Viewer' };

/** Find the tab node id for a component, or null. */
const tabNodeFor = (component: string): string | null => {
  const model = useLayoutStore.getState().model;
  if (!model) return null;
  let found: string | null = null;
  model.visitNodes((node: any) => {
    if (!found && node.getType() === 'tab' && node.getComponent() === component) {
      found = node.getId();
    }
  });
  return found;
};

/**
 * applyViewerMode brings the open tabs into line with the mode.
 *
 * # Why closing and re-creating, rather than hiding
 *
 * FlexLayout has no hidden state for a tab: its vocabulary is add, delete,
 * rename, select. So "hide the extra viewers" is a delete and "bring them
 * back" is an add.
 *
 * That is not a compromise, because the subject was never in the tab. A tab is
 * a frame around a slot in this store, so deleting it loses nothing and
 * re-creating it shows the same contact or file as before. What it does mean
 * is that the restore is session-scoped: this store has no persistence, so
 * after a reload there is nothing to bring back — which is the right answer
 * anyway, since there is also nothing to show.
 *
 * Only kinds that actually hold something are restored. Switching to split
 * mode having never opened a contact should not conjure an empty Contact tab;
 * one appears the next time a contact is opened, which is what every other tab
 * in this app does.
 */
function applyViewerMode(mode: ViewerMode): void {
  const model = useLayoutStore.getState().model;
  if (!model) return;

  if (mode === 'reuse') {
    // The message tab is the one that survives, because its component id is
    // the one reuse mode routes everything to.
    for (const kind of ['contact', 'file'] as const) {
      const tabId = tabNodeFor(VIEWER_TABS[kind].id);
      if (!tabId) continue;
      try {
        model.doAction({ type: 'FlexLayout_DeleteTab', data: { node: tabId } } as any);
      } catch {
        // A tab that will not close is a stale view, not a broken app.
      }
    }
    renameViewerTab('Viewer');
    return;
  }

  renameViewerTab(VIEWER_TABS.message.label);

  const { contact, file } = useViewerStore.getState();
  if (contact) openTool(VIEWER_TABS.contact.id, VIEWER_TABS.contact.label);
  if (file) openTool(VIEWER_TABS.file.id, VIEWER_TABS.file.label);
}

/**
 * renameViewerTab retitles the shared viewer as the mode changes.
 *
 * "Viewer" is right when one tab holds all three kinds and wrong when it holds
 * only messages, so the three read as a set either way.
 */
function renameViewerTab(label: string): void {
  const model = useLayoutStore.getState().model;
  const tabId = tabNodeFor(VIEWER_TABS.message.id);
  if (!model || !tabId) return;
  try {
    model.doAction({ type: 'FlexLayout_RenameTab', data: { node: tabId, text: label } } as any);
  } catch {
    // Cosmetic. A tab called the wrong thing still shows the right thing.
  }
}

/**
 * Show a message in the viewer, opening the tab when it is not already there.
 *
 * This is explicit activation — a click, or Enter on a focused row — so
 * bringing the viewer forward is what the user asked for.
 */
export const openMessage = (message: any): void => {
  useViewerStore.getState().setMessage(message);
  const target = viewerTarget('message');
  openTool(target.id, target.label);
};

/**
 * Show a message the caller only knows the id of.
 *
 * # Why this is separate from openMessage
 *
 * Every existing caller is a list that already holds the whole message, and
 * making them all fetch again to open a row they are looking at would be
 * absurd. But a link out of something that is not a message list — a file's
 * occurrences, a ticket's evidence — has an id and nothing else. Fetching here
 * keeps that one case from either duplicating the fetch at each call site or
 * pushing an id-shaped message into a viewer that expects a real one.
 *
 * The viewer is opened only once the message is in hand, so a failed fetch
 * leaves the user where they were rather than on an empty viewer.
 */
export const openMessageByID = async (id: string): Promise<void> => {
  try {
    const response = await fetch(`/api/message?id=${encodeURIComponent(id)}`);
    if (!response.ok) return;
    openMessage(await response.json());
  } catch {
    // A message that will not load is not worth an error state here: the user
    // asked to follow a link, and the link simply goes nowhere.
  }
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
  const target = viewerTarget('contact');
  openTool(target.id, target.label);
};

/** Show a file in the viewer, opening the tab when it is not already there. */
export const openAttachment = (file: any): void => {
  useViewerStore.getState().setFile(file);
  const target = viewerTarget('file');
  openTool(target.id, target.label);
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
