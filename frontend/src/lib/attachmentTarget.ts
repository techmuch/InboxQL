import { create } from 'zustand';
import type { ViewerMode } from './tabs';

/**
 * Where clicking an attachment chip puts the preview.
 *
 * # Two places, and they are not the same question
 *
 * A file has always had two homes. Inside the message it arrived on, below the
 * chips, where it is evidence for what you are reading — and in the viewer's
 * own file slot, where it is an entity with a size, a history and a list of
 * every message it came on. Which one somebody wants depends on what they are
 * doing, and the application was answering for them: chips went inline, the
 * Desk's file list went to the viewer, and neither could be asked for from the
 * other place.
 *
 * # Why it lives in the browser
 *
 * Beside the viewer-tab and threading preferences, which are the same kind of
 * thing: what this machine's workspace looks like, not what the mailbox is.
 */
export type AttachmentTarget = 'message' | 'file' | 'both';

const TARGET_KEY = 'inboxql.attachmentTarget';

const isTarget = (v: string | null): v is AttachmentTarget =>
  v === 'message' || v === 'file' || v === 'both';

function readTarget(): AttachmentTarget {
  try {
    const stored = localStorage.getItem(TARGET_KEY);
    return isTarget(stored) ? stored : 'message';
  } catch {
    // A browser with storage blocked still gets a working app on the default.
    return 'message';
  }
}

interface AttachmentTargetState {
  target: AttachmentTarget;
  setTarget: (t: AttachmentTarget) => void;
}

export const useAttachmentTargetStore = create<AttachmentTargetState>((set, get) => ({
  target: readTarget(),
  setTarget: (target) => {
    if (get().target === target) return;
    try {
      localStorage.setItem(TARGET_KEY, target);
    } catch {
      // An unwritable store costs the preference its persistence, not the
      // switch its effect.
    }
    set({ target });
  },
}));

/** Where a preview should go, read outside React. */
export const attachmentTarget = () => readTarget();

/** The two places a preview can appear, as independent answers. */
export interface PreviewPlaces {
  /** Below the chips, inside the message being read. */
  inline: boolean;
  /** The viewer's file slot — its own tab in split mode. */
  viewer: boolean;
}

/**
 * Resolve the preference against the viewer-tab mode.
 *
 * # Why the two settings cannot be independent
 *
 * The viewer's file slot and its message slot are the same tab in `reuse` mode,
 * and setting one clears the other — that is what reuse mode *is*. So sending a
 * file there replaces the message you are reading, which is a reasonable thing
 * to ask for and a nonsensical thing to be given when you asked for **both**:
 * the message would be gone, the inline preview with it, and "both" would have
 * produced exactly one place.
 *
 * So `both` keeps the half it can keep. The inline one, because that is the
 * preview attached to the message the chip belongs to.
 *
 * `file` is obeyed as written, because evicting the message is precisely what
 * it asks for. The setting says so where it is chosen rather than quietly
 * doing something else.
 */
export function previewPlaces(target: AttachmentTarget, mode: ViewerMode): PreviewPlaces {
  switch (target) {
    case 'file':
      return { inline: false, viewer: true };
    case 'both':
      return { inline: true, viewer: mode === 'split' };
    default:
      return { inline: true, viewer: false };
  }
}
