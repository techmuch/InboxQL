# Files: every time one was sent

*A phased plan. 4 October 2026.*

## The hole in the middle

`in:attachments` lists each distinct file once, however many messages carried
it. That is the design, and the table prints **"2 messages"** to prove it.

The text is dead. There is no link, and — the part that matters — no term to
write:

```
in:attachments id:80202167…      → 1 file
has:attachment file:80202167…    → has: "attachment" is not one of text
```

So the payoff of content addressing is unreachable from the interface *and*
from the language. Everything else in this plan is decoration on a model with a
hole in it.

## Measured on this mailbox

```
8  files sent in genuinely different messages
2  files attached more than once within ONE message
```

That second number is the trap. A naive count over attachment rows says those
two were "sent twice" when they were sent **once, with the file attached
twice** — a signature image referenced in two places. The count must be over
distinct messages while the list still shows both rows, or the feature
misleads about the one thing it exists to show.

## Two questions, only one worth building

**Same bytes, several messages.** What the content hash already captures, and
what was asked for.

**Same name, different bytes** — a revised contract. On this mailbox the only
hits are `image001.png` (3 versions) and `image.png` (2): Outlook signature
images. Pure noise. It sounds like the more interesting feature and the data
says it is not, so it is not built.

---

## Phase 1 — `file:` on a message

A message field naming an attachment by content hash, so "mail carrying this
file" is a query somebody could have typed. The panel is then a drill-down like
every other one here rather than a bespoke endpoint.

## Phase 2 — Every time it was sent

A panel in the file preview: when, from whom, which message, oldest first, each
row opening that message. Counted over distinct messages; listed over rows, so
a file attached twice to one mail shows both without claiming two sends.

It makes forwarding visible for the first time — a document that arrives, goes
round and comes back reads as one thing travelling rather than four unrelated
files.

## Phase 3 — The questions the table cannot answer

Sortable columns. `| sort size desc` already works in the language and the
headers are not clickable, so "what is filling my disk" is three keystrokes of
a language you have to know first.

And the silences counted where somebody would see them: `is:unread` is nobody
has looked inside, `is:scanned` is read and holds no text. Five files here are
scans nothing can search.

## Phase 4 — Acting from where you are standing

A scan says "scanned" and the fix lives in Maintenance. The thing in front of
you should offer it. Storage too: `blobstore.Usage()` exists and only the CLI
has ever shown it.

## Not doing

**Revision tracking.** Measured above: noise on this mailbox.

**Folders or tags for files.** That is inventing a filesystem inside a mail
client. The query language is the organiser.

**A thumbnail grid.** The biggest *felt* gap and the least fundamental; it
wants its own plan once the model is whole.

---

## What was built

All four phases, verified against the real mailbox (50 files).

### Phase 1 — `attached:`, not `file:`

Shipped as **`attached:`** with alias `carries:`, not `file:` as the plan wrote
it. The rename came out of the implementation: `file:` is already a term on an
attachment query, where it means the name a file arrived under. The same spelling
meaning *filename* in one query and *this exact file* in another, selected by an
`in:` clause somewhere else in the expression, is a bug nobody finds twice.

- `internal/query/compile.go` — `attachedTerm`, an `EXISTS` over `attachments`.
  The value is a content-hash prefix when it is 8–64 hex characters
  (`isHashPrefix`) and a filename otherwise, so `=` and `*` work as elsewhere.
- `internal/query/fields.go` — registered with the comment recording *why* the
  name is not `file:`, because the next person will want to shorten it.
- Negation wraps the `EXISTS`, so `-attached:x` is *no attachment of this
  message is x* — the same reading as `-to:x`.

Verified: `attached:128072f0de5e` → 2 messages; `-attached:128072f0de5e` → 50;
`attached:*.pdf` → 26; and `in:attachments file:*health*` → 2 attachments,
unchanged.

### Phase 2 — the correction

**The occurrence panel already existed.** `ListAttachmentOccurrences`,
`/api/attachments/occurrences` and the panel in `AttachmentPreview.tsx` were all
there, and I wrote a second `FileOccurrences`/`CountFileSends` before noticing.
Deleted.

What was actually missing was the two things the plan named as consequences
rather than features: the **language term** (Phase 1, above) and the
**counting precision**. The panel was summing rows.

```tsx
{file.messages} {file.messages === 1 ? 'message' : 'messages'}
{occurrences.length > file.messages && (<> · attached {occurrences.length} times</>)}
```

Named only on disagreement. On this mailbox that is exactly one file —
`image002.png`, one message, two rows — which now reads **"1 message · attached
2 times"** instead of claiming two sends. `Screenshot 2024-02-13…` is the honest
case: 2 messages, 2 rows, both in *Re: Bitwarden Support*, three days apart.

### Phase 3 — sorting and the silences

- `SortHeader` in `Desk/Results.tsx`, wired through the existing composer
  (`onDrillDown([], 'sort size desc')`) rather than a second sort path — so the
  query bar shows `in:attachments | sort size desc` and the user learns the
  language by clicking.
- `FileSilences`: the three absences counted where they are seen, each a
  drill-down. On this mailbox: *5 are scans nothing can search*.

### Phase 4 — acting from here

- `GET /api/attachments/usage` → `handleAttachmentUsage`, over
  `blobstore.Usage()` and the new `store.AttachmentStorageCounts()` — which
  counts over the content address, not rows, for the same reason as above.
- The bar reads **"10.7 MB in 50 files · 5 are scans nothing can search"**, with
  `not stored` drilling to `is:missing` when it is non-zero.
- OCR offered from the preview as a **link into Maintenance**, deliberately not
  a one-click button: OCR sends every page to a model that may be remote, and
  that is a decision, not a convenience.

### Documented

`AGENTS.md` gained the `attached:` row in the mail term table, a
*"Going the other way"* section covering the hash-or-name rule and why the name
is not `file:`, and a *"Sends are not rows"* section recording the 8-vs-2
measurement so the next caller counts over distinct messages and lists over
rows.
