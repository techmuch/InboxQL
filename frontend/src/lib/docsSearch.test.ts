import { describe, it, expect } from 'vitest';
import { searchDocs, snippet, tokens, type DocSection } from './docsSearch';

const index: DocSection[] = [
  { slug: 'labels', title: 'Labels, extractors and review', heading: 'Calibration', id: 'calibration',
    text: 'Once an annotator has at least twenty reviewed rulings, iql laya calibrate fits its scores.' },
  { slug: 'settings', title: 'Settings and the System section', heading: 'Machine settings', id: 'machine-settings',
    text: 'Heavy work on battery lets the annotation pass after a sync run on battery.' },
  { slug: 'installing', title: 'Installing, updating and removing', heading: 'On battery', id: 'on-battery',
    text: 'On a laptop running on battery, InboxQL holds back the heavy work nobody asked for.' },
];

describe('searchDocs', () => {
  it('needs every word, matched at the start of a word', () => {
    expect(searchDocs(index, 'calib').map(h => h.section.id)).toEqual(['calibration']);
    expect(searchDocs(index, 'alibration')).toEqual([]);
    expect(searchDocs(index, 'battery calibrate')).toEqual([]);
  });

  it('ranks a heading match above a mention in the text', () => {
    const hits = searchDocs(index, 'battery');
    expect(hits[0].section.id).toBe('on-battery');
    expect(hits.map(h => h.section.id)).toContain('machine-settings');
  });

  it('ranks the whole phrase above scattered words', () => {
    const hits = searchDocs(index, 'heavy work');
    expect(hits.length).toBe(2);
    expect(hits.every(h => h.score > 0)).toBe(true);
  });

  it('finds query syntax', () => {
    expect(tokens('label:x OR in:contacts')).toEqual(['label:x', 'or', 'in:contacts']);
  });

  it('returns nothing for an empty query', () => {
    expect(searchDocs(index, '   ')).toEqual([]);
  });
});

describe('snippet', () => {
  it('marks the matches and trims to a window', () => {
    const s = snippet('a '.repeat(100) + 'the calibration step ' + 'b '.repeat(200), ['calibration']);
    expect(s.some(p => p.mark && p.text.toLowerCase() === 'calibration')).toBe(true);
    const text = s.map(p => p.text).join('');
    expect(text.startsWith('… ')).toBe(true);
    expect(text.endsWith(' …')).toBe(true);
  });
});
