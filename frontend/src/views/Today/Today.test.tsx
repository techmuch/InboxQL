import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Today } from './index';
import { waited } from '../../lib/waited';
import { usePlaceStore } from '../../lib/places';

const owed = [{ id: 'm1', from: 'rafael@ortiz.example', fromName: 'Rafael Ortiz', subject: 'Cabinet hardware', preview: 'Brass or black?', date: new Date(Date.now() - 2 * 86_400_000).toISOString() }];

function serve(calls: string[]) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push(`${init?.method ?? 'GET'} ${decodeURIComponent(url)}`);
    const u = decodeURIComponent(url.replace(/\+/g, ' '));
    const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200 });
    if (u.includes('| count')) return json({ kind: 'count', total: u.includes('awaiting:me') ? 1 : 2 });
    if (u.includes('q=awaiting:me')) return json({ kind: 'messages', messages: owed });
    if (u.includes('| top from')) return json({ kind: 'groups', groups: [{ label: 'news@shop.example', value: 7 }] });
    if (u.includes('sentby:person')) return json({ kind: 'messages', messages: [] });
    if (u.includes('in:events next:2d')) return json({ kind: 'events', events: [] });
    if (u.includes('/api/work/disagreements')) return json({ open: 0 });
    if (u.includes('/labels/waiting')) return json({});
    if (u.includes('/api/messages/bulk')) return json({ matched: 7, changed: 7, undo: { labels: [] } });
    return json({});
  }));
}

beforeEach(() => { usePlaceStore.setState({ id: 'today', recent: [] }); });

describe('Today', () => {
  it('says what needs you in one sentence', async () => {
    serve([]);
    render(<Today />);
    await waitFor(() => expect(screen.getByTestId('today-summary').textContent).toMatch(/You owe 1 reply/));
  });

  it('lists replies owed with the person, the subject and how long it has waited', async () => {
    serve([]);
    render(<Today />);
    expect(await screen.findByText('Rafael Ortiz')).toBeTruthy();
    expect(screen.getByText('Cabinet hardware')).toBeTruthy();
    expect(screen.getByText('2 days')).toBeTruthy();
  });

  it('"Not waiting on me" records a ruling on the waiting element', async () => {
    const calls: string[] = [];
    serve(calls);
    render(<Today />);
    fireEvent.click(await screen.findByRole('button', { name: 'Not waiting on me: Cabinet hardware' }));
    await waitFor(() => expect(calls).toContain('PUT /api/messages/m1/labels/waiting'));
    expect(await screen.findByRole('status')).toBeTruthy();
  });

  it('archives a sender’s new mail as a group, and offers Undo', async () => {
    const calls: string[] = [];
    serve(calls);
    render(<Today />);
    fireEvent.click(await screen.findByRole('button', { name: 'Archive 7 messages from news@shop.example' }));
    expect(await screen.findByRole('button', { name: 'Undo' })).toBeTruthy();
    expect(calls.some(c => c.startsWith('POST /api/messages/bulk'))).toBe(true);
  });

  it('Show all goes to Replies I owe', async () => {
    serve([]);
    render(<Today />);
    fireEvent.click(screen.getAllByRole('button', { name: 'Show all' })[0]);
    expect(usePlaceStore.getState().id).toBe('replies');
  });
});

describe('waited', () => {
  it('speaks in hours, then days', () => {
    const now = Date.parse('2026-11-01T12:00:00Z');
    expect(waited('2026-11-01T11:50:00Z', now)).toBe('just now');
    expect(waited('2026-11-01T07:00:00Z', now)).toBe('5 h');
    expect(waited('2026-10-31T12:00:00Z', now)).toBe('1 day');
    expect(waited('2026-10-29T12:00:00Z', now)).toBe('3 days');
  });
});
