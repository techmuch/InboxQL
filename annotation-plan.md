# Opinionated annotation: rulings first, engines measured, rules out

*A phased plan. 6 October 2026.*

## Where this starts

- **Rulings have no provenance.** A human annotation is a ruling, but nothing
  records whether it was made because somebody noticed a mistake or because a
  random sample put it in front of them. Calibration fits on all of them, so a
  set made mostly of corrections drags every score toward 50%.
- **Labels are nearly invisible.** They appear only under "Also matched" in the
  Values view of a message, as text. There is nothing to right-click.
- **Laya is asked only yes/no.** The wrapper supports choice and score questions;
  the annotator engine never uses them, and annotators only ever see one message.
- **Laya, measured on four obvious messages, got the open-loop question wrong**
  ("Can you send the signed copy by Friday?" → no reply needed) and called a
  promotion a receipt. Its escalation output reads ±1,500 and rounds to 0, so it
  cannot be used to route. The engine for each question has to be chosen by
  measurement, not by message count.
- **Five rule labels**, no human corrections, four extractors gated on them.
- **Contact tags** — zero rows on either mailbox.
- **Contact kinds** — 10,584 of 10,591 unknown on uea-test; the free header
  classifier was never run there and would settle 2,143.

## Phase 1 — rulings carry where they came from

Schema: `annotations.ruled_via` — `inflow` (noticed while reading) or `review`
(drawn at random). A ruling can carry a level as well as yes/no. Calibration and
accuracy use review rulings, and say so when there are too few.

## Phase 2 — labels you can see and rule on

Label chips in the message header. A context menu on each: **Right**, **Wrong**,
and for a levelled label **Should be: …**, plus **Clear my ruling**. Right costs
exactly what Wrong costs.

## Phase 3 — a review queue

Ten evaluated, unruled messages for one annotator, drawn across the whole score
range rather than only the doubtful end, ruled with one key each.

## Phase 4 — a score, so engines can be compared

`iql annotate score <name>` and the same in the Annotators tab: accuracy on
review rulings, and for a yes/no label the cut-off that would have been most
accurate. Two annotators asking the same question on different engines are a
bake-off.

## Phase 5 — conversations, levels, and the two questions

An annotator can be about a **thread**: it is evaluated on the newest message,
shown a one-line summary of the conversation first, and a new reply makes it
pending again. Laya answers **score** questions, so a label can carry a level.
Two starters replace the rule starters: `loops` (yes/no, whether the last message
expects a reply, with the direction worked out from who sent it) and
`importance` (four levels).

## Phase 6 — open-loop lists read the labels

The contact card's two lists use `loops` where it has evaluated, and say when
they are falling back to who-sent-last.

## Phase 7 — rules out

Delete the rule annotators (after a backup), disable the extractors whose scope
named them, and refuse to delete an annotator another one's scope still uses
without saying which.

## Phase 8 — contact tags become labels

Same table, renamed, with a source and confidence. `label:` on contacts, `tag:`
kept as an alias; `contact label` / `unlabel`, with the old verbs kept.

## Phase 9 — people and systems

Run the header classifier. Then Laya for the senders it leaves, reading their
last three subject lines, writing `kind` only where it is decisive. Your rulings
on the card are its corrections.

## Not in this plan, and why

**The bake-off result.** Phases 1–4 build the instrument; the measurement needs
about fifty of your rulings per question, which only you can make.

**Fixing Laya's escalation head.** Unusable as read today, and nothing here
depends on it now that routing is by measurement.

---

## What was built

All nine phases. Tested (24 Go packages, race-clean; 195 frontend tests; real
typecheck), applied to the dev mailbox after two verified backups, and the
ruling and review flows checked in a browser.

### Rulings, labels, review, score — phases 1–4

- **v39** `annotations.ruled_via`: `inflow` or `review`; older rulings left NULL
  rather than guessed. `RecordRuling` / `ClearRuling`; rulings can carry a level.
- **Label chips** under the sender, from `GET /api/messages/{id}/labels`.
  Right-click, or the menu key, for Right / Wrong / *Should be …* / Clear. A
  ruling on a conversation label lands on the conversation's newest message.
- **Review** in the Annotators tab: ten drawn across the score range (one per
  band), the model's answer hidden until you answer, `y`/`n` or `1–4`.
- **`iql annotate score`** and a score line on each label card: accuracy on
  reviewed rulings only, and the cut-off that would have been right most often.
- **Calibration refuses** on rulings made while reading; `--include-inflow`
  overrides.

Verified in the browser on a copy: ruled `purchases` wrong on *Project X
Status* → stored `empty / human / inflow`; one review ruling → "1 of 1 right
(100%) · too few to trust".

### Conversations and levels — phase 5

- `"unit":"thread"` in a label's schema: evaluated once per conversation, on the
  newest message; a reply makes it pending again. Coverage counts conversations.
- Laya reads a one-line digest of the conversation, then the newest message;
  levelled labels are score questions (levels reversed to the model's
  bottom-up numbering). The LLM reads the whole conversation, newest kept when
  it is too long, and an invented level is refused.
- Starter pack: `loops`, `importance` (90 days), `purchased`, `automated`, and
  `receipts` / `bills` / `paperwork`. **No rule labels.** `bookings`, `parcels`,
  `cases` were only reachable through a rule and left the pack.

### Open loops — phase 6

The contact card's lists drop conversations `loops` (or you) closed, mark rows
it has not judged, and say which they are built on.

### Rules out — phase 7

Deleting a label another annotator is scoped on is refused unless
`--disable-dependents`. On the dev mailbox: `billing money shipping support
travel` deleted; `bills bookings cases parcels` switched off, their 68 results
kept. A switched-off annotator with a broken scope now says why it is off rather
than warning about a run that cannot happen.

### Contact labels — phase 8

**v40** `contact_labels` with source and confidence; tags copied as `human`.
`in:contacts label:`, `tag:` kept and still implying contacts, `label:` alone
still a message label. `contact label / unlabel` with the old verbs kept; the
API sends both `labels` and `tags`.

### People and systems — phase 9

`contact classify --model laya`: header pass first, then the model on unknown
senders only, from their last three subjects, writing a kind only above 0.85 or
below 0.15. On the dev mailbox: headers 1 system, 1 person; model 2 person, 1
left unknown — **and one of the two was wrong**: `marketing@travel.com`
("Flight to San Francisco") marked a person. Left for a ruling.

### Things found on the way

- **A double key press ruled one review item twice** — the guard was on state set
  only after the save returned. Fixed with a synchronous guard; tested.
- **A label ruled "no" vanished** into "+1 said no", reading as a click that did
  nothing. Your own rulings now always show.
- **`ui open <window> <message-id>` did not open the message** in the browser
  check. Not investigated.

### Not done, and why

- **The bake-off.** Every instrument is in place; it needs ~50 of your reviewed
  rulings per question.
- **`uea-test`.** Held open by your running `--dev` server. Nothing was run
  against it.
