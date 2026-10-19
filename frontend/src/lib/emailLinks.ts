/**
 * Links inside an HTML message.
 *
 * The message is shown in a sandboxed frame with no scripts. A link left to
 * itself tries to load the site inside that frame, which most sites refuse,
 * and a link with target=_blank opens a tab that keeps the sandbox, so the
 * site's own scripts do not run. Clicks are therefore handled here, from the
 * page that owns the frame:
 *
 * - http and https open in a new tab, with no opener and no referrer, so the
 *   site can neither reach back into InboxQL nor learn its address;
 * - mailto opens New message, filled in;
 * - #anchor scrolls within the message;
 * - anything else — javascript:, data:, file: — does nothing.
 *
 * prepareHtml is the fallback for a click that gets past the handler: every
 * link targets a new tab by default, and no referrer is sent. It also holds
 * back remote images until they are asked for.
 */

export type MailtoDraft = { to: string; cc: string; subject: string; body: string };

export type LinkAction =
  | { kind: 'web'; url: string }
  | { kind: 'mail'; draft: MailtoDraft }
  | { kind: 'anchor'; id: string }
  | { kind: 'ignore' };

const HEAD = '<base target="_blank"><meta name="referrer" content="no-referrer">';

/**
 * What the message may load. Remote images, stylesheets and fonts tell the
 * sender the message was opened, when, and from what address — a tracking
 * pixel is nothing more — so until the person asks, only what the message
 * carries inside itself is allowed. Frames and plugins never are.
 */
const POLICY = {
  blocked: "default-src 'none'; img-src data: blob:; media-src data: blob:; font-src data:; style-src 'unsafe-inline'",
  allowed: "default-src * data: blob: 'unsafe-inline'; frame-src 'none'; object-src 'none'",
};
const csp = (remote: boolean, inlineFrom?: string) => {
  // Images carried as parts of the message (cid:) are served by InboxQL.
  // Exactly that path is allowed, not the whole origin: a message has no
  // business fetching anything else from the API with the user's cookie.
  const policy = remote || !inlineFrom
    ? (remote ? POLICY.allowed : POLICY.blocked)
    : POLICY.blocked.replace('img-src data: blob:', `img-src data: blob: ${inlineFrom}`);
  return `<meta http-equiv="Content-Security-Policy" content="${policy}">`;
};

/** Where InboxQL serves an attachment's bytes, by content hash. */
export const INLINE_PATH = '/api/attachments/content';

/**
 * Point cid: references at the parts they name. A message's logo or a
 * pasted screenshot arrives as a part with a Content-ID, referenced from the
 * HTML as cid:<id>; a browser cannot resolve that on its own. A cid that
 * names no stored part is left alone and shows as a missing image.
 */
export const resolveCids = (html: string, parts: Record<string, string>): string =>
  html.replace(/cid:([^"'\s)>]+)/gi, (whole, id: string) => {
    let key = id;
    try { key = decodeURIComponent(id); } catch { /* keep it as written */ }
    return parts[key] ?? parts[key.toLowerCase()] ?? whole;
  });

/** The cid map for a message's attachments: Content-ID to the URL of its bytes. */
export const cidParts = (attachments: { contentId?: string; contentHash?: string; storagePath?: string }[]) => {
  const parts: Record<string, string> = {};
  for (const a of attachments) {
    if (!a.contentId || !a.contentHash) continue;
    const url = `${INLINE_PATH}?key=${encodeURIComponent(a.contentHash)}`;
    parts[a.contentId] = url;
    parts[a.contentId.toLowerCase()] = url;
  }
  return parts;
};

/**
 * The message's HTML with a default target, no referrer, and a policy on
 * remote content. Inserted inside <head>, or after the doctype and <html>,
 * never before the doctype: that would put the message into quirks mode and
 * change how it lays out. Ours comes first, so the policy covers everything
 * the message loads, and the first <base> target is the one a browser uses.
 */
export const prepareHtml = (
  html: string,
  opts: { remote?: boolean; parts?: Record<string, string>; origin?: string } = {},
): string => {
  const hasParts = !!opts.parts && Object.keys(opts.parts).length > 0;
  if (hasParts) html = resolveCids(html, opts.parts!);
  const ours = csp(!!opts.remote, hasParts && opts.origin ? opts.origin + INLINE_PATH : undefined) + HEAD;
  const head = /<head(\s[^>]*)?>/i.exec(html);
  if (head) {
    const at = head.index + head[0].length;
    return html.slice(0, at) + ours + html.slice(at);
  }
  const lead = /^\s*(<!doctype[^>]*>)?\s*(<html(\s[^>]*)?>)?/i.exec(html);
  const at = lead ? lead[0].length : 0;
  return html.slice(0, at) + ours + html.slice(at);
};

const REMOTE = [
  // <img src>, srcset, <video poster>, <td background>, and a base that makes
  // every relative address remote.
  /\b(?:src|srcset|background|poster)\s*=\s*["']?\s*(?:https?:)?\/\//i,
  /<base\b[^>]*\bhref\s*=\s*["']?\s*(?:https?:)?\/\//i,
  // A stylesheet, and CSS that fetches: url(…) and @import.
  /<link\b[^>]*\bhref\s*=\s*["']?\s*(?:https?:)?\/\//i,
  /url\(\s*["']?\s*(?:https?:)?\/\//i,
  /@import\s+["']?\s*(?:https?:)?\/\//i,
];

/** Whether the message would load anything from the web, so Load images has something to do. */
export const hasRemoteContent = (html: string): boolean => REMOTE.some(r => r.test(html));

const decode = (s: string) => {
  try { return decodeURIComponent(s.replace(/\+/g, '%20')); } catch { return s; }
};

/** RFC 6068: mailto:a@x,b@y?cc=…&subject=…&body=… */
export const parseMailto = (href: string): MailtoDraft => {
  const rest = href.replace(/^mailto:/i, '');
  const q = rest.indexOf('?');
  const draft: MailtoDraft = { to: decode(q < 0 ? rest : rest.slice(0, q)), cc: '', subject: '', body: '' };
  if (q >= 0) {
    for (const pair of rest.slice(q + 1).split('&')) {
      const eq = pair.indexOf('=');
      const key = (eq < 0 ? pair : pair.slice(0, eq)).toLowerCase();
      const value = eq < 0 ? '' : decode(pair.slice(eq + 1));
      if (key === 'to') draft.to = [draft.to, value].filter(Boolean).join(', ');
      else if (key === 'cc') draft.cc = value;
      else if (key === 'subject') draft.subject = value;
      else if (key === 'body') draft.body = value;
    }
  }
  return draft;
};

/**
 * What a link's href should do. `base` is the message's own <base href>, if
 * it has one, so a relative link resolves the way the sender meant it.
 */
export const linkAction = (href: string | null | undefined, base?: string): LinkAction => {
  const raw = (href ?? '').trim();
  if (!raw) return { kind: 'ignore' };
  if (raw.startsWith('#')) return raw.length > 1 ? { kind: 'anchor', id: decode(raw.slice(1)) } : { kind: 'ignore' };
  if (/^mailto:/i.test(raw)) return { kind: 'mail', draft: parseMailto(raw) };
  let url: URL;
  try { url = base ? new URL(raw, base) : new URL(raw); } catch { return { kind: 'ignore' }; }
  if (url.protocol === 'http:' || url.protocol === 'https:') return { kind: 'web', url: url.href };
  return { kind: 'ignore' };
};

const hostOf = (s: string) => s.toLowerCase().replace(/^www\./, '');

/**
 * The host the link's text claims, when it names one different from where
 * the link really goes — the shape of most phishing. "Click here" names no
 * host and is not a mismatch; a subdomain of the named host is not either.
 */
export const claimedHost = (text: string, url: string): string | null => {
  const m = /(?:https?:\/\/)?((?:[a-z0-9-]+\.)+[a-z]{2,})(?=[\s/:?#]|$)/i.exec(text.trim());
  if (!m) return null;
  let real: string;
  try { real = hostOf(new URL(url).hostname); } catch { return null; }
  const said = hostOf(m[1]);
  if (real === said || real.endsWith('.' + said)) return null;
  return said;
};

/** Where the pointer or focus is: the real address, and a host the text claims instead. */
export type LinkHover = { url: string; claims: string | null } | null;

export type LinkHandlers = {
  openWeb: (url: string) => void;
  openMail: (draft: MailtoDraft) => void;
  onHover?: (hover: LinkHover) => void;
};

const linkIn = (t: EventTarget | null): HTMLAnchorElement | HTMLAreaElement | null => {
  const el = t as Element | null;
  return el && typeof el.closest === 'function'
    ? (el.closest('a[href], area[href]') as HTMLAnchorElement | HTMLAreaElement | null)
    : null;
};

/**
 * Take over every link click in the message's document. Its listeners go
 * with the document, so nothing needs removing when the message changes.
 */
export const installLinkHandlers = (doc: Document, h: LinkHandlers) => {
  const base = doc.querySelector('base[href]')?.getAttribute('href') ?? undefined;
  const act = (e: MouseEvent) => {
    const a = linkIn(e.target);
    if (!a) return;
    if (e.type === 'auxclick' && e.button !== 1) return;
    e.preventDefault();
    const action = linkAction(a.getAttribute('href'), base);
    if (action.kind === 'web') h.openWeb(action.url);
    else if (action.kind === 'mail') h.openMail(action.draft);
    else if (action.kind === 'anchor') {
      const target = doc.getElementById(action.id) ?? doc.getElementsByName(action.id)[0];
      target?.scrollIntoView?.({ block: 'start' });
    }
  };
  doc.addEventListener('click', act, true);
  doc.addEventListener('auxclick', act, true);

  if (!h.onHover) return;
  const show = (e: Event) => {
    const a = linkIn(e.target);
    if (!a) return;
    const action = linkAction(a.getAttribute('href'), base);
    if (action.kind === 'web') h.onHover!({ url: action.url, claims: claimedHost(a.textContent ?? '', action.url) });
    else if (action.kind === 'mail') h.onHover!({ url: a.getAttribute('href')!.trim(), claims: null });
    else h.onHover!(null);
  };
  const hide = (e: Event) => {
    const next = (e as MouseEvent | FocusEvent).relatedTarget;
    if (linkIn(e.target) && linkIn(next) !== linkIn(e.target)) h.onHover!(null);
  };
  doc.addEventListener('mouseover', show);
  doc.addEventListener('focusin', show);
  doc.addEventListener('mouseout', hide);
  doc.addEventListener('focusout', hide);
};

/** A new tab that cannot reach back into InboxQL and is not told where it came from. */
export const openInNewTab = (url: string) => {
  window.open(url, '_blank', 'noopener,noreferrer');
};
