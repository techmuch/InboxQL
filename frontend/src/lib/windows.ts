import { create } from 'zustand';
import { openTool, openMessageByID, openQuery, openErrorLog, DESK_TAB } from './tabs';
import { useLayoutStore } from 'nexus-shell';
import { useQueryStore } from './filters';

/**
 * This window's connection to the backend, and the other windows it can see.
 *
 * # What this is, and what it is not
 *
 * Not a sync. There is no canonical state being replicated and no conflict to
 * resolve, because only one side ever writes a given fact: this window owns
 * what it is showing, and the server owns the list of which windows exist.
 *
 * A command is an instruction to a live window, not a new source of truth about
 * it. Point this window at a query from the command line, reload it, and it
 * comes back showing whatever it had persisted for itself.
 *
 * # Why it registers
 *
 * So that something outside the browser can answer "what is on screen" and
 * "put this in front of them". That is the whole feature: an agent that has
 * found something should be able to show it to you rather than describe it,
 * and should be able to see what you are already looking at rather than ask.
 */

/** What this window reports about itself. Small on purpose: "what am I looking
 *  at", answered well enough to read in a list — not a serialisation of the UI. */
export interface Showing {
  query?: string;
  tabs?: string[];
  active?: string;
}

export interface WindowInfo {
  name: string;
  title?: string;
  showing: Showing;
  connected: boolean;
  since: string;
  lastSeen: string;
  lastCommand?: { verb: string; arg: string; note?: string; at: string };
}

interface Command {
  verb: 'query' | 'open' | 'notice';
  arg: string;
  note?: string;
  at: string;
}

interface WindowsState {
  /** This window's name, as a person would type it. Null until registered. */
  name: string | null;
  /** Whether the command channel is live, which is not the same as registered. */
  connected: boolean;
  /** The last thing this window was told, so it can say why the screen moved. */
  lastNote: { text: string; at: number } | null;
  setName: (name: string | null) => void;
  setConnected: (connected: boolean) => void;
  note: (text: string) => void;
  dismissNote: () => void;
}

export const useWindowsStore = create<WindowsState>((set) => ({
  name: null,
  connected: false,
  lastNote: null,
  setName: (name) => set({ name }),
  setConnected: (connected) => set({ connected }),
  note: (text) => set({ lastNote: { text, at: Date.now() } }),
  dismissNote: () => set({ lastNote: null }),
}));

/** What this window is showing, read at the moment it is asked. */
export function currentShowing(): Showing {
  const query = useQueryStore.getState().text;
  const tabs: string[] = [];
  let active: string | undefined;
  try {
    // The same walk `openTool` does, because the layout is a FlexLayout model
    // and `visitNodes` is the only way through it. Reported as component ids —
    // desk, log, viewer — rather than tab labels, because the ids are what
    // `iql ui open` takes and a listing somebody acts on should name things
    // the way the command that acts on them does.
    const model = (useLayoutStore.getState() as any)?.model;
    model?.visitNodes((node: any) => {
      if (node.getType() !== 'tab') return;
      const comp = node.getComponent();
      if (comp && !tabs.includes(comp)) tabs.push(comp);
      // A FlexLayout tab knows whether it is the selected one in its tabset.
      try {
        const parent = node.getParent?.();
        if (parent?.getSelectedNode?.() === node && !active) active = comp;
      } catch {
        // A node shape this build does not know costs the listing its active
        // tab, not the window its registration.
      }
    });
  } catch {
    // A shell that reports its layout differently costs the listing its tab
    // names, not the window its registration.
  }
  return { query, tabs, active };
}

/** Run a command sent from outside. */
function apply(c: Command) {
  const store = useWindowsStore.getState();
  // The note first, so the explanation is on screen before the thing it
  // explains. A screen that reorganises itself unannounced is alarming.
  if (c.note) store.note(c.note);

  switch (c.verb) {
    case 'query':
      openQuery(c.arg);
      break;
    case 'notice':
      store.note(c.arg);
      break;
    case 'open':
      openNamed(c.arg);
      break;
  }
}

/**
 * Open something by name.
 *
 * A short vocabulary rather than an address into the layout: the openers are
 * the six things this application knows how to show, and anything outside them
 * is a message id, which is the one identifier somebody is likely to have.
 */
function openNamed(what: string) {
  switch (what.toLowerCase()) {
    case 'desk':
      openTool(DESK_TAB, 'Desk');
      return;
    case 'log':
    case 'errors':
      openErrorLog();
      return;
    case 'settings':
      openTool('settings', 'Settings');
      return;
    case 'annotators':
      openTool('annotators', 'Annotators');
      return;
    case 'windows':
      openTool(WINDOWS_TAB, 'Windows');
      return;
    default:
      // Anything else is taken as a message id, which fails visibly rather
      // than silently if it is not one.
      void openMessageByID(what);
  }
}

export const WINDOWS_TAB = 'windows';

let started = false;
let heartbeat: number | null = null;
let stream: EventSource | null = null;

/**
 * Register this window and keep the channel open.
 *
 * Idempotent: a second call does nothing, because two heartbeats for one window
 * would make it look like two.
 */
export function startWindowSession() {
  if (started) return;
  started = true;

  const register = async () => {
    try {
      const res = await fetch('/api/ui/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: document.title || 'InboxQL' }),
      });
      if (!res.ok) return;
      const body = await res.json();
      const name: string = body.name;
      useWindowsStore.getState().setName(name);
      connect(name);
      beat(name, body.heartbeatMs ?? 10000);
    } catch {
      // No server, or it is restarting. Tried again on the next beat rather
      // than retried in a tight loop.
      window.setTimeout(() => { started = false; startWindowSession(); }, 15000);
    }
  };

  void register();
}

function connect(name: string) {
  stream?.close();
  const es = new EventSource(`/api/ui/${encodeURIComponent(name)}/events`);
  stream = es;

  es.onopen = () => useWindowsStore.getState().setConnected(true);
  es.onerror = () => {
    // EventSource reconnects on its own, so this reports the gap rather than
    // rebuilding the stream — rebuilding it here would race the browser's own
    // retry and open two.
    useWindowsStore.getState().setConnected(false);
  };
  es.onmessage = (e) => {
    try {
      apply(JSON.parse(e.data) as Command);
    } catch {
      // A command this build does not understand is ignored rather than
      // allowed to break the channel carrying the ones it does.
    }
  };
}

function beat(name: string, everyMs: number) {
  if (heartbeat !== null) window.clearInterval(heartbeat);
  const send = async () => {
    try {
      const res = await fetch(`/api/ui/${encodeURIComponent(name)}/state`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(currentShowing()),
      });
      if (res.status === 404) {
        // Expired while this machine slept, or the server restarted. Register
        // again rather than heartbeat forever into nothing.
        started = false;
        useWindowsStore.getState().setName(null);
        useWindowsStore.getState().setConnected(false);
        startWindowSession();
      }
    } catch {
      // The next beat tries again.
    }
  };
  void send();
  heartbeat = window.setInterval(send, everyMs);

  // A tab that closes cleanly says so, rather than leaving a ghost in the
  // listing until it expires.
  window.addEventListener('pagehide', () => {
    navigator.sendBeacon?.(`/api/ui/${encodeURIComponent(name)}/close`);
    fetch(`/api/ui/${encodeURIComponent(name)}`, { method: 'DELETE', keepalive: true }).catch(() => {});
  });
}

/** Every open window, for the panel that lists them. */
export async function listWindows(): Promise<WindowInfo[]> {
  const res = await fetch('/api/ui');
  if (!res.ok) throw new Error(`${res.status}`);
  const body = await res.json();
  return body.windows ?? [];
}

/** Point another window at something. */
export async function sendToWindow(name: string, verb: string, arg: string, note?: string) {
  const res = await fetch(`/api/ui/${encodeURIComponent(name)}/command`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ verb, arg, note }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    throw new Error(body?.error ?? `${res.status}`);
  }
}
