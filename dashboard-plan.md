# A dashboard that says what it charts

*21 October 2026.*

The dashboard shares the Desk's query and had not kept up with it: it charted
the inbox while calling itself "your mail at a glance", its clicks filtered on
something other than what its bars counted, and it went stale after a bulk
archive. This brings it in line with the Desk.

## Decisions

- **The folder is a chip, as on the Desk.** *Inbox ›* stands in front of the
  pills with a picker beside it (Inbox, Starred, Sent, All Mail, Spam, Trash);
  the pills are only what was added, and **Clear** keeps the folder. The query
  stays shared with the Desk, so choosing All Mail here is All Mail there.
- **A click shows what the bar counted.**
  - A sender adds `from:=address`, exact, not a substring.
  - A topic an annotator extracted adds `topic:=name`; a first word of a
    subject (where nothing was extracted) adds `subject:word` as before. The
    server says which each one is, and the card says which it is showing.
- **It refreshes when mail changes** — a move, a bulk action, Undo, a sync —
  as the Desk does.
- **The calendar is one year, chosen.** The volume query is limited to that
  year (`on:YYYY`) rather than grouping the whole mailbox to draw one; arrows
  step between years, and a query on a day in another year opens on that year.
- **Two new cards:** **Importance** and **Kind of sender**, from
  `| count by importance` and `| count by sentby`; a click adds
  `importance:low` or `sentby:system`, ready to open in the Desk and archive.
- **Outside mail, a sentence and a way back.** On Cards, Files or People the
  page says it charts mail and offers **Chart All Mail**.
- **Tidy:** Top Senders shows the ten it fetches; a filtered card names its
  filter instead of "(Filtered)"; the component moves out of `App.tsx` into
  its own file, with tests.

## Phases

1. **Move the dashboard to its own file;** the folder chip and picker.
2. **Exact clicks:** senders, and topics with their kind from the server.
3. **Refresh on mail changes.**
4. **The year:** the server's `year`, the arrows, following an `on:` day.
5. **Importance and Kind of sender cards.**
6. **Outside mail; the tidy items.**
7. **Docs, tests and the browser.**

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5
- [x] Phase 6
- [x] Phase 7

## Checked

All Go tests, the e2e suite under -race, and 312 frontend tests pass —
seven new ones for the dashboard, one API test for the groups' terms and the
year, and a store test for the bug below. In Chrome against devmail:

- The folder chip reads *Inbox ›* with the picker; nothing added reads
  "everything in Inbox — click a chart to narrow it".
- A sender click added `from:=alerts@bank.example`, the card's heading read
  *Top Senders — alerts@bank.example*, and every chart narrowed to it.
- **Kind of sender → Not classified** added `sentby:unknown` and named it.
- The year arrow stepped to 2025, which said "No mail in 2025".
- **Clear** took both pills off and kept *Inbox ›*.

Found on the way:

- **Every filtered topic chart was broken.** The topic grouping's ignored
  words were bound in front of the filter's arguments, while their
  placeholders stand after it: `from:x | top topic` matched nothing and
  `subject:x | top topic` failed with "no such column: re". The dashboard
  showed "No topics" as soon as anything was clicked. They are bound after
  the filter now (`havingArgs` in the planner).
- **The calendar drew two years.** nivo read "2026-01-01" as UTC midnight,
  still 31 December west of Greenwich, and added a near-empty row for the
  year before. It is given local dates now.
