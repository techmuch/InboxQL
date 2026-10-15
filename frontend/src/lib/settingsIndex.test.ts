import { describe, it, expect } from 'vitest';
import { SETTINGS, SECTION_LABEL, matchesSetting } from './settingsIndex';

// Every source file, as text: the anchors are in many components and most of
// them only render inside a running Settings tab.
const sources = import.meta.glob(['../**/*.tsx', '!../**/*.test.tsx'], { query: '?raw', import: 'default', eager: true }) as Record<string, string>;
const all = Object.values(sources).join('\n');

describe('settings index', () => {
  it('has an anchor on the page for every entry', () => {
    const missing = SETTINGS.filter(s => {
      if (s.section === 'ai') return false; // one anchor per tab, built from the tab name
      return !all.includes(`settingAnchor('${s.id}')`) && !all.includes(`setting="${s.id}"`)
        && !all.includes(`: '${s.id}'`); // machine settings, mapped from their settings.json key
    });
    expect(missing.map(s => s.id)).toEqual([]);
  });

  it('names a real section for every entry, and no id twice', () => {
    for (const s of SETTINGS) {
      expect(SECTION_LABEL[s.section], s.id).toBeTruthy();
      // A fallback is another entry, in the same section.
      if (s.within) expect(SETTINGS.find(x => x.id === s.within)?.section, s.id).toBe(s.section);
    }
    expect(new Set(SETTINGS.map(s => s.id)).size).toBe(SETTINGS.length);
  });

  it('matches on every word, by label, section or keyword, in any order', () => {
    const theme = SETTINGS.find(s => s.id === 'theme')!;
    expect(matchesSetting(theme, 'dark mode')).toBe(true);
    expect(matchesSetting(theme, 'general color')).toBe(true);
    expect(matchesSetting(theme, 'theme password')).toBe(false);
    const port = SETTINGS.filter(s => matchesSetting(s, 'port')).map(s => s.id);
    expect(port).toContain('listen-address');
    // Word starts, not substrings: "port" is not inside "import".
    expect(port).not.toContain('import-source');
    expect(matchesSetting(SETTINGS.find(s => s.id === 'calendars-connected')!, 'calen')).toBe(true);
  });
});
