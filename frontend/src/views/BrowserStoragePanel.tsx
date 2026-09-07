import { useCallback, useEffect, useState } from 'react';
import {
  AlertTriangle, Check, Copy, HardDrive, RefreshCw,
  RotateCcw, Search, Trash2, ChevronDown, ChevronRight,
  Info,
} from 'lucide-react';

export interface StorageItem {
  key: string;
  type: 'localStorage' | 'sessionStorage';
  raw: string;
  sizeBytes: number;
  isJSON: boolean;
  parsed?: any;
  category: 'layout' | 'theme' | 'profile' | 'dev' | 'custom';
  categoryLabel: string;
  description: string;
}

function categorizeKey(key: string): { category: StorageItem['category']; label: string; description: string } {
  if (key === 'nexus-shell-layout') {
    return {
      category: 'layout',
      label: 'Workspace Layout',
      description: 'Serialized FlexLayout model persisting active tabs, tabset splits, and panel arrangement across sessions.',
    };
  }
  if (key === 'nexus-shell-theme') {
    return {
      category: 'theme',
      label: 'UI Theme',
      description: 'Selected color theme preference (light, dark, or Georgia Tech gold).',
    };
  }
  if (key === 'nexus-shell-user-profile') {
    return {
      category: 'profile',
      label: 'User Profile Cache',
      description: 'Cached client-side display name, role, and avatar preferences for the workbench shell.',
    };
  }
  if (key.startsWith('iql_dev_reloaded_')) {
    return {
      category: 'dev',
      label: 'Dev Reload Guard',
      description: 'Session flag that prevents reload loops when the server restarts with a new version in development mode.',
    };
  }
  return {
    category: 'custom',
    label: 'Application Data',
    description: 'General browser storage key for client-side state or cached settings.',
  };
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

export const BrowserStoragePanel = () => {
  const [items, setItems] = useState<StorageItem[]>([]);
  const [search, setSearch] = useState('');
  const [scope, setScope] = useState<'all' | 'localStorage' | 'sessionStorage'>('all');
  const [expandedKeys, setExpandedKeys] = useState<Set<string>>(new Set());
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [quotaInfo, setQuotaInfo] = useState<{ usage: number; quota: number } | null>(null);
  const [statusMessage, setStatusMessage] = useState<{ text: string; type: 'success' | 'info' } | null>(null);

  const scanStorage = useCallback(() => {
    const list: StorageItem[] = [];

    // Local Storage
    try {
      for (let i = 0; i < localStorage.length; i++) {
        const key = localStorage.key(i);
        if (!key) continue;
        const raw = localStorage.getItem(key) ?? '';
        const sizeBytes = new Blob([key, raw]).size;
        let isJSON = false;
        let parsed: any;
        try {
          parsed = JSON.parse(raw);
          isJSON = typeof parsed === 'object' && parsed !== null;
        } catch {
          // not JSON
        }
        const { category, label, description } = categorizeKey(key);
        list.push({
          key,
          type: 'localStorage',
          raw,
          sizeBytes,
          isJSON,
          parsed,
          category,
          categoryLabel: label,
          description,
        });
      }
    } catch (e) {
      console.error('Failed to read localStorage', e);
    }

    // Session Storage
    try {
      for (let i = 0; i < sessionStorage.length; i++) {
        const key = sessionStorage.key(i);
        if (!key) continue;
        const raw = sessionStorage.getItem(key) ?? '';
        const sizeBytes = new Blob([key, raw]).size;
        let isJSON = false;
        let parsed: any;
        try {
          parsed = JSON.parse(raw);
          isJSON = typeof parsed === 'object' && parsed !== null;
        } catch {
          // not JSON
        }
        const { category, label, description } = categorizeKey(key);
        list.push({
          key,
          type: 'sessionStorage',
          raw,
          sizeBytes,
          isJSON,
          parsed,
          category,
          categoryLabel: label,
          description,
        });
      }
    } catch (e) {
      console.error('Failed to read sessionStorage', e);
    }

    list.sort((a, b) => b.sizeBytes - a.sizeBytes);
    setItems(list);

    if (navigator.storage && navigator.storage.estimate) {
      navigator.storage.estimate()
        .then(est => {
          if (est.usage !== undefined && est.quota !== undefined) {
            setQuotaInfo({ usage: est.usage, quota: est.quota });
          }
        })
        .catch(() => {});
    }
  }, []);

  useEffect(() => {
    scanStorage();
    const onStorageChange = () => scanStorage();
    window.addEventListener('storage', onStorageChange);
    return () => window.removeEventListener('storage', onStorageChange);
  }, [scanStorage]);

  const toggleExpand = (compositeKey: string) => {
    setExpandedKeys(prev => {
      const next = new Set(prev);
      if (next.has(compositeKey)) next.delete(compositeKey);
      else next.add(compositeKey);
      return next;
    });
  };

  const copyToClipboard = async (item: StorageItem) => {
    try {
      const textToCopy = item.isJSON ? JSON.stringify(item.parsed, null, 2) : item.raw;
      await navigator.clipboard.writeText(textToCopy);
      setCopiedKey(item.key + item.type);
      setTimeout(() => setCopiedKey(null), 1800);
    } catch (e) {
      console.error('Copy failed', e);
    }
  };

  const deleteItem = (item: StorageItem) => {
    if (item.type === 'localStorage') {
      localStorage.removeItem(item.key);
    } else {
      sessionStorage.removeItem(item.key);
    }
    setStatusMessage({ text: `Removed "${item.key}" from ${item.type}`, type: 'info' });
    setTimeout(() => setStatusMessage(null), 3000);
    scanStorage();
  };

  const resetLayout = () => {
    localStorage.removeItem('nexus-shell-layout');
    setStatusMessage({
      text: 'Workspace layout reset. Reload the tab to restore default Desk layout.',
      type: 'success',
    });
    scanStorage();
  };

  const clearAllStorage = () => {
    if (!window.confirm('Clear all browser local and session storage? This will reset your layout, theme, and cached client state.')) {
      return;
    }
    localStorage.clear();
    sessionStorage.clear();
    window.location.reload();
  };

  const filteredItems = items.filter(item => {
    if (scope !== 'all' && item.type !== scope) return false;
    if (!search.trim()) return true;
    const query = search.toLowerCase();
    return (
      item.key.toLowerCase().includes(query) ||
      item.categoryLabel.toLowerCase().includes(query) ||
      item.raw.toLowerCase().includes(query)
    );
  });

  const totalBytes = items.reduce((acc, i) => acc + i.sizeBytes, 0);
  const localCount = items.filter(i => i.type === 'localStorage').length;
  const sessionCount = items.filter(i => i.type === 'sessionStorage').length;

  return (
    <div className="animate-in fade-in duration-300 space-y-6">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <div>
          <h2 className="text-2xl font-bold text-foreground flex items-center gap-2">
            <HardDrive className="w-6 h-6 text-primary" />
            Browser Storage
          </h2>
          <p className="text-muted-foreground text-sm mt-1">
            Inspect, manage, and reset cached workspace layouts, themes, and client-side storage.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={resetLayout}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-primary/10 text-primary hover:bg-primary/20 text-xs font-semibold rounded-none border border-primary/30 transition-colors"
            title="Clears nexus-shell-layout to unstick misconfigured workbench tabs"
          >
            <RotateCcw className="w-3.5 h-3.5" />
            Reset Layout
          </button>
          <button
            type="button"
            onClick={scanStorage}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-muted text-foreground hover:bg-accent text-xs font-semibold rounded-none border border-border transition-colors"
            title="Re-scan browser storage"
          >
            <RefreshCw className="w-3.5 h-3.5" />
            Refresh
          </button>
        </div>
      </div>

      {statusMessage && (
        <div className={`p-3 text-xs flex items-center justify-between border ${
          statusMessage.type === 'success'
            ? 'bg-green-500/10 border-green-500/30 text-green-600 dark:text-green-400'
            : 'bg-primary/10 border-primary/30 text-primary'
        }`}>
          <span>{statusMessage.text}</span>
          {statusMessage.type === 'success' && (
            <button
              type="button"
              onClick={() => window.location.reload()}
              className="ml-3 px-2 py-0.5 bg-primary text-primary-foreground font-semibold text-[11px] hover:opacity-90 transition-opacity"
            >
              Reload Page
            </button>
          )}
        </div>
      )}

      {/* Summary Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="p-4 bg-card border border-border shadow-sm">
          <div className="text-[10px] uppercase font-bold text-muted-foreground tracking-wider mb-1">
            Total Entries
          </div>
          <div className="text-2xl font-bold tabular-nums">
            {items.length}
          </div>
          <div className="text-[11px] text-muted-foreground mt-1">
            {localCount} local · {sessionCount} session
          </div>
        </div>

        <div className="p-4 bg-card border border-border shadow-sm">
          <div className="text-[10px] uppercase font-bold text-muted-foreground tracking-wider mb-1">
            Storage Consumed
          </div>
          <div className="text-2xl font-bold tabular-nums">
            {formatBytes(totalBytes)}
          </div>
          <div className="text-[11px] text-muted-foreground mt-1">
            Client-side state overhead
          </div>
        </div>

        <div className="p-4 bg-card border border-border shadow-sm">
          <div className="text-[10px] uppercase font-bold text-muted-foreground tracking-wider mb-1">
            Origin Quota
          </div>
          <div className="text-2xl font-bold tabular-nums">
            {quotaInfo ? formatBytes(quotaInfo.usage) : 'Active'}
          </div>
          <div className="text-[11px] text-muted-foreground mt-1">
            {quotaInfo ? `of ${formatBytes(quotaInfo.quota)} available` : 'Managed by browser sandbox'}
          </div>
        </div>
      </div>

      {/* Layout Recovery Explainer Alert */}
      <div className="p-4 bg-card border border-primary/20 flex items-start gap-3 text-xs leading-relaxed text-muted-foreground">
        <Info className="w-4 h-4 text-primary shrink-0 mt-0.5" />
        <div>
          <span className="font-semibold text-foreground">Layout Persistence & Recovery:</span>{' '}
          The workbench layout is persisted in your browser's <code className="font-mono text-primary">nexus-shell-layout</code> key so your open tabs and split panels remain between refreshes. If you ever encounter duplicate or mislabeled tabs from older versions, click <strong>Reset Layout</strong> above to safely clear the cached model without affecting your email database or settings.
        </div>
      </div>

      {/* Filter and Scope Bar */}
      <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3 pt-2">
        <div className="flex items-center gap-1.5">
          {(['all', 'localStorage', 'sessionStorage'] as const).map(tab => (
            <button
              key={tab}
              type="button"
              onClick={() => setScope(tab)}
              className={`px-3 py-1.5 text-xs font-semibold transition-colors ${
                scope === tab
                  ? 'bg-primary text-primary-foreground shadow-sm'
                  : 'bg-muted/70 hover:bg-accent text-muted-foreground hover:text-foreground'
              }`}
            >
              {tab === 'all' ? 'All Storage' : tab === 'localStorage' ? 'Local Storage' : 'Session Storage'}
            </button>
          ))}
        </div>

        <div className="relative flex-1 sm:max-w-xs">
          <Search className="w-3.5 h-3.5 text-muted-foreground absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" />
          <input
            type="text"
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Filter keys or contents..."
            className="w-full bg-background border border-border pl-8 pr-3 py-1.5 text-xs focus:ring-1 focus:ring-primary outline-none transition-all"
          />
        </div>
      </div>

      {/* Storage Items List */}
      <div className="border border-border bg-card divide-y divide-border">
        {filteredItems.length === 0 ? (
          <div className="p-8 text-center text-muted-foreground text-xs italic">
            No storage keys match the current filter.
          </div>
        ) : (
          filteredItems.map(item => {
            const compositeKey = item.type + ':' + item.key;
            const isExpanded = expandedKeys.has(compositeKey);
            const isCopied = copiedKey === (item.key + item.type);

            return (
              <div key={compositeKey} className="transition-colors hover:bg-muted/20">
                <div
                  className="p-3.5 flex items-center gap-3 cursor-pointer select-none"
                  onClick={() => toggleExpand(compositeKey)}
                >
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground shrink-0"
                    aria-label={isExpanded ? 'Collapse' : 'Expand'}
                  >
                    {isExpanded ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
                  </button>

                  <div className="flex-1 min-w-0 flex flex-col sm:flex-row sm:items-center gap-1.5 sm:gap-3">
                    <span className="font-mono text-xs font-bold text-foreground truncate">
                      {item.key}
                    </span>
                    <div className="flex items-center gap-2">
                      <span className={`px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider ${
                        item.type === 'localStorage'
                          ? 'bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20'
                          : 'bg-purple-500/10 text-purple-600 dark:text-purple-400 border border-purple-500/20'
                      }`}>
                        {item.type === 'localStorage' ? 'local' : 'session'}
                      </span>
                      <span className="px-1.5 py-0.5 text-[9px] font-semibold bg-muted text-muted-foreground">
                        {item.categoryLabel}
                      </span>
                    </div>
                  </div>

                  <div className="flex items-center gap-3 shrink-0">
                    <span className="text-xs font-mono text-muted-foreground tabular-nums">
                      {formatBytes(item.sizeBytes)}
                    </span>

                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        copyToClipboard(item);
                      }}
                      className="p-1.5 text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                      title="Copy raw value"
                      aria-label="Copy raw value"
                    >
                      {isCopied ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
                    </button>

                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        deleteItem(item);
                      }}
                      className="p-1.5 text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                      title="Delete key"
                      aria-label={`Delete ${item.key}`}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                </div>

                {isExpanded && (
                  <div className="px-4 pb-4 pt-1 bg-muted/10 border-t border-border/50 text-xs space-y-3">
                    <p className="text-muted-foreground text-[11px] leading-relaxed">
                      {item.description}
                    </p>
                    <div className="relative">
                      <div className="flex justify-between items-center bg-muted/60 px-3 py-1 border border-b-0 border-border text-[10px] uppercase font-bold text-muted-foreground tracking-wider font-mono">
                        <span>{item.isJSON ? 'Formatted JSON' : 'Raw Text Value'}</span>
                        <span>{item.raw.length} characters</span>
                      </div>
                      <pre className="p-3 bg-card border border-border text-[11px] font-mono overflow-auto max-h-64 leading-tight text-foreground whitespace-pre-wrap break-all">
                        {item.isJSON ? JSON.stringify(item.parsed, null, 2) : item.raw}
                      </pre>
                    </div>
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>

      {/* Danger Zone: Clear All Storage */}
      <div className="border border-destructive/20 bg-destructive/5 p-6 mt-8">
        <div className="flex items-start gap-4">
          <AlertTriangle className="w-6 h-6 text-destructive shrink-0 mt-0.5" />
          <div className="space-y-2">
            <h3 className="text-sm font-bold text-destructive">Clear All Browser Storage</h3>
            <p className="text-xs text-muted-foreground leading-relaxed">
              Permanently wipes all keys stored in the browser's <code className="font-mono">localStorage</code> and <code className="font-mono">sessionStorage</code> for InboxQL. This will reset your theme to default, erase custom workbench splits, and reload the application. This does not modify your local SQLite message database or credentials.
            </p>
            <div className="pt-2">
              <button
                type="button"
                onClick={clearAllStorage}
                className="bg-destructive hover:bg-destructive/90 text-destructive-foreground px-4 py-2 text-xs font-semibold transition-colors flex items-center gap-2"
              >
                <Trash2 className="w-3.5 h-3.5" />
                Clear All Browser Storage & Reload
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
