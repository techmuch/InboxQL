import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useDevReload } from './devReload';

describe('useDevReload', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    sessionStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('does nothing when dev is false', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ version: '0.0.31', dev: false, instanceId: '1' }),
    });
    vi.stubGlobal('fetch', fetchSpy);

    renderHook(() => useDevReload());

    await vi.runAllTimersAsync();
    // Only one check on mount, no polling
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });

  it('polls when dev is true and triggers reload when server restarts with a new version', async () => {
    const reloadMock = vi.fn();
    Object.defineProperty(window, 'location', {
      writable: true,
      value: { ...window.location, reload: reloadMock },
    });

    let pollCount = 0;
    const fetchSpy = vi.fn().mockImplementation(async () => {
      pollCount++;
      if (pollCount === 1) {
        return {
          ok: true,
          json: async () => ({ version: '0.0.31', dev: true, instanceId: 'inst-1' }),
        };
      }
      // Server restarted with new version
      return {
        ok: true,
        json: async () => ({ version: '0.0.32', dev: true, instanceId: 'inst-2' }),
      };
    });
    vi.stubGlobal('fetch', fetchSpy);

    renderHook(() => useDevReload());

    // First fetch
    await vi.advanceTimersByTimeAsync(10);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(reloadMock).not.toHaveBeenCalled();

    // Second fetch after 1500ms
    await vi.advanceTimersByTimeAsync(1500);
    expect(fetchSpy).toHaveBeenCalledTimes(2);
    expect(reloadMock).toHaveBeenCalledTimes(1);
  });
});
