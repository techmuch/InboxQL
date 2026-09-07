import { useState } from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { navRow, useRovingFocus } from './rovingFocus';

/**
 * A stand-in for Desk: a text field, some flat rows, and an expandable group
 * whose children are rows too.
 *
 * The group matters. Desk's timeline is nested, and the bug this hook replaced
 * could not express a row that was not a message, let alone one inside another
 * row.
 */
const Harness = ({ onActivate = () => {} }: { onActivate?: () => void }) => {
  const { ref, onKeyDown } = useRovingFocus<HTMLDivElement>();
  const [open, setOpen] = useState(false);

  return (
    <div ref={ref} onKeyDown={onKeyDown} tabIndex={0} data-testid="pane">
      <input aria-label="query" />
      <div {...navRow} data-testid="first">first</div>
      <button {...navRow} aria-expanded={open} onClick={() => setOpen(o => !o)}>
        group
      </button>
      {open && (
        <div>
          <div {...navRow} data-testid="child-1">child 1</div>
          <div {...navRow} data-testid="child-2">child 2</div>
        </div>
      )}
      <div {...navRow} data-testid="last" onClick={onActivate}>last</div>
    </div>
  );
};

const press = (key: string, opts: Record<string, unknown> = {}) =>
  fireEvent.keyDown(document.activeElement ?? document.body, { key, bubbles: true, ...opts });

describe('useRovingFocus', () => {
  it('moves down and up through the rows', () => {
    render(<Harness />);
    screen.getByTestId('pane').focus();

    // From nowhere, Down enters the list at the top.
    press('ArrowDown');
    expect(document.activeElement).toBe(screen.getByTestId('first'));

    press('ArrowDown');
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'group' }));

    press('ArrowUp');
    expect(document.activeElement).toBe(screen.getByTestId('first'));

    // It stops at the ends rather than wrapping, so holding a key does not
    // silently teleport you to the other end of a long list.
    press('ArrowUp');
    expect(document.activeElement).toBe(screen.getByTestId('first'));
  });

  it('jumps to the ends with Home and End', () => {
    render(<Harness />);
    screen.getByTestId('pane').focus();

    press('End');
    expect(document.activeElement).toBe(screen.getByTestId('last'));

    press('Home');
    expect(document.activeElement).toBe(screen.getByTestId('first'));
  });

  it('walks into an expanded group rather than over it', () => {
    render(<Harness />);
    screen.getByTestId('pane').focus();

    press('ArrowDown');
    press('ArrowDown'); // the group header
    const group = screen.getByRole('button', { name: 'group' });
    expect(document.activeElement).toBe(group);

    // Collapsed, Down skips past it: the children are not in the DOM.
    press('ArrowDown');
    expect(document.activeElement).toBe(screen.getByTestId('last'));

    // Right expands. The hook reads aria-expanded, so nothing had to tell it
    // this row was a group.
    group.focus();
    press('ArrowRight');
    expect(group.getAttribute('aria-expanded')).toBe('true');

    // And now Down walks the children, in the order they are painted.
    press('ArrowDown');
    expect(document.activeElement).toBe(screen.getByTestId('child-1'));
    press('ArrowDown');
    expect(document.activeElement).toBe(screen.getByTestId('child-2'));
    press('ArrowDown');
    expect(document.activeElement).toBe(screen.getByTestId('last'));
  });

  it('collapses with Left, and escapes to the parent from inside', () => {
    render(<Harness />);
    const group = screen.getByRole('button', { name: 'group' });
    group.focus();
    press('ArrowRight');

    // From inside the group, Left goes out to the row that owns it — which is
    // what makes "collapse this and move on" two keystrokes.
    screen.getByTestId('child-2').focus();
    press('ArrowLeft');
    expect(document.activeElement).toBe(group);

    press('ArrowLeft');
    expect(group.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByTestId('child-1')).not.toBeInTheDocument();
  });

  it('activates a row that is not a button', () => {
    const onActivate = vi.fn();
    render(<Harness onActivate={onActivate} />);

    screen.getByTestId('last').focus();
    press('Enter');
    expect(onActivate).toHaveBeenCalledTimes(1);

    press(' ');
    expect(onActivate).toHaveBeenCalledTimes(2);
  });

  it('leaves buttons to activate themselves', () => {
    render(<Harness />);
    const group = screen.getByRole('button', { name: 'group' });
    group.focus();

    // Synthesising a click here would fire the handler twice — once from this
    // hook and once from the browser's own button activation.
    press('Enter');
    expect(group.getAttribute('aria-expanded')).toBe('false');
  });

  it('does not hijack the arrow keys while typing', () => {
    render(<Harness />);
    const input = screen.getByLabelText('query');
    input.focus();

    press('ArrowDown');
    // Still in the text field: Down there means "move the caret", not
    // "leave the query I am halfway through writing".
    expect(document.activeElement).toBe(input);
  });

  it('keeps exactly one row in the tab order', () => {
    render(<Harness />);
    const rows = () => Array.from(document.querySelectorAll<HTMLElement>('[data-nav-row]'));

    // Tab reaches the list once, not once per row — otherwise fifty stops sit
    // between the query bar and anything after it.
    expect(rows().filter(r => r.tabIndex === 0)).toHaveLength(1);

    screen.getByTestId('last').focus();
    expect(rows().filter(r => r.tabIndex === 0)).toHaveLength(1);
    // And the tab stop follows you, so leaving and returning comes back to
    // where you were rather than to the top.
    expect(screen.getByTestId('last').tabIndex).toBe(0);
  });

  it('picks up rows that appear later', async () => {
    const { rerender } = render(<Harness />);
    const group = screen.getByRole('button', { name: 'group' });
    group.focus();
    press('ArrowRight');
    rerender(<Harness />);

    // The children were added after mount. Nothing notified the hook — it
    // watches the subtree, because the row set changes for reasons no single
    // caller knows about: a query returns, a thread expands, scrolling appends
    // a page.
    await vi.waitFor(() => {
      expect(screen.getByTestId('child-1').tabIndex).toBe(-1);
    });
  });

  it('skips rows that are hidden', () => {
    render(
      <HarnessWithHidden />,
    );
    screen.getByTestId('pane').focus();

    press('ArrowDown');
    expect(document.activeElement).toBe(screen.getByTestId('visible-1'));
    press('ArrowDown');
    expect(document.activeElement).toBe(screen.getByTestId('visible-2'));
  });
});

const HarnessWithHidden = () => {
  const { ref, onKeyDown } = useRovingFocus<HTMLDivElement>();
  return (
    <div ref={ref} onKeyDown={onKeyDown} tabIndex={0} data-testid="pane">
      <div {...navRow} data-testid="visible-1">one</div>
      <div hidden>
        <div {...navRow} data-testid="buried">buried</div>
      </div>
      <div {...navRow} data-testid="visible-2">two</div>
    </div>
  );
};
