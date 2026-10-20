import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { HELP_TOPICS } from './helpTopics';
import { Help } from '../views/Help';
import * as docsNav from './docsNav';

// Every component, as text: a "?" can be anywhere, and most only render deep
// inside a running tab.
const sources = import.meta.glob(['../**/*.tsx', '!../**/*.test.tsx'], { query: '?raw', import: 'default', eager: true }) as Record<string, string>;
const all = Object.values(sources).join('\n');
const asked = [...all.matchAll(/<Help\s+topic="([^"]+)"|\bhelp="([^"]+)"|openHelp\('([^']+)'\)|helpTopic=\{?["']([^"']+)["']/g)].map(m => m[1] ?? m[2] ?? m[3] ?? m[4]);

describe('help topics', () => {
  it('has no id twice', () => {
    expect(new Set(HELP_TOPICS.map(t => t.id)).size).toBe(HELP_TOPICS.length);
  });

  it('uses every topic somewhere', () => {
    // Settings sections are asked for by a template, settings-${section}.
    const used = (id: string) => asked.includes(id) || (id.startsWith('settings-') && all.includes('topic={`settings-${'));
    expect(HELP_TOPICS.filter(t => !used(t.id)).map(t => t.id)).toEqual([]);
  });

  it('asks only for topics that exist', () => {
    const ids = new Set(HELP_TOPICS.map(t => t.id));
    expect(asked.filter(a => !ids.has(a))).toEqual([]);
  });

  it('opens the guide at the section', () => {
    const open = vi.spyOn(docsNav, 'openDoc').mockImplementation(() => {});
    render(<Help topic="load-images" />);
    fireEvent.click(screen.getByRole('button', { name: 'Help: Links and images' }));
    expect(open).toHaveBeenCalledWith('desk', 'links-and-images');
    open.mockRestore();
  });

  it('draws nothing for an unknown topic', () => {
    const { container } = render(<Help topic="nope" />);
    expect(container.innerHTML).toBe('');
  });
});
