/**
 * Opening Settings at a particular section — Help → About opens System.
 *
 * Settings is a tab like any other, so it may not be mounted yet when the
 * request is made. The request is kept until a Settings view takes it, and
 * also announced, for one that is already open.
 */
let pending: string | null = null;

export const SETTINGS_SECTION_EVENT = 'iql:settings-section';

export function requestSettingsSection(id: string) {
  pending = id;
  window.dispatchEvent(new CustomEvent(SETTINGS_SECTION_EVENT, { detail: id }));
}

const STORED = 'iql:settings-section';

export function takeSettingsSection(): string | null {
  let id = pending;
  pending = null;
  // A section asked for before a restart, read once after the reload.
  try {
    const stored = sessionStorage.getItem(STORED);
    if (stored) {
      sessionStorage.removeItem(STORED);
      id = id ?? stored;
    }
  } catch { /* storage unavailable: start at the default section */ }
  return id;
}

/** Return to a section after the page reloads — across a restart. */
export function rememberSettingsSection(id: string) {
  try { sessionStorage.setItem(STORED, id); } catch { /* best effort */ }
}
