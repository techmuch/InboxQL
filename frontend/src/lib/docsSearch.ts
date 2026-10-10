/**
 * Searching the guides.
 *
 * The whole index — every section of every guide — is about a hundred
 * kilobytes, so it is fetched once and searched here, on every keystroke,
 * with nothing to wait for. The site's search does the same over the same
 * index, written out by cmd/docsgen.
 *
 * # How a result is ranked
 *
 * Every word of the query must appear in the section, as a word or the start
 * of one, so "calib" finds calibration. Then a match in the guide's title
 * outranks one in the section's heading, which outranks one in its text, and
 * the whole query appearing as a phrase outranks its words scattered about.
 * Nothing cleverer: a ranking somebody cannot predict is one they cannot
 * search with.
 */

export interface DocSection {
  slug: string;
  title: string;
  heading: string;
  id: string;
  text: string;
}

export interface DocHit {
  section: DocSection;
  score: number;
  /** The text around the first match, with the matches marked. */
  snippet: { text: string; mark: boolean }[];
}

export const tokens = (q: string): string[] =>
  q.toLowerCase().split(/[^\p{L}\p{N}_.:-]+/u).map(t => t.replace(/^[.:-]+|[.:-]+$/g, '')).filter(Boolean);

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Whether a word starts somewhere in text. */
const wordStart = (word: string) => new RegExp(`(^|[^\\p{L}\\p{N}])${escape(word)}`, 'iu');

export function searchDocs(index: DocSection[], query: string, limit = 30): DocHit[] {
  const words = tokens(query);
  if (words.length === 0) return [];
  const phrase = query.trim().toLowerCase();
  const tests = words.map(wordStart);
  const hits: DocHit[] = [];

  for (const section of index) {
    const all = `${section.title} ${section.heading} ${section.text}`;
    if (!tests.every(t => t.test(all))) continue;

    let score = 0;
    for (const t of tests) {
      if (t.test(section.title)) score += 8;
      if (t.test(section.heading)) score += 5;
      const inText = section.text.match(new RegExp(t.source, 'giu'));
      score += Math.min(inText?.length ?? 0, 5);
    }
    const lower = all.toLowerCase();
    if (words.length > 1 && lower.includes(phrase)) score += 10;
    if (section.heading.toLowerCase().includes(phrase)) score += 6;

    hits.push({ section, score, snippet: snippet(section.text, words) });
  }
  hits.sort((a, b) => b.score - a.score);
  return hits.slice(0, limit);
}

/** About 180 characters of text around the first match, matches marked. */
export function snippet(text: string, words: string[], width = 180): DocHit['snippet'] {
  const lower = text.toLowerCase();
  let first = -1;
  for (const w of words) {
    const i = lower.search(wordStart(w));
    if (i >= 0 && (first < 0 || i < first)) first = i;
  }
  let start = Math.max(0, first < 0 ? 0 : first - 50);
  if (start > 0) {
    const space = text.indexOf(' ', start);
    if (space >= 0 && space - start < 20) start = space + 1;
  }
  let piece = text.slice(start, start + width);
  if (start + width < text.length) piece = piece.replace(/\s+\S*$/, '') + ' …';
  if (start > 0) piece = '… ' + piece;

  const re = new RegExp(`(${words.map(escape).join('|')})`, 'giu');
  const out: DocHit['snippet'] = [];
  let last = 0;
  for (const m of piece.matchAll(re)) {
    const i = m.index ?? 0;
    if (i > last) out.push({ text: piece.slice(last, i), mark: false });
    out.push({ text: m[0], mark: true });
    last = i + m[0].length;
  }
  if (last < piece.length) out.push({ text: piece.slice(last), mark: false });
  return out;
}
