import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Ask } from './index';
import { looksLikeQuestion, useAsk } from '../../lib/ask';

const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status });

describe('looksLikeQuestion', () => {
  it('tells a question from a query', () => {
    expect(looksLikeQuestion('how much have we spent on the kitchen')).toBe(true);
    expect(looksLikeQuestion('who owes me a reply?')).toBe(true);
    expect(looksLikeQuestion('from:alice is:unread')).toBe(false);
    expect(looksLikeQuestion('invoice')).toBe(false);
    expect(looksLikeQuestion('what | count')).toBe(false);
  });
});

describe('Ask in words', () => {
  beforeEach(() => useAsk.getState().reset());

  it('is off until turned on, and says what would leave the computer', async () => {
    const calls: string[] = [];
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${url}`);
      if (init?.method === 'PUT') return json({ cli: 'claude', enabled: true, available: { claude: '/c' }, sends: 'to Anthropic' });
      return json({ cli: '', enabled: false, available: { claude: '/c' }, sends: 'Your question … to Anthropic' });
    }));
    render(<Ask />);
    expect(await screen.findByText(/Your question … to Anthropic/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Turn on asking with Claude Code' }));
    await waitFor(() => expect(calls).toContain('PUT /api/ask'));
    expect(await screen.findByLabelText('Your question')).toBeTruthy();
  });

  it('shows the answer, and every command it ran in the evidence', async () => {
    vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') return json({ question: 'q', text: 'You spent $5,412.20.', commands: [{ command: 'iql --json query "topic:kitchen"' }, { command: 'iql mail archive x', refused: true }], seconds: 12, costUsd: 0.2, cli: 'Claude Code', session: 's1' });
      return json({ cli: 'claude', enabled: true, available: { claude: '/c' }, sends: 'to Anthropic' });
    }));
    render(<Ask />);
    fireEvent.change(await screen.findByLabelText('Your question'), { target: { value: 'How much on the kitchen?' } });
    fireEvent.click(screen.getByRole('button', { name: /Ask/ }));
    expect(await screen.findByText('You spent $5,412.20.')).toBeTruthy();
    expect(screen.getByText(/ran 2 commands · reported cost \$0.20/)).toBeTruthy();
    expect(useAsk.getState().turns.at(-1)?.session).toBe('s1');
  });
});
