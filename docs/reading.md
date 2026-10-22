# Reading your mail with an AI harness

Some things in your mail are only found by reading it. A Home Depot receipt
says `885911470902 FLEXVOLT 20-` and never "DeWalt"; "I'll get back to you
Monday" means you owe the next message even though you wrote last. No search
finds those and no rule decides them.

So InboxQL keeps a short list of things worth knowing later — **elements** —
and lets the AI you already use (Claude Code, Antigravity, anything that can
run a command) read your conversations once and answer them. The answers are
checked, kept with who gave them, and become as easy to ask about as `from:`.

## The elements

| Element | About | What it records |
|---|---|---|
| `receipts` | each message | merchant, date, order number, every item with its brand and price, total |
| `travel` | each message | flights, trains, hotels, cars: provider, confirmation, from, to, when |
| `events` | each message | things to put in your calendar — proposed, for you to approve |
| `tasks` | a conversation | what someone has to do, and who owes it — proposed as tickets |
| `waiting` | a conversation | whether someone is waiting: on you, on them, or no one |
| `importance` | a conversation | important, normal, low or ignorable |
| `sender` | a sender | person, organization or system |
| `topics` | a conversation | a few tags, chosen from the ones in use |

Install them with `iql elements install`, or from Settings → AI Configuration →
[Starters](#setting/ai-starters). `iql elements show receipts` prints one in
full: the instructions the reader is given, and its fields or levels.

## Pointing your AI at it

Tell your AI to run:

```
iql work guide
```

That prints everything it needs. The loop is two commands:

```
iql --json work next --limit 10          conversations to read, newest first
iql --json work submit < answers.json    one answer per conversation
```

`work next` hands over each conversation readably — the email rather than its
HTML, the text of its attachments, who is in it — with what to answer, and
leases it for half an hour so two runs do not read the same thing. Narrow it
with `--only receipts,travel` or `--query "from:homedepot"`.

A prompt that works well: *"Use iql. Run `iql work guide`, then answer the
newest 100 conversations."*

## Why you can trust the answers

- **Checked before they are kept.** Levels must be the element's levels,
  records must have its fields and types, and every judgement and record must
  quote words that really are in the conversation. One problem keeps nothing
  for that conversation and says what to fix.
- **Kept with who gave them.** Every answer carries the model that gave it, a
  confidence and why. `iql work status` counts them by element and model.
- **Yours win.** Right-click a label chip to rule on it; your ruling outranks
  the AI's, now and after re-reading. **Review** in Annotators draws a random
  sample, and `iql annotate score waiting --model claude-opus-5-5` says how
  often that model was right.
- **Undoable.** `iql work clear --model claude-opus-5-5 --since 1d` removes a
  run's answers, and those conversations are read again next time.
- **Proposals, not actions.** Tasks arrive as proposed tickets on the
  [Board](tickets.md) and events as proposed calendar events; nothing happens
  until you accept them.

## Measuring the loop

**Intelligence → [Work loop](#open/work-loop)** shows how well the reading is
going: how often each kind of answer is right, how much effort it takes, and
where it should read more carefully.

**Guesses are kept apart from conclusions.** `work next --template` fills every
answer in advance with a guess, so the AI only edits what differs. An answer
sent back exactly as guessed is stored as a guess: it is counted separately, a
deeper read (`iql work next --deep`) asks it again, and it does not teach the
next template anything about that sender. The template never guesses where the
conversation looks like it holds the thing — a receipt-looking subject is never
pre-filled with "no receipt" — and never adds mail labels; it only suggests them.

**Checks.** `iql work audit next` hands answered conversations to a second
reader — ideally a different or stronger model — to read again from scratch,
without seeing the first answers. Half of each batch is drawn at random, and
only those measure accuracy; the other half goes where a wrong answer is
likeliest and costliest. Where the two readers agree, the answer stands. Where
they disagree, the conversation is flagged for a deep read and the pair waits
for you under **Waiting for you**, as *Answer A* and *Answer B* — which was the
first read and which the check is shown only after you choose (keys **1**,
**2**, **3** for neither). Your choice becomes a ruling that outranks both.

**Targets.** Each element has a target (how often it must be right) and a cost
(what a wrong answer does: a missed receipt costs more than a wrong topic).
Where checks show an element missing its target, the template stops guessing it
and it has to be read; costlier and less certain elements are checked more
often. A sender whose answers passed ten checks in a row earns the cheap path
back. Change targets on the page, or with
`iql work targets set receipts --target 0.99 --cost high`.

**What the page shows:**

- **Right, on random checks** — with the range it is probably in. A range
  that dips below the target means more checks are needed before trusting it.
- **Your reviews** — your rulings from **Review** in Annotators.
- **What you did with its work** — proposed cards and events you approved or
  rejected, labels you took off, conversations you moved back to the inbox:
  corrections you make without being asked.
- **Effort** — conversations per day, refusals and why (rework), leases that
  ran out, time from lease to submit, and cost per conversation when the
  harness reports it (`work submit --input-tokens … --output-tokens … --cost-usd …`).
- **Confidence** — whether answers that say they are 90% sure are right 90% of
  the time.

From a terminal: `iql work metrics --since 30d`, `iql work audit list`, and
`iql work audit resolve <id> original|checker|neither`.

## Asking about the answers

```
extract:receipts.items.brand=DeWalt          anything DeWalt you bought
extract:receipts.merchant="Home Depot" after:2025
extract:travel.kind=flight after:2026-01-01
awaiting:me | timeline                       conversations you owe a reply
in:contacts awaiting:me -kind:system         people waiting on you
extract:importance.level=important after:7d
topic:kitchen-remodel
```

A path through a list — `receipts.items.brand` — matches if any item does.
`unlabeled:waiting` is what nobody has judged yet.

## When something changes

A reply makes a conversation's `waiting`, `importance`, `tasks` and `topics`
pending again, but not the receipts in the messages before it. Changing an
element's instructions makes it a new question: earlier answers stay, marked
as answers to the old one, and everything is read again.
