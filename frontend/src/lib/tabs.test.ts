import { describe, it, expect, beforeEach } from 'vitest';
import { openTool, openQuery } from './tabs';
import { useQueryStore } from './filters';
import { usePlaceStore } from './places';

describe('openTool and openQuery go to places', () => {
  beforeEach(() => {
    useQueryStore.getState().clear();
    usePlaceStore.setState({ id: 'today', recent: [] });
  });

  it('goes to the place named', () => {
    openTool('calendar', 'Calendar');
    expect(usePlaceStore.getState().id).toBe('calendar');
  });

  it('treats the old mail ids as the mail list', () => {
    openTool('mail', 'Mailbox');
    expect(usePlaceStore.getState().id).toBe('desk');
    openTool('query', 'Query');
    expect(usePlaceStore.getState().id).toBe('desk');
  });

  it('treats the old per-kind viewers as the one viewer', () => {
    openTool('viewer-file', 'File');
    expect(usePlaceStore.getState().id).toBe('viewer');
  });

  it('puts the address in the URL, so Back works', () => {
    openTool('board', 'Board');
    expect(window.location.hash).toBe('#/board');
  });

  it('openQuery sets the query and goes to the mail list', () => {
    openQuery('from:alice');
    expect(useQueryStore.getState().text).toBe('from:alice');
    expect(usePlaceStore.getState().id).toBe('desk');
  });
});
