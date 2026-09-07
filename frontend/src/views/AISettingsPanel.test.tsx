import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { AISettingsPanel } from './AISettingsPanel';

describe('AISettingsPanel', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  const mockStatus = {
    config: {
      provider: '',
      model: '',
      endpoint: '',
      hasApiKey: false,
      configured: false,
      autoStart: false,
      launchMode: 'headless',
      lifecycleMode: 'managed',
    },
    runtimes: [
      {
        provider: 'swama',
        name: 'Swama (macOS MLX)',
        installed: true,
        binaryPath: '/usr/local/bin/swama',
        running: false,
        endpoint: 'http://localhost:28100/v1',
        launchModes: ['headless', 'menubar'],
        models: ['mlx-community/Llama-3.2-3B-Instruct-4bit', 'mlx-community/gemma-3-4b-it-4bit'],
      },
      {
        provider: 'ollama',
        name: 'Ollama',
        installed: false,
        running: false,
        endpoint: 'http://localhost:11434',
        launchModes: ['headless'],
        models: [],
      },
    ],
    activeStatus: {
      running: false,
      endpoint: '',
      provider: '',
      model: '',
    },
  };

  it('renders runtime detection and provider options', async () => {
    vi.spyOn(window, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === '/api/llm/status') {
        return {
          ok: true,
          json: async () => mockStatus,
        } as Response;
      }
      if (url.includes('/api/settings?key=ignore_words')) {
        return {
          ok: true,
          json: async () => ({ value: 're:,fwd:' }),
        } as Response;
      }
      return { ok: false } as Response;
    });

    render(<AISettingsPanel />);

    expect(screen.getByText(/Scanning local AI runtimes.../i)).toBeDefined();

    expect(await screen.findByText('Swama')).toBeDefined();
    expect(screen.getByText('Ollama')).toBeDefined();
    expect(screen.getByText('Custom / Cloud')).toBeDefined();
    expect(screen.getByText('100% Private (No data leaves your device)')).toBeDefined();
    expect(screen.getByText(/re:,fwd:/)).toBeDefined();
  });

  it('starts the local server when Start Server is clicked', async () => {
    const fetchMock = vi.spyOn(window, 'fetch').mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === '/api/llm/status') {
        return {
          ok: true,
          json: async () => mockStatus,
        } as Response;
      }
      if (url.includes('/api/settings?key=ignore_words')) {
        return {
          ok: true,
          json: async () => ({ value: '' }),
        } as Response;
      }
      if (url === '/api/llm/start' && init?.method === 'POST') {
        return {
          ok: true,
          json: async () => ({ ok: true, provider: 'swama', pid: 12345 }),
        } as Response;
      }
      return { ok: false } as Response;
    });

    render(<AISettingsPanel />);

    const startBtn = await screen.findByRole('button', { name: /Start Server/i });
    fireEvent.click(startBtn);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/llm/start',
        expect.objectContaining({
          method: 'POST',
        })
      );
    });

    expect(await screen.findByText(/Server started successfully \(PID 12345\)/i)).toBeDefined();
  });

  it('switches to Custom/Cloud provider and displays API key and endpoint fields', async () => {
    vi.spyOn(window, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === '/api/llm/status') {
        return {
          ok: true,
          json: async () => mockStatus,
        } as Response;
      }
      if (url.includes('/api/settings?key=ignore_words')) {
        return {
          ok: true,
          json: async () => ({ value: '' }),
        } as Response;
      }
      return { ok: false } as Response;
    });

    render(<AISettingsPanel />);

    const customTab = await screen.findByText('Custom / Cloud');
    fireEvent.click(customTab);

    expect(screen.getByText('API Base URL')).toBeDefined();
    expect(screen.getByText(/API Key \(Optional for local proxies\)/i)).toBeDefined();
    expect(screen.getByText('Remote Network Endpoint')).toBeDefined();
  });

  it('tests connection when Test Connection is clicked', async () => {
    const fetchMock = vi.spyOn(window, 'fetch').mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === '/api/llm/status') {
        return {
          ok: true,
          json: async () => mockStatus,
        } as Response;
      }
      if (url.includes('/api/settings?key=ignore_words')) {
        return {
          ok: true,
          json: async () => ({ value: '' }),
        } as Response;
      }
      if (url === '/api/llm/test' && init?.method === 'POST') {
        return {
          ok: true,
          json: async () => ({ ok: true, provider: 'swama', elapsedMs: 42, reply: 'ok' }),
        } as Response;
      }
      return { ok: false } as Response;
    });

    render(<AISettingsPanel />);

    // Click the test connection button in form
    const testBtns = await screen.findAllByRole('button', { name: /Test Connection/i });
    fireEvent.click(testBtns[0]);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/llm/test',
        expect.objectContaining({ method: 'POST' })
      );
    });

    expect(await screen.findByText(/Connected! Responded with "ok" in 42ms/i)).toBeDefined();
  });

  it('saves configuration when Save & Activate is clicked', async () => {
    const fetchMock = vi.spyOn(window, 'fetch').mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === '/api/llm/status') {
        return {
          ok: true,
          json: async () => mockStatus,
        } as Response;
      }
      if (url.includes('/api/settings?key=ignore_words')) {
        return {
          ok: true,
          json: async () => ({ value: '' }),
        } as Response;
      }
      if (url === '/api/llm/config' && init?.method === 'POST') {
        return {
          ok: true,
          json: async () => ({
            status: 'saved',
            config: { provider: 'swama', model: 'mlx-community/Llama-3.2-3B-Instruct-4bit' },
          }),
        } as Response;
      }
      return { ok: false } as Response;
    });

    render(<AISettingsPanel />);

    const saveBtn = await screen.findByRole('button', { name: /Save & Activate Configuration/i });
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/llm/config',
        expect.objectContaining({ method: 'POST' })
      );
    });

    expect(await screen.findByText(/Configuration saved and activated/i)).toBeDefined();
  });
});
