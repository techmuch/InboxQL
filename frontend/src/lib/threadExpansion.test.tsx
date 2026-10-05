import { describe, it, expect, beforeEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { ENTRY_CAP, isOpen, useThreadExpansionStore } from './threadExpansion';
import { ThreadResult } from '../views/Desk/Timeline';
import type { Thread } from '../views/Desk/api';

describe('isOpen', () => {
  it('follows the preference when nothing else decides', () => {
    expect(isOpen(null, false, 'collapsed')).toBe(false);
    expect(isOpen(null, false, 'expanded')).toBe(true);
  });

  // The thing the query asked to read. A page holding one closed row is a
  // dead end, whatever the preference.
  it('opens a conversation that is alone on screen', () => {
    expect(isOpen(null, true, 'collapsed')).toBe(true);
  });

  it('lets what the user did outrank both', () => {
    expect(isOpen(false, true, 'expanded')).toBe(false);
    expect(isOpen(true, false, 'collapsed')).toBe(true);
  });
});

describe('the stored preference', () => {
  beforeEach(() => {
    localStorage.clear();
    useThreadExpansionStore.setState({ expansion: 'collapsed' });
  });

  it('persists what was chosen', () => {
    useThreadExpansionStore.getState().setExpansion('expanded');
    expect(localStorage.getItem('inboxql.threadExpansion')).toBe('expanded');
  });
});

const message = (id: string, i: number) => ({
  id, from: 'a@x.com', subject: `Message ${i}`, date: '2026-03-01T10:00:00Z', flags: [],
});

const thread = (key: string, n: number): Thread => ({
  key, subject: `Conversation ${key}`, participants: ['a@x.com'],
  messageCount: n, ticketCount: 0, draftCount: 0,
  start: '2026-03-01T10:00:00Z', end: '2026-03-02T10:00:00Z',
  entries: Array.from({ length: n }, (_, i) => ({
    kind: 'message', at: '2026-03-01T10:00:00Z', actor: 'a@x.com',
    summary: `${key} entry ${i}`, message: message(`${key}-${i}`, i),
  })),
} as unknown as Thread);

const header = (key: string) =>
  screen.getByText(`Conversation ${key}`).closest('[aria-expanded]') as HTMLElement;

describe('ThreadResult', () => {
  beforeEach(() => useThreadExpansionStore.setState({ expansion: 'collapsed' }));

  /**
   * The bug a plain setting would have had.
   *
   * The row used `useState(defaultOpen)`, which reads its argument once, and
   * rows are keyed by thread so they survive across queries. Flipping the
   * preference would have changed nothing on screen.
   */
  it('follows the preference when it changes, for rows nobody touched', () => {
    render(<ThreadResult threads={[thread('a', 2), thread('b', 2)]} onDrillDown={() => {}} />);
    expect(header('a')).toHaveAttribute('aria-expanded', 'false');

    act(() => useThreadExpansionStore.getState().setExpansion('expanded'));
    expect(header('a')).toHaveAttribute('aria-expanded', 'true');
    expect(header('b')).toHaveAttribute('aria-expanded', 'true');
  });

  it('keeps a row the user closed by hand closed', () => {
    useThreadExpansionStore.setState({ expansion: 'expanded' });
    render(<ThreadResult threads={[thread('a', 2), thread('b', 2)]} onDrillDown={() => {}} />);

    fireEvent.click(header('a'));
    expect(header('a')).toHaveAttribute('aria-expanded', 'false');

    // The preference moving again does not undo what was done by hand.
    act(() => useThreadExpansionStore.getState().setExpansion('collapsed'));
    act(() => useThreadExpansionStore.getState().setExpansion('expanded'));
    expect(header('a')).toHaveAttribute('aria-expanded', 'false');
    expect(header('b')).toHaveAttribute('aria-expanded', 'true');
  });

  /**
   * The largest conversation on a real mailbox held 1,088 messages. Rendering
   * all of them from one chevron stalled the page, and with "open" as a default
   * it would have happened on load.
   */
  it('shows the first entries of a long conversation and names the rest', () => {
    render(<ThreadResult threads={[thread('big', 100)]} onDrillDown={() => {}} />);

    expect(screen.getByText(`big entry ${ENTRY_CAP - 1}`)).toBeTruthy();
    expect(screen.queryByText(`big entry ${ENTRY_CAP}`)).toBeNull();

    fireEvent.click(screen.getByText(new RegExp(`and ${100 - ENTRY_CAP} more`)));
    expect(screen.getByText('big entry 99')).toBeTruthy();
  });
});
