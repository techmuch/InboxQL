import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { AISettingsPanel } from './AISettingsPanel';

/**
 * These cover the Models tab, which replaced the single-gateway form.
 *
 * The old suite asserted "Save & Activate", one provider card set, and one
 * model field, because there was one configuration. Those affordances are gone
 * on purpose — a profile is the whole address of a model, and there are
 * several — so the assertions moved rather than being dropped.
 */
describe('AISettingsPanel — Models', () => {
  beforeEach(() => { vi.restoreAllMocks(); });

  const runtimes = [
    {
      provider: 'swama', name: 'Swama (macOS MLX)', installed: true,
      binaryPath: '/usr/local/bin/swama', running: true,
      endpoint: 'http://localhost:28100/v1', launchModes: ['headless', 'menubar'],
      models: ['mlx-community/gemma-3-4b-it-4bit'],
    },
    {
      provider: 'ollama', name: 'Ollama', installed: false, running: false,
      endpoint: 'http://localhost:11434', launchModes: ['headless'], models: [],
    },
  ];

  const profiles = [
    {
      id: '1', name: 'local-fast', provider: 'swama',
      model: 'mlx-community/gemma-3-4b-it-4bit', endpoint: 'http://localhost:28100/v1',
      scope: 'local', isDefault: true, autoStart: false, launchMode: 'headless',
      hasApiKey: false,
    },
    {
      id: '2', name: 'cloud', provider: 'openai', model: 'gpt-4o-mini',
      endpoint: 'https://api.openai.com/v1', scope: 'remote', isDefault: false,
      autoStart: false, launchMode: 'headless', hasApiKey: true,
    },
  ];

  const status = {
    config: { provider: 'swama', model: 'gemma', endpoint: '', configured: true, hasApiKey: false },
    profiles,
    runtimes,
    activeStatus: { running: true, endpoint: 'http://localhost:28100/v1', provider: 'swama', model: 'gemma' },
  };

  /** Records every request so a test can assert on what was actually sent. */
  function mockServer(overrides: Record<string, unknown> = {}) {
    const calls: Array<{ url: string; method: string; body: any }> = [];
    vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      const method = init?.method ?? 'GET';
      let body: any = null;
      if (typeof init?.body === 'string') body = JSON.parse(init.body);
      calls.push({ url, method, body });

      const key = Object.keys(overrides).find(k => url.startsWith(k));
      const payload = key ? overrides[key] : url.startsWith('/api/llm/status') ? status : {};
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(payload),
        json: async () => payload,
      } as Response;
    });
    return calls;
  }

  it('shows local runtimes and what each one is doing', async () => {
    mockServer();
    render(<AISettingsPanel />);

    expect(await screen.findByText('Swama (macOS MLX)')).toBeInTheDocument();
    expect(screen.getByText(/running · 1 model/)).toBeInTheDocument();
    // A runtime that is not installed says so rather than looking merely idle.
    expect(screen.getByText('Ollama')).toBeInTheDocument();
    expect(screen.getByText('not installed')).toBeInTheDocument();
  });

  it('lists every configured profile with its scope', async () => {
    mockServer();
    render(<AISettingsPanel />);

    expect(await screen.findByText('local-fast')).toBeInTheDocument();
    expect(screen.getByText('cloud')).toBeInTheDocument();
    // Scope is the fact that decides whether mail leaves the machine, so it is
    // on every row rather than hidden behind an edit.
    expect(screen.getByText('local')).toBeInTheDocument();
    expect(screen.getByText('remote')).toBeInTheDocument();
    expect(screen.getByText('key stored')).toBeInTheDocument();
  });

  it('starts a stopped runtime', async () => {
    const stopped = {
      ...status,
      runtimes: [{ ...runtimes[0], running: false }, runtimes[1]],
    };
    const calls = mockServer({ '/api/llm/status': stopped });
    render(<AISettingsPanel />);

    fireEvent.click(await screen.findByText('Start'));
    await waitFor(() => {
      const start = calls.find(c => c.url === '/api/llm/start');
      expect(start?.body.provider).toBe('swama');
    });
  });

  it('reports why a refresh found nothing, instead of doing nothing visible', async () => {
    const calls = mockServer({
      '/api/llm/refresh': {
        models: 0,
        runtimes: [{
          ...runtimes[0], models: [],
          problem: 'Swama is running but reported no models. Pull one, or check http://localhost:28100/v1 is reachable.',
        }],
      },
    });
    render(<AISettingsPanel />);

    fireEvent.click(await screen.findByText('Re-scan all'));

    // The old button swallowed its errors, so a broken model endpoint looked
    // exactly like "no new models".
    expect(await screen.findByText(/running but reported no models/)).toBeInTheDocument();
    expect(calls.some(c => c.url === '/api/llm/refresh')).toBe(true);
  });

  it('warns that a remote endpoint sends message bodies off the machine', async () => {
    mockServer();
    render(<AISettingsPanel />);

    fireEvent.click(await screen.findByText('Add model'));
    expect(await screen.findByText(/Nothing leaves this machine/)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText?.('Provider') ?? screen.getAllByRole('combobox')[0], {
      target: { value: 'openai' },
    });

    // The consequence of the endpoint is shown while it is being chosen, not
    // discovered later when an annotator refuses to run.
    expect(await screen.findByText(/send message bodies to/)).toBeInTheDocument();
  });

  it('does not erase a stored key when saving a form that cannot show it', async () => {
    const calls = mockServer();
    render(<AISettingsPanel />);

    // Edit the cloud profile, which has a key, and change only the model.
    fireEvent.click(await screen.findByText('cloud'));
    const model = await screen.findByDisplayValue('gpt-4o-mini');
    fireEvent.change(model, { target: { value: 'gpt-4o' } });
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      const save = calls.find(c => c.url === '/api/llm/profiles' && c.method === 'POST');
      expect(save).toBeTruthy();
      expect(save!.body.model).toBe('gpt-4o');
      // Absent, not empty: the server reads an absent key as "leave it alone"
      // and an empty one as "remove it".
      expect('apiKey' in save!.body).toBe(false);
    });
  });
});
