# Trash and Spam stay out of results

*19 October 2026.*

Only the `folder:` views left out Trash and Spam. Any other query — `from:bob`,
`anyone:x | count by day`, a contact's counts, the dashboard with no filter —
counted mail you had thrown away. Gmail's search leaves both out unless asked,
and so will this.

## The rule

A query about mail leaves out Trash and Spam unless it mentions them. Each
on its own: mentioning Trash brings back Trash, not Spam.

- Trash is mentioned by `folder:trash`, `label:trash`, `is:deleted` /
  `is:trashed`; Spam by `folder:spam`, `label:spam`, `is:junk` / `is:spam`;
  both by `folder:all`. Anywhere in the query — inside `( … OR … )`, after a
  `-`, or inside a `saved:` query it uses.
- `id:` names messages exactly, so a query with one is not narrowed: a
  selection, or an annotator run on one message, still finds it in Trash.
- Opening a message by its id is not a query and is unaffected.

## Phase 1 — the compiler

- The predicates are the compiler's own `label:trash` / `label:spam` tests
  (`query/binned.go`), and `query.Unbinned` is the same rule for SQL outside
  the compiler, so a contact's counts cannot disagree with a query.
- The compiler notes what a query mentions as it compiles it, so a `saved:`
  query's own terms count. `Build` and `CompileFilter` add the rest.
- Applied to mail and to files (a file only in trashed mail is not listed);
  not to tickets, drafts, contacts, events or the log.
- `| thread` widens to whole conversations and then is narrowed again, so a
  trashed reply does not come back in through its thread.
- `Plan.LeftOut` says what was left out, for the result and for `explain`.

## Phase 2 — the store

- Message lists and conversation lists report how many matching messages were
  left out (counted up to 1,000), so the Desk can offer them.
- A conversation built for a narrowed query leaves out its trashed messages
  and says how many there were.
- Contact counts, first and last seen, topics and responsiveness count only
  mail outside Trash and Spam, so the card's numbers agree with its chart.
- Annotator scopes and rules are filters and follow the rule.

## Phase 3 — the Desk

- Under results: "3 more in Trash or Spam — Show them", which adds
  `folder:all` to the query.
- A conversation row says "1 in Trash".

## Phase 4 — the guides

- The query language guide, `AGENTS.md` (a change in behaviour for agents),
  and the contacts guide.

## Phase 5 — checked

- In Chrome against devmail; every test.

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5

Found on the way: `in:attachments folder:trash` failed with "ambiguous column
name: id" — the folder predicates correlated on a bare `id`, which a files
query has twice. They now take the `m` alias (`folderClauseOver`). The legacy
`/api/messages?folder=all` now says `folder:all` rather than implying it.

Checked in Chrome against devmail: `from:bob` showed 2 of Bob's 4 with "2 more
in Trash or Spam"; Show them made it `from:bob folder:all` and showed 4;
`folder:trash` showed the one in Trash; Alice's conversation read "1 message ·
1 in Trash"; Bob's card counted 2. No page errors.
