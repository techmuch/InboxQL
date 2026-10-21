import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react';
import { Dashboard } from './Dashboard';
import { useQueryStore } from '../lib/filters';
import { mailChanged } from '../lib/mail';

/** The dashboard charts the Desk's query, and a click adds exactly its group. */
describe('Dashboard', () => {
  let calls: string[] = [];

  const terms = (q: string) => {
    let at = 0;
    const out: any[] = [];
    for (const text of q.split(' ').filter(Boolean)) {
      if (text === '|') break;
      const start = q.indexOf(text, at);
      at = start + text.length;
      const bare = text.replace(/^-/, '');
      out.push({ text, field: bare.includes(':') ? bare.slice(0, bare.indexOf(':')) : undefined, negated: text.startsWith('-'), start, end: at });
    }
    return out;
  };

  beforeEach(() => {
    vi.restoreAllMocks();
    calls = [];
    useQueryStore.getState().set('folder:inbox');
    vi.spyOn(window, 'fetch').mockImplementation(async input => {
      const url = String(input);
      const params = new URL(url, 'http://x').searchParams;
      let body: any = {};
      if (url.startsWith('/api/analytics')) {
        calls.push(url);
        body = {
          volume: [{ label: '2026-05-01', value: 3 }],
          senders: [{ label: 'alice@acme.com', value: 9, term: 'from:=alice@acme.com' }],
          topics: [
            { label: 'Invoices', value: 4, term: 'topic:=Invoices', kind: 'topic' },
            { label: 'lunch', value: 2, term: 'subject:lunch', kind: 'word' },
          ],
          importance: [{ label: 'low', value: 40, term: 'importance:low' }, { label: '(not judged)', value: 5 }],
          sentby: [{ label: 'system', value: 60, term: 'sentby:system' }],
        }[params.get('type')!];
      } else if (url.startsWith('/api/query/terms')) {
        body = { terms: terms(params.get('q') ?? ''), stages: [] };
      } else if (url.startsWith('/api/query/compose')) {
        const q = params.get('q') ?? '';
        if (params.get('add')) body = { query: `${q} ${params.get('add')}`.trim() };
        else if (params.get('remove')) body = { query: q.split(' ').filter(t => t !== params.get('remove')).join(' ') };
        else if (params.get('field') === 'folder') body = { query: q.replace(/folder:\S+/, params.get('term')!) };
        else body = { query: q };
      }
      return { ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body } as Response;
    });
  });

  it('shows the folder as a chip and keeps it when the additions are cleared', async () => {
    useQueryStore.getState().set('folder:inbox from:=alice@acme.com');
    render(<Dashboard />);
    expect(((await screen.findByLabelText('Folder')) as HTMLSelectElement).value).toBe('inbox');
    await screen.findByText('from:=alice@acme.com');
    expect(screen.queryByText('folder:inbox')).toBeNull();
    fireEvent.click(screen.getByText('Clear'));
    expect(useQueryStore.getState().text).toBe('folder:inbox');
  });

  it('switches the folder from the picker', async () => {
    render(<Dashboard />);
    fireEvent.change(await screen.findByLabelText('Folder'), { target: { value: 'allmail' } });
    await waitFor(() => expect(useQueryStore.getState().text).toBe('folder:allmail'));
  });

  it('adds the exact term of what was clicked, and names it in the heading', async () => {
    render(<Dashboard />);
    fireEvent.click(await screen.findByText('alice@acme.com'));
    await waitFor(() => expect(useQueryStore.getState().text).toBe('folder:inbox from:=alice@acme.com'));
    expect(await screen.findByText('Top Senders — alice@acme.com')).toBeTruthy();

    fireEvent.click(screen.getByText('Invoices'));
    await waitFor(() => expect(useQueryStore.getState().text).toContain('topic:=Invoices'));
    // A first word says what it is.
    expect(screen.getByText('lunch').closest('button')!.className).toContain('italic');
  });

  it('charts importance and kind of sender, and a group with no term is not a button to nowhere', async () => {
    render(<Dashboard />);
    const imp = await screen.findByRole('region', { name: 'Importance' });
    fireEvent.click(await within(imp).findByText('low'));
    await waitFor(() => expect(useQueryStore.getState().text).toContain('importance:low'));
    expect((within(imp).getByText('(not judged)').closest('button') as HTMLButtonElement).disabled).toBe(true);
    const kind = screen.getByRole('region', { name: 'Kind of sender' });
    fireEvent.click(within(kind).getByText('Systems'));
    await waitFor(() => expect(useQueryStore.getState().text).toContain('sentby:system'));
  });

  it('counts one year, steps between years, and opens on the year of an on: day', async () => {
    useQueryStore.getState().set('folder:inbox on:2024-03-02');
    render(<Dashboard />);
    await waitFor(() => expect(calls.some(c => c.includes('type=volume') && c.includes('year=2024'))).toBe(true));
    fireEvent.click(screen.getByLabelText('Previous year'));
    await waitFor(() => expect(calls.some(c => c.includes('year=2023'))).toBe(true));
  });

  it('refreshes when mail changes', async () => {
    render(<Dashboard />);
    await screen.findByText('alice@acme.com');
    const before = calls.length;
    act(() => mailChanged('local'));
    await waitFor(() => expect(calls.length).toBeGreaterThan(before));
  });

  it('says it charts mail when the Desk is on something else, and offers All Mail', async () => {
    useQueryStore.getState().set('in:tickets');
    render(<Dashboard />);
    const button = await screen.findByText('Chart All Mail');
    expect(calls).toHaveLength(0);
    fireEvent.click(button);
    expect(useQueryStore.getState().text.startsWith('folder:allmail')).toBe(true);
  });
});
