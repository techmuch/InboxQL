import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { ErrorLog } from './ErrorLog';

/**
 * The misreading this guards against.
 *
 * Ordinary work logs at debug and only the exceptional at warn, so above a
 * debug floor the query category can *only* produce warnings. The tab then
 * shows a list of exceptions with no population behind them, and every query
 * line being a slow or failed one reads as "every query is slow".
 */
describe('Log, when the floor hides the baseline', () => {
  const mockLog = (level: string) => {
    vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      const body = url.startsWith('/api/log/level')
        ? { level, dropped: 0 }
        : { entries: [], total: 0 };
      return { ok: true, status: 200, json: async () => body } as Response;
    });
  };

  beforeEach(() => vi.restoreAllMocks());

  it('says the proportions are not visible', async () => {
    mockLog('info');
    render(<ErrorLog />);

    await waitFor(() => expect(screen.getByText(/the exceptions, not a sample/i)).toBeTruthy());
    // And offers the way to make them visible.
    expect(screen.getByRole('button', { name: /record at debug/i })).toBeTruthy();
  });

  // At debug the baseline is present, so the view is not lying and the note
  // would just be noise.
  it('stays quiet at debug, where the baseline is recorded', async () => {
    mockLog('debug');
    render(<ErrorLog />);

    await waitFor(() => expect(screen.queryByText(/the exceptions, not a sample/i)).toBeNull());
  });
});
