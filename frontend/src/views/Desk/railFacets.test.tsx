import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Rail } from './Rail';

/** The rail's numbers preview the click once there are facets. */
describe('Rail counts inside the query', () => {
  const base = {
    folders: [
      { id: 'inbox', label: 'Inbox', badge: 40 },
      { id: 'drafts', label: 'Drafts', badge: 3 },
      { id: 'spam', label: 'Spam', badge: 9 },
    ],
    hiddenFolders: [],
    saved: [],
    annotated: [{ id: 'a1', name: 'receipts', kind: 'extract', progress: { matched: 300 } } as any],
    labels: [
      { name: 'banking', system: false, total: 312, unread: 5 },
      { name: 'travel', system: false, total: 80, unread: 0 },
    ],
    queryText: '',
    onFolder: () => {},
    onQuery: () => {},
    annotationQuery: (a: any) => `extract:${a.name}`,
  };

  const row = (name: string) => screen.getByText(name).closest('button') as HTMLButtonElement;

  it('shows global numbers without facets', () => {
    render(<Rail {...base} />);
    expect(row('banking').textContent).toContain('5');
    expect(row('receipts').textContent).toContain('300');
    expect(screen.queryByText(/^In /)).toBeNull();
  });

  it('counts labels inside the frame, dims a zero, and says what it is inside', () => {
    render(<Rail {...base} facets={{
      within: 'Starred', rest: '', total: 40, unread: 2,
      labels: [{ name: 'banking', total: 12, unread: 0 }],
      annotators: [{ name: 'receipts', total: 7, unread: 0 }],
    }} />);
    expect(screen.getByText('In Starred')).toBeTruthy();
    expect(row('banking').textContent).toContain('12');
    expect(row('banking').title).toBe('12 of 40 in Starred');
    expect(row('travel').className).toContain('opacity-40');
    expect(row('receipts').textContent).toContain('7');
    // Nothing added: the folders keep their own badges.
    expect(row('Inbox').textContent).toContain('40');
    expect(screen.queryByText(/^With /)).toBeNull();
  });

  it('counts what was added inside each folder; drafts keep their own', () => {
    render(<Rail {...base} facets={{
      within: 'Inbox › label:banking', rest: 'label:banking', total: 12, unread: 0,
      labels: [{ name: 'banking', total: 12, unread: 0 }],
      annotators: [],
      folders: [{ name: 'inbox', total: 12, unread: 0 }, { name: 'spam', total: 0, unread: 0 }],
    }} />);
    expect(screen.getByText('With label:banking')).toBeTruthy();
    expect(row('Inbox').textContent).toContain('12');
    expect(row('Inbox').title).toBe('12 with label:banking in Inbox');
    expect(row('Spam').className).toContain('opacity-40');
    expect(row('Drafts').textContent).toContain('3');
  });

  it('dims the old numbers while new ones load', () => {
    render(<Rail {...base} facetsStale facets={{
      within: 'Inbox', rest: '', total: 40, unread: 0,
      labels: [{ name: 'banking', total: 12, unread: 0 }], annotators: [],
    }} />);
    expect(row('banking').querySelector('.tabular-nums')!.className).toContain('opacity-40');
  });
});
