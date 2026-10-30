import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

const openDoc = vi.fn();
vi.mock('../lib/docsNav', () => ({ openDoc: (...a: unknown[]) => openDoc(...a) }));

import { AppLockup } from './AppLockup';
import { appVersion, notesAnchor } from '../lib/version';

describe('AppLockup', () => {
  beforeEach(() => { localStorage.clear(); openDoc.mockClear(); });

  it('names the version as a way to its release notes, and opens them at this release', () => {
    render(<AppLockup />);
    const button = screen.getByRole('button', { name: `Version ${appVersion}, release notes` });
    expect(button.textContent).toContain(`v${appVersion}`);
    fireEvent.click(button);
    expect(openDoc).toHaveBeenCalledWith('release-notes', notesAnchor(appVersion));
  });

  it('says nothing is new on a first visit', () => {
    render(<AppLockup />);
    expect(screen.getByRole('button').getAttribute('aria-label')).not.toMatch(/new/);
    expect(localStorage.getItem('iql.releaseNotes.seen')).toBe(appVersion);
  });

  it('after an update, says there is something new until the notes are opened', () => {
    localStorage.setItem('iql.releaseNotes.seen', '2026.1.1.1');
    const { unmount } = render(<AppLockup />);
    const button = screen.getByRole('button', { name: `Version ${appVersion}, release notes, new since you last looked` });
    fireEvent.click(button);
    expect(screen.getByRole('button').getAttribute('aria-label')).toBe(`Version ${appVersion}, release notes`);
    unmount();
    render(<AppLockup />);
    expect(screen.getByRole('button').getAttribute('aria-label')).toBe(`Version ${appVersion}, release notes`);
  });

  it('turns a version into its notes anchor', () => {
    expect(notesAnchor('2026.10.30.2')).toBe('v2026-10-30-2');
    expect(notesAnchor('2026.1.11.1')).not.toBe(notesAnchor('2026.11.1.1'));
  });
});

describe('AppLockup, as a screen reader hears it', () => {
  it('is a button named for the version and its notes, with the new-release state in the name', async () => {
    const { virtual } = await import('@guidepup/virtual-screen-reader');
    localStorage.setItem('iql.releaseNotes.seen', '2026.1.1.1');
    render(<AppLockup />);
    await virtual.start({ container: document.body });
    screen.getByRole('button').focus();
    expect(await virtual.lastSpokenPhrase()).toBe(`button, Version ${appVersion}, release notes, new since you last looked, Release notes for this version`);
    // The dot is decoration; only the name says it.
    expect((await virtual.spokenPhraseLog()).join(' ')).not.toMatch(/•|dot/);
    await virtual.stop();
  });
});
