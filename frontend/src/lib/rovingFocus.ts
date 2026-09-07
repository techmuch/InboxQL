import { useCallback, useEffect, useRef } from 'react';

/**
 * Mark an element as a keyboard-navigable row.
 *
 * Spread onto any row a user should be able to arrow through:
 * `<div {...navRow} />`. Nothing else is required — the hook finds rows by
 * this attribute and manages their tab stops.
 */
export const navRow = { 'data-nav-row': '' } as const;

const ROW_SELECTOR = '[data-nav-row]';

/**
 * Whether a row can be navigated to.
 *
 * Not `offsetParent !== null`, which is the usual shortcut: it is null for
 * anything `position: fixed` even when plainly visible, so a row inside a
 * drawer would be skipped. It is also null for everything under jsdom, which
 * does no layout — so that check would make this hook pass in a browser and be
 * untestable.
 *
 * checkVisibility is the precise answer where it exists. Where it does not,
 * the honest default is "navigable": a list that silently refuses to move is a
 * worse failure than one that focuses something off-screen.
 */
/**
 * Put a row in or out of the tab order.
 *
 * setAttribute rather than the tabIndex property, and unconditionally when the
 * attribute is absent: a <div> that has never carried tabindex reads back as
 * -1 already, so "write only when it differs" would leave it without the
 * attribute — and an element with no tabindex attribute cannot be focused at
 * all. That made every row that was not a <button> silently unreachable.
 */
function setTabStop(el: HTMLElement, isStop: boolean): void {
  const want = isStop ? '0' : '-1';
  if (el.getAttribute('tabindex') !== want) el.setAttribute('tabindex', want);
}

function isVisible(el: HTMLElement): boolean {
  if (el.hasAttribute('hidden') || el.closest('[hidden]')) return false;
  if (typeof el.checkVisibility === 'function') return el.checkVisibility();
  return true;
}

/**
 * Arrow-key navigation over whatever is on screen.
 *
 * # Why DOM order rather than an index into an array
 *
 * Desk's navigation used to be `messages[focusedIndex]`, which meant it could
 * only navigate one shape of result. Every other mode — groups, counts,
 * tickets, drafts, conversations — set `messages` to empty, and the handler's
 * first line was `if (messages.length === 0) return`, so the keyboard silently
 * did nothing in all of them.
 *
 * Walking the DOM instead means the handler never learns what a row *is*. A
 * conversation, an aggregate bucket and a message are all just rows, in the
 * order they are painted. Nesting falls out for free: an expanded thread's
 * entries sit between its header and the next thread, so pressing Down walks
 * into them without anyone writing tree traversal.
 *
 * # Why a roving tab stop
 *
 * Exactly one row is in the Tab order at a time. Tab enters the list once and
 * leaves once; arrows move within it. Making every row tabbable would put
 * fifty stops between the query bar and anything after it.
 */
export function useRovingFocus<T extends HTMLElement>() {
  const ref = useRef<T>(null);

  const rows = useCallback((): HTMLElement[] => {
    const root = ref.current;
    if (!root) return [];
    return Array.from(root.querySelectorAll<HTMLElement>(ROW_SELECTOR)).filter(isVisible);
  }, []);

  /**
   * Keep exactly one row tabbable.
   *
   * Run from an observer rather than from callers, because the row set
   * changes for reasons no single caller knows about: a query returns, a
   * thread expands, infinite scroll appends a page. Anything that had to be
   * notified would eventually not be.
   */
  const syncTabStops = useCallback(() => {
    const all = rows();
    if (all.length === 0) return;
    // Read the attribute, not the property. `el.tabIndex` on a <div> with no
    // tabindex attribute already returns -1, and a <button> already returns 0
    // — so a property comparison both skips writing the attribute a plain div
    // needs to be focusable at all, and mistakes the first button for the
    // current tab stop.
    const stop = all.find(el => el.getAttribute('tabindex') === '0') ?? all[0];
    for (const el of all) setTabStop(el, el === stop);
  }, [rows]);

  useEffect(() => {
    const root = ref.current;
    if (!root) return;

    syncTabStops();

    // childList only. Observing attributes would see this hook's own tabindex
    // writes and loop.
    const observer = new MutationObserver(syncTabStops);
    observer.observe(root, { childList: true, subtree: true });

    // Focusing a row makes it the tab stop, so tabbing away and back returns
    // to where you were rather than to the top of the list.
    const onFocusIn = (e: FocusEvent) => {
      const row = (e.target as HTMLElement | null)?.closest?.(ROW_SELECTOR) as HTMLElement | null;
      if (!row || !root.contains(row)) return;
      for (const el of rows()) setTabStop(el, el === row);
    };
    root.addEventListener('focusin', onFocusIn);

    return () => {
      observer.disconnect();
      root.removeEventListener('focusin', onFocusIn);
    };
  }, [rows, syncTabStops]);

  const focusRow = (el: HTMLElement | undefined) => {
    if (!el) return;
    // Made focusable here rather than trusting the observer to have got to it.
    // MutationObserver callbacks are microtasks, so a row that appeared in
    // this same task — expanding a thread and pressing Down without yielding —
    // would still have no tabindex attribute, and focus would quietly not move.
    if (!el.hasAttribute('tabindex')) setTabStop(el, false);
    el.focus();
    // Guarded because scrolling is a nicety and focusing is the point. jsdom
    // has no layout and so no scrollIntoView; an unguarded call threw *after*
    // the focus landed, which aborted the rest of the handler and would have
    // done the same in any browser missing it.
    //
    // `nearest` rather than `center`: a list that recentres on every keypress
    // makes it impossible to track where you are.
    el.scrollIntoView?.({ block: 'nearest' });
  };

  const onKeyDown = useCallback((e: React.KeyboardEvent) => {
    const root = ref.current;
    if (!root) return;

    // Typing in the query bar is not navigating. Rows live outside the
    // editor, so an event that started inside a text field is left alone.
    const target = e.target as HTMLElement;
    if (target.closest('input, textarea, select, [contenteditable="true"]')) return;

    const all = rows();
    if (all.length === 0) return;

    const active = document.activeElement as HTMLElement | null;
    const currentRow = active?.closest?.(ROW_SELECTOR) as HTMLElement | null;
    const index = currentRow ? all.indexOf(currentRow) : -1;

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        focusRow(all[Math.min(index + 1, all.length - 1)]);
        return;

      case 'ArrowUp':
        e.preventDefault();
        // From nowhere, Up goes to the last row rather than staying nowhere.
        focusRow(index <= 0 ? all[index === 0 ? 0 : all.length - 1] : all[index - 1]);
        return;

      case 'Home':
        e.preventDefault();
        focusRow(all[0]);
        return;

      case 'End':
        e.preventDefault();
        focusRow(all[all.length - 1]);
        return;

      case 'ArrowRight':
        // Expandable rows declare aria-expanded. Anything else ignores this,
        // so a message list is unaffected.
        if (currentRow?.getAttribute('aria-expanded') === 'false') {
          e.preventDefault();
          currentRow.click();
        }
        return;

      case 'ArrowLeft': {
        if (currentRow?.getAttribute('aria-expanded') === 'true') {
          e.preventDefault();
          currentRow.click();
          return;
        }
        // Inside an expanded group, Left goes out to the row that owns it —
        // which is the nearest expandable row above. That is what makes
        // "collapse this and move on" two keystrokes rather than a scroll.
        if (currentRow) {
          e.preventDefault();
          for (let i = index - 1; i >= 0; i--) {
            if (all[i].getAttribute('aria-expanded') === 'true') {
              focusRow(all[i]);
              return;
            }
          }
        }
        return;
      }

      case 'Enter':
      case ' ':
        if (!currentRow || currentRow !== active) return;
        // A button or a link activates itself; anything else is a row we made
        // focusable, so the click has to be synthesised.
        if (active.tagName === 'BUTTON' || active.tagName === 'A') return;
        e.preventDefault();
        currentRow.click();
        return;
    }
  }, [rows]);

  return { ref, onKeyDown };
}
