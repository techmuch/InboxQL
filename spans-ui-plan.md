# Showing extracted spans in the UI

*A phased plan. 20 September 2026.*

## Why

The case for the span engine is that it cannot fabricate: every value it
returns is a substring of the message, so a surprising claim can be checked
against the characters the model actually scored.

**Today that check is impossible.** `MessageViewer.tsx` contains no reference
to annotations, there is no endpoint that returns a message's spans, and the
offsets — the only thing this engine has that the LLM extractor does not — are
discarded at the API boundary. There are 258 anchored values on this mailbox
and `sqlite3` is the only way to look at them.

So the work is not "add a feature". It is: stop throwing away the thing that
makes the feature worth having.

---

## Phase 1 — The endpoint, and who does the slicing

`GET /api/messages/{id}/annotations`

`store.ListAnnotations(messageID)` already returns every row for a message,
joined against the current annotator version. What is missing is the shape the
client needs.

### The server segments the text, not the client

The offsets are **byte** offsets into UTF-8. JavaScript strings are UTF-16.
Hand raw offsets to the browser and it lands short on exactly the messages
containing a narrow no-break space — the `9:50 PM` a mail client writes — and
the result looks like a decoding bug rather than an encoding mismatch. That
mistake was already made once in this work, in the verification script, and it
cost an hour.

So the endpoint returns the text **already cut into runs**:

```json
{ "field": "body",
  "segments": [
    {"text": "Total "},
    {"text": "$80.44", "label": "amount", "score": 0.82, "id": "…"},
    {"text": "\nCheck #319"}
  ] }
```

A segment with no `label` is ordinary text. The client concatenates and marks;
it never computes an offset, so the whole class of error is gone from the
browser.

Overlaps cannot occur within one annotator — the decode is greedy and
non-overlapping — but two annotators over one message can overlap, so the
segmenter takes the highest-scoring span when they do and says so.

### Scope

Span annotators only. A rule or LLM annotator has no offsets and is reported
separately, as a plain list, so the panel can still say what else matched
without pretending it knows where.

---

## Phase 2 — Marks in the message

The plain-text view renders `{message.body}` inside a `whitespace-pre-wrap`
div. That string becomes the segment array; a labelled run gets a `<mark>`.

What a reader gets: open a receipt, and `$80.44` is marked "amount". The
value of this is not decoration — it is that **confidence becomes legible**.
Seeing `Table 31` marked "invoice number" at 0.53 next to `$675.00` at 0.90
teaches the threshold in a way a column of numbers never does.

The subject is marked too, because that is where the amount often is.

Colour carries the label, not the score; the score is in the hover. A palette
that encodes confidence would be a second thing to learn and would fight the
one control that matters, below.

---

## Phase 3 — The confidence floor as a control

A slider over the marks, with live counts.

This is not a nicety. On this mailbox, `0.6` is the line between the real
extractions and three card-shaped strings — and the only way to discover that
was to write SQL:

| floor | records | card-shaped |
|---|---|---|
| 0.5 | 258 | **3** |
| 0.6 | 201 | 0 |

A property that decides whether payment data lands in a queryable table should
not be tribal knowledge. Moving the slider dims the marks below it, so the
tradeoff is visible on real mail rather than described.

---

## Phase 4 — Correcting one span

The storage supports this already: `source='human'` outranks the machine,
survives a re-run, and takes the message out of the pending queue. The
**correction API does not reach it**:

```go
func SetHumanAnnotation(name, messageID string, matched bool, data map[string]any) error
```

It deletes every human row for that annotator and writes exactly one at
`seq 0`. That is label-shaped — it can say "this message does not match", not
"this one value of twelve is wrong".

So Phase 4 is mostly a store change: a function that writes *several* human
rows, one per span, preserving `seq`. Then the mark gets an affordance:

- **reject** — this is not an amount
- **relabel** — this is a tracking number, not an order number
- **add** — select text the model missed and label it

The last is the interesting one. A human-placed anchor is the shape training
data has, and it survives every re-run, so the corrections accumulate into
something worth more than the corrections themselves.

---

## Phase 5 — Extraction that reads the attachments

`spanText()` is subject plus body, and touches `attachment_text` zero times.
On a mailbox where the invoice PDF holds the line items, that is most of the
data — and it is why `content:invoice` matches none of these files while the
filenames say invoice.

This is last because it is the only phase that changes stored data's shape.
`field` is currently `"subject" | "body"`; an attachment needs a third form
that names *which* file, and the Files viewer needs the same marking that
Phase 2 builds for messages. Doing it after Phase 2 means reusing that
renderer rather than writing a second one.

---

---

# Built, 20 September 2026

All five phases, verified against the live mailbox rather than a fixture.

| Phase | Result |
|---|---|
| 1 — endpoint | `GET /api/messages/{id}/annotations`, text returned as runs |
| 2 — marks | a fourth view in the message toolbar, appearing only when there is something to show |
| 3 — the floor | a slider reading 20/18/7 at 0/0.6/0.9 on a real receipt, solid and dimmed agreeing at each step |
| 4 — correction | click a mark to relabel or reject it |
| 5 — attachments | extraction reads readable files, and the panel shows them by filename |

The rendered DOM text is an exact 1820-character match for the stored body on
the message it was checked against, so the marking adds and drops nothing.
Every stored record still resolves to its own value at its own offsets — 36 of
36 on the attachment-aware run, across all three field kinds.

## What Phase 5 found

**Twenty of thirty-six values came from inside attachments** — more than the
body (9) and subject (7) combined. `$5.00`, `6.99`, `Aug 17, 2024`, `07/30/24`:
none of them in any message body. On a mailbox of receipts the PDF really is
where the data is, and reading only the body was missing most of it.

## Two things this uncovered

**A human ruling had never survived a re-run.** `SaveAnnotations` deletes
`WHERE source != 'human'`, which reads as careful, and then inserts with
`INSERT OR REPLACE` against `UNIQUE(message_id, annotator_id,
annotator_version, seq)` — so a machine row at seq 0 replaced the human row at
seq 0 immediately after being spared. `PendingMessages` hides it nearly always
by never offering a ruled-on message to a run. `ApplyRule` never had the bug;
the other path now matches it.

**The build order matters.** `make frontend` copies `frontend/dist` into
`internal/embed/static`, which is what the binary embeds. Building the Go
binary first embeds the previous bundle, and the symptom is a feature that
"does not work" while the source is correct.

## Not doing

**A ledger of extracted values** — amounts over time, totals by merchant. It
is the tempting one and `query | extract receipts | sum amount by month`
already does it. The asset here is the anchoring, not the aggregating; spend
the effort where nothing else competes.
