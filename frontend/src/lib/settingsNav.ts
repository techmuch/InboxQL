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

export function takeSettingsSection(): string | null {
  const id = pending;
  pending = null;
  return id;
}
