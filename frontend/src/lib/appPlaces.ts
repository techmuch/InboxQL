/**
 * The places a guide can open: `[Annotators](#open/annotators)`.
 *
 * A short vocabulary of tabs, not an address into the layout, so a guide
 * keeps working when the layout changes. appPlaces.test.ts checks every
 * `#open/` and `#setting/` in the guides against this list and the settings
 * index, so a guide cannot link to something that is not there.
 */
import { CALENDAR_TAB } from './calendarNav';
import { openErrorLog, openTool, DESK_TAB } from './tabs';

export const PLACES: Record<string, { label: string; open: () => void }> = {
  desk: { label: 'Desk', open: () => openTool(DESK_TAB, 'Desk') },
  calendar: { label: 'Calendar', open: () => openTool(CALENDAR_TAB, 'Calendar') },
  board: { label: 'Board', open: () => openTool('board', 'Board') },
  annotators: { label: 'Annotators', open: () => openTool('annotators', 'Annotators') },
  approvals: { label: 'Waiting for approval', open: () => openTool('approvals', 'Waiting for approval') },
  log: { label: 'Log', open: () => openErrorLog() },
  windows: { label: 'Windows', open: () => openTool('windows', 'Windows') },
  settings: { label: 'Settings', open: () => openTool('settings', 'Settings') },
};

/** Open a place by name. Unknown names do nothing; the test keeps the guides from using one. */
export function openPlace(name: string) {
  PLACES[name]?.open();
}
