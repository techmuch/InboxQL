import { create } from 'zustand';

/**
 * Whether the Desk starts threaded.
 *
 * # A default, not a mode
 *
 * Grouping by conversation is a stage in the query — `| timeline` — not a
 * display flag beside it. That is deliberate: the query bar always explains
 * what you are looking at, and there is no second piece of state that can
 * disagree with the results.
 *
 * So this preference does not switch a renderer. It decides what the Desk's
 * query starts as, and the Threads button still decides what any one query
 * does. In ordinary use that is indistinguishable from "always threaded",
 * because a rail folder click preserves pipeline stages — Inbox → Starred →
 * Sent keeps the stage it was given.
 *
 * Forcing it onto every query instead would break three things, the first
 * silently: a terminal aggregate would be replaced (`| count by week` would
 * quietly lose its count), entities with no conversations would be asked for
 * theirs, and the Threads button would have nothing left to turn off.
 *
 * # Why it lives in the browser
 *
 * Beside the viewer-tab preference, which is the same kind of thing, in the
 * section that says what it is: these are stored here rather than on the
 * server, so each machine can differ.
 */
export type Threading = 'threaded' | 'flat';

/** The stage that groups a message list into conversations. */
export const THREAD_STAGE = 'timeline';

const THREADING_KEY = 'inboxql.threading';

function readThreading(): Threading {
  try {
    return localStorage.getItem(THREADING_KEY) === 'threaded' ? 'threaded' : 'flat';
  } catch {
    // A browser with storage blocked still gets a working app on the default.
    return 'flat';
  }
}

interface ThreadingState {
  threading: Threading;
  setThreading: (t: Threading) => void;
}

export const useThreadingStore = create<ThreadingState>((set, get) => ({
  threading: readThreading(),
  setThreading: (threading) => {
    if (get().threading === threading) return;
    try {
      localStorage.setItem(THREADING_KEY, threading);
    } catch {
      // An unwritable store costs the preference its persistence, not the
      // switch its effect.
    }
    set({ threading });
  },
}));

/** Whether the Desk should start threaded, read outside React. */
export const startsThreaded = () => readThreading() === 'threaded';

/**
 * Whether a query is one the thread stage can be added to.
 *
 * Two things rule it out, and neither is a matter of taste:
 *
 *   - **A terminal aggregate.** A query may end in only one, and adding a
 *     stage replaces whichever is there — so threading `| count by week` does
 *     not thread anything, it deletes the count.
 *   - **Anything but mail.** Contacts, files, tickets and drafts have no
 *     conversations to group into.
 *
 * Deliberately conservative: an unrecognised shape is left alone. A query
 * somebody wrote is a statement of what they wanted, and the cost of not
 * threading it is a button press.
 */
export function canThread(query: string, stageVerbs: string[]): boolean {
  if (stageVerbs.some(v => TERMINAL_STAGES.has(v))) return false;
  return isMailQuery(query);
}

/** Whether a query is about mail, which is the default kind. */
export const isMailQuery = (query: string) => !NON_MAIL.test(query);

/**
 * A folder query, threaded if that is the preference.
 *
 * Used when the rail moves between kinds, where composing onto the old query
 * would carry an `in:` term that makes the folder meaningless.
 */
export const openingFolderQuery = (folderTerm: string) =>
  startsThreaded() ? `${folderTerm} | ${THREAD_STAGE}` : folderTerm;

/**
 * Stages that produce something other than a message list.
 *
 * `thread` is absent on purpose: it expands a list rather than ending it, so a
 * query carrying it can still be grouped.
 */
const TERMINAL_STAGES = new Set([
  'count', 'top', 'series', 'sum', 'avg', 'min', 'max',
  'participants', 'timeline', 'network', 'topics',
]);

/**
 * A query about something other than mail.
 *
 * Matched on the `in:` term rather than inferred, because `in:` is how the
 * language says which kind a query is about. `in:mail` is the default and is
 * threadable, so it is not listed.
 */
const NON_MAIL = /\bin:\s*(contacts|attachments|tickets|drafts)\b/i;
