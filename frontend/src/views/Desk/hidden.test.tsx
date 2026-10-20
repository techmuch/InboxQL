import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { HiddenNote, hiddenText } from './HiddenNote';
import { summarise } from './Timeline';
import type { Thread } from './api';

describe('what a query left out of Trash and Spam', () => {
  it('says how many, and where', () => {
    expect(hiddenText({ hidden: 0, leftOut: ['trash', 'spam'] })).toBeNull();
    expect(hiddenText({})).toBeNull();
    expect(hiddenText({ hidden: 3, leftOut: ['trash', 'spam'] })).toBe('3 more in Trash or Spam');
    expect(hiddenText({ hidden: 1, leftOut: ['spam'] })).toBe('1 more in Spam');
    expect(hiddenText({ hidden: 1000, leftOut: ['trash'] })).toBe('1,000+ more in Trash');
  });

  it('offers them back', () => {
    const onShow = vi.fn();
    render(<HiddenNote result={{ hidden: 2, leftOut: ['trash'] }} onShow={onShow} />);
    expect(screen.getByRole('status').textContent).toContain('2 more in Trash, not shown because the query does not mention it.');
    fireEvent.click(screen.getByRole('button', { name: 'Show them' }));
    expect(onShow).toHaveBeenCalledOnce();
  });

  it('is not there when nothing was left out', () => {
    const { container } = render(<HiddenNote result={{ hidden: 0 }} onShow={() => {}} />);
    expect(container.textContent).toBe('');
  });

  it('a conversation row says what it holds back', () => {
    const thread: Thread = {
      key: 'k', entries: [], messageCount: 2, ticketCount: 0, draftCount: 0,
      inTrash: 1, start: '', end: '',
    };
    expect(summarise(thread)).toBe('2 messages · 1 in Trash');
  });
});
