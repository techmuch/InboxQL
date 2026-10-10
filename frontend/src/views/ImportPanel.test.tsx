import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { PermissionNotice } from './ImportPanel';

afterEach(() => vi.restoreAllMocks());

const blocked = (kind: 'app' | 'binary' | 'unsure') => ({
  available: true, readable: false, detail: '/Users/x/Library/Mail exists but cannot be read',
  grantee: kind === 'app'
    ? { name: 'iTerm', path: '/Applications/iTerm.app', kind, binary: '/Users/x/.iql/bin/iql' }
    : { name: 'iql', path: '/Users/x/.iql/bin/iql', kind, binary: '/Users/x/.iql/bin/iql' },
});

describe('PermissionNotice', () => {
  it('names the terminal, not iql, when a terminal started InboxQL', () => {
    render(<PermissionNotice detection={blocked('app')} onRetry={() => {}} />);
    expect(screen.getAllByText(/iTerm/).length).toBeGreaterThan(0);
    expect(screen.getByText('Show iTerm in Finder')).toBeTruthy();
    expect(screen.queryByText('Show iql in Finder')).toBeNull();
  });

  it('offers iql itself under the login service', () => {
    render(<PermissionNotice detection={blocked('binary')} onRetry={() => {}} />);
    expect(screen.getByText('Show iql in Finder')).toBeTruthy();
  });

  it('asks the server to open Settings, and sends no path to reveal', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: true, text: async () => '' });
    vi.stubGlobal('fetch', fetchSpy);
    render(<PermissionNotice detection={blocked('app')} onRetry={() => {}} />);
    fireEvent.click(screen.getByText('Open Full Disk Access settings'));
    fireEvent.click(screen.getByText('Show iTerm in Finder'));
    await Promise.resolve();
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/import/disk-access/open');
    expect(fetchSpy.mock.calls[1][0]).toBe('/api/import/disk-access/reveal');
    expect(JSON.parse(fetchSpy.mock.calls[1][1].body)).toEqual({ target: 'grantee' });
  });
});
