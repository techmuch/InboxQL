# Starters in Settings, and a progress bar that moves

*A phased plan. 20 September 2026.*

## Phase 1 — The progress bar, because it is a bug

The Annotators panel draws a bar from `progress.evaluated / progress.total`.
That is **coverage**, not a running job, and it only moves when the request
returns. The run is a synchronous `POST /api/annotators/run` capped at 200
messages.

For a rule annotator that is milliseconds, so it looks like it works. For a
span annotator it is 200 × ~20s — **over an hour in one HTTP request**. The
browser gives up, the bar never moves, and the work that did happen is
invisible until the page is reloaded.

The code says why it was built this way:

> Runs are bounded and synchronous — a batch at a time, with progress in
> between. Firing a goroutine and drawing a progress bar over a job system
> that does not exist would look finished and lose work.

**That job system now exists.** Triggers needed one, so `internal/maintenance`
already runs annotator work with progress, SSE, cancellation and
one-at-a-time-per-kind. The comment's premise is simply out of date.

### The fix

A run of a slow engine becomes a job; a rule run stays synchronous.

Not uniformity for its own sake: a rule pass over a whole mailbox finishes
before a job could report that it started, and routing it through SSE would
add a spinner to something instant. The split is by what the engine costs,
which is the same thing `Offer.Slow` already decides in the viewer.

The job needs to run **one named annotator**, where today it sweeps a trigger.
That is one more field on the options it already takes.

## Phase 2 — Starters in Settings → AI

`iql annotate starters` is CLI-only, which makes the pack invisible to anyone
who found the Annotators panel first — the people it is most for.

A third tab beside Models and Analysis, showing each starter with:

- what it is for, in one line;
- **how much of this mailbox it reaches**, because a rule matching nothing
  here is not worth installing and the CLI listing already says so;
- whether it is installed already;
- for an extractor, the label that gates it, and that the gate must run first.

Selection by checkbox, defaulting to everything not yet installed. Installing
creates and does not run — six extractors over a mailbox is an hour of CPU,
and the panel says what to do next rather than starting it.

### What this does not become

A second annotator editor. The pack creates ordinary annotators; editing them
is the Annotators panel, which already does that. This tab exists to get
someone from nothing to something worth editing.
