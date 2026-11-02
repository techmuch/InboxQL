import { describe, it, expect, beforeEach } from 'vitest';
import {
  openMessage, openContact, openAttachment,
  previewMessage, previewContact, previewAttachment,
  useViewerStore,
} from './tabs';
import { usePlaceStore } from './places';

/**
 * One viewer, one subject at a time.
 *
 * The viewer used to be a tab, and could be split into a tab per kind. With
 * places there is one viewer: opening a message, a contact or a file goes to
 * it, and the newest subject replaces the last.
 */
beforeEach(() => {
  useViewerStore.getState().clear();
  usePlaceStore.setState({ id: 'desk', recent: [] });
});

describe('opening', () => {
  it('sends every kind to the one viewer', () => {
    openMessage({ id: 'm1' });
    expect(usePlaceStore.getState().id).toBe('viewer');
    usePlaceStore.setState({ id: 'desk' });
    openContact('a@x.com');
    // A person is a page of their own.
    expect(usePlaceStore.getState().id).toBe('person');
    usePlaceStore.setState({ id: 'desk' });
    openAttachment({ key: 'k', filename: 'f.pdf' });
    expect(usePlaceStore.getState().id).toBe('viewer');
  });

  it('holds one subject at a time', () => {
    openMessage({ id: 'm1' });
    openContact('a@x.com');
    const s = useViewerStore.getState();
    expect(s.contact).toBe('a@x.com');
    expect(s.message).toBeNull();
  });
});

describe('preview, as distinct from open', () => {
  it('sets the subject without leaving the list', () => {
    previewMessage({ id: 'm2' });
    previewContact('b@x.com');
    previewAttachment({ key: 'k2', filename: 'g.pdf' });
    expect(usePlaceStore.getState().id).toBe('desk');
    expect(useViewerStore.getState().file?.key).toBe('k2');
  });
});
