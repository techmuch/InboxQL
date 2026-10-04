# A rail you can arrange

*A phased plan. 4 October 2026.*

## It already exists, under another name

A `SavedQuery` is `{title, query, pinned, description}`, created and deleted
from `iql saved` and the API. The rail's **Other** block is six saved queries
somebody wrote in TSX instead of saving:

```tsx
{ label: 'Files',  icon: Paperclip, q: 'in:attachments' },
{ label: 'People', icon: Users,     q: 'in:contacts kind:person' },
```

So "edit, remove, organize" is not a new system. It is: make the rail **be** the
saved-query list, and give a saved query the two things it lacks — a
**position** and an **icon**.

"A set of defaults to add from a list" is the starter-pack pattern already here
twice for annotators. Show the candidates, say what each would match **in this
mailbox**, install the ones wanted. An entry that matches nothing is not worth
adding, and the number is what makes that visible.

## The rail is not one kind of thing

Three categories, treated differently on purpose:

| | What it is | What it gets |
|---|---|---|
| **Folders** (Inbox…Trash) | the mailbox itself, live unread badges, wired to `setFolder` | **hidden**, not deleted |
| **Saved** | user content | edit, remove, reorder |
| **Annotations** | derived, already filtered to those that matched something | left automatic |

Deleting Inbox is a trap and the badges are half the point of those rows.
Hand-ordering the Annotations section would fight its own derivation: run a new
annotator tomorrow and the manual order is stale.

Saying this out loud beats quietly making "edit the rail" mean three things
depending on where somebody clicks.

## Four decisions

**Ordering needs a column.** Today it is `ORDER BY pinned DESC, name ASC` —
pinned is ordering, badly. `position` supersedes it; `pinned` keeps working
because it is in the CLI, but stops being the sort.

**Icons are a fixed vocabulary.** Twelve identical bookmarks is worse than six
distinct ones, and arbitrary SVG in a rail is not a thing to accept from a
query.

**Move before drag.** Drag is the obvious UI and the expensive one: a dependency
or a lot of code, inaccessible by default. Explicit move works from a keyboard
and is what the CLI needs anyway.

**The CLI does all of it.** A rail only the web UI can rearrange is a state an
agent cannot see or set — the rule that made `annotate enable` and `ui list`
worth building.

---

## Phase 1 — Position and icon

Schema, store, CLI and API. `iql saved move <name> up|down|<n>` and
`iql saved icon <name> <icon>`.

## Phase 2 — The rail becomes data-driven

It is inline in `Desk/index.tsx` today. One list of entries with a kind, built
from folders, saved queries and annotators, rendered once. The enabling
refactor, and the phase with the regression risk.

## Phase 3 — Arranging it

Edit, remove and reorder saved entries in the UI; show and hide folders. A
restore-defaults escape, because a rail with no Inbox and no way back is a bad
afternoon.

## Phase 4 — The defaults

Today's hardcoded six, plus a few worth having, as a pack that reports what each
would match here.

## Not doing

**Deleting folders.** Hidden, with a way back.

**Hand-ordering the Annotations section.** It earns its place by being
automatic.

**Drag and drop.** After the explicit move works, if it is still wanted.

---

## What was built

| Phase | Where |
|---|---|
| 1 — position and icon | schema v35; `internal/store/saved.go`, `rail.go`, `raildefaults.go` |
| 2 — data-driven | `frontend/src/views/Desk/Rail.tsx`, extracted from `index.tsx` |
| 3 — arranging | arrange mode in the rail; `/api/rail/*` |
| 4 — the defaults | `RailDefaults` in Settings, and `iql saved defaults` |

### The refactor caught the thing it was meant to

Removing the hardcoded six broke three frontend tests, and they were right to
break: contacts and files became unreachable without typing `in:contacts`,
which is the exact gap that block was written to close.

So the migration **seeds them**. An upgrade changes nothing visible, a fresh
database gets the same six, and all of them are now ordinary rows:

```
$ iql saved list          # a fresh database
#  NAME          ICON       TITLE         QUERY
0  tickets       ticket     Tickets       in:tickets -status:done -status:rejected
1  proposed      ticket     Proposed      in:tickets status:proposed
2  files         paperclip  Files         in:attachments
3  people        users      People        in:contacts kind:person
4  systems       bot        Systems       in:contacts kind:system
5  unclassified  search     Unclassified  in:contacts kind:unknown
```

### Verified both ways round

Moving an entry in the browser changed the database; moving it from the CLI
changed the browser. Hiding Spam in the rail showed as `hidden` in
`iql saved folders`, and `reset` brought it back.

`saved defaults` reports what each would match **here** — files 50,
unclassified 57, awaiting-me 4, tickets 0 — which is the number that decides
whether a row is worth adding.

### One test was asserting the fixture, not the behaviour

`TestCompletionUsesRealData` checked that `saved:` offered exactly one
candidate. With six seeded rows that stopped being a fact about completion and
became a fact about an empty database. It asserts membership now.

It was nearly missed: the first full-suite grep let migration log lines through
and printed "GO OK" over a real failure. Worth remembering that a filter which
passes noise can also pass a FAIL.

### Three kinds of row, kept apart

**Folders** hide and come back. **Saved** entries are edited, moved and
removed. **Annotations** are left automatic, because they are derived and a
manual order would be stale the next time an annotator runs.

Arranging is a mode rather than always-on controls: a rail with a delete button
beside every row is a rail somebody deletes something from by accident, and the
common act is clicking a row rather than editing one.

### Not done

**Drag and drop.** Explicit move works from a keyboard, is what the CLI does,
and needs no dependency. Worth revisiting now that the ordering exists.

**Editing a saved query's text in the rail.** The pencil puts it in the query
bar, which is where it is edited and saved today.
