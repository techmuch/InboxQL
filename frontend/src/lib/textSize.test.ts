import { describe, expect, it } from 'vitest';

/**
 * The smallest text the interface draws is 11px.
 *
 * Counts, badges and chips had drifted down to 9 and 10px — under the size
 * that stays legible on an ordinary display at arm's length, and the first
 * thing to go when somebody's eyes are tired. Raising them once is easy; this
 * keeps them raised, because an arbitrary size is one keystroke away.
 */
const MIN = 11;

// Every source file's text, through the bundler rather than the file system,
// so the test needs nothing the app's own types do not have.
const files = import.meta.glob<string>(['../**/*.{ts,tsx,css}', '!../**/*.test.{ts,tsx}'], {
  query: '?raw', import: 'default', eager: true,
});

describe('text size', () => {
  it(`never goes below ${MIN}px`, () => {
    expect(Object.keys(files).length).toBeGreaterThan(50);
    const small: string[] = [];
    for (const [file, text] of Object.entries(files)) {
      for (const m of text.matchAll(/text-\[(\d+(?:\.\d+)?)px\]|fontSize:\s*(\d+(?:\.\d+)?)\b|font-size:\s*(\d+(?:\.\d+)?)px/g)) {
        const size = Number(m[1] ?? m[2] ?? m[3]);
        if (size < MIN) small.push(`${file}: ${m[0]}`);
      }
    }
    expect(small).toEqual([]);
  });
});
