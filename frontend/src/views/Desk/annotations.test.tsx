import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Desk } from './index';
import { useQueryStore } from '../../lib/filters';
import { useViewerStore } from '../../lib/tabs';
import { useSelectionStore } from '../../lib/selection';

/**
 * An annotator's results were reachable only by knowing to type `label:money`
 * or `extract:receipts` — the same gap the Other block was written to close
 * for contacts and files.
 *
 * These assert the doors, not the capability: the queries behind them already
 * worked.
 */
describe('Desk annotations rail', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useQueryStore.getState().set('folder:inbox');
    useViewerStore.getState().clear();
    useSelectionStore.getState().clear();
  });

  const annotators = [
    {
      id: 'a1', name: 'money', kind: 'label', engine: 'rule', version: 1,
      instructions: 'subject:invoice', allowRemote: false, enabled: true,
      progress: { total: 188, evaluated: 188, matched: 34, empty: 154, failed: 0, humanCorrections: 0 },
    },
    {
      id: 'a2', name: 'receipts', kind: 'extract', engine: 'gliner', version: 1,
      instructions: 'find things', allowRemote: false, enabled: true,
      progress: { total: 32, evaluated: 32, matched: 31, empty: 1, failed: 0, humanCorrections: 19 },
    },
    {
      id: 'a3', name: 'travel', kind: 'label', engine: 'rule', version: 1,
      instructions: 'subject:flight', allowRemote: false, enabled: false,
      progress: { total: 188, evaluated: 188, matched: 9, empty: 179, failed: 0, humanCorrections: 0 },
    },
    {
      id: 'a4', name: 'parcels', kind: 'extract', engine: 'gliner', version: 1,
      instructions: 'find things', allowRemote: false, enabled: true,
      progress: { total: 0, evaluated: 0, matched: 0, empty: 0, failed: 0, humanCorrections: 0 },
    },
  ];

  function mockServer(onQuery?: (q: string) => void) {
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      let body: any = {};
      if (url.startsWith('/api/query?')) {
        const q = new URL(url, 'http://x').searchParams.get('q') ?? '';
        onQuery?.(q);
        body = { query: q, kind: 'messages', count: 0, messages: [] };
      } else if (url.startsWith('/api/query/terms')) {
        body = { terms: [], stages: [] };
      } else if (url.startsWith('/api/annotators')) {
        body = annotators;
      } else if (url.startsWith('/api/queries')) {
        body = [];
      }
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });
  }

  it('offers each annotator that found something, with its count', async () => {
    mockServer();
    render(<Desk />);

    expect(await screen.findByText('Annotations')).toBeTruthy();
    expect(screen.getByText('money')).toBeTruthy();
    expect(screen.getByText('receipts')).toBeTruthy();
    expect(screen.getByText('34')).toBeTruthy();
    expect(screen.getByText('31')).toBeTruthy();
  });

  it('asks the right question for each kind', async () => {
    const ran: string[] = [];
    mockServer(q => ran.push(q));
    render(<Desk />);

    // A label answers yes or no, so its mail is label:x. An extractor pulls
    // records out, so its mail is extract:x. Two different questions.
    fireEvent.click(await screen.findByText('money'));
    await waitFor(() => expect(ran).toContain('label:money'));

    fireEvent.click(screen.getByText('receipts'));
    await waitFor(() => expect(ran).toContain('extract:receipts'));
  });

  it('shows a switched-off annotator, because its results are still there', async () => {
    mockServer();
    render(<Desk />);

    // Off stops new work; it does not hide what was already found. Hiding the
    // entry would contradict what the switch means.
    expect(await screen.findByText('travel')).toBeTruthy();
    expect(screen.getByText('off')).toBeTruthy();
  });

  it('hides one that has found nothing', async () => {
    mockServer();
    render(<Desk />);

    await screen.findByText('Annotations');
    // Eleven starters, most empty on a fresh mailbox, would bury the entries
    // that lead somewhere. An empty annotator is a job to run, not a place.
    expect(screen.queryByText('parcels')).toBeNull();
  });

  it('survives an annotator listing it cannot draw', async () => {
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      let body: any = {};
      if (url.startsWith('/api/annotators')) body = { unexpected: true };
      else if (url.startsWith('/api/query?')) body = { kind: 'messages', count: 0, messages: [] };
      else if (url.startsWith('/api/query/terms')) body = { terms: [], stages: [] };
      else if (url.startsWith('/api/queries')) body = [];
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(body),
        json: async () => body,
      } as Response;
    });

    render(<Desk />);

    // No section, no crash. A shape change must not take the mailbox down.
    await screen.findByText('Other');
    expect(screen.queryByText('Annotations')).toBeNull();
  });
});
