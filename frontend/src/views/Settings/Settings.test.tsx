import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SettingsOverview } from './Overview';
import { KeyboardHelp } from '../AppDialogs';
import { usePlaceStore } from '../../lib/places';
import { PLACE_HELP } from '../../lib/placeHelp';

const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200 });

describe('the settings overview', () => {
  it('leads with what needs attention, and opens a section', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      if (url === '/api/health') return json({ checks: [{ name: 'database', status: 'ok', detail: 'fine' }, { name: 'attachment text', status: 'warn', detail: '914 files never read', remedy: 'iql maintenance text' }] });
      if (url === '/api/backups') return json({ backups: [{ at: new Date(Date.now() - 20 * 86_400_000).toISOString() }] });
      if (url === '/api/system/update') return json({ newer: true, latest: '2026.11.2.1', current: '2026.11.1.1' });
      if (url === '/api/accounts') return json([{ id: 'gmail' }]);
      return json({});
    }));
    const open = vi.fn();
    render(<SettingsOverview groups={[{ id: 'data', label: 'Your data', about: 'Backups.', sections: [{ id: 'data', label: 'Backups and data' }] }]} open={open} />);
    expect(await screen.findByText(/914 files never read/)).toBeTruthy();
    expect(await screen.findByText(/2026.11.2.1 is ready/)).toBeTruthy();
    expect(await screen.findByText(/The last backup was 20 days ago/)).toBeTruthy();
    expect(screen.getByTestId('settings-summary').textContent).toMatch(/3 things need you/);
    fireEvent.click(screen.getByRole('button', { name: 'Backups' }));
    expect(open).toHaveBeenCalledWith('data');
  });
});

describe('help on a screen', () => {
  it('? shows what the screen is for and its keys, above every key', () => {
    usePlaceStore.setState({ id: 'ahead', recent: [] });
    render(<KeyboardHelp onClose={() => {}} />);
    expect(screen.getByRole('heading', { name: 'On this screen: Ahead' })).toBeTruthy();
    expect(screen.getByText('Mark a date as important')).toBeTruthy();
  });

  it('every place in the help names a guide that exists', async () => {
    const guides = ['getting-started', 'calendar', 'contacts', 'tickets', 'reading', 'desk', 'labels', 'command-line', 'settings'];
    for (const [place, h] of Object.entries(PLACE_HELP)) {
      if (h.guide) expect(guides, place).toContain(h.guide);
    }
  });
});
