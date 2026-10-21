# A board worth keeping

*20 October 2026.*

The board becomes the place personal to-dos and personal projects live, at
parity with Trello for one person: cards you open and edit, lists you make,
order you choose, checklists, labels, comments, several boards, due dates on
the calendar, and simple automation. Not shared, so no members, permissions
or power-ups.

What it keeps that Trello has not got: every card traces to its mail, the
machine proposes and you accept, one query language filters everything, and
anything the page does the CLI does too — the harness drives the board.

## Decisions

- **Lists are yours.** A list has an id (what a card's `status` holds, so
  `status:todo` keeps working), a title you can rename freely, a position and
  a kind: `proposed` (the one queue proposals land in), `open`, or `closed`
  (entering it stamps the card finished). You start with Proposed, To do,
  Doing and Done. Lists can be added, renamed, reordered, archived, and
  deleted once their cards have moved. Rejected stays a hidden status, not a
  list: it is evidence about an extractor, not work.
- **Labels are topics.** A card's labels share one vocabulary with the topics
  on mail. A card raised from mail starts with its conversation's topics, and
  from then on they are yours. In a ticket query `label:` and `topic:` mean
  the card's own labels. Each label can have a colour; one that has none gets
  a stable colour from its name.
- **Order is yours.** Cards keep a position within their list; a new card
  goes to the bottom, as in Trello. A move says which card it follows, so it
  is unambiguous even on a filtered board.
- **Archive is not delete.** An archived card or list leaves the board and
  stays searchable: ticket queries leave archived cards out unless they
  mention `archived:`, the same rule Trash follows for mail.
- **A board is a named filter** over the same lists. "All cards" is the
  default; "Home" might be `label:home`. A card added on a board whose filter
  names labels gets those labels.
- **Automation is rules**, each a trigger and actions: *a card enters a list*
  (check every item, add or remove a label, set or clear the due date,
  archive) and *mail arrives matching a query* (make a card in a list, with
  labels — one per conversation per rule). Mail rules run after each sync,
  only on mail that arrived after the rule was made unless backfilled.

## Phases

1. **Cards you can open.** Schema v46 for everything below. A card panel:
   title, Markdown description, list, priority, start and due dates, every
   message behind it, and its history. An update API, and `iql ticket edit`.
2. **Order, quick-add, archive.** Drag within and between lists to a place,
   "+ Add a card" on every list, keyboard moves, archive and restore;
   `archived:`.
3. **Checklists, labels, comments.** Checklist items with a progress badge,
   labels shared with topics and coloured, comments in the card's history;
   `label:`, `topic:`, `comment:` and bare words over cards.
4. **Your own lists.** Add, rename, reorder, archive, delete with a
   destination; open and closed; `status:` completes from your lists, `list:`
   is the same field, `state:open|closed`. `iql ticket lists`.
5. **Boards, calendar, automation.** Boards as named filters with a picker;
   cards with due dates on the calendar; rules, run on entry and after sync.
   `iql ticket boards`, `iql ticket rules`.
6. **Documented and checked.** tickets.md, AGENTS.md, help; tests throughout;
   checked in the browser against devmail.

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5
- [x] Phase 6

## Checked

All Go tests (store, API, CLI, e2e under -race) and 290 frontend tests pass.
Against devmail, seeded through the CLI alone, then in Chrome:

- Lists of your own (Waiting on vendor), labels in colour, an overdue date in
  red, checklist and comment counts on the card's front.
- A card opened: Markdown description, dates, checklist with progress, the
  agent's comment, and the history.
- A card dragged into a list at a place; Shift+→ moving one list at a time.
- Into Done: the rule ticked the checklist and added `finished`; the due date
  turned green.
- The Home board (`label:home`): quick-add gave the card the `home` label.
- The calendar's month view: three cards due under "Cards due"; clicking one
  opened it on the board.
- A mail rule (`subject:invoice`, backfilled) made one card from the invoice.

Found on the way: a second Shift+→ pressed before the first redrew acted on
the card's old place, and a label made by a rule drew grey until the page was
reopened. Both fixed: keys read the board as it now is, and the labels reload
with the board. Renumbering a list in one UPDATE could leave two cards on one
position; it is read first and written after.
