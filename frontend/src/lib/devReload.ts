import { useEffect } from 'react';
import { version as appVersion } from '../../package.json';

interface VersionResponse {
  version: string;
  revision?: string;
  dev: boolean;
  instanceId: string;
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
        if (!data || !data.dev) {
          // If the server is not running in --dev mode, stop polling.
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

      if (!isCancelled) {
        schedule(1500);
      }
    };

    const schedule = (ms: number) => {
      if (isCancelled) return;
      pollTimer = setTimeout(check, ms);
    };

    check();

    return () => {
      isCancelled = true;
      if (pollTimer) clearTimeout(pollTimer);
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
