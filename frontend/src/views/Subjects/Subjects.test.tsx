import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Topic } from './Topic';
import { useSubjectStore } from '../../lib/subjects';
import { usePlaceStore } from '../../lib/places';

const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200 });

beforeEach(() => {
  usePlaceStore.setState({ id: 'topic', recent: [] });
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const u = decodeURIComponent(url.replace(/\+/g, ' '));
    if (u.startsWith('/api/topics')) return json({ topics: [{ topic: 'kitchen', count: 52 }, { topic: 'church', count: 30 }] });
    if (u.includes('| count')) return json({ kind: 'count', total: 52 });
    if (u.includes('awaiting:me')) return json({ kind: 'messages', messages: [{ id: 'm1', from: 'r@x', fromName: 'Rafael', subject: 'Hardware finish', date: new Date().toISOString() }] });
    if (u.includes('awaiting:them')) return json({ kind: 'messages', messages: [] });
    if (u.includes('sum receipts.total')) return json({ kind: 'groups', groups: [{ label: 'total', value: 5412.2 }] });
    if (u.includes('in:tickets')) return json({ kind: 'tickets', tickets: [{ id: 't', title: 'Order hardware', status: 'todo' }] });
    if (u.includes('| top from')) return json({ kind: 'groups', groups: [{ label: 'r@x', value: 38 }] });
    return json({ kind: 'messages', messages: [] });
  }));
});

describe('topics', () => {
  it('lists topics, and one opens as a page with its loops, money, cards and people', async () => {
    useSubjectStore.setState({ topic: null });
    render(<Topic />);
    fireEvent.click(await screen.findByRole('button', { name: /kitchen/ }));
    expect(useSubjectStore.getState().topic).toBe('kitchen');
  });

  it('the page shows what is open, what it cost and who is in it', async () => {
    useSubjectStore.setState({ topic: 'kitchen' });
    render(<Topic />);
    expect(await screen.findByRole('heading', { level: 1, name: 'kitchen' })).toBeTruthy();
    expect(await screen.findByText('Hardware finish')).toBeTruthy();
    await waitFor(() => expect(screen.getByText('$5,412.20')).toBeTruthy());
    expect(screen.getByText('Order hardware')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'r@x' })).toBeTruthy();
  });
});
