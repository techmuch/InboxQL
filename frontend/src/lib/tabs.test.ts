import { describe, it, expect, vi, beforeEach } from 'vitest';
import { openTool, openQuery, DESK_TAB } from './tabs';
import { useQueryStore } from './filters';
import { useLayoutStore } from 'nexus-shell';

describe('openTool and openQuery', () => {
  beforeEach(() => {
    useQueryStore.getState().clear();
  });

  it('normalizes legacy mail and query IDs to desk', () => {
    const addTabMock = vi.fn();
    useLayoutStore.setState({
      model: {
        visitNodes: vi.fn(),
        doAction: vi.fn(),
      } as any,
      addTab: addTabMock,
    });

    openTool('mail', 'Mailbox');
    expect(addTabMock).toHaveBeenCalledWith('desk', 'Desk');

    addTabMock.mockClear();
    openTool('query', 'Query');
    expect(addTabMock).toHaveBeenCalledWith('desk', 'Desk');
  });

  it('retargets and renames legacy tab nodes to Desk when already open', () => {
    const doActionMock = vi.fn();
    const legacyNode = {
      getId: () => 'tab-legacy-mail',
      getType: () => 'tab',
      getComponent: () => 'mail',
      getName: () => 'Mailbox',
      getParent: () => ({
        getSelected: () => 0,
        getChildren: () => [{ getId: () => 'tab-legacy-mail' }],
      }),
      _attributes: { component: 'mail', name: 'Mailbox' },
    };

    useLayoutStore.setState({
      model: {
        visitNodes: (cb: (node: any) => void) => cb(legacyNode),
        doAction: doActionMock,
        getNodeById: () => legacyNode,
      } as any,
      addTab: vi.fn(),
    });

    openTool('desk', 'Desk');

    // Should have renamed tab to Desk
    expect(doActionMock).toHaveBeenCalledWith({
      type: 'FlexLayout_RenameTab',
      data: { node: 'tab-legacy-mail', text: 'Desk' },
    });
    // Should have updated attributes
    expect(doActionMock).toHaveBeenCalledWith({
      type: 'FlexLayout_UpdateNodeAttributes',
      data: { node: 'tab-legacy-mail', json: { component: 'desk', name: 'Desk' } },
    });
    // Should have focused the tab
    expect(doActionMock).toHaveBeenCalledWith({
      type: 'FlexLayout_SelectTab',
      data: { tabNode: 'tab-legacy-mail' },
    });
    expect(legacyNode._attributes.name).toBe('Desk');
    expect(legacyNode._attributes.component).toBe('desk');
  });

  it('openQuery updates the shared query store and opens Desk', () => {
    const addTabMock = vi.fn();
    useLayoutStore.setState({
      model: {
        visitNodes: vi.fn(),
        doAction: vi.fn(),
      } as any,
      addTab: addTabMock,
    });

    openQuery('is:unread from:alice');
    expect(useQueryStore.getState().text).toBe('is:unread from:alice');
    expect(addTabMock).toHaveBeenCalledWith(DESK_TAB, 'Desk');
  });
});
