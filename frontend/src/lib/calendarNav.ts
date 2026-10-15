import { create } from 'zustand';
import { openTool } from './tabs';

export const CALENDAR_TAB = 'calendar';

export type CalendarView = 'day' | 'week' | 'month' | 'agenda';

/**
 * Where the calendar is looking, kept outside the tab so other tabs can point
 * it: a message's invitation card opens the calendar at that event.
 */
interface CalendarNav {
  view: CalendarView;
  anchor: string; // ISO date the view is around
  selected: string | null; // event id whose card is open
  setView: (v: CalendarView) => void;
  setAnchor: (d: Date) => void;
  select: (id: string | null) => void;
  /** Bumped to ask the calendar to open a new event's editor. */
  newRequest: number;
}

const readView = (): CalendarView => {
  try {
    const v = localStorage.getItem('iql:calendar-view');
    if (v === 'day' || v === 'week' || v === 'month' || v === 'agenda') return v;
  } catch { /* storage unavailable */ }
  return 'week';
};

export const useCalendarNav = create<CalendarNav>((set) => ({
  view: readView(),
  anchor: new Date().toISOString(),
  selected: null,
  newRequest: 0,
  setView: (view) => {
    try { localStorage.setItem('iql:calendar-view', view); } catch { /* best effort */ }
    set({ view });
  },
  setAnchor: (d) => set({ anchor: d.toISOString() }),
  select: (selected) => set({ selected }),
}));

/** Open the Calendar tab, optionally at an event and its date. */
export function openCalendar(at?: { date?: string; eventId?: string }) {
  const nav = useCalendarNav.getState();
  if (at?.date) nav.setAnchor(new Date(at.date));
  if (at?.eventId) nav.select(at.eventId);
  openTool(CALENDAR_TAB, 'Calendar');
}

/** File → New event: open the calendar with a new event's editor. */
export function newCalendarEvent() {
  useCalendarNav.setState(s => ({ newRequest: s.newRequest + 1 }));
  openTool(CALENDAR_TAB, 'Calendar');
}
