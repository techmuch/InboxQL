import type { MailtoDraft } from './emailLinks';

/** What New message opens with: a mailto link's fields, or a reply or forward. */
export type ComposeStart = MailtoDraft & {
  /** The account the original arrived on, which the answer goes out from. */
  accountId?: string;
  /** The original's Message-ID, so the reply joins its conversation. */
  inReplyTo?: string;
  kind?: 'reply' | 'forward';
};

interface Original {
  accountId?: string;
  messageId?: string;
  from?: string;
  fromName?: string;
  to?: string[];
  cc?: string[];
  subject?: string;
  date?: string;
  body?: string;
}

const prefixed = (prefix: 'Re' | 'Fwd', subject = '') =>
  new RegExp(`^${prefix}:`, 'i').test(subject.trim()) ? subject.trim() : `${prefix}: ${subject.trim()}`.trim();

const when = (date?: string) =>
  date ? new Date(date).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : '';

const sender = (m: Original) => (m.fromName ? `${m.fromName} <${m.from}>` : m.from ?? '');

/** A reply to the sender, quoting what they wrote, in their conversation. */
export function replyTo(m: Original): ComposeStart {
  const text = (m.body ?? '').replace(/\s+$/, '');
  const quoted = text ? text.split('\n').map(l => `> ${l}`).join('\n') : '';
  return {
    kind: 'reply',
    accountId: m.accountId,
    inReplyTo: m.messageId,
    to: m.from ?? '',
    cc: '',
    subject: prefixed('Re', m.subject),
    body: quoted ? `\n\nOn ${when(m.date)}, ${sender(m)} wrote:\n${quoted}\n` : '',
  };
}

/**
 * The message passed on, with who sent it, when, and to whom above it. Its
 * text only: the attachments do not travel with it.
 */
export function forward(m: Original): ComposeStart {
  const head = [
    '---------- Forwarded message ----------',
    `From: ${sender(m)}`,
    m.date ? `Date: ${when(m.date)}` : '',
    `Subject: ${m.subject ?? ''}`,
    m.to?.length ? `To: ${m.to.join(', ')}` : '',
    m.cc?.length ? `Cc: ${m.cc.join(', ')}` : '',
  ].filter(Boolean).join('\n');
  return {
    kind: 'forward',
    accountId: m.accountId,
    to: '',
    cc: '',
    subject: prefixed('Fwd', m.subject),
    body: `\n\n${head}\n\n${(m.body ?? '').replace(/\s+$/, '')}\n`,
  };
}
