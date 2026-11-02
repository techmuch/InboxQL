import { create } from 'zustand';
import { useQueryStore } from './filters';
import { goTo, normalisePlace, usePlaceStore } from './places';

/**
 * Opening things from anywhere.
 *
 * This used to open workbench tabs. The workspace is now places — one screen
 * at a time in a fixed frame — so `openTool` goes to a place instead, and the
 * name stayed because every surface in the application already calls it.
 * See lib/places.ts.
 */

/** Place id of the mail list. */
export const DESK_TAB = 'desk';
/** The query place, the same as the mail list. */
export const QUERY_TAB = 'desk';

/**
 * Go to a place. The label is no longer shown anywhere — a place's name comes
 * from the left-hand pane — but callers still pass it, and it costs nothing
 * to keep the signature they were written against.
 */
export const openTool = (id: string, _label?: string): void => {
  goTo(normalisePlace(id));
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

/**
 * There is one viewer now, so it always holds one subject at a time. The
 * split mode, which gave messages, contacts and files a tab each, went with
 * the tabs; the type stays for the code that asks.
 */
function readViewerMode(): ViewerMode {
  return 'reuse';
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
  setMode: () => { /* one viewer: nothing to switch */ },
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

/** Whether a place is the one on screen. */
export const isToolOpen = (id: string): boolean => usePlaceStore.getState().id === normalisePlace(id);

/** Where a subject of any kind opens: the one viewer. */
const viewerTarget = (_kind: ViewerKind): { id: string; label: string } =>
  ({ id: VIEWER_TABS.message.id, label: 'Viewer' });

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
  const message = await fetchMessage(id);
  if (message) openMessage(message);
};

/**
 * Fetch one message, or undefined.
 *
 * A message that will not load is not worth an error state at either call site:
 * the user asked to follow a link or moved onto a row, and the subject simply
 * is not there.
 */
async function fetchMessage(id: string): Promise<any | undefined> {
  try {
    const response = await fetch(`/api/message?id=${encodeURIComponent(id)}`);
    if (!response.ok) return undefined;
    return await response.json();
  } catch {
    return undefined;
  }
}

/**
 * Monotonic counter guarding the preview slot against an out-of-order fetch.
 *
 * Arrowing a list issues one request per keystroke and they can land in any
 * order, so without this a slow response for a row the user has already left
 * overwrites the one they are now on — and the viewer shows a message that is
 * not the highlighted row. The newest request always wins.
 */
let previewSeq = 0;

/**
 * Point the viewer at a message the caller only knows the id of, without
 * bringing it forward.
 *
 * `previewMessage` for a list that holds whole messages; this for one that does
 * not — a file's occurrences know a message id and nothing else.
 */
export const previewMessageByID = async (id: string): Promise<void> => {
  const seq = ++previewSeq;
  const message = await fetchMessage(id);
  if (!message || seq !== previewSeq) return;
  useViewerStore.getState().setMessage(message);
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
  previewSeq++; // A fetched preview in flight must not land on top of this one.
  useViewerStore.getState().setMessage(message);
};

/**
 * Point the viewer at a contact without bringing it forward.
 *
 * The preview half of `openContact`, so arrowing a contact list updates an
 * already-open card instead of switching tab on the first keystroke.
 */
export const previewContact = (address: string): void => {
  useViewerStore.getState().setContact(address);
};

/**
 * Point the viewer at a file without bringing it forward.
 *
 * # Why this was the gap that mattered
 *
 * `openAttachment` calls `openTool`, which selects the viewer's tab. Arrowing
 * the file list through it would switch away from Desk on the first Down key and
 * the second would go nowhere, because focus had left the list — the exact
 * failure `previewMessage` above exists to fix, still live for files until now.
 */
export const previewAttachment = (file: any): void => {
  useViewerStore.getState().setFile(file);
};

/** Show a contact in the viewer, opening the tab when it is not already there. */
export const openContact = (address: string): void => {
  useViewerStore.getState().setContact(address);
  // A person is a page of their own now, not a viewer tab.
  openTool('person', 'Person');
};

/** Show a file in the viewer, opening the tab when it is not already there. */
export const openAttachment = (file: any): void => {
  useViewerStore.getState().setFile(file);
  const target = viewerTarget('file');
  openTool(target.id, target.label);
};

/**
 * Show a file the caller only knows the content hash of.
 *
 * The same split as `openMessageByID`: a file list holds whole files, but an
 * attachment chip on a message holds a filename and a hash — the entity's size,
 * reach and history are not on the message, because they are not facts about
 * this arrival of it.
 *
 * The viewer opens only once the file is in hand, so a hash that resolves to
 * nothing leaves the user where they were rather than on an empty tab.
 */
export const openAttachmentByKey = async (key: string): Promise<void> => {
  try {
    const response = await fetch(`/api/attachments/file?key=${encodeURIComponent(key)}`);
    if (!response.ok) return;
    const file = await response.json();
    if (file) openAttachment(file);
  } catch {
    // A file that will not load is not worth an error state: the user clicked
    // a chip, and the chip simply goes nowhere.
  }
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
  openTool(ERROR_LOG_TAB, 'Log');
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
