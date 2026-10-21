# Rail counts that preview the click

*21 October 2026.*

The rail's numbers were global: *banking 312* whatever you were looking at,
*Inbox 40* unread whether or not you had added anything to the frame. Now
that a label adds to the frame and a folder swaps it, each number can say
exactly what clicking that row would leave you with, before you click.

## Decisions

- **Labels and annotator rows count inside the current query.** On
  *Starred*, *banking 12* means 12 starred messages carry banking — what
  clicking it shows. A row already in the query shows the list's total.
- **Folders are scoped too** (your call): a folder swaps the frame and keeps
  what was added, so its count is the additions inside that folder —
  on *Inbox › banking*, *Archive 3* is the banking mail in Archive. With
  nothing added the folders show their usual badges (Inbox unread, Starred
  total, …), because then the click shows the whole folder. Drafts are a
  different kind of thing and keep their own count.
- **Zero dims, never hides,** so the rail does not reshuffle under you.
- **It says it is scoped.** A small line over the labels — *In Starred* —
  and over the folders when something is added — *With banking*. Each count's
  tooltip reads "12 of 40 in Starred".
- **One request per query.** `GET /api/messages/facets?q=…&rest=…`: the
  frame's total, its labels in one `GROUP BY`, each annotator's term and each
  folder alongside, counted concurrently as the folder counts already are.
- **Never slows the list.** It runs after the list's own query; until it
  answers, the previous numbers stay, dimmed. It refreshes when mail changes,
  which bulk actions and Undo already announce.
- **Outside mail** (Cards, Files, People) the rail shows its plain totals and
  no scoped line. In All Items the words typed are the additions, so the rail
  counts them in every folder.

## Phases

1. **The store:** `MessageFacets(query, rest)`, with tests.
2. **The endpoint.**
3. **The rail:** scoped counts, dimmed zeros, the scope line and tooltips,
   stale-while-loading.
4. **Docs, tests and the browser.**

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4

## Checked

All Go tests and 304 frontend tests pass; `TestFacetsPreviewTheClick` covers
the store and four rail tests the rendering. In Chrome against devmail, with
15 banking messages in the inbox and 3 archived:

- Inbox: the labels read *In Inbox*, banking 15, unused labels dimmed.
- Clicking banking: folders read *With label:banking* — Inbox 15, All Mail
  18, the rest dimmed — and the labels *In Inbox › label:banking*.
- All Mail › banking: banking 18.
- All Items with `label:banking`: *In mail matching “label:banking”*, banking
  18, beside Mail 18 in the results.

The Drafts row keeps its own count, as planned. Found on the way: clicking
Drafts with a mail term added kept the term and showed an error ("drafts have
no label"); Drafts now opens on its own, as it does coming from Cards or
People.
