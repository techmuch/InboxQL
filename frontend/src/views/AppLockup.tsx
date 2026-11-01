import { useState } from 'react';
import { openDoc } from '../lib/docsNav';
import { appVersion, notesAnchor } from '../lib/version';

/** Which release's notes this browser has seen. */
const SEEN_KEY = 'iql.releaseNotes.seen';

const readSeen = (): string | null => {
  try { return localStorage.getItem(SEEN_KEY); } catch { return null; }
};
const writeSeen = (v: string) => {
  try { localStorage.setItem(SEEN_KEY, v); } catch { /* per viewer; nothing breaks without it */ }
};

/**
 * Whether there are notes this browser has not looked at.
 *
 * Only after an update: a browser that has never recorded a version is a
 * first visit, and "new since you last looked" means nothing to it, so it
 * starts at the current release without a dot.
 */
const unseenOnLoad = (): boolean => {
  const seen = readSeen();
  if (seen === null) { writeSeen(appVersion); return false; }
  return seen !== appVersion;
};

/**
 * InboxQL's name in the menu bar, with the version as a way to its notes.
 *
 * nexus-shell's AppTitle draws the same lockup but takes the subtitle as text,
 * and the version here is a control: clicking it opens Help → Release notes at
 * this release. A dot beside it, after an update, says there is something new
 * until the notes have been opened once in this browser.
 */
export const AppLockup = () => {
  const [unseen, setUnseen] = useState(unseenOnLoad);
  const open = () => {
    openDoc('release-notes', notesAnchor(appVersion));
    writeSeen(appVersion);
    setUnseen(false);
  };
  return (
    <div className="app-lockup flex items-center space-x-3 select-none">
      <div className="min-w-0">
        <h1 className="text-xs font-black uppercase tracking-wider text-foreground font-mono truncate">InboxQL</h1>
        <p className="flex items-center text-[11px] text-muted-foreground font-semibold truncate">
          <button
            type="button"
            onClick={open}
            title="Release notes for this version"
            aria-label={`Version ${appVersion}, release notes${unseen ? ', new since you last looked' : ''}`}
            // A larger target than the 9px text it wraps, without moving it.
            className="relative -mx-1 -my-1 inline-flex items-center gap-1 px-1 py-1 font-mono hover:text-foreground hover:underline
                       focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
          >
            v{appVersion}
            {unseen && <span aria-hidden className="inline-block h-1.5 w-1.5 rounded-full bg-primary" />}
          </button>
          <span aria-hidden className="mx-1">•</span>
          <span>Email for Engineers</span>
        </p>
      </div>
    </div>
  );
};
