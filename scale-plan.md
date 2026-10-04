# A million messages

*A phased plan. 4 October 2026.*

## The measurement that decides everything

`messages.header` does not hold headers. It holds the **entire raw RFC822
message, including base64 attachments**: 106 KB on average here, 2.6 MB at
worst, and **86.7% of everything in the table**.

SQLite keeps that in the same b-tree a scan walks, so every query an index
cannot answer drags 106 KB per row to read a 40-byte subject.

Measured on a generated 100k-message database, then on the same rows with the
raw moved to a side table:

| | raw inline | raw moved out | |
|---|---|---|---|
| `subject LIKE '%…%'` | 6.15 s | **0.022 s** | **280×** |
| `GROUP BY from_addr` | 6.18 s | **0.033 s** | **187×** |
| `flags LIKE '%Seen%'` | 7.88 s | **0.019 s** | **414×** |
| the messages table | 1.9 GB | **17 MB** | |

At a million messages that is **60–80 seconds against a fifth of a second** for
any filter, group or count the indexes do not cover.

## What was ruled out

- **The application is not slow at the current size.** 118 ms to start, 214 ms
  for a query. Nothing added this session shows up.
- **Paging is fine.** The `date` index means page 1 and page 1000 both return
  under 20 ms at 100k. An earlier measurement suggesting otherwise was
  cold-cache noise.
- **`COUNT(*)` is fine** — 68 ms at 100k, because it uses an index.
- **Compression is not the answer.** This mailbox's raw mail gzips 14.7 MB →
  10.1 MB, 32%, because base64 attachments are already-compressed PDFs and
  images. An obvious-looking fix that is not one.

---

## Phase 1 — The raw leaves the table

A `message_raw` side table keyed by message id, and every read path pointed at
it. This is where the measured speedup is and it is almost all of the value.

A side table rather than the blob store **first**, deliberately: the 280×
comes from the raw leaving the b-tree, which a side table achieves, and it
keeps the change inside one transaction and inside `iql backup`.

## Phase 2 — Lists stop reading what they do not show

`messageColumns` selects `header`, `body` and `html_body` to render a row
showing sender, subject, a snippet and a date. A list projection and a detail
one.

Smaller than phase 1 and still worth it: after the split, `html_body` is the
next largest thing being read fifty rows at a time to be thrown away.

## Phase 3 — The annotator listing's N+1

`/api/annotators` calls `Progress()` per annotator, and each runs five
aggregate queries — sixty per page load with twelve annotators. One grouped
query.

## Phase 4 — Measure again, at scale

Generate a million, run the suite, and report what is *still* slow. The
honest end to a performance plan is a second measurement, not a claim.

## Not doing yet

**The blob store on disk.** It is the right eventual home — `iql backup` stays
seconds, 106 GB stops living in a file `VACUUM` rewrites — and it is a second,
separately-measured step. Three things need deciding with evidence rather than
by analogy to attachments: a default backup must not silently stop containing
the mail, `Put` does not fsync before renaming and an attachment can be
re-extracted while a raw message cannot, and two hex characters of fanout is
3,900 files a directory at a million.

**Compression.** Measured, 32%, not worth the complexity.

---

## What was built, and what it measures

| Phase | Where |
|---|---|
| 1 — the raw leaves the table | schema v36; `message_raw`, `LoadRaw`, `WithRaw` |
| 2 — lists stop reading it | `messageColumns` no longer selects it |
| 3 — the annotator N+1 | `Progress` is one query instead of five |
| 4 — measured at a million | below |

### On the real mailbox

The messages table went **22.7 MB → 3.3 MB**, 85% smaller, with all 188 raw
messages preserved in `message_raw`. Reading, exporting and attachment
extraction all still work; an exported `.eml` carries the genuine
`MIME-Version` headers rather than synthesised ones.

### At a million

Generated, then measured through the real CLI:

| | |
|---|---|
| messages table | **185 MB** (it would have been ~20 GB with the raw inline) |
| a page of 50 | **0.6 s** |
| `is:unread --count` | 0.13 s |
| `from:… --count` | 0.11 s |
| `subject:number --count` | 0.23 s |
| `folder:inbox --count` | 1.24 s |
| `\| count by from` (5,000 senders) | **2.2 s** |
| `annotate list`, 12 annotators | **0.8 s** |

Against 6–8 seconds for a single scan at *100k* before the split.

### Two negative results worth keeping

**A covering index on `message_participants(role, address)` made `count by
from` worse** — 2.2 s to 4.0 s. The planner preferred it and should not have.
Not shipped. One index, tested once, so this is a reason not to add it rather
than a conclusion about indexes.

**Compression is not worth it.** Real raw mail gzips 32%, because base64
attachments are already-compressed PDFs and images.

### Three things the migration taught

**A migration must not share a Go helper with live code.** v24 recovers
participant names by reading `messages.header`. Repointing that helper at the
new table broke every fresh database — v24 runs twelve migrations before v36
creates the table it was now asking for. The helper is frozen at its own schema
and says so.

**A migration must survive the column already being gone.** Fixtures built at a
later schema, and retries after a partial failure, both reach v36 with nothing
to move. It checks rather than failing.

**A best-effort recovery should skip, not refuse.** v24 finding no column is a
reason to do nothing, not to refuse to open the database.

### Still slow, and honestly so

`| count by from` at 2.2 s is the slowest thing left. It is an unindexed
group-by over a million participant rows, and the obvious index made it worse.
That is where I would look next, and I have not solved it.

`folder:inbox --count` at 1.24 s is next, and is a count over every row by
definition.

### Not done

**The blob store on disk.** Still the right eventual home, and still a
separately-measured step: a default `iql backup` must not silently stop
containing the mail, `Put` does not fsync before renaming where an attachment
can be re-extracted and a raw message cannot, and two hex characters of fanout
is 3,900 files a directory at a million.

**`html_body` in list queries.** 12.8 KB a message here, read fifty rows at a
time to be thrown away. Smaller than the raw was, and the same shape of fix.

**`VACUUM`.** The table keeps the freed pages until one runs; scans already
skip them. `iql maintenance vacuum` is the user's to run, because on a large
mailbox it needs exclusive access and a while.
