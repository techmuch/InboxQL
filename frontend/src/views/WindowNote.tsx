import { useEffect } from 'react';
import { X, Megaphone } from 'lucide-react';
import { useWindowsStore } from '../lib/windows';

/**
 * Why the screen just moved.
 *
 * # Why this is not decoration
 *
 * The whole point of the command channel is that something outside the browser
 * can put a thing in front of you. A screen that reorganises itself with no
 * explanation is alarming rather than helpful — the natural reading of a query
 * bar changing on its own is that something is wrong, not that something is
 * being shown to you.
 *
 * So the note arrives in the same message as the change and is displayed
 * before it is applied. One sentence costs nothing and turns an unexplained
 * event into a handoff.
 *
 * It fades on its own, because an explanation for something that already
 * happened is not a thing to have to dismiss, and stays if dismissed by hand.
 */
export const WindowNote = () => {
  const note = useWindowsStore(s => s.lastNote);
  const dismiss = useWindowsStore(s => s.dismissNote);

  useEffect(() => {
    if (!note) return;
    // Long enough to read a sentence and look at what changed; short enough
    // not to become furniture.
    const t = window.setTimeout(dismiss, 12000);
    return () => window.clearTimeout(t);
  }, [note, dismiss]);

  if (!note) return null;

  return (
    <div
      role="status"
      className="fixed bottom-10 right-4 z-50 flex items-start gap-2 max-w-sm
                 border border-primary/40 bg-popover shadow-lg px-3 py-2"
    >
      <Megaphone className="w-3.5 h-3.5 mt-0.5 shrink-0 text-primary" />
      <p className="text-xs leading-snug flex-1">{note.text}</p>
      <button
        onClick={dismiss}
        aria-label="Dismiss"
        className="p-0.5 text-muted-foreground hover:text-foreground shrink-0"
      >
        <X className="w-3 h-3" />
      </button>
    </div>
  );
};
