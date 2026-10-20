import { describe, it, expect } from 'vitest';
import { PLACES } from './appPlaces';
import { SETTINGS } from './settingsIndex';

// The guides, as text, from the repository's docs folder and AGENTS.md.
const guides = import.meta.glob(['../../../docs/*.md', '../../../AGENTS.md'], { query: '?raw', import: 'default', eager: true }) as Record<string, string>;

const links = (kind: 'setting' | 'open') =>
  Object.entries(guides).flatMap(([file, text]) =>
    [...text.matchAll(new RegExp(`\\]\\(#${kind}/([^)]*)\\)`, 'g'))].map(m => ({ file: file.split('/').pop(), id: m[1] })));

describe('links from the guides into the application', () => {
  it('reads the guides', () => {
    expect(Object.keys(guides).length).toBeGreaterThan(10);
  });

  it('names a real setting in every #setting/ link', () => {
    const ids = new Set(SETTINGS.map(s => s.id));
    expect(links('setting').filter(l => !ids.has(l.id))).toEqual([]);
  });

  it('names a real place in every #open/ link', () => {
    expect(links('open').filter(l => !PLACES[l.id])).toEqual([]);
  });
});
