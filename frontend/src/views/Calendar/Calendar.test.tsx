import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Calendar } from './index';
import { useCalendarNav } from '../../lib/calendarNav';

/** A proposal in "Waiting for you" opens on the calendar, selected. */
describe('Calendar — waiting for you', () => {
  const proposedStart = '2026-11-29T00:00:00-05:00';
  const occurrence = {
    eventId: 'ev_baptism', calendarId: 'local', uid: 'u1', start: proposedStart, end: '2026-11-30T00:00:00-05:00',
    allDay: true, summary: 'Baptism: Nathaniel Brown', status: 'confirmed', showAs: 'busy', recurring: false,
    proposed: true, attendees: 0,
  };

  beforeEach(() => {
    vi.restoreAllMocks();
    useCalendarNav.setState({ view: 'month', anchor: new Date(2026, 9, 22).toISOString(), selected: null });
    vi.spyOn(window, 'fetch').mockImplementation(async input => {
      const url = new URL(String(input), 'http://x');
      let body: unknown = {};
      if (url.pathname === '/api/calendar/calendars') {
        body = { calendars: [{ id: 'local', name: 'My calendar', color: '#2563eb', writable: true, primary: true }], accounts: [] };
      } else if (url.pathname === '/api/calendar/changes') {
        body = { changes: [{
          id: 'chg_1', eventId: 'ev_baptism', calendarId: 'local', op: 'create', state: 'proposed', origin: 'agent',
          createdAt: '2026-10-20T09:57:13Z',
          payload: { summary: 'Create “Baptism: Nathaniel Brown”', event: { Start: proposedStart, AllDay: true } },
        }] };
      } else if (url.pathname === '/api/calendar/occurrences') {
        const from = new Date(url.searchParams.get('from')!);
        const to = new Date(url.searchParams.get('to')!);
        const at = new Date(proposedStart);
        body = { occurrences: at >= from && at < to ? [occurrence] : [] };
      } else if (url.pathname === '/api/calendar/events/ev_baptism') {
        body = { event: { id: 'ev_baptism', calendarId: 'local', summary: occurrence.summary, start: proposedStart,
          end: occurrence.end, allDay: true, proposed: true, status: 'confirmed', attendees: [] }, writable: true, changes: [] };
      } else if (url.pathname === '/api/board/due') {
        body = [];
      }
      return { ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body } as Response;
    });
  });

  it('opens the proposed day with the event selected, and says when it is', async () => {
    render(<Calendar />);
    const item = await screen.findByRole('button', { name: 'Create “Baptism: Nathaniel Brown”' });
    expect(screen.getByText(/Waiting for you · 1/)).toBeTruthy();
    expect(screen.getByText(/Sun.*29.*Nov|Nov.*29/)).toBeTruthy();

    fireEvent.click(item);
    await waitFor(() => expect(useCalendarNav.getState().selected).toBe('ev_baptism'));
    expect(new Date(useCalendarNav.getState().anchor).getMonth()).toBe(10);
    expect(await screen.findByRole('heading', { name: 'November 2026' })).toBeTruthy();
    // The details beside it, and the list says which one is open.
    await waitFor(() => expect(screen.getAllByText('Baptism: Nathaniel Brown').length).toBeGreaterThan(1));
    expect(item.closest('li')!.className).toContain('ring-primary');
  });
});
