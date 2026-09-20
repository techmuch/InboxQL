# Annotators people can actually start with

*A phased plan. 20 September 2026.*

## The problem, in one line

An annotator is the most capable thing in InboxQL and the hardest to begin
using: you must invent a schema, choose an engine, write a scope and run a
batch job, all before seeing a single result.

Three changes, in the order that each makes the next one worth having.

---

## Phase 1 — Run an annotator from the message you are reading

First because it is smallest, and because it turns an annotator from a batch
job into a tool. "Answer this question about *this* message, now" is a
different act from annotating a mailbox, and it is the one that makes the
feature legible.

Almost all of it exists. `POST /api/annotators/run` takes a scope, and
`scope: "id:<message-id>"` is one message — the path used throughout this
work's testing. The Values tab already shows what came back.

What is missing is the offer: the viewer cannot say which annotators have
*not* seen this message. That is `unlabeled:<name>` per annotator, or one
query, and it is the whole feature.

Two things the UI has to be honest about:

- **It takes 15–20 seconds.** A span run on one message is not instant and the
  control must not pretend otherwise.
- **A run writes.** Reading a message must not annotate it as a side effect,
  which is why this is a button and not the `on-open` trigger it superficially
  resembles.

---

## Phase 2 — Starters, from this mailbox rather than from imagination

The clusters are real. Counted against the 188 messages here:

| Cluster | Messages | Worth extracting |
|---|---|---|
| Receipts and purchases | 27 | amount, merchant, order number |
| Files | 41 | the PDFs, where 20 of 36 values already came from |
| Bills and statements | 7 | amount, due date, account reference |
| Support tickets | 9 | ticket number, status |
| Travel and bookings | 9 | confirmation number, location, dates |

### The mix is the lesson

A starter pack where everything is a span extractor teaches the wrong instinct
and costs hours of CPU. Each expensive extractor ships **paired with a cheap
rule label that scopes it**:

```
money      rule    subject:receipt OR subject:invoice OR subject:payment
receipts   gliner  --scope label:money
```

The rule is free, instant and deterministic; it narrows a 20-second-per-message
job to the messages that could possibly match. This is the pattern the gliner
plan argued for over a two-stage classifier pipeline — the label is stored,
queryable and human-overridable, which an in-flight variable is not — and the
starters should demonstrate it rather than merely providing extractors.

### They arrive inert

Creating them must not run them. Six extractors over 188 messages is an hour
of CPU nobody asked for, and on a real mailbox it is a day. They are created
with coverage at 0, and the user presses go.

### One floor, stated once

We know `order number` pulls card-shaped strings, and that 0.6 separates them
from the real values here. Six starters must not each rediscover that: the
pack documents the floor as its default and says why.

---

## Phase 3 — Triggers: when to drain a queue that already exists

The largest, and the one with real risk, so it goes last.

### Scope is the "what". Triggers are only the "when"

`--scope` already filters. A trigger that also filtered would be a second
system for the same job, free to disagree with the first.

### A trigger is not a hook

At 15–20 seconds per message, a trigger cannot run inline: on an import of ten
thousand messages that is days, inside whatever called it. It has to enqueue.

And the queue is already there. `PendingMessages` computes, for any annotator,
exactly which messages it has not evaluated at its current version — the
three-valued status is what makes that possible. So:

> A trigger is a policy for when to drain a queue that already exists.

That makes this much smaller than it looks. `internal/maintenance` already
runs jobs with start, cancel, progress and SSE, one at a time per kind. The
work is a `trigger` column, a decision about when to start one of those jobs,
and the caps below.

### The triggers worth having

| Trigger | Means |
|---|---|
| `manual` | today's behaviour, and the default |
| `after-sync` | drain what is pending once new mail has landed |
| `daily` | drain on a timer |

**Not `on-open`.** Reading a message should not write annotations as a side
effect, and at twenty seconds the result would not be there when you looked.
Phase 1 is the honest version of that wish.

### The runaway job is the risk

Triggers plus an expensive engine is how a laptop spends a night at 100%. So a
triggered run is capped, says what it is doing, and can be stopped — the same
machinery the maintenance panel already exposes.

---

---

# Built, 20 September 2026

All three phases, verified against the live mailbox.

## Phase 1 — run from the viewer

`GET /api/messages/{id}/annotators` answers "which of these has not seen this
message", which is the three-valued status and nothing more. The Values tab
offers those, says `~20s` before it is pressed, and names the annotator while
it works.

Opened a scanned Metro receipt no annotator had seen, pressed `receipts`,
waited eight seconds, got 11 marks — every one of them from inside the
attached PDF, the body being only "Created and shared using Adobe Scan".

**A flaw caught in the wiring:** the Values tab appeared only when there were
already marks, which hid the one control that could produce the first ones.

## Phase 2 — starters

Eleven, five of them free rule labels gating six span extractors. Measured
here: the labels evaluated all 188 messages instantly and matched 34, 7, 9, 1
and 9; `bills` then had **7 messages to read rather than 188**, and found 30
records in them.

Scope moved onto the annotator (schema v31), because a starter that has to be
told its own scope every time is not a starter — and because a triggered run
has nobody to pass a flag.

**Two things the listing had to be told to say.** A rule matching nothing is
not worth installing, so each reports its reach — which caught `shipping`
matching zero until its rule learned "pickup". And an extractor gated by a
label that has not run matches nothing *yet*, which read as "this finds
nothing" when it meant "nobody has asked the gate".

## Phase 3 — triggers

`Due` is the whole idea: the annotators that asked for this trigger **and have
something pending**. One with nothing waiting is skipped, because a job that
evaluates nothing is noise in every progress bar it touches.

Proved end to end through the API: a sync returned 202, the after-sync job
started on its own, and `bookings` read its 9 scoped messages and wrote 47
records — all 47 resolving to their stored value at their own offsets, across
subject, body and attachment.

**The split on where it fires.** The UI starts a job, because it can show
progress and be stopped. `iql account sync` only *says* what is waiting: it is
documented as synchronous and safe in a cron job, and silently gaining twenty
minutes of span extraction would make that false.

## Not doing

**A trigger language.** "Run when a message matches X and the sender is Y" is
`--scope` with extra steps. If a trigger needs to be selective, it is selective
by scope.
