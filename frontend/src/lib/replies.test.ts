import { describe, it, expect } from 'vitest';
import { forward, replyTo } from './replies';

const original = {
  accountId: 'gmail', messageId: '<abc@acme.com>', from: 'ada@acme.com', fromName: 'Ada',
  to: ['me@x.com'], cc: ['bob@acme.com'], subject: 'Quarterly report', date: '2026-10-30T13:08:00Z',
  body: 'Numbers attached.\nThanks,\nAda',
};

describe('replies', () => {
  it('replies to the sender, in the conversation, from the account it came to', () => {
    const r = replyTo(original);
    expect(r).toMatchObject({ kind: 'reply', to: 'ada@acme.com', subject: 'Re: Quarterly report', inReplyTo: '<abc@acme.com>', accountId: 'gmail' });
    expect(r.body).toMatch(/wrote:\n> Numbers attached\.\n> Thanks,\n> Ada\n$/);
    expect(r.body).toContain('Ada <ada@acme.com>');
  });

  it('does not stack Re: or Fwd:', () => {
    expect(replyTo({ ...original, subject: 'RE: Quarterly report' }).subject).toBe('RE: Quarterly report');
    expect(forward({ ...original, subject: 'Fwd: Quarterly report' }).subject).toBe('Fwd: Quarterly report');
  });

  it('forwards with who, when and to whom above the text, and no thread', () => {
    const f = forward(original);
    expect(f).toMatchObject({ kind: 'forward', to: '', subject: 'Fwd: Quarterly report', accountId: 'gmail' });
    expect(f.inReplyTo).toBeUndefined();
    expect(f.body).toMatch(/Forwarded message[\s\S]*From: Ada <ada@acme.com>[\s\S]*To: me@x.com[\s\S]*Cc: bob@acme.com[\s\S]*Numbers attached\./);
  });

  it('has nothing to quote when the message has no text', () => {
    expect(replyTo({ ...original, body: '' }).body).toBe('');
  });
});
