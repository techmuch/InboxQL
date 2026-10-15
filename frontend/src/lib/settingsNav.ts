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

/**
 * A tab inside a section — AI Configuration has three. Kept and announced the
 * same way as a section, for a panel that is not mounted yet or one that is.
 */
let pendingTab: string | null = null;
export const SETTINGS_TAB_EVENT = 'iql:settings-tab';

export function requestSettingsTab(tab: string) {
  pendingTab = tab;
  window.dispatchEvent(new CustomEvent(SETTINGS_TAB_EVENT, { detail: tab }));
}

export function takeSettingsTab(): string | null {
  const t = pendingTab;
  pendingTab = null;
  return t;
}

/**
 * Scroll to one setting and flash it, once its section has rendered. The
 * section mounts after the tab does, and some load before they draw, so it is
 * looked for for a few seconds rather than once.
 */
export function revealSetting(id: string, within?: string) {
  let tries = 0;
  const find = () => {
    tries++;
    // The setting itself if it is drawn; after a moment, the place it would
    // be — a section that loads first may not draw it at all.
    const el = document.getElementById(`setting-${id}`)
      ?? (within && tries > 10 ? document.getElementById(`setting-${within}`) : null);
    if (!el) {
      if (tries < 40) setTimeout(find, 100);
      return;
    }
    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    el.classList.remove('iql-setting-flash');
    void el.offsetWidth; // restart the animation if it is already running
    el.classList.add('iql-setting-flash');
    setTimeout(() => el.classList.remove('iql-setting-flash'), 2200);
  };
  setTimeout(find, 50);
}
