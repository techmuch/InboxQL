# A starter pack you can switch off

*A phased plan. 1 October 2026.*

## Why a checkbox cannot mean "exists"

`annotations` cascades on `annotator_id`, so deleting an annotator deletes
everything it ever said. On this mailbox, unticking `receipts` would destroy:

| | |
|---|---|
| 269 records over 32 messages | about 11 minutes of CPU to rebuild |
| **19 human corrections** | **not rebuildable at any price** |

Across the pack that is 57 message-runs, roughly 19 minutes. The CPU is
annoying. The corrections are someone's judgement, and no amount of re-running
recovers them.

A tick-box that deletes is a trapdoor, and it fails the requirement directly:
unticking creates the work of redoing everything if you ever tick it again.

## What the checkbox toggles instead

Participation, not existence. One flag, and both directions are a single
UPDATE:

- **Tick an absent starter** — create it, inert, zero records.
- **Untick** — it stops running. Every row it wrote stays.
- **Re-tick** — instantly back, results and corrections intact.

Deleting for real stays in the Annotators panel, where the record count is on
screen and it is a deliberate act. Settings answers *which of these do I want
on*; the Annotators panel answers *get rid of this*.

## The decision this rests on

**Off means it will not run. Everything it has already said still counts.**

So `label:money` still matches while `money` is off. Those labels are facts
about messages that really were observed, and the system already works this
way — a version bump keeps the old answers for provenance rather than deleting
them.

The alternative, where off also hides results, makes unticking destructive
again but invisibly: query answers change silently with no row deleted. Worse
than the trapdoor it was meant to replace.

---

## Phase 1 — The flag, and the three places that honour it

Schema v32 adds `enabled`, defaulting to on so every existing annotator is
unchanged.

| Honoured in | Because |
|---|---|
| `Due()` | a trigger must not fire a switched-off annotator |
| `offersFor()` | the viewer must not offer to run one |
| `annotate.Run()` | running by name refuses, and says how to switch it on |

**Deliberately not the query compiler.** See the decision above.

`enabled` is not `trigger`. `trigger: manual` means *only when told*;
`enabled: false` means *not even then*. Folding the second into the first as a
fourth trigger value would put *whether* and *when* in one field and make
every error message about it ambiguous.

## Phase 2 — The CLI

`iql annotate enable <name>` and `disable <name>`, and the state shown by
`list` and `show`. The CLI is the contract; a flag only the web UI can reach
would be a state an agent cannot see or set.

## Phase 3 — Starters as live toggles

Every row becomes a switch rather than a one-way install, with its record
count beside it so what a row is holding is visible before it is touched.

Unticking says what it kept. The point of the design is that nothing is lost,
and a UI that stays silent about that is asking to be distrusted.

## Phase 4 — The Annotators panel

An off annotator reads as off, with the same switch, so the two surfaces do
not disagree about a state they both show.

## Not doing

**A delete button in Settings.** Deletion is destructive and belongs where its
cost is visible. Two paths to the same irreversible act, one of them in a
settings page next to a row of checkboxes, is how it gets done by accident.

---

## What was built

All four phases, verified against the real mailbox.

| Phase | Where |
|---|---|
| 1 — the flag | schema v32; `Annotator.Enabled`; honoured in `Due`, `offersFor`, `annotate.Run` |
| 2 — the CLI | `annotate enable`/`disable`, state in `list` and `show` |
| 3 — Starters | `PUT /api/starters/{name}`, the tab as live toggles |
| 4 — the panel | `PUT /api/annotators?name=`, switch and `off` badge on each card |

### The decision held

Switching `receipts` off on the live mailbox and querying it:

```
$ iql annotate disable receipts
receipts is now off.
  kept: 269 records over 32 messages, 19 values set by hand.

$ iql query "label:receipts" --count          → 31
$ iql query "extract:receipts.amount" --count → 17
$ iql sql "SELECT COUNT(*) FROM annotations …" → 269
$ iql annotate run receipts
  annotator "receipts" is switched off; `iql annotate enable receipts` to run it
```

Nothing hidden, nothing lost, nothing new started.

### One thing the plan had wrong

The plan's "269 records, 19 human corrections" came from counting annotation
rows. The first build read them from `store.Progress`, which counts **distinct
messages** — so the UI would have said *31 results and 1 correction* for the
same annotator.

That is the one number the whole design exists to protect, understated
nineteen-fold. `store.AnnotationVolumeOf` now counts rows, and every "what was
kept" line reads from it; `Progress` is left alone, because coverage is still
the right question for a progress bar.

`annotate show` gained a `holds` line for the same reason — the detail view was
otherwise the one place that understated it:

```
  corrections    1 (kept across re-runs)     ← messages
  holds          269 records, 19 values set by hand
```

### A consequence worth surfacing

Switching off a **gate** freezes what it gates, for new mail only. `receipts`
is scoped to `label:money`; with `money` off, nothing labels new mail, so
`receipts` still covers what `money` already labelled and will never reach
anything else. Two rows apart in the same list, and invisible without being
told — so the listing reports `gateOff` and the row says it.

### Tests

- `internal/annotate/enabled_test.go` — the landmine (`SaveAnnotator` must not
  write the flag, or a bool's zero value switches annotators off by omission),
  `Due` skipping, `Run` refusing, results surviving, the off/on round trip
  creating no work, and rows-not-messages.
- `internal/api/startertoggle_test.go` — offers skipping, a PUT with no
  `enabled` field refused rather than read as "off", the three starter states,
  and the gate warning.
- `frontend/src/views/ai/Starters.test.tsx` — unticking sends a PUT and
  **never** a DELETE, says what it kept, and survives a response it cannot draw.

Full suites green: `go test ./... -tags sqlite_fts5` (and `-race` on the four
changed packages), 109 frontend tests, `tsc --noEmit`.
