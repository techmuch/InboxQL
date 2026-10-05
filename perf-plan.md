# What is actually slow, and the inbox that is not an inbox

*A phased plan. 4 October 2026. Measured against `../uea-test` — 7.8 GB,
43,553 messages — not against the 188-message development mailbox.*

## The measurements that set the priorities

```
714,983 ms   in:contacts awaiting:me        11.9 minutes, 2,868 rows
 59,060 ms   in:attachments png
 51,417 ms   in:attachments pdf
 25 of 28 warnings in the log are genuinely slow
```

A process pegged at 100% CPU for 17 minutes with no browser window open and no
maintenance job running. Sampled: 4152 of 4152 samples inside `sqlite3_step`.

## Two causes, and neither is scale

**Expression keys defeat every index.** The 11.9-minute query ends in
`COALESCE(m_sub.thread_key, m_sub.id) = COALESCE(m_latest.thread_key,
m_latest.id)`. An expression over two columns cannot be served by an index, so
SQLite walks `idx_messages_date` and evaluates it per row, per message, per
contact.

The `COALESCE` guards a null that **does not exist**: 0 of 43,553 messages have
a null `thread_key`, and 0 of 9,533 attachments have an empty `content_hash`.
Removing it on the contacts query:

```
SEARCH m_sub USING INDEX idx_messages_date         714,983 ms
SEARCH m_sub USING INDEX idx_messages_thread_key       470 ms   same answer
```

**No query can be cancelled.** Zero `QueryContext`/`ExecContext` calls in
`internal/`; 305 plain ones; `RunQuery(src string, limit, offset int)` takes no
context. An abandoned request runs to completion holding one of 8 pool
connections. Eight of those and every later query blocks before it starts —
which is what "major slowdowns" feels like from the outside.

## And the inbox is not an inbox

```
messages whose mailbox contains INBOX :      0
what the rail reports as Inbox        : 39,068
```

Every message lives in `…/Archive.mbox` or `…/[Gmail].mbox/All Mail.mbox`.
`folderClause(FolderInbox)` is defined as the *remainder* — not sent, not
deleted, not junk — so archived mail is counted as inbox.

Nothing is unreachable (39,068 + 4,485 sent = 43,553). The defect is that the
rail names it wrongly and offers no way to see Archive as Archive, which is what
"I don't see all of the email" is actually pointing at.

---

## Phase 1 — The keys

Drop `COALESCE(thread_key, id)` and `COALESCE(NULLIF(content_hash,''), id)` from
every predicate, join and grouping key. A migration backfills the nulls the
guard was defending so dropping it is sound rather than optimistic, and the
columns become NOT NULL so nothing reintroduces one.

Biggest win available by three orders of magnitude. First.

## Phase 2 — Cancellable queries

Thread `context.Context` from the HTTP handler through `RunQuery` into
`QueryContext`. `mattn/go-sqlite3` turns a cancelled context into
`sqlite3_interrupt`, so closing a tab stops the work instead of donating a core
and a connection to it.

Narrow scope: the read path that serves `/api/query`. Not all 305 call sites.

## Phase 3 — Paging, which is really truncation

`in:contacts` returns 50 of 57 with nothing on screen saying so. Six list kinds,
one of which pages.

- `OFFSET` in `buildContacts` and `buildAttachments` — the other four have it.
- Paging moved out of `Desk/index.tsx` into the result lists, so it is per-kind
  rather than message-only.

Aggregates keep refusing to page, for the reason `Options.Offset` already gives.

## Phase 4 — Reads in parallel

`FolderCounts` is a `for` loop of `QueryRow` on an 8-connection pool and an
8-core machine. Measured on a million rows: 2.84 s serial, 586 ms concurrent.

Not a rewrite into one `GROUP BY` — measured, that is *slower* (0.84 s → 1.43 s),
because the per-folder index seek beats a scan that also walks every row the
folder does not want.

## Phase 5 — Archive, and an honest Inbox

Rail entries for the mailboxes that are really there, derived from the data
rather than from a fixed list of six. Inbox stops claiming archived mail.

## Phase 6 — The log's denominator

A normal query logs at `debug`, a slow or failed one at `warn`. At the default
`info` level the query category can therefore only ever produce warnings, so the
tab shows a list of exceptions with nothing to compare them against. It should
say "8 queries · 3 failed · 0 slow" rather than implying the exceptions are the
population.

## Not doing

**Parallelism as the headline.** It is 4.85×; Phase 1 is 1,500×. Ordering
matters more than either.

**The synthetic million-row database.** It taught me nothing, because clean
generated data has no nulls and simple keys and so never reaches the slow paths.
Deleted; `uea-test` is the benchmark.

---

## What was built

All six phases, measured against `../uea-test` (7.3 GB, 43,553 messages) and
verified in a browser.

### Phase 1 — the keys, and a correction to the plan

| query | before | after | |
|---|---|---|---|
| `in:attachments png` | 59,060 ms | **193 ms** | 306× |
| `in:attachments pdf` | 51,417 ms | **217 ms** | 237× |
| `in:contacts awaiting:me` | 714,983 ms | **4,068 ms** | 176× |
| `folder:inbox \| timeline` | 1,140 ms | **522 ms** | 2× |

Warm, best of three, like-for-like against the logged originals. A cold first
reading on a 7.3 GB file is 4 s regardless, which briefly looked like a
regression and was not — the old numbers came from a long-running server with a
warm page cache, and comparing a cold CLI run against them is not a comparison.

Schema **v38** backfills `messages.thread_key` and `attachments.content_hash`,
and `SaveAttachment` falls back to the row's own id so new writes cannot
reintroduce the null. 25 expression keys became bare columns.

**The index I added was wrong and I removed it.** A composite
`(thread_key, date DESC)` looked obviously right — every one of these subqueries
wants the newest message in a thread. Measured: 522 ms and 4,068 ms with it,
522 ms and 4,068 ms without. v38 drops it instead.

**Two places the fallback was real.** A standalone ticket has no conversation
and is named by its own id, so `buildTimeline` and a new `threadMatchOrOwn`
keep the `COALESCE` for tickets and drafts. Messages cannot have a null
thread_key, which is what makes removing it there sound rather than optimistic.
A test caught this; the first attempt lost every hand-raised ticket.

### Phase 2 — cancellable queries

`RunQueryContext` and `planRows`, with `QueryContext` so a cancellation becomes
`sqlite3_interrupt`. `RunQuery` stays as the uncancellable entry point for the
CLI. `handleQuery` passes `r.Context()`, and a cancelled request is not reported
as a failure.

This is the one that explains the *symptom*. Any single query being slow is an
annoyance; an abandoned one holding a pool connection for twelve minutes is how
eight of them stop the application.

### Phase 3 — the truncation

The plan said `buildContacts` and `buildAttachments` needed `OFFSET`. **They
already had it** — every one of the six kinds did, and the whole defect was in
the client, which asked for a next page only for messages.

On this mailbox `in:contacts` was showing 50 of **10,591**. Now 50 → 100 → 150
on scroll, verified in the browser.

`onScroll` is handed *down* to each list rather than wrapped around them: every
list owns its own `overflow-auto`, and the scroll event does not bubble, so a
handler on the parent never fires. Aggregates still refuse to page, for the
reason `Options.Offset` already gives.

### Phase 4 — the counts, concurrently

`FolderCounts` fans out over the folders. Each goroutine owns one slice index,
so there is no shared write and the result keeps the rail's order. Measured
earlier: 2.84 s serial, 586 ms concurrent on eight independent counts.

Not rewritten as one `GROUP BY` — measured, that is slower (0.84 s → 1.43 s),
because each folder's clause is served by an index and grouping scans rows no
folder wants.

### Phase 5 — Archive, and an inbox that is one

```
          before    after
Inbox     39,068     1,526 unread of 3,896
Archive        —    35,172
Sent       4,485     4,485
```

Zero messages were in anything named INBOX, and the rail said "Inbox 39,068".
`folder:archive` matches the mailbox name's trailing segment — the RFC's
special-use name, Apple Mail's `Archive.mbox` — and the inbox remainder now
excludes it.

**Gmail's All Mail is deliberately not archive.** It contains the inbox, so
claiming it would empty the inbox of mail that really is in it. Gmail archiving
is the absence of the Inbox label, which this schema does not record, so those
accounts keep the old behaviour rather than getting a confident wrong answer.

**Sent beats Archive**, because the folders have to partition or the sidebar
totals exceed the mailbox — which they did, by 4,295 messages that were both.
Sent is about who wrote it and never stops being true; archive is where it is
filed. Verified: 3,896 + 4,485 + 35,172 = 43,553 exactly.

### Phase 6 — what the log cannot tell you

A note, shown only above a debug floor:

> Recording at **info**, so ordinary work is not written down — a query, request
> or sync appears here only when it was slow or it failed. These are the
> exceptions, not a sample: there is nothing here to tell you how many of each
> there were.

**Not a denominator**, because there isn't one: the rows that would make the
proportion were never recorded, and a figure assembled from the rows that were
would say 100% of queries are slow, forever. Naming the question the view cannot
answer is the honest version; the button makes it answerable.

### A check that was not checking anything

`npx tsc --noEmit` exits 0 against this repo **without type-checking a single
file** — the root `tsconfig.json` is `{"files": [], "references": [...]}`, so
the default project is empty. Every "typecheck clean" reported before this point
was vacuous. The real command is `npx tsc --noEmit -p tsconfig.app.json`, which
immediately found six errors in the first thing it was pointed at.

### Verified

24 Go packages, 171 frontend tests, `-race` clean on store/query/api, no console
errors. The rail, the paging and the log note all confirmed in a browser against
the real mailbox.
