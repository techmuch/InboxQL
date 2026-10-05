import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  previewPlaces, useAttachmentTargetStore, attachmentTarget,
  type AttachmentTarget,
} from './attachmentTarget';

describe('previewPlaces', () => {
  it('puts the preview where it was asked for', () => {
    expect(previewPlaces('message', 'split')).toEqual({ inline: true, viewer: false });
    expect(previewPlaces('file', 'split')).toEqual({ inline: false, viewer: true });
    expect(previewPlaces('both', 'split')).toEqual({ inline: true, viewer: true });
  });

  /**
   * The two settings are not independent.
   *
   * In reuse mode the file slot and the message slot are the same tab, and
   * setting one clears the other. So "both" cannot be both: sending the file
   * there would clear the message, and with it the inline preview — leaving one
   * place, which is what "both" was chosen to avoid.
   */
  it('keeps the half it can keep when one tab has to hold everything', () => {
    expect(previewPlaces('both', 'reuse')).toEqual({ inline: true, viewer: false });
  });

  // Obeyed as written: evicting the message is precisely what this asks for,
  // and the setting says so where it is chosen.
  it('still sends the file to the viewer in reuse mode when that is the choice', () => {
    expect(previewPlaces('file', 'reuse')).toEqual({ inline: false, viewer: true });
  });

  // Whatever is chosen, a preview has somewhere to go. A combination that
  // showed the file nowhere would make the chip look broken.
  it('never leaves a click with nothing to show', () => {
    const targets: AttachmentTarget[] = ['message', 'file', 'both'];
    for (const t of targets) {
      for (const mode of ['reuse', 'split'] as const) {
        const places = previewPlaces(t, mode);
        expect(places.inline || places.viewer).toBe(true);
      }
    }
  });
});

describe('the stored preference', () => {
  beforeEach(() => {
    localStorage.clear();
    useAttachmentTargetStore.setState({ target: 'message' });
  });

  it('defaults to the message, which is what it did before there was a choice', () => {
    expect(attachmentTarget()).toBe('message');
  });

  it('persists what was chosen', () => {
    useAttachmentTargetStore.getState().setTarget('both');
    expect(localStorage.getItem('inboxql.attachmentTarget')).toBe('both');
    expect(attachmentTarget()).toBe('both');
  });

  // A key someone edited by hand, or left behind by an older version, must not
  // put the app in a state none of the buttons match.
  it('falls back to the default for a value it does not recognise', () => {
    localStorage.setItem('inboxql.attachmentTarget', 'sideways');
    expect(attachmentTarget()).toBe('message');
  });

  it('survives storage being unavailable', () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });

    expect(attachmentTarget()).toBe('message');
    // The switch still takes effect; only its persistence is lost.
    useAttachmentTargetStore.getState().setTarget('file');
    expect(useAttachmentTargetStore.getState().target).toBe('file');

    getItem.mockRestore();
    setItem.mockRestore();
  });
});
