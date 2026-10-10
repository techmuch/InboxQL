import { useEffect, useSyncExternalStore } from 'react';
import { version as appVersion } from '../../package.json';

interface VersionResponse {
  version: string;
  revision?: string;
  dev: boolean;
  instanceId: string;
}

/**
 * A restart this page asked for — Settings → System → Restart or Update — and
 * is waiting out. Outside --dev nothing polls, so a page that started a
 * restart has to say so; it then watches for a server with a different
 * instance id and reloads onto it, whatever mode the server runs in.
 */
export interface RestartExpectation {
  reason: 'restart' | 'update';
  /** The instance that was running when the restart was asked for. */
  from: string;
  since: number;
  /** Set when nothing came back in time, so the page can say so. */
  gaveUp?: boolean;
}

/** How long to wait for a server to come back before saying it has not. */
export const RESTART_PATIENCE_MS = 120_000;

let expectation: RestartExpectation | null = null;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach(l => l());

export function expectRestart(reason: RestartExpectation['reason'], from: string) {
  expectation = { reason, from, since: Date.now() };
  emit();
  window.dispatchEvent(new Event('iql:expect-restart'));
}

export function clearRestartExpectation() {
  expectation = null;
  emit();
}

export function useRestartExpectation(): RestartExpectation | null {
  return useSyncExternalStore(
    l => { listeners.add(l); return () => listeners.delete(l); },
    () => expectation,
  );
}

/**
 * useDevReload watches the server's version and instance state when in --dev mode.
 * If the server restarts with a new version or new binary build, it automatically
 * refreshes the browser page so the frontend reflects the updated code/version.
 */
export function useDevReload() {
  useEffect(() => {
    let initialVersion: string | null = null;
    let initialInstanceId: string | null = null;
    let pollTimer: ReturnType<typeof setTimeout> | null = null;
    let isCancelled = false;

    const check = async () => {
      try {
        const res = await fetch('/api/version', { cache: 'no-store' });
        if (!res.ok) {
          schedule(1500);
          return;
        }

        const data: VersionResponse = await res.json();
        if (expectation && !expectation.gaveUp) {
          if (data?.instanceId && data.instanceId !== expectation.from) {
            window.location.reload();
            return;
          }
        } else if (!data || !data.dev) {
          // Outside --dev, nothing to watch until a restart is asked for.
          return;
        }

        if (initialVersion === null) {
          initialVersion = data.version;
          initialInstanceId = data.instanceId;

          // If on initial load the client bundle version differs from the server's version,
          // refresh once to ensure the latest assets are loaded (guarded by sessionStorage).
          if (data.version && data.version !== appVersion) {
            const key = `iql_dev_reloaded_${data.version}_${data.instanceId}`;
            if (!safeSessionGet(key)) {
              safeSessionSet(key, '1');
              console.log(`[dev] New server version detected (${data.version} vs client ${appVersion}). Refreshing...`);
              window.location.reload();
              return;
            }
          }
        } else {
          // Check if version changed or server restarted
          const versionChanged = Boolean(data.version && (data.version !== initialVersion || data.version !== appVersion));
          const serverRestarted = Boolean(data.instanceId && data.instanceId !== initialInstanceId);

          if (versionChanged || serverRestarted) {
            const key = `iql_dev_reloaded_${data.version}_${data.instanceId}`;
            if (!safeSessionGet(key)) {
              safeSessionSet(key, '1');
              console.log(`[dev] Server restarted with new version (${data.version}). Refreshing frontend...`);
              window.location.reload();
              return;
            }
          }
        }
      } catch {
        // Server might be temporarily restarting; continue polling
      }

      if (expectation && !expectation.gaveUp && Date.now() - expectation.since > RESTART_PATIENCE_MS) {
        expectation = { ...expectation, gaveUp: true };
        emit();
        return;
      }

      if (!isCancelled) {
        schedule(1500);
      }
    };

    const schedule = (ms: number) => {
      if (isCancelled) return;
      pollTimer = setTimeout(check, ms);
    };

    check();

    // A restart asked for while not polling starts the watch again.
    const onExpect = () => {
      if (pollTimer) clearTimeout(pollTimer);
      schedule(1000);
    };
    window.addEventListener('iql:expect-restart', onExpect);

    return () => {
      isCancelled = true;
      if (pollTimer) clearTimeout(pollTimer);
      window.removeEventListener('iql:expect-restart', onExpect);
    };
  }, []);
}

function safeSessionGet(key: string): string | null {
  try {
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

function safeSessionSet(key: string, val: string): void {
  try {
    sessionStorage.setItem(key, val);
  } catch {
    // Ignore storage quota / security errors
  }
}
