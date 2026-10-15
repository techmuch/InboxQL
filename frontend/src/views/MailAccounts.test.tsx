import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MailAccounts, statusOf, ago, type Account } from './MailAccounts';

const json = (body: unknown, status = 200) =>
  new Response(typeof body === 'string' ? body : JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

const base: Account = {
  id: 'work', name: 'Work', email: 'me@acme.com', host: 'imap.acme.com', port: 993, user: 'me@acme.com', ssl: true,
  smtpHost: 'smtp.acme.com', smtpPort: 587, lastSyncStatus: 'success', lastSyncError: '',
};

type Handler = (url: string, init?: RequestInit) => Response | undefined;
function server(handler: Handler) {
  const calls: { url: string; method: string; body: any }[] = [];
  vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input);
    calls.push({ url, method: init?.method ?? 'GET', body: init?.body ? JSON.parse(String(init.body)) : undefined });
    return handler(url, init) ?? json({});
  });
  return calls;
}

describe('account status', () => {
  const now = Date.parse('2026-10-14T12:00:00Z');
  it('says when it last synced, not when the newest mail arrived', () => {
    expect(ago('2026-10-14T11:56:00Z', now)).toBe('4 min ago');
    expect(statusOf({ ...base, lastSyncedAt: new Date(Date.now() - 4 * 60_000).toISOString() }).text).toBe('Synced 4 min ago');
    expect(statusOf({ ...base, lastSyncedAt: undefined }).text).toBe('Not synced yet');
  });
  it('names the problem when a sync failed', () => {
    const s = statusOf({ ...base, lastSyncStatus: 'error', lastSyncError: 'failed to connect: [AUTHENTICATIONFAILED] Invalid credentials' });
    expect(s.tone).toBe('bad');
    expect(s.text).toContain('rejected the sign-in');
  });
  it('says a disconnected account keeps its mail', () => {
    expect(statusOf({ ...base, disconnected: true }).text).toContain('mail is kept');
  });
});

describe('MailAccounts', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('adds an account from the address: finds the servers, signs in, then saves', async () => {
    let verified = false;
    const calls = server((url, init) => {
      if (url === '/api/accounts' && init?.method === 'POST') return json({ id: 'ada-fastmail' }, 201);
      if (url === '/api/accounts') return json([]);
      if (url.startsWith('/api/accounts/discover')) return json({ found: true, tried: [], settings: {
        provider: 'Fastmail', source: 'builtin', imap: { host: 'imap.fastmail.com', port: 993, tls: true },
        smtp: { host: 'smtp.fastmail.com', port: 465, tls: true }, username: 'ada@fastmail.com',
        note: 'Fastmail needs an app password.', passwordUrl: 'https://app.fastmail.com/settings/security/apps' } });
      if (url === '/api/accounts/verify') {
        const ok = verified; verified = true;
        return json({ imap: ok ? { ok: true } : { ok: false, kind: 'auth', advice: 'The server rejected the username or password.' },
          smtp: ok ? { ok: true } : { ok: false, kind: 'auth', advice: 'The server rejected the username or password.' } });
      }
      return undefined;
    });
    render(<MailAccounts />);
    fireEvent.click(await screen.findByText('Add one'));
    fireEvent.change(screen.getByLabelText('Email address'), { target: { value: 'ada@fastmail.com' } });
    fireEvent.click(screen.getByText('Continue'));
    expect(await screen.findByText('Fastmail')).toBeTruthy();
    expect(screen.getByText(/needs an app password/)).toBeTruthy();

    fireEvent.change(screen.getByLabelText('App password'), { target: { value: 'wrong' } });
    fireEvent.click(screen.getByText('Sign in and add'));
    expect((await screen.findAllByText(/rejected the username or password/)).length).toBe(2);
    expect(calls.some(c => c.url === '/api/accounts' && c.method === 'POST')).toBe(false);

    fireEvent.click(screen.getByText('Sign in and add'));
    await waitFor(() => expect(calls.some(c => c.url === '/api/accounts' && c.method === 'POST')).toBe(true));
    const saved = calls.find(c => c.url === '/api/accounts' && c.method === 'POST')!.body;
    expect(saved).toMatchObject({ email: 'ada@fastmail.com', host: 'imap.fastmail.com', smtpPort: 465, user: 'ada@fastmail.com', password: 'wrong' });
    // Saving starts the first sync.
    await waitFor(() => expect(calls.some(c => c.url === '/api/accounts/sync?id=ada-fastmail' && c.method === 'POST')).toBe(true));
  });

  it('asks Mozilla only when told to, and says what that reveals', async () => {
    const calls = server(url => {
      if (url === '/api/accounts') return json([]);
      if (url.startsWith('/api/accounts/discover')) return json({ found: false, tried: [{ source: 'mx', asked: 'tiny.org', result: 'nothing published' }] });
      return undefined;
    });
    render(<MailAccounts />);
    fireEvent.click(await screen.findByText('Add one'));
    fireEvent.change(screen.getByLabelText('Email address'), { target: { value: 'x@tiny.org' } });
    fireEvent.click(screen.getByText('Continue'));
    expect(await screen.findByText(/No settings found/)).toBeTruthy();
    expect(screen.getByText(/tells Mozilla the domain \(tiny\.org\)/)).toBeTruthy();
    expect(calls.filter(c => c.url.includes('ispdb=1'))).toHaveLength(0);
    fireEvent.click(screen.getByText("Ask Mozilla's directory"));
    await waitFor(() => expect(calls.filter(c => c.url.includes('ispdb=1'))).toHaveLength(1));
  });

  it("shows the server's reason when a save fails, instead of closing", async () => {
    server((url, init) => {
      if (url === '/api/accounts' && init?.method === 'POST') return new Response('the server changed, so the stored password no longer applies; type the password again', { status: 400 });
      if (url === '/api/accounts') return json([base]);
      if (url.startsWith('/api/accounts/stats')) return json({ totalMessages: 3, storageSize: 2048 });
      return undefined;
    });
    render(<MailAccounts />);
    fireEvent.click(await screen.findByText('Edit'));
    fireEvent.change(screen.getByLabelText('Server', { selector: '#f-imap' }), { target: { value: 'imap.other.com' } });
    fireEvent.click(screen.getByText('Save'));
    expect(await screen.findByText(/type the password again/)).toBeTruthy();
    expect(screen.getByText('Edit Work')).toBeTruthy();
  });

  it('removes only after the name is typed, and offers to disconnect instead', async () => {
    const calls = server(url => {
      if (url === '/api/accounts') return json([base]);
      if (url.startsWith('/api/accounts/stats')) return json({ totalMessages: 12408, storageSize: 1288490188 });
      if (url.startsWith('/api/accounts/footprint')) return json({ messages: 12408, bytes: 1288490188, drafts: 2 });
      return undefined;
    });
    render(<MailAccounts />);
    fireEvent.click(await screen.findByText('Remove'));
    expect(await screen.findByText(/12,408 messages \(1\.2 GB\)/)).toBeTruthy();
    expect(screen.getByText(/disconnect it instead/)).toBeTruthy();
    const go = screen.getByText('Remove and delete the mail').closest('button')!;
    expect(go.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText(/to confirm/), { target: { value: 'Work' } });
    expect(go.disabled).toBe(false);
    fireEvent.click(go);
    await waitFor(() => expect(calls.some(c => c.method === 'DELETE' && c.url === '/api/accounts?id=work')).toBe(true));
  });

  it('tests a saved account against its servers', async () => {
    const calls = server(url => {
      if (url === '/api/accounts') return json([base]);
      if (url === '/api/accounts/verify') return json({ imap: { ok: true }, smtp: { ok: false, kind: 'unreachable', advice: 'smtp.acme.com did not answer on that port.' } });
      return undefined;
    });
    render(<MailAccounts />);
    fireEvent.click(await screen.findByText('Test'));
    expect(await screen.findByText(/did not answer on that port/)).toBeTruthy();
    expect(calls.find(c => c.url === '/api/accounts/verify')!.body).toEqual({ id: 'work' });
  });
});
