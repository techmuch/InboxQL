# Timings in the log

*A phased plan. 4 October 2026.*

## The log cannot answer the question it exists for

`error_log` has no numeric column, so a duration can only go into the message
text. That makes this impossible:

```
in:logs duration>1000 after:1h | count by category
```

For a log whose stated purpose is *understand what my system is doing or has
done*, **elapsed time is the single most useful number**, and today it would be
prose. That is the enabling change; everything else here depends on it.

## Every query, but not at the same level

Logging every query at `info` drowns the thing worth knowing. A page load fires
several, the Windows panel polls every five seconds, and an annotator listing
runs one per annotator.

So: **every query at `debug`, anything slow at `warn`**, over a threshold. The
default level then surfaces exactly *"this took 2.2 seconds"* and nothing else
— a line that does not exist at any level today.

**The compiled SQL goes on the slow ones only.** "Took 2.2 s" says something is
wrong; the statement says what, because it can be pasted into `iql sql
--explain`. On every row it would be bloat; above the threshold it is the
reason the row is there.

## Debug is all-or-nothing, and that is the real problem

Turn it up to chase one thing and ten thousand HTTP lines bury it. The global
level should be the floor; the high-volume categories should be an allow-list
on top, with requests off by default.

---

## Phase 1 — A number the log can be asked about

`duration_ms` on the log table, a `duration:` term beside `level:`, and the
settings that drive the rest: threshold, categories, retention, and whether
query text is recorded.

## Phase 2 — Queries

Timed in `RunQuery`, which is the one place every query goes through. Debug
always, warn over the threshold, SQL above it.

**Query text is user content.** `from:solicitor@…` is as sensitive as the mail
it finds, and the log outlives the query. Recorded by default because this is a
local database, and behind a visible switch because that should be a decision
rather than a discovery when somebody attaches a log to a bug report.

## Phase 3 — The rest of what is invisible

HTTP requests (method, path, status, elapsed), sync per account, annotator
runs, and model loads — `laya.Open` takes four seconds and says nothing, which
is the obvious suspect when the first annotation is slow.

## Phase 4 — The settings

A **Logging** section of its own. Not General, which says in its own text that
it is per-browser while these are server-side; not Maintenance, which is a page
of verbs and would become a junk drawer.

The level stays in the Log tab, where somebody is standing when they want it.
One line in Settings says where it is, because people will look here first.

## Not doing

**A second level control.** Two controls for one state is the thing avoided
three times already in this codebase.

**Logging message bodies or request payloads.** Not at any level.

---

## What was built

| Phase | Where |
|---|---|
| 1 — a number to ask about | schema v37 `duration_ms`; `duration:` term; `internal/store/logsettings.go` |
| 2 — queries | `internal/store/querylog.go`, timed in `RunQuery` **and** `CountQuery` |
| 3 — the rest | `internal/api/requestlog.go`; annotator runs; model loads |
| 4 — the settings | `LogSettings.tsx`, its own Settings section |

### It answers the question now

```
$ iql log --category query
WHEN                       LEVEL  CATEGORY  TOOK  REFERENCE
2026-10-04T13:51:18-04:00  debug  query     1ms   folder:inbox | limit 3
2026-10-04T13:45:24-04:00  debug  query     2ms   folder:inbox | limit 5

$ iql query "in:logs duration>0 | count by category"
query 2
```

HTTP requests, once switched on:

```
debug  http  8ms  GET /api/annotators
debug  http  0ms  GET /api/ui
```

### Two bugs this found, both invisible before

**Every CLI log line was being lost.** `logging.Stop()` runs in the command
runner, *after* each command's deferred `store.CloseDB()` — so the buffered
batch was flushed into a closed handle. Lines reached stderr and never the
table, which is exactly the failure a log is supposed to prevent. `CloseDB` now
runs a hook registered at `logging.Start`.

**The logger could not be restarted in-process.** `Stop` left its `sync.Once`
fired, so a second `Start` did nothing. Harmless for a process about to exit,
a trap for anything that opens a database twice — and it made a test that
checked the log silently disable logging for every test after it.

### Two tests that were asserting the wrong thing

A zero threshold means *off*, not "everything is slow" — my first test had that
backwards, and the code was right. And a test that waits for a threshold fails
on a fast machine, so the slow-query test judges against the duration that was
actually recorded rather than against a sleep.

### One scare that was my own test

Wrapping the server handler to time requests looked like it had broken SSE —
the command channel delivered nothing. It was my probe opening a second
`EventSource` on a window the application was already reading. A window of its
own received the command immediately. The `Flush` passthrough on the recorder
is what keeps that true, and it is there deliberately.

### Tests

`internal/store/querylog_ext_test.go`, outside the package because `logging`
imports `store` — which makes it a better test, since it drives `RunQuery` and
`CountQuery` and reads what came out rather than calling an unexported helper.
Covers: the duration being a column, both query paths being timed, slow
becoming a warning with its SQL, ordinary staying debug without it, zero
meaning off, query text being omitted while the timing survives, a switched-off
category recording nothing, a failed query being kept, and settings falling
back per field.

### Not done

**Per-category levels.** On or off is enough; a level per category is a matrix
nobody would configure.

**Logging message bodies or request payloads.** Not at any level, deliberately.

**Silencing the heartbeat.** `quiet()` exists in the request logger and is
unused — the obvious next lever if `http` turns out too noisy at ten seconds a
window.
