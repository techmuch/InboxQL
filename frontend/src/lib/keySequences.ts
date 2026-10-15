/**
 * Keyboard: two-key sequences for places, single keys for the commonest
 * actions, and ⌘K / Ctrl+K for Find anything.
 *
 * # Why sequences
 *
 * Ctrl+Shift+letter collides with the browser on Windows and Linux —
 * Ctrl+Shift+C opens developer tools in Chrome — and there are only so many
 * letters. "g" then a letter is what Gmail and GitHub taught people, never
 * reaches the browser, and leaves room for every place to have one.
 *
 * Keys typed into a field are never taken: a sequence starts only when focus
 * is not in an input, a text area, a select or something editable.
 */

export interface Binding {
  keys: string;        // as shown: "g d", "c", "?"
  label: string;
  command: string;     // command id to execute
  group: 'Go' | 'Create' | 'Help';
}

export const BINDINGS: Binding[] = [
  { keys: 'g d', label: 'Desk', command: 'iql.open-desk', group: 'Go' },
  { keys: 'g c', label: 'Calendar', command: 'iql.open-calendar', group: 'Go' },
  { keys: 'g b', label: 'Board', command: 'iql.open-board', group: 'Go' },
  { keys: 'g p', label: 'People (Contacts)', command: 'iql.open-contacts', group: 'Go' },
  { keys: 'g f', label: 'Files', command: 'iql.open-files', group: 'Go' },
  { keys: 'g a', label: 'Waiting for approval', command: 'iql.open-approvals', group: 'Go' },
  { keys: 'g s', label: 'Settings', command: 'iql.open-settings', group: 'Go' },
  { keys: 'c', label: 'New message', command: 'iql.new-message', group: 'Create' },
  { keys: 't', label: 'New task', command: 'iql.new-task', group: 'Create' },
  { keys: '?', label: 'Keyboard shortcuts', command: 'iql.keyboard', group: 'Help' },
];

const editing = (el: EventTarget | null) => {
  const t = el as HTMLElement | null;
  return !!t?.closest?.('input, textarea, select, [contenteditable="true"], [role="textbox"]');
};

/**
 * Install the handler. Returns a function that removes it.
 *
 * `run` executes a command id; `palette` opens Find anything.
 */
let pending: string | null = null;

/**
 * Whether a sequence has started — `g` was pressed and the next key finishes
 * it. A pane with its own single keys (the Desk's `s` to star) leaves the
 * key alone then, or `g s` would star a message instead of opening Settings.
 */
export const sequencePending = () => pending !== null;

export function installKeySequences(run: (command: string) => void, palette: () => void): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null;
  const reset = () => { pending = null; if (timer) clearTimeout(timer); timer = null; };

  const onKey = (e: KeyboardEvent) => {
    // Find anything works from anywhere, a field included: it is the way out.
    if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      reset();
      palette();
      return;
    }
    if (e.metaKey || e.ctrlKey || e.altKey || editing(e.target) || e.defaultPrevented) return;
    // A tab with its own single-letter keys (the calendar's d/w/m) keeps them
    // unless a sequence has started.
    const key = e.key === '?' ? '?' : e.key.toLowerCase();
    if (pending) {
      const hit = BINDINGS.find(b => b.keys === `${pending} ${key}`);
      reset();
      if (hit) { e.preventDefault(); run(hit.command); }
      return;
    }
    if (key === 'g') {
      pending = 'g';
      timer = setTimeout(reset, 1500);
      e.preventDefault();
      return;
    }
    const single = BINDINGS.find(b => b.keys === key);
    if (single && !document.querySelector('[data-calendar-root]')?.contains(document.activeElement as Node)) {
      e.preventDefault();
      run(single.command);
    }
  };
  window.addEventListener('keydown', onKey);
  return () => { window.removeEventListener('keydown', onKey); reset(); };
}
