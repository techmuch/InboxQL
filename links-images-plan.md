# Links and remote content in HTML mail

*19 October 2026.*

An HTML message is shown in a sandboxed frame, with scripts off because the
frame shares the app's origin. Two problems followed from that: links did not
work, and remote images loaded the moment a message opened.

## Phase 1 — links open in a new tab

Clicks inside the message are handled from the page that owns the frame
(`frontend/src/lib/emailLinks.ts`):

- http and https: a new tab, `noopener,noreferrer`;
- `mailto:`: New message, filled in;
- `#anchor`: scrolls within the message;
- anything else: nothing.

## Phase 2 — fallbacks

- `<base target="_blank">` and `<meta name="referrer" content="no-referrer">`
  inserted into the message, after the doctype so it stays in standards mode;
- `allow-popups-to-escape-sandbox`, so a tab the message opens works normally.

## Phase 3 — where a link really goes

The real address shows at the bottom left of the window on hover or focus,
with a warning when the link's text names a different site.

## Phase 4 — remote images wait until asked

Remote images, stylesheets and fonts tell the sender the message was opened,
and from where. A Content-Security-Policy in the message allows only content
carried inside it (`data:`) until **Load images** is pressed for that message.
The bar offering it appears only when the message has something remote.

## Phase 5 — images carried in the message

A logo or a pasted screenshot arrives as a part with a Content-ID, referenced
from the HTML as `cid:…`, which a browser cannot resolve, so these never
showed. Each `cid:` is pointed at the stored part's bytes
(`/api/attachments/content?key=<hash>`), and the message's policy allows
exactly that path — not the whole origin, so a message cannot fetch anything
else from the API with the user's cookie.

## Phase 6 — the first click after a reload

In browser checks the first click on a message after reloading the page often
did nothing. Investigated: no event reached the page at all — not even
`pointerdown` — until the automation tool had taken a screenshot. Events
dispatched in the page, and real clicks once input arrived, opened the message
first time. A quirk of the testing tool, not of InboxQL; nothing to change.

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5
- [x] Phase 6 (no defect)

Checked in a browser against `internal/imaptest/devmail`, whose `/deliver`
now takes a raw message.
