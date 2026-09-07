import React, { useState, useEffect, useCallback } from 'react';
import { 
  Cpu, 
  Server, 
  Zap, 
  Check, 
  AlertCircle, 
  RefreshCw, 
  Play, 
  Square, 
  Lock, 
  Globe, 
  ShieldCheck, 
  Eye,
  EyeOff
} from 'lucide-react';

export interface RuntimeInfo {
  provider: string;
  name: string;
  installed: boolean;
  binaryPath?: string;
  running: boolean;
  endpoint: string;
  launchModes: string[];
  models: string[];
}

export interface LLMConfig {
  provider: string;
  model: string;
  endpoint: string;
  autoStart: boolean;
  launchMode: string;
  lifecycleMode: string;
  hasApiKey: boolean;
  configured: boolean;
}

export interface LLMStatusResponse {
  config: LLMConfig;
  runtimes: RuntimeInfo[];
  activeStatus: {
    running: boolean;
    endpoint: string;
    provider: string;
    model: string;
  };
}

export const AISettingsPanel: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [config, setConfig] = useState<LLMConfig | null>(null);
  const [runtimes, setRuntimes] = useState<RuntimeInfo[]>([]);
  const [activeStatus, setActiveStatus] = useState<{
    running: boolean;
    endpoint: string;
    provider: string;
    model: string;
  }>({ running: false, endpoint: '', provider: '', model: '' });

  // Form state
  const [selectedProvider, setSelectedProvider] = useState<'swama' | 'ollama' | 'openai'>('swama');
  const [selectedModel, setSelectedModel] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [showApiKey, setShowApiKey] = useState(false);
  const [autoStart, setAutoStart] = useState(false);
  const [launchMode, setLaunchMode] = useState('headless');
  
  // Remote models discovery
  const [remoteModels, setRemoteModels] = useState<string[]>([]);
  const [discoveringModels, setDiscoveringModels] = useState(false);

  // Actions
  const [serverActionLoading, setServerActionLoading] = useState<string | null>(null);
  const [serverActionMessage, setServerActionMessage] = useState<{ success: boolean; text: string } | null>(null);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; message: string; elapsedMs?: number } | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveMessage, setSaveMessage] = useState<{ success: boolean; text: string } | null>(null);

  // Topic trend ignore words
  const [ignoreWords, setIgnoreWords] = useState('');
  const [savingIgnoreWords, setSavingIgnoreWords] = useState(false);
  const [ignoreWordsMessage, setIgnoreWordsMessage] = useState<{ success: boolean; text: string } | null>(null);

  const fetchStatus = useCallback(async () => {
    try {
      const res = await fetch('/api/llm/status');
      if (!res.ok) throw new Error('Failed to fetch status');
      const data: LLMStatusResponse = await res.json();
      setConfig(data.config);
      setRuntimes(data.runtimes || []);
      setActiveStatus(data.activeStatus || { running: false, endpoint: '', provider: '', model: '' });

      // Initialize form if configured
      if (data.config && data.config.configured) {
        setSelectedProvider(data.config.provider as 'swama' | 'ollama' | 'openai');
        setSelectedModel(data.config.model || '');
        setEndpoint(data.config.endpoint || '');
        setAutoStart(data.config.autoStart ?? false);
        setLaunchMode(data.config.launchMode || 'headless');
      } else {
        // Default to first installed local runtime if available
        const installedLocal = (data.runtimes || []).find(r => r.installed);
        if (installedLocal) {
          setSelectedProvider(installedLocal.provider as 'swama' | 'ollama');
          if (installedLocal.models && installedLocal.models.length > 0) {
            setSelectedModel(installedLocal.models[0]);
          }
        }
      }
    } catch (err) {
      console.error('Failed to fetch LLM status', err);
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchIgnoreWords = useCallback(async () => {
    try {
      const res = await fetch('/api/settings?key=ignore_words');
      if (res.ok) {
        const data = await res.json();
        setIgnoreWords(data.value || '');
      }
    } catch (err) {
      console.error('Failed to fetch ignore words', err);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    fetchIgnoreWords();
  }, [fetchStatus, fetchIgnoreWords]);

  const handleRefreshDetection = async () => {
    setRefreshing(true);
    try {
      const res = await fetch('/api/llm/detect');
      if (res.ok) {
        const data = await res.json();
        setRuntimes(data.runtimes || []);
      }
    } catch (err) {
      console.error('Failed to detect runtimes', err);
    } finally {
      setRefreshing(false);
    }
  };

  const currentRuntime = runtimes.find(r => r.provider === selectedProvider);

  // When provider changes, update model suggestions if empty
  const handleSelectProvider = (prov: 'swama' | 'ollama' | 'openai') => {
    setSelectedProvider(prov);
    setSaveMessage(null);
    setTestResult(null);
    setServerActionMessage(null);

    const rt = runtimes.find(r => r.provider === prov);
    if (rt && rt.models && rt.models.length > 0) {
      setSelectedModel(rt.models[0]);
    } else if (prov === 'openai') {
      if (!endpoint) setEndpoint('https://api.openai.com/v1');
      if (!selectedModel) setSelectedModel('gpt-4o-mini');
    }
  };

  const handleStartServer = async () => {
    setServerActionLoading('start');
    setServerActionMessage(null);
    try {
      const res = await fetch('/api/llm/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider: selectedProvider,
          launchMode: launchMode,
        }),
      });
      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.error || 'Failed to start server');
      }
      setServerActionMessage({
        success: true,
        text: `Server started successfully (PID ${data.pid}).`,
      });
      await fetchStatus();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setServerActionMessage({
        success: false,
        text: msg,
      });
    } finally {
      setServerActionLoading(null);
    }
  };

  const handleStopServer = async () => {
    setServerActionLoading('stop');
    setServerActionMessage(null);
    try {
      const res = await fetch('/api/llm/stop', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider: selectedProvider }),
      });
      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.error || 'Failed to stop server');
      }
      setServerActionMessage({
        success: true,
        text: 'Server stopped.',
      });
      await fetchStatus();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setServerActionMessage({
        success: false,
        text: msg,
      });
    } finally {
      setServerActionLoading(null);
    }
  };

  const handleDiscoverRemoteModels = async () => {
    setDiscoveringModels(true);
    setSaveMessage(null);
    try {
      const res = await fetch('/api/llm/models', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          endpoint: endpoint,
          apiKey: apiKey,
          provider: selectedProvider,
        }),
      });
      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.error || 'Could not fetch models');
      }
      setRemoteModels(data.models || []);
      if (data.models && data.models.length > 0) {
        setSelectedModel(data.models[0]);
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setSaveMessage({ success: false, text: msg });
    } finally {
      setDiscoveringModels(false);
    }
  };

  const handleTestConnection = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const res = await fetch('/api/llm/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider: selectedProvider,
          model: selectedModel,
          endpoint: endpoint,
          apiKey: apiKey,
        }),
      });
      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.error || 'Test completion failed');
      }
      setTestResult({
        success: true,
        message: `Connected! Responded with "${data.reply}" in ${data.elapsedMs}ms`,
        elapsedMs: data.elapsedMs,
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setTestResult({
        success: false,
        message: msg,
      });
    } finally {
      setTesting(false);
    }
  };

  const handleSaveConfig = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setSaveMessage(null);
    try {
      const res = await fetch('/api/llm/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider: selectedProvider,
          model: selectedModel,
          endpoint: endpoint,
          apiKey: apiKey,
          autoStart: autoStart,
          launchMode: launchMode,
          lifecycleMode: 'managed',
        }),
      });
      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.error || 'Failed to save configuration');
      }
      setSaveMessage({
        success: true,
        text: `Configuration saved and activated for ${selectedProvider} (${selectedModel}).`,
      });
      await fetchStatus();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setSaveMessage({
        success: false,
        text: msg,
      });
    } finally {
      setSaving(false);
    }
  };

  const handleDisableGateway = async () => {
    if (!confirm('Disable LLM provider? InboxQL will fall back to emitting structured context for external agents.')) {
      return;
    }
    setSaving(true);
    try {
      const res = await fetch('/api/llm/disable', { method: 'POST' });
      if (res.ok) {
        await fetchStatus();
        setSaveMessage({
          success: true,
          text: 'LLM Gateway disabled.',
        });
      }
    } catch {
      setSaveMessage({
        success: false,
        text: 'Failed to disable LLM gateway.',
      });
    } finally {
      setSaving(false);
    }
  };

  const handleSaveIgnoreWords = async () => {
    setSavingIgnoreWords(true);
    setIgnoreWordsMessage(null);
    try {
      const res = await fetch('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key: 'ignore_words', value: ignoreWords }),
      });
      if (res.ok) {
        setIgnoreWordsMessage({ success: true, text: 'Ignore words updated successfully.' });
      } else {
        throw new Error();
      }
    } catch {
      setIgnoreWordsMessage({ success: false, text: 'Failed to update ignore words.' });
    } finally {
      setSavingIgnoreWords(false);
    }
  };

  if (loading) {
    return (
      <div className="p-8 text-center text-muted-foreground text-sm">
        <RefreshCw className="w-5 h-5 animate-spin mx-auto mb-2 text-primary" />
        Scanning local AI runtimes...
      </div>
    );
  }

  const isLocal = selectedProvider === 'swama' || selectedProvider === 'ollama';
  const availableModels = isLocal 
    ? (currentRuntime?.models || []) 
    : (remoteModels.length > 0 ? remoteModels : []);

  return (
    <div className="animate-in fade-in duration-300 space-y-8 max-w-4xl">
      {/* Header */}
      <div>
        <h2 className="text-2xl font-bold text-foreground flex items-center gap-2">
          <Cpu className="w-6 h-6 text-primary" />
          AI & Analysis Configuration
        </h2>
        <p className="text-muted-foreground text-sm mt-1">
          Configure local-first machine learning runtimes (Apple Silicon MLX, Ollama) or connect to external/cloud LLM providers.
        </p>
      </div>

      {/* Active Gateway Status Banner */}
      <div className="bg-card border border-border p-5 rounded-none shadow-sm space-y-3">
        <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-3">
          <div className="flex items-center gap-3">
            <div className={`w-3 h-3 rounded-full ${
              config?.configured 
                ? (activeStatus.running ? 'bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.6)]' : 'bg-amber-500') 
                : 'bg-muted-foreground/30'
            }`} />
            <div>
              <div className="flex items-center gap-2">
                <span className="font-bold text-base text-foreground">
                  {config?.configured ? `Active Gateway: ${config.provider}` : 'No LLM Gateway Active'}
                </span>
                {config?.configured && (
                  <span className="text-xs px-2 py-0.5 font-mono bg-primary/10 text-primary border border-primary/20">
                    {config.model}
                  </span>
                )}
              </div>
              <p className="text-xs text-muted-foreground mt-0.5">
                {config?.configured 
                  ? (activeStatus.running 
                      ? `Online and responsive at ${activeStatus.endpoint}` 
                      : `Offline or stopped (${activeStatus.endpoint || 'default endpoint'})`)
                  : 'Commands like "analyze" and "draft" emit structured context for external agents.'}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2 self-end sm:self-center">
            {config?.configured && (
              <>
                <button
                  type="button"
                  onClick={handleTestConnection}
                  disabled={testing}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-primary/10 text-primary hover:bg-primary/20 text-xs font-semibold rounded-none border border-primary/30 transition-colors disabled:opacity-50"
                  title="Ping the active model"
                >
                  <Zap className={`w-3.5 h-3.5 ${testing ? 'animate-pulse' : ''}`} />
                  {testing ? 'Testing...' : 'Test Connection'}
                </button>
                <button
                  type="button"
                  onClick={handleDisableGateway}
                  className="px-3 py-1.5 bg-muted text-muted-foreground hover:text-destructive hover:bg-destructive/10 text-xs font-semibold rounded-none border border-border transition-colors"
                >
                  Disable
                </button>
              </>
            )}
          </div>
        </div>

        {testResult && (
          <div className={`p-3 text-xs flex items-center gap-2 border ${
            testResult.success 
              ? 'bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/20' 
              : 'bg-destructive/10 text-destructive border-destructive/20'
          }`}>
            {testResult.success ? <Check className="w-4 h-4 shrink-0" /> : <AlertCircle className="w-4 h-4 shrink-0" />}
            <span>{testResult.message}</span>
          </div>
        )}
      </div>

      {/* Provider Selector Cards */}
      <div className="space-y-3">
        <div className="flex justify-between items-center">
          <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">
            Choose Provider
          </label>
          <button
            type="button"
            onClick={handleRefreshDetection}
            disabled={refreshing}
            className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground transition-colors"
          >
            <RefreshCw className={`w-3 h-3 ${refreshing ? 'animate-spin' : ''}`} />
            Re-scan Local Runtimes
          </button>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {/* Swama Card */}
          {(() => {
            const swamaRt = runtimes.find(r => r.provider === 'swama');
            const isSelected = selectedProvider === 'swama';
            return (
              <div
                role="button"
                tabIndex={0}
                onClick={() => handleSelectProvider('swama')}
                className={`p-4 border text-left cursor-pointer transition-all ${
                  isSelected 
                    ? 'border-primary bg-primary/5 ring-1 ring-primary' 
                    : 'border-border hover:border-primary/50 bg-card'
                }`}
              >
                <div className="flex justify-between items-start mb-2">
                  <div className="flex items-center gap-2">
                    <Server className="w-4 h-4 text-primary" />
                    <span className="font-bold text-sm">Swama</span>
                  </div>
                  <span className="px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider bg-green-500/10 text-green-600 dark:text-green-400 border border-green-500/20">
                    macOS MLX
                  </span>
                </div>
                <p className="text-xs text-muted-foreground mb-3">
                  Swift-native Apple Silicon inference engine.
                </p>
                <div className="text-[11px] space-y-1 font-mono">
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Installed:</span>
                    <span className={swamaRt?.installed ? 'text-foreground font-semibold' : 'text-muted-foreground'}>
                      {swamaRt?.installed ? 'Yes' : 'Not found'}
                    </span>
                  </div>
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Server:</span>
                    <span className={swamaRt?.running ? 'text-green-500 font-semibold' : 'text-muted-foreground'}>
                      {swamaRt?.running ? 'Running (:28100)' : 'Stopped'}
                    </span>
                  </div>
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Models:</span>
                    <span>{swamaRt?.models?.length || 0} installed</span>
                  </div>
                </div>
              </div>
            );
          })()}

          {/* Ollama Card */}
          {(() => {
            const ollamaRt = runtimes.find(r => r.provider === 'ollama');
            const isSelected = selectedProvider === 'ollama';
            return (
              <div
                role="button"
                tabIndex={0}
                onClick={() => handleSelectProvider('ollama')}
                className={`p-4 border text-left cursor-pointer transition-all ${
                  isSelected 
                    ? 'border-primary bg-primary/5 ring-1 ring-primary' 
                    : 'border-border hover:border-primary/50 bg-card'
                }`}
              >
                <div className="flex justify-between items-start mb-2">
                  <div className="flex items-center gap-2">
                    <Cpu className="w-4 h-4 text-primary" />
                    <span className="font-bold text-sm">Ollama</span>
                  </div>
                  <span className="px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20">
                    Local
                  </span>
                </div>
                <p className="text-xs text-muted-foreground mb-3">
                  Popular cross-platform model runner and daemon.
                </p>
                <div className="text-[11px] space-y-1 font-mono">
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Installed:</span>
                    <span className={ollamaRt?.installed ? 'text-foreground font-semibold' : 'text-muted-foreground'}>
                      {ollamaRt?.installed ? 'Yes' : 'Not found'}
                    </span>
                  </div>
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Server:</span>
                    <span className={ollamaRt?.running ? 'text-green-500 font-semibold' : 'text-muted-foreground'}>
                      {ollamaRt?.running ? 'Running (:11434)' : 'Stopped'}
                    </span>
                  </div>
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Models:</span>
                    <span>{ollamaRt?.models?.length || 0} installed</span>
                  </div>
                </div>
              </div>
            );
          })()}

          {/* Custom / Cloud Provider Card */}
          {(() => {
            const isSelected = selectedProvider === 'openai';
            return (
              <div
                role="button"
                tabIndex={0}
                onClick={() => handleSelectProvider('openai')}
                className={`p-4 border text-left cursor-pointer transition-all ${
                  isSelected 
                    ? 'border-primary bg-primary/5 ring-1 ring-primary' 
                    : 'border-border hover:border-primary/50 bg-card'
                }`}
              >
                <div className="flex justify-between items-start mb-2">
                  <div className="flex items-center gap-2">
                    <Globe className="w-4 h-4 text-primary" />
                    <span className="font-bold text-sm">Custom / Cloud</span>
                  </div>
                  <span className="px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20">
                    OpenAI API
                  </span>
                </div>
                <p className="text-xs text-muted-foreground mb-3">
                  Connect to OpenAI, LM Studio, vLLM, or self-hosted servers.
                </p>
                <div className="text-[11px] space-y-1 font-mono">
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Protocol:</span>
                    <span>/v1/chat/completions</span>
                  </div>
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Key Storage:</span>
                    <span className="text-green-500">AES-256 Vault</span>
                  </div>
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Scope:</span>
                    <span className="text-amber-500">Remote / LAN</span>
                  </div>
                </div>
              </div>
            );
          })()}
        </div>
      </div>

      {/* Provider Details & Lifecycle Controls */}
      <form onSubmit={handleSaveConfig} className="bg-card border border-border p-6 space-y-6">
        <div className="flex flex-col sm:flex-row justify-between sm:items-center gap-2 pb-4 border-b border-border">
          <div>
            <h3 className="font-bold text-lg text-foreground flex items-center gap-2">
              <span>Configure {selectedProvider === 'swama' ? 'Swama' : selectedProvider === 'ollama' ? 'Ollama' : 'Custom / External Provider'}</span>
              {isLocal ? (
                <span className="inline-flex items-center gap-1 text-[10px] font-bold text-green-600 dark:text-green-400 bg-green-500/10 border border-green-500/20 px-2 py-0.5">
                  <ShieldCheck className="w-3 h-3" />
                  100% Private (No data leaves your device)
                </span>
              ) : (
                <span className="inline-flex items-center gap-1 text-[10px] font-bold text-amber-600 dark:text-amber-400 bg-amber-500/10 border border-amber-500/20 px-2 py-0.5">
                  <Globe className="w-3 h-3" />
                  Remote Network Endpoint
                </span>
              )}
            </h3>
            {currentRuntime?.binaryPath && (
              <p className="text-xs font-mono text-muted-foreground mt-1">
                Binary path: {currentRuntime.binaryPath}
              </p>
            )}
          </div>
        </div>

        {/* Local Server Lifecycle Controls (Swama / Ollama) */}
        {isLocal && (
          <div className="p-4 bg-muted/20 border border-border/80 space-y-4">
            <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-3">
              <div>
                <span className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                  Local Server Process
                </span>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Status: {currentRuntime?.running ? (
                    <span className="text-green-500 font-semibold">Active and listening on port</span>
                  ) : (
                    <span className="text-muted-foreground font-semibold">Server is stopped</span>
                  )}
                </p>
              </div>

              <div className="flex items-center gap-2">
                {currentRuntime?.running ? (
                  <button
                    type="button"
                    onClick={handleStopServer}
                    disabled={serverActionLoading === 'stop'}
                    className="flex items-center gap-1.5 px-3 py-1.5 bg-destructive/10 text-destructive hover:bg-destructive/20 border border-destructive/20 text-xs font-semibold transition-colors disabled:opacity-50"
                  >
                    <Square className="w-3.5 h-3.5" />
                    {serverActionLoading === 'stop' ? 'Stopping...' : 'Stop Server'}
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={handleStartServer}
                    disabled={serverActionLoading === 'start' || !currentRuntime?.installed}
                    className="flex items-center gap-1.5 px-4 py-1.5 bg-primary text-primary-foreground hover:opacity-90 text-xs font-bold shadow-sm transition-opacity disabled:opacity-50"
                  >
                    <Play className="w-3.5 h-3.5" />
                    {serverActionLoading === 'start' ? 'Starting...' : 'Start Server'}
                  </button>
                )}
              </div>
            </div>

            {serverActionMessage && (
              <div className={`p-2.5 text-xs flex items-center gap-2 border ${
                serverActionMessage.success 
                  ? 'bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/20' 
                  : 'bg-destructive/10 text-destructive border-destructive/20'
              }`}>
                {serverActionMessage.success ? <Check className="w-3.5 h-3.5 shrink-0" /> : <AlertCircle className="w-3.5 h-3.5 shrink-0" />}
                <span>{serverActionMessage.text}</span>
              </div>
            )}

            {/* Launch Options */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-2 border-t border-border/50 text-xs">
              {currentRuntime?.launchModes && currentRuntime.launchModes.length > 1 && (
                <div className="space-y-1.5">
                  <label className="font-bold uppercase tracking-wider text-muted-foreground text-[10px]">
                    Launch Mode
                  </label>
                  <div className="flex gap-4">
                    <label className="flex items-center gap-2 cursor-pointer">
                      <input
                        type="radio"
                        name="launchMode"
                        value="headless"
                        checked={launchMode === 'headless'}
                        onChange={() => setLaunchMode('headless')}
                        className="text-primary focus:ring-primary"
                      />
                      <span>Headless CLI (<code>{selectedProvider} serve</code>)</span>
                    </label>
                    <label className="flex items-center gap-2 cursor-pointer">
                      <input
                        type="radio"
                        name="launchMode"
                        value="menubar"
                        checked={launchMode === 'menubar'}
                        onChange={() => setLaunchMode('menubar')}
                        className="text-primary focus:ring-primary"
                      />
                      <span>macOS Menu Bar (<code>swama menubar</code>)</span>
                    </label>
                  </div>
                </div>
              )}

              <div className="space-y-1.5">
                <label className="font-bold uppercase tracking-wider text-muted-foreground text-[10px]">
                  Startup Hook
                </label>
                <label className="flex items-center gap-2 cursor-pointer mt-1">
                  <input
                    type="checkbox"
                    checked={autoStart}
                    onChange={e => setAutoStart(e.target.checked)}
                    className="w-4 h-4 text-primary border-border focus:ring-primary rounded-none"
                  />
                  <span>Auto-start {selectedProvider} when InboxQL starts</span>
                </label>
              </div>
            </div>
          </div>
        )}

        {/* Remote / Custom Endpoint Settings */}
        {!isLocal && (
          <div className="space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">
                API Base URL
              </label>
              <input
                type="url"
                value={endpoint}
                onChange={e => setEndpoint(e.target.value)}
                placeholder="https://api.openai.com/v1"
                required
                className="w-full bg-background border border-border px-3 py-2 text-sm font-mono focus:ring-1 focus:ring-primary outline-none"
              />
              <p className="text-[11px] text-muted-foreground">
                Points to any endpoint speaking the OpenAI <code>/v1/chat/completions</code> protocol.
              </p>
            </div>

            <div className="space-y-1.5">
              <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">
                API Key (Optional for local proxies)
              </label>
              <div className="relative flex items-center">
                <input
                  type={showApiKey ? 'text' : 'password'}
                  value={apiKey}
                  onChange={e => setApiKey(e.target.value)}
                  placeholder={config?.hasApiKey && selectedProvider === config?.provider ? '•••••••••••••••• (Encrypted in vault)' : 'sk-...'}
                  className="w-full bg-background border border-border px-3 py-2 pr-10 text-sm font-mono focus:ring-1 focus:ring-primary outline-none"
                />
                <button
                  type="button"
                  onClick={() => setShowApiKey(!showApiKey)}
                  className="absolute right-3 text-muted-foreground hover:text-foreground"
                  title={showApiKey ? 'Hide key' : 'Show key'}
                >
                  {showApiKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>
              <p className="text-[11px] text-muted-foreground flex items-center gap-1">
                <Lock className="w-3 h-3 text-green-500" />
                Sealed in machine-local AES-256 vault. Never sent to the client in plaintext.
              </p>
            </div>

            <div className="pt-1">
              <button
                type="button"
                onClick={handleDiscoverRemoteModels}
                disabled={discoveringModels || !endpoint}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-muted hover:bg-accent text-xs font-semibold rounded-none border border-border transition-colors disabled:opacity-50"
              >
                <RefreshCw className={`w-3.5 h-3.5 ${discoveringModels ? 'animate-spin' : ''}`} />
                {discoveringModels ? 'Querying endpoint...' : 'Query Remote Models'}
              </button>
            </div>
          </div>
        )}

        {/* Model Selection */}
        <div className="space-y-1.5">
          <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">
            Model Name
          </label>
          
          {availableModels.length > 0 ? (
            <div className="flex gap-2">
              <select
                value={selectedModel}
                onChange={e => setSelectedModel(e.target.value)}
                required
                className="flex-1 bg-background border border-border px-3 py-2 text-sm font-mono focus:ring-1 focus:ring-primary outline-none"
              >
                {availableModels.map(m => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
              <input
                type="text"
                placeholder="Or custom model..."
                value={selectedModel}
                onChange={e => setSelectedModel(e.target.value)}
                className="w-48 bg-background border border-border px-3 py-2 text-xs font-mono focus:ring-1 focus:ring-primary outline-none"
              />
            </div>
          ) : (
            <input
              type="text"
              value={selectedModel}
              onChange={e => setSelectedModel(e.target.value)}
              placeholder={selectedProvider === 'swama' ? 'mlx-community/Llama-3.2-3B-Instruct-4bit' : selectedProvider === 'ollama' ? 'llama3' : 'gpt-4o-mini'}
              required
              className="w-full bg-background border border-border px-3 py-2 text-sm font-mono focus:ring-1 focus:ring-primary outline-none"
            />
          )}

          <p className="text-[11px] text-muted-foreground">
            {isLocal && availableModels.length === 0 
              ? `No local models detected. Pull a model using "${selectedProvider} pull <model>" or start the server.`
              : 'The target model used for thread summarization and drafting.'}
          </p>
        </div>

        {/* Feedback message */}
        {saveMessage && (
          <div className={`p-3 text-xs flex items-center gap-2 border ${
            saveMessage.success 
              ? 'bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/20' 
              : 'bg-destructive/10 text-destructive border-destructive/20'
          }`}>
            {saveMessage.success ? <Check className="w-4 h-4 shrink-0" /> : <AlertCircle className="w-4 h-4 shrink-0" />}
            <span>{saveMessage.text}</span>
          </div>
        )}

        {/* Submit Actions */}
        <div className="flex justify-between items-center pt-2">
          <button
            type="button"
            onClick={handleTestConnection}
            disabled={testing || !selectedModel}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-muted text-foreground hover:bg-accent text-xs font-semibold rounded-none border border-border transition-colors disabled:opacity-50"
          >
            <Zap className={`w-3.5 h-3.5 ${testing ? 'animate-pulse' : ''}`} />
            {testing ? 'Verifying...' : 'Test Connection'}
          </button>

          <button
            type="submit"
            disabled={saving || !selectedModel}
            className="flex items-center gap-2 bg-primary text-primary-foreground px-5 py-2 text-xs font-bold shadow-sm hover:opacity-90 active:scale-[0.98] transition-all disabled:opacity-50"
          >
            <Check className="w-3.5 h-3.5" />
            {saving ? 'Saving...' : 'Save & Activate Configuration'}
          </button>
        </div>
      </form>

      {/* Analysis Settings: Topic Trend Ignore Words */}
      <div className="bg-card border border-border p-6 space-y-4">
        <h3 className="font-bold text-lg text-foreground">Analysis & Topic Trends</h3>
        <div className="space-y-2">
          <label className="text-xs font-bold text-muted-foreground uppercase tracking-wider">
            Topic Trend Ignore Words
          </label>
          <p className="text-xs text-muted-foreground text-balance leading-relaxed">
            Common words or prefixes to exclude from topic extraction and dashboard trends. Separate multiple words with commas.
          </p>
          <textarea
            className="w-full bg-background border border-border px-4 py-3 text-sm focus:ring-2 focus:ring-primary/20 outline-none transition-all h-28 font-mono"
            value={ignoreWords}
            onChange={e => setIgnoreWords(e.target.value)}
            placeholder="re:,fwd:,the,and,etc..."
          />
        </div>

        {ignoreWordsMessage && (
          <div className={`p-2.5 text-xs flex items-center gap-2 border ${
            ignoreWordsMessage.success 
              ? 'bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/20' 
              : 'bg-destructive/10 text-destructive border-destructive/20'
          }`}>
            {ignoreWordsMessage.success ? <Check className="w-3.5 h-3.5 shrink-0" /> : <AlertCircle className="w-3.5 h-3.5 shrink-0" />}
            <span>{ignoreWordsMessage.text}</span>
          </div>
        )}

        <button
          type="button"
          onClick={handleSaveIgnoreWords}
          disabled={savingIgnoreWords}
          className="bg-primary text-primary-foreground px-4 py-1.5 font-semibold text-xs shadow-sm hover:opacity-90 active:scale-[0.98] transition-all disabled:opacity-50"
        >
          {savingIgnoreWords ? 'Saving...' : 'Save Ignore Words'}
        </button>
      </div>
    </div>
  );
};
