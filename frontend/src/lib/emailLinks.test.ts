import { describe, it, expect, vi } from 'vitest';
import { cidParts, claimedHost, hasRemoteContent, resolveCids, installLinkHandlers, linkAction, parseMailto, prepareHtml } from './emailLinks';

describe('prepareHtml', () => {
  const ours = '<base target="_blank"><meta name="referrer" content="no-referrer">';

  it('goes inside <head>, first', () => {
    const out = prepareHtml('<!DOCTYPE html><html><head><title>x</title></head><body>hi</body></html>');
    expect(out.startsWith('<!DOCTYPE html><html><head><meta http-equiv="Content-Security-Policy"')).toBe(true);
    expect(out).toContain(ours + '<title>x</title>');
  });

  it('never goes before the doctype', () => {
    const out = prepareHtml('<!doctype html>\n<html lang="en"><body>hi</body></html>');
    expect(out.startsWith('<!doctype html>\n<html lang="en"><meta http-equiv=')).toBe(true);
  });

  it('prefixes a fragment', () => {
    const out = prepareHtml('<p>hi</p>');
    expect(out.startsWith('<meta http-equiv=')).toBe(true);
    expect(out.endsWith(ours + '<p>hi</p>')).toBe(true);
  });

  it('holds back remote content until asked', () => {
    expect(prepareHtml('<p>hi</p>')).toContain(`img-src data: blob:;`);
    expect(prepareHtml('<p>hi</p>')).not.toContain('default-src *');
    const allowed = prepareHtml('<p>hi</p>', { remote: true });
    expect(allowed).toContain('default-src *');
    expect(allowed).toContain("frame-src 'none'");
  });
});

describe('hasRemoteContent', () => {
  it('finds what would be fetched', () => {
    for (const h of [
      '<img src="https://t.example/p.gif">',
      "<img src='http://x.example/a.png'>",
      '<img src=//cdn.example/a.png>',
      '<img srcset="https://x.example/a.png 2x">',
      '<td background="https://x.example/bg.jpg">',
      '<link rel="stylesheet" href="https://x.example/s.css">',
      '<div style="background:url(https://x.example/a.png)">',
      "<style>@import 'https://x.example/s.css';</style>",
      '<base href="https://x.example/"><img src="a.png">',
    ]) expect(hasRemoteContent(h)).toBe(true);
  });

  it('is quiet when everything is inside the message', () => {
    for (const h of [
      '<p>Visit https://example.com today</p>',
      '<a href="https://example.com">a link is not a fetch</a>',
      '<img src="data:image/png;base64,AAAA">',
      '<img src="cid:logo@x">',
    ]) expect(hasRemoteContent(h)).toBe(false);
  });
});

describe('linkAction', () => {
  it('opens the web in a tab', () => {
    expect(linkAction('https://example.com/a?b=1')).toEqual({ kind: 'web', url: 'https://example.com/a?b=1' });
    expect(linkAction(' HTTP://Example.com ')).toEqual({ kind: 'web', url: 'http://example.com/' });
  });

  it('resolves a relative link against the message base', () => {
    expect(linkAction('/track/1', 'https://news.example.com/x/')).toEqual({ kind: 'web', url: 'https://news.example.com/track/1' });
    expect(linkAction('/track/1')).toEqual({ kind: 'ignore' });
  });

  it('refuses everything that is not the web, mail or an anchor', () => {
    for (const h of ['javascript:alert(1)', 'JavaScript:alert(1)', 'data:text/html,<b>', 'file:///etc/passwd', 'vbscript:x', '', '#', null]) {
      expect(linkAction(h)).toEqual({ kind: 'ignore' });
    }
  });

  it('keeps anchors in the message', () => {
    expect(linkAction('#section-2')).toEqual({ kind: 'anchor', id: 'section-2' });
  });

  it('turns mailto into a draft', () => {
    expect(linkAction('mailto:a@x.com')).toEqual({ kind: 'mail', draft: { to: 'a@x.com', cc: '', subject: '', body: '' } });
  });
});

describe('parseMailto', () => {
  it('reads every field', () => {
    expect(parseMailto('mailto:a@x.com,b@y.com?cc=c@z.com&subject=Hello%20there&body=Line%201%0ALine+2'))
      .toEqual({ to: 'a@x.com,b@y.com', cc: 'c@z.com', subject: 'Hello there', body: 'Line 1\nLine 2' });
  });

  it('adds to= to the address', () => {
    expect(parseMailto('mailto:a@x.com?to=b@y.com').to).toBe('a@x.com, b@y.com');
    expect(parseMailto('mailto:?to=b@y.com').to).toBe('b@y.com');
  });

  it('survives bad escapes', () => {
    expect(parseMailto('mailto:a@x.com?subject=100%').subject).toBe('100%');
  });
});

describe('claimedHost', () => {
  it('flags text that names another site', () => {
    expect(claimedHost('www.paypal.com', 'https://evil.example/login')).toBe('paypal.com');
    expect(claimedHost('https://mybank.com/login', 'https://mybank.com.evil.io/')).toBe('mybank.com');
  });

  it('is quiet when the text names the same site, a parent, or none', () => {
    expect(claimedHost('paypal.com', 'https://www.paypal.com/x')).toBeNull();
    expect(claimedHost('example.com', 'https://mail.example.com/')).toBeNull();
    expect(claimedHost('Click here', 'https://anything.io/')).toBeNull();
    expect(claimedHost('Read more…', 'https://anything.io/')).toBeNull();
  });
});

describe('installLinkHandlers', () => {
  const setup = (html: string) => {
    const doc = document.implementation.createHTMLDocument('m');
    doc.documentElement.innerHTML = html;
    const h = { openWeb: vi.fn(), openMail: vi.fn(), onHover: vi.fn() };
    installLinkHandlers(doc, h);
    return { doc, h };
  };
  const click = (el: Element, init: MouseEventInit = {}, type = 'click') => {
    const e = new MouseEvent(type, { bubbles: true, cancelable: true, ...init });
    el.dispatchEvent(e);
    return e;
  };

  it('opens a link in a tab, from a click anywhere inside it', () => {
    const { doc, h } = setup('<body><a href="https://example.com" target="_self"><b id="in">Go</b></a></body>');
    const e = click(doc.getElementById('in')!);
    expect(e.defaultPrevented).toBe(true);
    expect(h.openWeb).toHaveBeenCalledWith('https://example.com/');
  });

  it('middle-click too, but not a right click', () => {
    const { doc, h } = setup('<body><a id="a" href="https://example.com/">Go</a></body>');
    click(doc.getElementById('a')!, { button: 1 }, 'auxclick');
    click(doc.getElementById('a')!, { button: 2 }, 'auxclick');
    expect(h.openWeb).toHaveBeenCalledTimes(1);
  });

  it('uses the message base for relative links', () => {
    const { doc, h } = setup('<head><base href="https://shop.example/"></head><body><a id="a" href="orders/7">Order</a></body>');
    click(doc.getElementById('a')!);
    expect(h.openWeb).toHaveBeenCalledWith('https://shop.example/orders/7');
  });

  it('opens New message for mailto', () => {
    const { doc, h } = setup('<body><a id="a" href="mailto:hi@x.com?subject=Hey">Write</a></body>');
    click(doc.getElementById('a')!);
    expect(h.openMail).toHaveBeenCalledWith({ to: 'hi@x.com', cc: '', subject: 'Hey', body: '' });
    expect(h.openWeb).not.toHaveBeenCalled();
  });

  it('swallows javascript: and leaves non-links alone', () => {
    const { doc, h } = setup('<body><a id="a" href="javascript:alert(1)">x</a><p id="p">text</p></body>');
    expect(click(doc.getElementById('a')!).defaultPrevented).toBe(true);
    expect(click(doc.getElementById('p')!).defaultPrevented).toBe(false);
    expect(h.openWeb).not.toHaveBeenCalled();
  });

  it('scrolls to an anchor', () => {
    const { doc } = setup('<body><a id="a" href="#end">down</a><h2 id="end">End</h2></body>');
    const target = doc.getElementById('end')!;
    target.scrollIntoView = vi.fn();
    click(doc.getElementById('a')!);
    expect(target.scrollIntoView).toHaveBeenCalled();
  });

  it('reports where a hovered link goes, and what its text claims', () => {
    const { doc, h } = setup('<body><a id="a" href="https://evil.example/">paypal.com</a><p id="p">x</p></body>');
    const a = doc.getElementById('a')!;
    a.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
    expect(h.onHover).toHaveBeenLastCalledWith({ url: 'https://evil.example/', claims: 'paypal.com' });
    a.dispatchEvent(new MouseEvent('mouseout', { bubbles: true, relatedTarget: doc.getElementById('p') }));
    expect(h.onHover).toHaveBeenLastCalledWith(null);
  });
});

describe('inline parts (cid:)', () => {
  const parts = cidParts([
    { contentId: 'Logo@Shop', contentHash: 'abc123' },
    { contentId: 'no-bytes@x' },
    { contentHash: 'def456' },
  ]);

  it('maps each Content-ID to its bytes', () => {
    expect(parts['Logo@Shop']).toBe('/api/attachments/content?key=abc123');
    expect(parts['logo@shop']).toBe('/api/attachments/content?key=abc123');
    expect(parts['no-bytes@x']).toBeUndefined();
  });

  it('rewrites references, and leaves unknown ones alone', () => {
    expect(resolveCids('<img src="cid:Logo@Shop"><img src=\'CID:logo%40shop\'><img src="cid:gone@x">', parts))
      .toBe('<img src="/api/attachments/content?key=abc123"><img src=\'/api/attachments/content?key=abc123\'><img src="cid:gone@x">');
    expect(resolveCids('<td style="background:url(cid:Logo@Shop)">', parts))
      .toBe('<td style="background:url(/api/attachments/content?key=abc123)">');
  });

  it('allows exactly that path, and only when there are parts', () => {
    const out = prepareHtml('<img src="cid:Logo@Shop">', { parts, origin: 'http://127.0.0.1:8420' });
    expect(out).toContain('img-src data: blob: http://127.0.0.1:8420/api/attachments/content;');
    expect(out).toContain('src="/api/attachments/content?key=abc123"');
    expect(prepareHtml('<p>x</p>', { parts: {}, origin: 'http://127.0.0.1:8420' })).not.toContain('8420');
  });
});
