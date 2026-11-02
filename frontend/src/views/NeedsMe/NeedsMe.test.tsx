import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Replies } from './Replies';
import { Outbox } from './Outbox';
import { queryCount } from '../../lib/counts';

const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200 });
const day = 86_400_000;

describe('Replies I owe', () => {
  it('splits the last two weeks from older, and turns round to waiting on others', async () => {
    const calls: string[] = [];
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = decodeURIComponent(url.replace(/\+/g, ' '));
      calls.push(`${init?.method ?? 'GET'} ${u}`);
      if (u.includes('| count')) return json({ kind: 'count', total: u.includes('awaiting:me') ? 2 : 1 });
      if (u.includes('awaiting:me')) return json({ kind: 'messages', messages: [
        { id: 'a', from: 'a@x', fromName: 'Ana', subject: 'Recent', date: new Date(Date.now() - 2 * day).toISOString() },
        { id: 'b', from: 'b@x', fromName: 'Ben', subject: 'Ancient', date: new Date(Date.now() - 30 * day).toISOString() },
      ] });
      if (u.includes('awaiting:them')) return json({ kind: 'messages', messages: [{ id: 'c', from: 'me@x', to: ['cy@x'], subject: 'Asked', date: new Date().toISOString() }] });
      return json({});
    }));
    render(<Replies />);
    expect(await screen.findByText('Recent')).toBeTruthy();
    expect(screen.getByRole('heading', { name: /Older than two weeks/ })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Waiting on no one: Ancient' }));
    await waitFor(() => expect(calls).toContain('PUT /api/messages/b/labels/waiting'));
    fireEvent.click(screen.getByRole('button', { name: /Waiting on others/ }));
    expect(await screen.findByText('Asked')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Nudge' })).toBeTruthy();
  });
});

describe('Drafts and outbox', () => {
  it('sorts drafts into waiting to send, failed and drafts, and says who wrote each', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ drafts: [
      { id: 'q', to: ['a@x'], subject: 'Queued one', body: 'hi', status: 'queued', origin: 'agent' },
      { id: 'f', to: ['b@x'], subject: 'Failed one', body: 'big', status: 'failed', origin: 'human', lastError: '552 too large' },
      { id: 'd', to: [], subject: 'Draft one', body: '', status: 'draft', origin: 'human' },
      { id: 's', to: ['c@x'], subject: 'Sent one', body: '', status: 'sent', origin: 'human' },
    ] })));
    render(<Outbox />);
    expect(await screen.findByText('· Queued one')).toBeTruthy();
    expect(screen.getByText('written by an agent')).toBeTruthy();
    expect(screen.getByText(/552 too large/)).toBeTruthy();
    expect(screen.queryByText('· Sent one')).toBeNull();
    expect(screen.getByTestId('outbox-summary').textContent).toMatch(/1 waiting for you to send, 1 failed, 1 draft\./);
  });
});

describe('queryCount', () => {
  it('reads a total of zero, which the server leaves out, as zero', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ kind: 'count', count: 1 })));
    expect(await queryCount('in:drafts origin:agent')).toBe(0);
  });
});
