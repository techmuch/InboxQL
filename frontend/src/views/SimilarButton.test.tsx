import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { SimilarButton } from './SimilarButton';
import { useQueryStore } from '../lib/filters';

/**
 * The door onto `similar:`.
 *
 * The query term, the threshold and the histogram all existed and nothing in
 * the UI ran any of it — the same gap that shipped for contacts, in the same
 * session.
 */
describe('More like this', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useQueryStore.getState().set('folder:inbox');
  });

  const distribution = {
    id: 'm1',
    buckets: [0.3, 0.4, 0.5, 0.6, 0.7],
    counts: { '0.30': 185, '0.40': 134, '0.50': 24, '0.60': 0, '0.70': 0 },
    threshold: 0.5,
  };

  function server(body: unknown, ok = true) {
    vi.spyOn(window, 'fetch').mockImplementation(async () => ({
      ok, status: ok ? 200 : 400,
      text: async () => JSON.stringify(body),
      json: async () => body,
    } as Response));
  }

  it('shows how many messages sit at each threshold', async () => {
    server(distribution);
    render(<SimilarButton messageId="m1" />);

    fireEvent.click(screen.getByText('More like this'));

    // The counts are the point. A bare number is meaningless because the
    // usable band depends on the embedding model.
    expect(await screen.findByText('134 messages')).toBeInTheDocument();
    expect(screen.getByText('24 messages')).toBeInTheDocument();
    expect(screen.getByText('0.50')).toBeInTheDocument();
    expect(screen.getByText('default')).toBeInTheDocument();
  });

  it('runs the query at the threshold you pick', async () => {
    server(distribution);
    render(<SimilarButton messageId="m1" />);

    fireEvent.click(screen.getByText('More like this'));
    fireEvent.click(await screen.findByText('134 messages'));

    await waitFor(() => {
      expect(useQueryStore.getState().text).toBe('similar:m1>0.40');
    });
  });

  it('will not offer a threshold that matches nothing', async () => {
    server(distribution);
    render(<SimilarButton messageId="m1" />);

    fireEvent.click(screen.getByText('More like this'));
    const empty = (await screen.findAllByText('nothing'))[0].closest('button')!;

    // Running it would produce an empty result that reads as "there is nothing
    // similar" rather than "you picked a threshold nothing reaches".
    expect(empty).toBeDisabled();
  });

  it('says why it cannot work rather than doing nothing', async () => {
    server({ error: 'no embedding for m1' }, false);
    render(<SimilarButton messageId="m1" />);

    fireEvent.click(screen.getByText('More like this'));

    // A message with no vector has no neighbours. Silence would read as "there
    // is nothing similar", which is the wrong conclusion entirely.
    expect(await screen.findByText(/no embedding for m1/)).toBeInTheDocument();
    expect(screen.getByText(/annotate embed/)).toBeInTheDocument();
  });
});
