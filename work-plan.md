# What is worth knowing, read once

*20 October 2026.*

Your AI harness (Claude Code, Antigravity, anything that can run `iql`) reads a
conversation once and answers, in one structured reply, everything worth
knowing about it later: what was bought, travel booked, events to put in the
calendar, tasks, who is waiting on whom, what kind of sender someone is, what
it is about, and how much it matters. Everything is done from the CLI.

The motivating miss: "a Home Depot receipt for anything made by DeWalt" found
one of three, because the receipts say `885911470902 FLEXVOLT 20-` and never
"DeWalt". Knowing that is reading, not searching.

## The pieces

- **Elements** — the things worth knowing. Each is an annotator answered by
  the `agent` engine: instructions, a unit (a message, a conversation, a
  sender), a shape (levels, records with fields, tags), a version, and where
  its answers go besides the label store (tickets, the calendar, contacts,
  topics).
- **`iql work next`** — the next conversations to read, newest first: cleaned
  text including attachment text, who is in it and what is known about them,
  which elements to answer and why, under a lease so two runs do not overlap.
- **`iql work submit`** — one JSON answer per conversation, checked before
  anything is stored: every element answered, `none`d or skipped; levels and
  fields as declared; every judgement and record carrying a quote that is
  really in the conversation; instructions not changed since it was read.
- **Trust** — answers record the harness, the model and the time; your rulings
  outrank them; `annotate score` measures them; `work clear` undoes a run.

## The catalog

| Element | About | Shape | Also goes to |
|---|---|---|---|
| `receipts` | a message | records: merchant, date, order, items (name, brand, sku, quantity, price), total | |
| `travel` | a message | records: kind, provider, confirmation, traveler, from, to, start, end, number | |
| `events` | a message | records: title, start, end, all day, location | proposed calendar events |
| `tasks` | a conversation | records: what, owner, due | proposed tickets |
| `waiting` | a conversation | on me / on them / no one | `awaiting:` |
| `importance` | a conversation | important / normal / low / ignorable | |
| `sender` | a sender | person / organization / system | the contact's kind |
| `topics` | a conversation | tags | `topic:` |

## Phases

1. **Text worth reading.** Readable text from HTML (no stylesheet, entities
   decoded, rows kept); a bare word also searches attachment text.
2. **The catalog.** The `agent` engine and source; the eight elements replace
   the laya and span starters; installing replaces a same-named annotator only
   if it has never answered.
3. **`work next`.** Pending per element, leases, the payload.
4. **`work submit`.** Validation, quotes, storing, and the four destinations.
5. **Reading the answers.** `extract:` reaches into lists
   (`extract:receipts.items.brand=DeWalt`); `awaiting:` and the contact card
   read the `waiting` element and nothing else — no judgement, not waiting.
6. **`work guide`, `work clear`, `work status`; the guides and `AGENTS.md`.**
7. **Checked end to end:** this session acting as the harness against devmail.

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5
- [x] Phase 6
- [x] Phase 7

## Checked

This session acted as the harness against devmail: four conversations (a Home
Depot receipt, a request and a lunch invitation from Alice, a Delta
confirmation, a newsletter). A made-up quote and an invented level were refused
with nothing stored. The real answers were stored, and then:

- `extract:receipts.items.brand=DeWalt` found the receipt that says only
  `885911470902 FLEXVOLT 20-`; `…brand=Ryobi` found it too.
- `extract:travel.confirmation=GHT7QK` found the flight.
- `awaiting:me | timeline` was Alice's conversation; her card listed it.
- Two proposed tickets ("Send Alice the signed contract", due Friday) and a
  proposed calendar event ("Lunch with Alice Moreno") awaited approval.
- The receipt's chips read importance: low, sender: system, waiting: no-one,
  and its Values tab shows the items with their brands.

Found on the way: the Values tab counted only span-engine marks, and showed an
HTML message's stored text (stylesheet first) — it now counts and shows the
harness's records, and shows readable text when nothing needs aligning;
`annotate score` could not tell models apart — it takes `--model`.
