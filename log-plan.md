# A log you can query

*A phased plan. 3 October 2026.*

## The gap is not the tab's name

`error_log` has **one producer**: the importer. Meanwhile **82 `log.Print*`
calls across nine packages** go to stderr and never reach the UI — syncs,
annotator runs, LLM calls, migrations, maintenance jobs.

So the tab is not mislabelled. It shows one subsystem's failures while
everything else is invisible, and renaming it to "Log" without fixing that
makes it *more* misleading: a thing called Log that still only knows about
imports.

The table is already nearly right — `category, job_id, account_id, context,
reference, message, created_at`, indexed on time, job and category. It is
missing a level and a producer.

## What makes it worth doing

Everything else here is an `in:` kind. A log that is one too answers the
question that was actually asked — *what has this thing been doing* — in the
language the rest of the application already speaks:

```
in:logs level:error after:7d | count by category
in:logs job:<import-id>                 everything that one import did
in:logs after:1h | count by level
in:logs account:work level>warn
```

That composes with the pills, the rail, saved queries and every aggregate. It
is the difference between a scrolling list and an instrument.

## The 81 other sites come free

Go 1.27, and `slog` is not used anywhere yet. A `slog.Handler` that writes to
the table, plus `slog.SetDefault` and routing the standard `log` package into
it, means every existing `log.Printf` lands in the log without a call site
being touched — unstructured and at a default level, but there. The
interesting ones convert over time rather than in one pass.

`slog.LevelVar` also changes level atomically at runtime, which is the only
version worth having: logging is turned up *after* something goes wrong.

---

## Phase 1 — A level, and a writer

Schema: `level` on the existing table, defaulting to the level an error was.

`internal/logging`: a handler that batches into the store, a `LevelVar`, and a
tee so **stderr keeps everything**. If the database is the only sink, the logs
that matter most are the ones that cannot be written — the ones from before it
opened and the ones about it failing.

**Writes are batched and asynchronous.** Logging into the same SQLite the
application is working in means every line is a write transaction, during a
sync, contending with the thing being logged. This is the engineering risk in
the whole plan, well ahead of the schema.

## Phase 2 — `in:logs`

A sixth entity beside mail, drafts, tickets, contacts and files.

| Term | Matches |
|---|---|
| `level:` | `debug info warn error`, and `level>warn` |
| `category:` | the subsystem |
| `job:` | one import or maintenance run |
| `account:` | which account it was about |
| `message: context: reference:` | the text |
| `after: before: on:` | as everywhere |

## Phase 3 — The tab becomes Log

Level and category filters, and the level control itself, because somewhere
has to set it and this is where somebody is standing when they want it.

It keeps its job-scoped view: `openErrorLog(jobId)` is how an import reports
what went wrong, and that is still the right link.

## Phase 4 — Retention

Debug-logging a sync is thousands of rows a minute. A row cap, pruned by
`maintenance`, and a note in the UI about what is kept.

The level is partly a volume control, so the two belong in the same phase.

## Not doing

**Mail-protocol tracing.** IMAP and SMTP wire traces are a bigger, noisier,
more secret-laden thing. If it is wanted it is its own category later.

**A separate log database.** It would remove the write contention and keep the
record of what happened when mail is erased. But `in:logs` composing with the
query engine is most of the value here, and ATTACH-ing a second file to get it
back is more complexity than batched writes.

**Secrets.** `sanitiseForLog` strips control characters; it does not redact.
Debug level does not get a licence to record credentials or message bodies —
whatever is added later must earn it explicitly.

---

## What was built

| Phase | Where |
|---|---|
| 1 — a level and a writer | schema v34; `internal/logging` |
| 2 — `in:logs` | a sixth entity across `fields.go`, `compile.go`, `plan.go`, `query.go` |
| 3 — the tab | `ErrorLog.tsx` becomes Log; `iql log`; `GET/PUT /api/log/level` |
| 4 — retention | `store.PruneLog`, `iql maintenance prune-log`, capped at 50,000 |

### The eighty-one other call sites

Routing the standard library's logger into the handler captured them without
one being touched. Verified: a bare `log.Println` lands as `info` under
category `app`, and `slog` attributes this package knows about become columns —
category, job, account, context, reference — which is what makes
`in:logs job:x` and `| count by category` possible at all.

### `in:logs`, on real rows

```
in:logs                     5 lines
in:logs level>warn          1
in:logs | count by level    info 2, debug 1, error 1, warn 1
in:logs | count by category app 2, annotate 1, import 1, sync 1
```

Levels are ranked in SQL rather than expanded into a list of the levels above,
so adding one later cannot leave the comparison behind. A row written before
v34 reads as an error, because that is the only thing the table ever held.

### Two controls that look alike and are not

The tab has **showing** and **recording**, and the distinction is the whole
feature: one hides rows that exist, the other decides whether they are written.
Both verified in the browser — `showing: warn` cut five rows to two, and
`recording: warn` reached the server and survived into a fresh CLI process.

### A bug this found in its own shutdown path

`Stop()` closed its channel unconditionally, so a second call panicked — and
`Execute` calls it on every exit. A shutdown path that panics loses exactly the
lines it exists to save. It is a `sync.Once` now.

### Honest limits

**Nothing from before the database opens.** The log is a table, so the lines
written while opening and migrating reach stderr only. Unavoidable, and
documented rather than papered over.

**stderr still gets everything.** The database copy is the one you can query;
stderr is the one that works when the database does not.

**Dropped lines are reported, not hidden.** A full buffer drops rather than
blocks — a log line is never worth stalling a sync for — and the gap is written
into the log itself and shown in the tab, because a burst that overran the
writer would otherwise read as a quiet period.

### Tests

`internal/logging` — the standard library's output landing in the table, known
attributes becoming columns, unknown ones surviving in the message, the floor
stopping writes rather than filtering reads, `level>=` on read, a full buffer
dropping without blocking, the gap being reported, and pruning keeping the
newest.

`internal/query` — `in:logs` selecting the kind, level comparing by severity,
old rows reading as errors, a mail field being refused, dates using the line's
own time, and the groupings.

Full Go suite green, `-race` on the four changed packages, 131 frontend tests,
`tsc` clean, no console errors.

### Not done

**Mail-protocol tracing.** Still its own thing, and still noisier and more
secret-laden than this.

**Redaction.** `sanitiseForLog` strips control characters; it does not redact.
Nothing added here logs a credential, and anything added later must earn debug
level explicitly.
