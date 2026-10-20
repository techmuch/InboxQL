# One meaning for "waiting"

*19 October 2026.*

The rail's **Waiting on me** (`in:contacts awaiting:me`) and the contact
card's **Awaiting your reply** answered the same question two ways. The card
asked the `loops` label and the rail did not; neither had heard of the new rule
that Trash and Spam stay out. So a newsletter, a "thanks!", or a spammer whose
message sits newest in a conversation in Spam was someone waiting on you —
forever, since nothing ages out.

## The definition

A conversation is **waiting** when its newest message outside Trash and Spam
has not been called closed by `loops` (a person's ruling first, then the
model's at its current version). It waits **on me** when that message is not
mine, **on them** when it is.

- A contact is waiting on me when a conversation waits on me and they have
  written in it since I last did.
- I am waiting on a contact when a conversation waits on them and they are in
  it.
- Where `loops` has not looked, who sent last decides, as before.

## Phase 1 — one definition

- `query/awaiting.go` writes it once, as SQL. The compiler's `awaiting:` and
  `has:awaiting` use it, and so does the contact card.

## Phase 2 — conversations, not only people

- `awaiting:` on mail: the newest message of each conversation waiting on me
  (or them). `awaiting:me | timeline` is the conversations you owe a reply.
- Bare `awaiting:me` is now about mail; `in:contacts awaiting:me` still lists
  people.

## Phase 3 — the rail

- **Waiting on me** becomes `in:contacts awaiting:me -kind:system`: machines
  do not wait on anyone. A new **Owe a reply** entry,
  `awaiting:me | timeline`.
- **Unread** becomes `is:unread`: Trash and Spam are left out without saying
  so now.
- Entries already saved with the old text are brought up to date.

## Phase 4 — the guides

- The contacts guide, the query language guide and `AGENTS.md`.

## Phase 5 — checked

- The card's existing tests unchanged and passing; new tests for each case;
  Chrome against devmail.

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5

Checked: the card's existing tests pass unchanged on the shared definition. In
Chrome against devmail, `awaiting:me | timeline` listed the open
conversations and not the one in Spam; Alice's card listed the same
conversation; Settings → Rail entries offers **Owe a reply** and the new text
for Waiting on me and Unread. Two corrections found on the way: you were
listed among the people you wait on (your own addresses are now left out), and
a test of mine expected an answered question to wait on nobody — on who sent
last it waits on the asker, which is what `loops` is for.

`-kind:system` leaves out machines once a contact has been classified; until
then a newsletter's sender is `unknown` and still listed.
