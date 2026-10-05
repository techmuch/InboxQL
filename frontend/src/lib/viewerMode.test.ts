import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useLayoutStore } from 'nexus-shell';
import {
  openMessage, openContact, openAttachment,
  previewMessage, previewContact, previewAttachment, previewMessageByID,
  useViewerStore, VIEWER_TABS,
} from './tabs';

/** A layout with no tabs open, so every open is an addTab. */
function emptyLayout() {
  const addTab = vi.fn();
  const doAction = vi.fn();
  useLayoutStore.setState({
    model: { visitNodes: vi.fn(), doAction, getNodeById: () => null } as any,
    addTab,
  });
  return { addTab, doAction };
}

/** A layout holding the named component ids as open tabs. */
function layoutWith(components: string[]) {
  const addTab = vi.fn();
  const doAction = vi.fn();
  const nodes = components.map(c => ({
    getId: () => `tab-${c}`,
    getType: () => 'tab',
    getComponent: () => c,
    getName: () => c,
    getParent: () => ({ getSelected: () => 0, getChildren: () => [{ getId: () => `tab-${c}` }] }),
    _attributes: { component: c, name: c },
  }));
  useLayoutStore.setState({
    model: {
      visitNodes: (cb: (n: any) => void) => nodes.forEach(cb),
      doAction,
      getNodeById: (id: string) => nodes.find(n => n.getId() === id) ?? null,
    } as any,
    addTab,
  });
  return { addTab, doAction };
}

const reset = (mode: 'reuse' | 'split') => {
  useViewerStore.setState({
    message: null, messageId: null, contact: null, file: null,
    previousMessage: null, selectedCount: 0, mode,
  });
};

describe('reuse mode', () => {
  beforeEach(() => reset('reuse'));

  it('sends every kind to the one Viewer tab', () => {
    const { addTab } = emptyLayout();

    openMessage({ id: 'm1' });
    openContact('alice@acme.com');
    openAttachment({ key: 'abc', filename: 'invoice.pdf' });

    for (const call of addTab.mock.calls) {
      expect(call).toEqual(['viewer', 'Viewer']);
    }
    expect(addTab).toHaveBeenCalledTimes(3);
  });

  // The mutual exclusion is what makes one tab unambiguous: it must never hold
  // two subjects and have to guess which to render.
  it('clears the other subjects', () => {
    emptyLayout();

    openMessage({ id: 'm1' });
    expect(useViewerStore.getState().message).toEqual({ id: 'm1' });

    openAttachment({ key: 'abc' });
    const s = useViewerStore.getState();
    expect(s.file).toEqual({ key: 'abc' });
    expect(s.message).toBeNull();
    expect(s.contact).toBeNull();
  });
});

describe('split mode', () => {
  beforeEach(() => reset('split'));

  it('gives each kind its own tab', () => {
    const { addTab } = emptyLayout();

    openMessage({ id: 'm1' });
    openContact('alice@acme.com');
    openAttachment({ key: 'abc' });

    expect(addTab).toHaveBeenCalledWith(VIEWER_TABS.message.id, 'Message');
    expect(addTab).toHaveBeenCalledWith(VIEWER_TABS.contact.id, 'Contact');
    expect(addTab).toHaveBeenCalledWith(VIEWER_TABS.file.id, 'File');
  });

  // The point of the mode. Three tabs share one store, so if setting a file
  // still blanked the message the Message tab would go empty the moment you
  // opened a file — which is the behaviour split mode exists to avoid.
  it('keeps the other subjects', () => {
    emptyLayout();

    openMessage({ id: 'm1' });
    openContact('alice@acme.com');
    openAttachment({ key: 'abc' });

    const s = useViewerStore.getState();
    expect(s.message).toEqual({ id: 'm1' });
    expect(s.contact).toBe('alice@acme.com');
    expect(s.file).toEqual({ key: 'abc' });
  });
});

describe('switching mode', () => {
  it('closes the contact and file tabs when reuse is chosen', () => {
    reset('split');
    const { doAction } = layoutWith([
      VIEWER_TABS.message.id, VIEWER_TABS.contact.id, VIEWER_TABS.file.id,
    ]);

    useViewerStore.getState().setMode('reuse');

    expect(doAction).toHaveBeenCalledWith({
      type: 'FlexLayout_DeleteTab', data: { node: `tab-${VIEWER_TABS.contact.id}` },
    });
    expect(doAction).toHaveBeenCalledWith({
      type: 'FlexLayout_DeleteTab', data: { node: `tab-${VIEWER_TABS.file.id}` },
    });
    // The message tab survives: its component id is the one reuse routes to.
    expect(doAction).not.toHaveBeenCalledWith({
      type: 'FlexLayout_DeleteTab', data: { node: `tab-${VIEWER_TABS.message.id}` },
    });
  });

  it('renames the shared tab to match the mode', () => {
    reset('split');
    const { doAction } = layoutWith([VIEWER_TABS.message.id]);

    useViewerStore.getState().setMode('reuse');
    expect(doAction).toHaveBeenCalledWith({
      type: 'FlexLayout_RenameTab',
      data: { node: `tab-${VIEWER_TABS.message.id}`, text: 'Viewer' },
    });
  });

  // A tab is a frame around a slot in the store, so closing it loses nothing
  // and reopening shows the same thing. That is what makes "hide" honest even
  // though FlexLayout can only delete.
  it('reopens the tabs that still hold something', () => {
    reset('reuse');
    useViewerStore.setState({ file: { key: 'abc' }, contact: null });
    const { addTab } = layoutWith([VIEWER_TABS.message.id]);

    useViewerStore.getState().setMode('split');

    expect(addTab).toHaveBeenCalledWith(VIEWER_TABS.file.id, 'File');
    // Nothing was ever opened for a contact, so no empty Contact tab appears.
    expect(addTab).not.toHaveBeenCalledWith(VIEWER_TABS.contact.id, 'Contact');
  });

  it('does nothing when the mode is already what was asked for', () => {
    reset('reuse');
    const { doAction, addTab } = layoutWith([VIEWER_TABS.message.id]);

    useViewerStore.getState().setMode('reuse');

    expect(doAction).not.toHaveBeenCalled();
    expect(addTab).not.toHaveBeenCalled();
  });

  it('persists the choice', () => {
    reset('reuse');
    layoutWith([]);

    useViewerStore.getState().setMode('split');
    expect(localStorage.getItem('inboxql.viewerMode')).toBe('split');

    useViewerStore.getState().setMode('reuse');
    expect(localStorage.getItem('inboxql.viewerMode')).toBe('reuse');
  });
});

/**
 * Preview points the viewer at something; open also brings it forward.
 *
 * The distinction is the whole reason arrowing a list works at all. A list that
 * called `open` on every keystroke would select the viewer's tab, so the first
 * Down key switched away from the list and the second went nowhere — focus had
 * left with the tab. Files and contacts had only `open` until now.
 */
describe('preview, as distinct from open', () => {
  beforeEach(() => reset('split'));

  it('sets the slot without adding a tab', () => {
    const { addTab, doAction } = emptyLayout();

    previewMessage({ id: 'm1' });
    previewContact('alice@acme.com');
    previewAttachment({ key: 'abc', filename: 'invoice.pdf' });

    const s = useViewerStore.getState();
    expect(s.message).toEqual({ id: 'm1' });
    expect(s.contact).toBe('alice@acme.com');
    expect(s.file).toEqual({ key: 'abc', filename: 'invoice.pdf' });

    expect(addTab).not.toHaveBeenCalled();
    expect(doAction).not.toHaveBeenCalled();
  });

  // An already-open viewer is what preview is for: it keeps up with the arrows
  // without the list losing focus.
  it('updates a viewer that is already open, still without touching the layout', () => {
    const { addTab, doAction } = layoutWith([VIEWER_TABS.file.id]);

    previewAttachment({ key: 'def' });

    expect(useViewerStore.getState().file).toEqual({ key: 'def' });
    expect(addTab).not.toHaveBeenCalled();
    expect(doAction).not.toHaveBeenCalled();
  });

  it('and open does bring it forward', () => {
    const { addTab } = emptyLayout();
    openAttachment({ key: 'abc' });
    expect(addTab).toHaveBeenCalledWith(VIEWER_TABS.file.id, 'File');
  });
});

/**
 * A fetched preview must not land on top of a newer one.
 *
 * Arrowing a list of occurrences issues one request per keystroke and they can
 * land in any order, so without the sequence guard a slow response for a row the
 * user has already left overwrites the row they are now on — and the viewer
 * shows a message that is not the highlighted one.
 */
describe('previewMessageByID', () => {
  beforeEach(() => {
    reset('split');
    emptyLayout();
  });

  it('shows the message it fetched', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true, json: async () => ({ id: 'm9', subject: 'Found' }),
    }) as never;

    await previewMessageByID('m9');
    expect(useViewerStore.getState().messageId).toBe('m9');
  });

  it('lets the newest request win when an older one lands late', async () => {
    const resolvers: Array<(v: any) => void> = [];
    globalThis.fetch = vi.fn().mockImplementation(
      () => new Promise(resolve => { resolvers.push(resolve); }),
    ) as never;

    const slow = previewMessageByID('m-slow');
    const fast = previewMessageByID('m-fast');

    // The second request answers first, as a smaller message would.
    resolvers[1]({ ok: true, json: async () => ({ id: 'm-fast' }) });
    await fast;
    expect(useViewerStore.getState().messageId).toBe('m-fast');

    // Then the first one finally lands, and is dropped.
    resolvers[0]({ ok: true, json: async () => ({ id: 'm-slow' }) });
    await slow;
    expect(useViewerStore.getState().messageId).toBe('m-fast');
  });

  // A message that will not load leaves the viewer where it was, rather than
  // blanking it — the user arrowed onto a row, they did not ask for an error.
  it('leaves the viewer alone when the fetch fails', async () => {
    useViewerStore.setState({ message: { id: 'kept' }, messageId: 'kept' });
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 404 }) as never;

    await previewMessageByID('gone');
    expect(useViewerStore.getState().messageId).toBe('kept');
  });

  /**
   * A direct preview also bumps the guard.
   *
   * Otherwise arrowing a file's occurrences and then arrowing the message list
   * would let the occurrence fetch land afterwards and overwrite the row the
   * user is now on — the same race, crossing between two lists.
   */
  it('cannot overwrite a direct preview issued after it', async () => {
    const resolvers: Array<(v: any) => void> = [];
    globalThis.fetch = vi.fn().mockImplementation(
      () => new Promise(resolve => { resolvers.push(resolve); }),
    ) as never;

    const inFlight = previewMessageByID('m-fetched');
    previewMessage({ id: 'm-direct' });

    resolvers[0]({ ok: true, json: async () => ({ id: 'm-fetched' }) });
    await inFlight;
    expect(useViewerStore.getState().messageId).toBe('m-direct');
  });
});
