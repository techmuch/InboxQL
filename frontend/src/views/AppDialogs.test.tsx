import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { commandRegistry } from 'nexus-shell';
import { AppDialogs } from './AppDialogs';
import { useAppUi } from '../lib/appUi';

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

describe('AppDialogs', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    for (const k of ['compose', 'task', 'palette', 'keys'] as const) useAppUi.getState().set(k, false);
  });

  it('sends a new message, with a few seconds to undo it', async () => {
    const posted: { url: string; body: any }[] = [];
    vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === '/api/accounts') return json([{ id: 'acc', name: 'Work', email: 'me@x.com' }]);
      if (init?.method === 'POST') posted.push({ url, body: init.body ? JSON.parse(String(init.body)) : null });
      if (url === '/api/drafts') return json({ draft: { id: 'd1', subject: 'Hello' }, undoMs: 10000 }, 201);
      if (url === '/api/drafts/d1/undo') return json({ draft: { id: 'd1', status: 'draft' } });
      return json({});
    });
    useAppUi.getState().set('compose', true);
    render(<AppDialogs />);
    await waitFor(() => expect(screen.getByLabelText('To')).toBeTruthy());
    fireEvent.change(screen.getByLabelText('To'), { target: { value: 'ada@example.com' } });
    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'Hello' } });
    fireEvent.click(screen.getByText('Send'));
    await waitFor(() => expect(screen.getByText(/Sending “Hello” in \d+s/)).toBeTruthy());
    expect(posted[0]).toMatchObject({ url: '/api/drafts', body: { accountId: 'acc', to: ['ada@example.com'], subject: 'Hello', send: true } });
    // The window closes; the toast stays.
    expect(useAppUi.getState().compose).toBe(false);

    fireEvent.click(screen.getByText('Undo'));
    await waitFor(() => expect(screen.getByText(/back in Drafts/)).toBeTruthy());
    expect(posted.map(p => p.url)).toContain('/api/drafts/d1/undo');
  });

  it('saves a draft without sending it', async () => {
    const posted: any[] = [];
    vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === '/api/accounts') return json([{ id: 'acc', name: 'Work', email: 'me@x.com' }]);
      if (url === '/api/drafts') {
        posted.push(JSON.parse(String(init?.body)));
        return json({ draft: { id: 'd2' }, note: 'Saved as a draft.' }, 201);
      }
      return json({});
    });
    useAppUi.getState().set('compose', true);
    render(<AppDialogs />);
    await waitFor(() => expect(screen.getByLabelText('To')).toBeTruthy());
    fireEvent.click(screen.getByText('Save draft'));
    await waitFor(() => expect(screen.getByText('Saved as a draft.')).toBeTruthy());
    expect(posted[0].send).toBeUndefined();
  });

  it('adds a task to the board as To do', async () => {
    const posted: any[] = [];
    vi.spyOn(window, 'fetch').mockImplementation(async (_input, init) => {
      posted.push(JSON.parse(String(init?.body)));
      return json({ id: 't1' }, 201);
    });
    const run = vi.spyOn(commandRegistry, 'executeCommand').mockImplementation(() => {});
    useAppUi.getState().set('task', true);
    render(<AppDialogs />);
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Send the contract' } });
    fireEvent.click(screen.getByText('Add to board'));
    await waitFor(() => expect(run).toHaveBeenCalledWith('iql.open-board'));
    expect(posted[0]).toMatchObject({ title: 'Send the contract', status: 'todo', origin: 'human' });
    expect(useAppUi.getState().task).toBe(false);
  });

  it('offers commands and saved queries in Find anything', async () => {
    commandRegistry.registerCommand({ id: 'test.go-somewhere', label: 'Go to Somewhere', execute: () => {} });
    vi.spyOn(window, 'fetch').mockImplementation(async () => json([{ title: 'Acme invoices', query: 'from:*@acme.com' }]));
    useAppUi.getState().set('palette', true);
    render(<AppDialogs />);
    expect(screen.getByText('Go to Somewhere')).toBeTruthy();
    await waitFor(() => expect(screen.getByText('Saved query: Acme invoices')).toBeTruthy());
  });

  it('finds a setting by any of its words and opens Settings at it', async () => {
    vi.spyOn(window, 'fetch').mockImplementation(async () => json([]));
    const sections: string[] = [];
    const onSection = (e: Event) => sections.push((e as CustomEvent<string>).detail);
    window.addEventListener('iql:settings-section', onSection);
    useAppUi.getState().set('palette', true);
    render(<AppDialogs />);
    // Not listed until something is typed.
    expect(screen.queryByText(/Settings › General › Color theme/)).toBeNull();
    fireEvent.change(screen.getByPlaceholderText(/Find anything/), { target: { value: 'dark mode' } });
    const row = await screen.findByText('Settings › General › Color theme');
    fireEvent.click(row);
    expect(sections).toContain('general');
    window.removeEventListener('iql:settings-section', onSection);
  });
});
