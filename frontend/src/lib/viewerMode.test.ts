import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useLayoutStore } from 'nexus-shell';
import {
  openMessage, openContact, openAttachment,
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
