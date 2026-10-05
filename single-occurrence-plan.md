# A file on one message should link to it

*4 October 2026.*

`AttachmentOccurrences` returned "This file appears on one message only." for
any file with one occurrence or fewer, discarding the occurrence it had just
fetched — the row that would have opened the message.

## The three cases

| Occurrences | Where the panel is | Shows |
|---|---|---|
| 1 | File tab, or another message's viewer | the row, under "1 message" |
| 1, and it is the message being read | inline in that message | "Only on this message." |
| 0 | anywhere | "Not on any stored message." |

The middle case keeps a sentence because the row would link to the page it is
on. The last was previously reported as "one message", which was false.

Files attached twice to the same message keep today's behaviour: both rows,
since both are real parts of that mail.

## What was built

All three cases, in `AttachmentOccurrences`. Verified in a browser on a copy of
the dev mailbox with `Receipt.pdf` (one message):

- **File tab** — "1 message" and the row; clicking it opened
  *Fwd: Receipt for payment received 10/24/2024* in the Message tab.
- **Inline, in that same message** — "Only on this message.", no self-link.
- **No stored message** — covered by a test; the old wording claimed "one".

The `AttachmentViewer` tests had been waiting for the panel to settle by
matching the old sentence, so they broke on the rewording. They now wait for the
loading line to clear instead, which no future rewording can break.

181 frontend tests, real typecheck clean, no console errors. No Go changes.
