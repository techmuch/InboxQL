import { describe, it, expect, beforeEach, vi } from 'vitest';
import { canThread, startsThreaded, useThreadingStore, THREAD_STAGE } from './threading';

describe('threading preference', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
    useThreadingStore.setState({ threading: 'flat' });
  });

  it('defaults to flat and persists a change', () => {
    expect(startsThreaded()).toBe(false);
    useThreadingStore.getState().setThreading('threaded');
    expect(localStorage.getItem('inboxql.threading')).toBe('threaded');
    expect(startsThreaded()).toBe(true);
  });

  // A browser with storage blocked still gets a working app: the switch works,
  // it just does not survive a reload.
  it('survives a browser that will not store anything', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied');
    });
    expect(() => useThreadingStore.getState().setThreading('threaded')).not.toThrow();
    expect(useThreadingStore.getState().threading).toBe('threaded');
  });
});

/**
 * # What the guard is for
 *
 * The preference is a default, not a force, and these are the cases where
 * forcing would do damage rather than nothing.
 */
describe('canThread', () => {
  it('refuses a query that ends in an aggregate', () => {
    // The real failure: a query may end in only one terminal stage and adding
    // one replaces whichever is there, so "threading" a count deletes it.
    expect(canThread('folder:inbox | count by week', ['count'])).toBe(false);
    expect(canThread('| top domain 10', ['top'])).toBe(false);
    expect(canThread('| series receipts.amount by week', ['series'])).toBe(false);
    expect(canThread('| sum receipts.amount', ['sum'])).toBe(false);
  });

  it('refuses anything that is not mail', () => {
    for (const q of [
      'in:contacts kind:person',
      'in:attachments filetype:pdf',
      'in:tickets status:todo',
      'in:drafts origin:agent',
    ]) {
      expect(canThread(q, [])).toBe(false);
    }
  });

  it('allows an ordinary mail query', () => {
    expect(canThread('folder:inbox', [])).toBe(true);
    expect(canThread('from:stripe after:7d', [])).toBe(true);
    // in:mail is the default kind and is threadable, so it is not excluded.
    expect(canThread('in:mail is:unread', [])).toBe(true);
  });

  // `| thread` expands a list rather than ending it, so a query carrying it can
  // still be grouped into conversations.
  it('allows a query that already expands to whole conversations', () => {
    expect(canThread('folder:inbox | thread', ['thread'])).toBe(true);
  });

  // Already grouped: adding the stage again is not harmful, but the caller
  // should not be told to.
  it('refuses a query that is already grouped', () => {
    expect(canThread(`folder:inbox | ${THREAD_STAGE}`, [THREAD_STAGE])).toBe(false);
  });

  // A word that merely contains "in:contacts" inside a quoted value is not an
  // in: term — but the cost of being wrong here is one unthreaded query, so
  // the guard stays conservative rather than clever.
  it('is conservative about shapes it cannot read', () => {
    expect(canThread('subject:"in:tickets"', [])).toBe(false);
  });
});
