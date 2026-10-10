import { describe, it, expect } from 'vitest';
import { initials } from './avatars';

describe('initials', () => {
  it('takes the first and last word of a name', () => {
    expect(initials('Ada Augusta Lovelace', 'ada@x.com')).toBe('AL');
    expect(initials('"Ada Lovelace" <ada@x.com>', null)).toBe('AL');
    expect(initials('ada@x.com', null)).toBe('A');
    expect(initials('Ada', 'ada@x.com')).toBe('A');
  });
  it('falls back to the address', () => {
    expect(initials('', 'bob@x.com')).toBe('B');
    expect(initials(null, null)).toBe('?');
  });
});
