# A Desk that takes fewer clicks

*21 October 2026.*

The Desk led with the query language: a two-line box, a pill builder under it,
and a rail where every click replaced the whole query. Simple work — "the
banking mail in my inbox", "archive everything low-importance" — took typing,
or paging through selections fifty at a time. This makes the folder the
frame, makes adding to it one click, and lets one action reach every message
that matches.

## Decisions

- **The bar is one line.** The query box and Run are no taller than the
  buttons under them. The pill builder is behind **Builder**, off by default
  and remembered per browser.
- **A folder is always selected.** The query's top-level `folder:` (or its
  `in:` kind) is lifted out into a fixed chip in front of the box — *Inbox ›*
  — and the box holds only what is added to it. Switching folders keeps the
  additions. A mail query with no folder is **All Mail**. The query is still
  one string underneath, so everything that opens a query elsewhere keeps
  working, and the chip and the box can never disagree with it.
- **Labels add instead of replacing.** A label, or an annotator's results, in
  the rail toggles its term within the folder; two combine. Saved queries run
  inside the folder unless they name their own folder or kind, which they
  then keep.
- **Quick filters,** one click each, under the bar, by kind: for mail Unread,
  Starred, Attachments, Important / Low, From people / From systems, Waiting on
  me; for cards, files and people the ones that fit them. Backed by new
  spellings — `importance:low`, `sentby:system` — so a filter shows as a term
  anyone could type.
- **Select every match.** Selecting the whole page offers "Select all 1,234 in
  Inbox › Low"; Archive, Trash, Spam, Mark read, Star and Label then act on the
  query on the server, say how many first, and offer Undo.
- **Process by group.** A Group view over the folder: by sender, importance or
  topic, each group with its count and Archive all / Mark read / Label / Show.
- **All Items** sits above Inbox: one search across mail, files, cards, people
  and events, results side by side by kind, each with "See all". The log is
  not included. Bulk actions stay inside one kind.
- **Keys:** `x` selects the focused row, `* a` selects all, `* n` none; `e`,
  `l` and the rest act on the selection, all of it when every match is
  selected.

## Phases

1. **The compact bar,** and the builder behind a toggle.
2. **The folder frame:** the chip, additions, All Mail when there is no
   folder, label and saved-query rows that add.
3. **Quick filters** and `importance:`, `sentby:`.
4. **Select every match:** the count, the bulk endpoint, Undo.
5. **The Group view.**
6. **All Items:** the search endpoint and its view.
7. **Keys, docs, tests, and the browser.**

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5
- [x] Phase 6
- [x] Phase 7

## Checked

All Go tests (store, query, API, CLI, e2e under -race) and 300 frontend tests
pass. In Chrome against devmail, with 70 messages delivered into the inbox,
40 conversations judged low and two senders classified:

- The bar is one line, *Inbox ›* in front, Run no taller than the buttons
  under it; the builder is off.
- **Low** added `importance:low` inside the inbox: 40, on one page.
- **From systems** gave 60; selecting the page offered "Select all 60 in
  Inbox › sentby:system"; **Archive** said "Archived 60 of 60" and **Undo**
  put all 60 back, the inbox at 75 again.
- **Group by Domain**: shop.example 60, store.example 10, example.com 5;
  **Mark read** on store.example read all ten.
- **All Items**, "order": Mail 10 and People 2 side by side.
- `iql mail read --query … --dry-run` counted 10 and changed nothing.

Found on the way: the list header said "of 75" — the folder's total — for a
query that matched 40, and now counts the query; an empty filtered folder
said "Nothing in the inbox. Sync an account…", and now says nothing in the
folder matches, with **Show all of Inbox**. Unrelated to the Desk, an
earlier write of this session replaced `internal/store/search.go` by
mistake; it was restored from git before anything else ran.
