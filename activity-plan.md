# How often a contact writes

*19 October 2026.*

The dashboard's Communication Intensity chart, on the contact card: a day by
day picture of the mail one person sent you over the last year.

## Phase 1 — one chart, two places

- `views/ActivityCalendar.tsx`: the dashboard's calendar heatmap as a component
  — data, date range, theme colours, a click on a day. The dashboard uses it
  unchanged in appearance.

## Phase 2 — on the card

- A **How often they write** section below the counts: their mail
  (`from:=address after:<a year ago> | count by day`), the last twelve months
  rather than the calendar year, so January is not empty.
- A line above it for people who write rarely, where a grid of empty days says
  little: how many in the year, about how many a month, the busiest month.
- Clicking a day opens their mail from that day in Desk.

## Phase 3 — tests

- The summary arithmetic, the query the card runs, and the click.

## Phase 4 — the guide

- "What a contact card shows" names the chart. A contact card screenshot,
  from the made-up mailbox, in both themes.

## Phase 5 — checked

- In Chrome against devmail, both themes; every test passes.

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5

Found while checking: nivo's TimeRange stops the day before `to`, so today
was not drawn — fixed in ActivityCalendar (`dayAfter`) and tested. A click on
a day opened `from:=maya@harbor.example on:2026-10-16` and that day's mail.
