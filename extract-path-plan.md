# Naming an extracted value once

*A phased plan. 2 October 2026.*

## The complaint, and the bug under it

`| extract <annotator>` does nothing on its own:

```
| extract receipts   → 188    every message in the mailbox
extract:receipts     → 31     the ones it actually annotated
```

And its position is meaningless — `| sum amount | extract receipts` answers
identically to `| extract receipts | sum amount`. It is not a pipeline stage.
It is a parameter that supplies a name to a *later* aggregate, wearing a
stage's costume, and it reads as "first extract, then sum", which is not what
happens.

AGENTS.md calls it "read structured output". It reads nothing.

A third symptom, same root:

```
| extract receipts | count by merchant
  → cannot group by "merchant" (try: from, domain, to, cc, …)
```

The grouper is handed a bare word with no way to know which extractor it
belongs to, so it cannot resolve it at all.

## The shape instead

Fold the extractor into the field path — which the **filter** side already
does, and has done correctly all along.

| | |
|---|---|
| `extract:receipts.amount>100` | filter, unchanged |
| `\| sum receipts.amount by month` | was `\| extract receipts \| sum amount by month` |
| `\| series receipts.amount by week` | |
| `\| count by receipts.merchant` | not expressible today |
| `\| top receipts.merchant 10` | not expressible today |

One spelling for "this extractor's this field", everywhere. Today the same
thing is `receipts.amount` in a filter and `extract receipts` … `amount` in a
pipeline, split across two stages whose order does not matter.

`Stage.Annotator` already exists for exactly this. The parser simply never
fills it from the field.

## What happens to `| extract`

**Kept, working, undocumented.** It still supplies a default annotator for a
later aggregate, so every saved query that uses it keeps answering what it
answered yesterday.

**Not made to filter.** Changing what it returns would quietly alter the
results of saved queries — a silent change of answer is worse than a verb we
have stopped recommending. It is retired by not being taught.

---

## Phase 1 — The path in aggregates

`series`, `sum`, `avg`, `min`, `max` accept `<annotator>.<field>`.

Splitting belongs in the parser, because it is a question about syntax rather
than about what the number means. A field with no dot keeps its present
meaning: it falls back to whatever `| extract` named, which is what makes the
alias work.

Errors teach the new form. `| sum amount` with no extractor anywhere currently
says *name the extractor first, e.g. `| extract saas-metrics | …`*; it should
say `| sum saas-metrics.amount by month`.

## Phase 2 — Grouping by an extracted value

`| count by receipts.merchant` and `| top receipts.merchant 10`.

The group resolver already returns `{expr, join, having, preArgs}` and `label`
already joins annotations, so an extracted path is one more case rather than a
new mechanism.

**It counts messages, like every other grouping key.** An extracted field is
multi-valued in exactly the way `to:` is, and the `COUNT(DISTINCT m.id)` the
grouper already applies is the right answer: a message naming two merchants
contributes one to each, and one naming the same merchant twice still
contributes one.

Aggregates are the other case and stay as they are — `| sum receipts.amount`
adds up records, because collapsing a weekly digest's seven bars to one message
is exactly what would throw the series away.

## Phase 3 — Annotations in the rail

"All the mail with this annotation" needs the filter side, so it needs no new
syntax:

| Kind | Entry |
|---|---|
| label | `label:money` |
| extractor | `extract:receipts` |

Counts come from `/api/annotators`, whose `progress.matched` already agrees
with the query exactly — money 34, receipts 31 — so no new endpoint.

Two decisions:

- **A switched-off annotator still appears**, dimmed and badged `off`. Its
  results are still there and still queryable; that is the rule. Hiding the
  entry would contradict the switch we just built.
- **One that has matched nothing is hidden**, not shown as a zero. Eleven
  starters, most empty on a fresh mailbox, would bury the entries that work.

## Phase 4 — Completions and the contract

`--complete` offers `<annotator>.<field>` where a pipeline field is expected,
reading the schemas it already knows. AGENTS.md documents the path form and
stops documenting `| extract`.

## Not doing

**Removing `| extract`.** It costs one line to keep and breaks saved queries
to remove.

**Making a bare `| sum amount` search every extractor for an `amount`.** Two
extractors with the same field name would make the answer depend on which was
created first, and the error that asks for a path is better than a number
nobody can trace.

---

## What was built

| Phase | Where |
|---|---|
| 1 — the path in aggregates | `splitPath` in `internal/query/parse.go`; `buildAggregate` already preferred `Stage.Annotator` |
| 2 — grouping | `extractedGroup` in `internal/query/plan.go` |
| 3 — the rail | an Annotations section in `frontend/src/views/Desk/index.tsx` |
| 4 — completions & docs | `ValuesExtractPaths`, `extractPathCandidates`, AGENTS.md |

### On the real mailbox

```
| sum receipts.amount              → 1040        (same as the old two-stage form)
| avg receipts.amount              → 13.51
| count by receipts.merchant       → 63 groups: Sam's Club 9, Grocery 7, …
| count by "receipts.order number" → one per order
| sum amount                       → sum amount: say which extractor, e.g.
                                     `| sum <annotator>.amount by month`
```

`| extract receipts | sum amount` still answers 1040, so saved queries are
untouched.

### Something the grouping immediately showed

`| count by receipts.merchant` returns **Sam's Club**, **Sam's Club]** and
**Sam's Club** with a curly apostrophe as three separate groups. Those are span
boundaries the extractor got slightly wrong, and they were invisible while the
only way to see extracted values was one message at a time. Not fixed here —
but worth knowing that the first thing this view does is expose extraction
quality.

### Completion

The dropdown offers whole paths where a field is expected, in both positions:

```
| sum rec          → receipts.amount, "receipts.due date", …
| count by rec     → the built-in keys, and the same paths beside them
```

Fields whose names contain a space come back quoted, because a completion that
does not parse is worse than none.

One fix was needed to make any of it visible: the in-progress word was counted
twice — once as the token under the cursor and once as a committed word — so a
half-typed field looked like a field already chosen and offered buckets. That
affected `sort` and `top` as well, and all of them improve.

### A wrong turn worth recording

I spent a while convinced the editor's completion dropdown never refreshed
after mount, having watched it ignore correct server responses in the browser.
I changed `Editor.tsx` to guard on the box's text rather than a sequence
number, and the list started working.

It was a **stale embedded bundle**. Testing the original code against a clean
`make frontend` shows completions working correctly, so the change was
unnecessary and is reverted. The tests I wrote for it passed against the old
code too, which is what gave it away — a test that cannot fail is evidence that
there was nothing to fix.

`make frontend` before `go build`, and when a symptom makes no sense, suspect
the bundle before the code.

### Tests

- `internal/query/extractpath_test.go` — the path parsed into its halves, a
  bare field left unnamed so `| extract` still supplies it, first-dot-only
  splitting, the group resolver's join and its errors.
- `internal/store/extractpath_test.go` — whole paths offered, labels excluded,
  spaced fields quoted, matching through a half-typed opening quote, and the
  built-in group keys not displaced.
- `frontend/src/views/Desk/annotations.test.tsx` — the rail entry per kind, the
  count, a switched-off annotator still listed, an empty one hidden, and
  survival of a listing it cannot draw.

Full suites green: `go test ./... -tags sqlite_fts5` and `-race` on the three
changed packages, 114 frontend tests, `tsc -p tsconfig.app.json`.
