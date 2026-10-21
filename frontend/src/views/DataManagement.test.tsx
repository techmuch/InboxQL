import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { DataManagement } from './DataManagement';

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

const GB = 1 << 30;
const state = (over: Record<string, unknown> = {}) => ({
  backups: [
    { name: 'inboxql-pre-0.0.99-20261020-230018.db', at: '2026-10-20T23:00:18Z', bytes: 9.4 * GB, reason: 'update',
      version: '0.0.99', key: false, attachments: false, checked: 'ok' },
    { name: 'inboxql-pre-0.0.87-20261014-202942.db', at: '2026-10-14T20:29:42Z', bytes: 7.6 * GB, reason: 'update',
      version: '0.0.87', key: false, attachments: false, legacy: true },
  ],
  totalBytes: 17 * GB, freeBytes: 4.6 * GB, databaseBytes: 9.4 * GB, needBytes: 10.4 * GB,
  attachmentFiles: 1200, attachmentBytes: 3 * GB,
  policy: { keepUpdate: 2, keepScheduled: 2, keepRestore: 2, schedule: 'off' },
  canRestart: true, jobs: [],
  ...over,
});

type Call = { method: string; url: string; body: any };

const serve = (calls: Call[], s = state()) =>
  vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    if (url === '/api/backups' && method === 'GET') return json(s);
    if (url === '/api/backups' && method === 'POST') return json({ id: 'backup-1', kind: 'backup', status: 'running', done: 0, total: 9000 }, 202);
    if (url.startsWith('/api/backups/restore')) return json({ restoring: 'x', instanceId: 'old' }, 202);
    return json({ ok: true });
  });

describe('DataManagement', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('lists the backups, with the room they take and the room there is', async () => {
    serve([]);
    render(<DataManagement />);
    await waitFor(() => expect(screen.getByText('inboxql-pre-0.0.99-20261020-230018.db')).toBeTruthy());
    expect(screen.getByText('17.0 GB')).toBeTruthy();
    expect(screen.getByText('4.6 GB')).toBeTruthy();
    expect(screen.getAllByText('Before an update')).toHaveLength(2);
    expect(screen.getByText('newest')).toBeTruthy();
    // Short of room: said before the button is pressed.
    expect(screen.getByText(/A backup needs about 10.4 GB/)).toBeTruthy();
    // Not yet checked: offered.
    expect(screen.getByText('Check it')).toBeTruthy();
  });

  it('backs up now with what was asked for, and warns about the key', async () => {
    const calls: Call[] = [];
    serve(calls);
    render(<DataManagement />);
    await waitFor(() => expect(screen.getByText('Back up now')).toBeTruthy());
    fireEvent.click(screen.getByLabelText('Include the vault key'));
    expect(screen.getByText(/can decrypt your account passwords/)).toBeTruthy();
    fireEvent.click(screen.getByText('Back up now'));
    await waitFor(() => expect(calls.some(c => c.method === 'POST' && c.url === '/api/backups')).toBe(true));
    expect(calls.find(c => c.method === 'POST' && c.url === '/api/backups')!.body).toEqual({ key: true, attachments: false });
  });

  it('asks before deleting a backup, and asks for the word before restoring one', async () => {
    const calls: Call[] = [];
    serve(calls);
    render(<DataManagement />);
    await waitFor(() => expect(screen.getByText('inboxql-pre-0.0.87-20261014-202942.db')).toBeTruthy());

    fireEvent.click(screen.getByLabelText('Delete inboxql-pre-0.0.87-20261014-202942.db'));
    fireEvent.click(screen.getByText('Delete'));
    await waitFor(() => expect(calls.some(c => c.method === 'DELETE')).toBe(true));
    expect(calls.find(c => c.method === 'DELETE')!.url).toBe('/api/backups?name=inboxql-pre-0.0.87-20261014-202942.db');

    fireEvent.click(screen.getAllByText('Restore…')[0]);
    const go = screen.getByText('Restore and restart') as HTMLButtonElement;
    expect(go.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('Type restore to confirm'), { target: { value: 'restore' } });
    expect(go.disabled).toBe(false);
  });

  it('saves the keep rules and the schedule', async () => {
    const calls: Call[] = [];
    serve(calls);
    render(<DataManagement />);
    const rules = await screen.findByRole('region', { name: 'Keep rules' });
    fireEvent.change(within(rules).getByLabelText('Back up automatically'), { target: { value: 'weekly' } });
    fireEvent.change(within(rules).getByLabelText('Backups taken before updates'), { target: { value: '1' } });
    fireEvent.click(within(rules).getByText('Save'));
    await waitFor(() => expect(calls.some(c => c.method === 'PUT')).toBe(true));
    expect(calls.find(c => c.method === 'PUT')!.body).toEqual({ keepUpdate: 1, keepScheduled: 2, keepRestore: 2, schedule: 'weekly' });
  });

  it('offers a backup before erasing, and erases only after the word', async () => {
    const calls: Call[] = [];
    serve(calls, state({ backups: [] }));
    render(<DataManagement />);
    await waitFor(() => expect(screen.getByText('There is no backup of this mailbox.')).toBeTruthy());
    fireEvent.click(screen.getByText('Back up first'));
    await waitFor(() => expect(calls.some(c => c.method === 'POST' && c.url === '/api/backups')).toBe(true));

    fireEvent.click(screen.getByText('Erase All Local Data'));
    const erase = screen.getByText('Erase') as HTMLButtonElement;
    expect(erase.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('Type erase to confirm'), { target: { value: 'erase' } });
    expect(erase.disabled).toBe(false);
    expect(calls.some(c => c.url === '/api/data')).toBe(false);
  });
});
