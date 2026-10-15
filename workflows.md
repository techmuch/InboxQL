# Workflows: what the models are for

*13 October 2026. A design document for discussion; the next one is how AI
operationalises it.*

## The idea in one paragraph

Mail arrives as messages, but what it carries is obligations: something to
answer, something to do, somewhere to be, something someone else owes you.
InboxQL already stores the messages, the people, the files, the calendar and
the tickets. The value is in noticing the obligation inside a message, giving
it the right shape (a reply, a task, an event, a wait), linking it to
everything related, and keeping it in step as the work moves. That should
happen in the background and stay out of the way, and nothing should reach
another person without the user choosing to send it.

## Principles

1. **Background first, cheap first.** A judgement is made once, when the mail
   arrives or a conversation changes, and stored. Nothing is computed while
   someone waits for a screen. The cheapest thing that can decide does decide:
   headers and rules before a small local model, a small model before a large
   one.
2. **Suggest, don't act.** The system proposes a label, a card, an event or a
   draft. A person accepts it. Anything that tells another person something
   (sending mail, answering an invitation, declining) is the person's act, as
   now.
3. **Everything is linked, and every link says where it came from.** A card
   knows its thread; an event knows its prep tasks. Every link records whether
   a person made it, it was derived (same conversation), or it was suggested,
   so a suggestion can be told apart from a fact and undone.
4. **Explain every suggestion.** "Suggested because Ada asked *can you send the
   signed copy by Friday?*" with the sentence highlighted. A suggestion with no
   reason is noise.
5. **Corrections teach.** Rejecting a suggestion is kept as evidence, as with
   tickets and labels today, and it is what the models are measured and
   calibrated against.
6. **Quiet by default.** Most mail needs nothing. A system that asks about
   every message is worse than one that asks about none.

## The core idea: efforts

Several of the workflows below need one thing that doesn't exist yet: a way
to say "these all belong to the same piece of work".

An **effort** is a project, client, deal, trip, hire or case: anything a
person spends a stretch of time on. It is a hub. Messages (whole
conversations), tasks, events, contacts, files and drafts can all belong to
one or more efforts.

- **Membership is explicit, inferred, or by rule.** You add a thread to an
  effort; a reply in that thread joins it automatically; a rule such as
  `from:*@acme.com` or `attached:*contract*` brings matching mail in as it
  arrives, and a rule is an ordinary saved query. Suggested membership ("this
  looks like the Acme renewal") is shown as a suggestion, never filed silently.
- **An effort has a page:** its conversations, open tasks (a board filtered to
  it), upcoming events, people, files, and a timeline of everything in date
  order.
- **Boards become views of efforts.** One board can be filtered by effort, or
  each effort can have its own; both are the same board with a query, which is
  how the board already works.
- **Efforts answer the time-tracking question.** "Who is this time charged to?"
  is usually "which effort is this for?". A meeting linked to the Acme effort
  is Acme's time, so the charge reserved in the calendar plan becomes an
  effort link, with a charge code on the effort.
- **In the query language:** `effort:acme-renewal`, and `| count by effort`,
  `| sum hours by effort`, across every kind.

## The first decision for every message

Each new message, or a conversation that has just changed, gets one triage
outcome. It is stored on the conversation, because what matters is the state
of the exchange, not one message on its own.

| Outcome | Means | Where it goes |
|---|---|---|
| **Nothing needed** | FYI, newsletters, notifications, a thank-you that closes the loop | nowhere: the quiet default |
| **Needs a reply** | someone asked something answerable in words | the **Needs reply** list |
| **Needs action** | answering means doing work first: send a document, review something, fix something | a **proposed card** on the board, linked to the thread; also in Needs reply, marked "has a task" |
| **Scheduling** | a meeting being proposed, moved or confirmed; a booking | an **event suggestion** (see the calendar plan) |
| **Waiting on others** | you asked, they haven't answered | the **Waiting on others** list |
| **Your commitment** | you promised something ("I'll send it Friday") | a **proposed card** with that due date |

One message can produce more than one outcome. "Can you review the draft and
let me know if Thursday works?" is both an action (review) and scheduling
(Thursday).

The existing `loops` label already answers half of this, which way the
conversation is waiting. Triage adds what kind of response is needed. The
lists replace today's per-contact "awaiting your reply" with mailbox-wide ones,
and the contact card keeps showing its slice of them.

## The workflows you described

Each is written as trigger → what happens → what the user sees → what gets
linked. The decisions they need from a model are collected at the end.

### W1. Needs a reply

**Trigger:** a message asks the user something answerable without doing other
work first.
**System:** marks the conversation *needs reply*, with the sentence that asks.
**User sees:** the conversation in **Needs reply**, oldest waiting first, with
how long it has waited and the question quoted. The row's actions are Reply,
Snooze until…, and Not needed (a correction).
**Clears when:** the user replies, the thread's next message from them answers
it, or they mark it handled.

### W2. Needs a task

**Trigger:** the response requires work.
**System:** proposes a card with a short imperative title ("Send Ada the signed
contract"), any deadline found ("by Friday"), a link to the thread, and the
person waiting.
**User sees:** the card in **Proposed** on the board and as a chip on the
message ("1 proposed task"). Accept, edit, or reject, as tickets work today.
**Also:** the conversation is in Needs reply too, marked "has a task", so
nobody answers it without noticing the work.

### W3. Updates as the card moves

**Trigger:** a linked card moves between columns (To do → Doing → Blocked …).
**System:** offers, without insisting, a short update to the people in the
linked thread. For example: "Started on this; I'll have it to you by
Thursday." It is pre-filled from the card and its new column, and editable.
**User sees:** a small prompt on the moved card, "Let Ada know?", with the
draft. Ignoring it is fine; it doesn't come back for that move.
**Settings:** per board or per effort, off, ask, or always ask for certain
columns. Never automatic.

### W4. Done → the reply

**Trigger:** the card is dragged to Done.
**System:** opens a reply to the linked thread, pre-filled from what the card
was ("Here's the signed contract.") and anything attached to it.
**User sees:** the reply composer, with the card's files offered as
attachments, and a link to close the card without replying ("handled another
way").
**Clears:** the thread's Needs-reply state and the card, together.

### W5. Replying where cards exist

**Trigger:** the user replies to a thread with open cards linked to it.
**System:** after the reply is written, before it's sent, reads what it says
against each card ("Attached is the signed copy" against "Send the signed
contract") and offers: "Mark *Send the signed contract* done?" or "Move to
Doing?".
**User sees:** one line under the composer per linked card, with the suggested
move. One click, or ignore.

### W6. Several boards, or one board filtered

**Decision taken here:** one board, filtered. A board is a query plus the
column field, as now; efforts add `effort:` to the filter. "Acme renewal" and
"Q4 hiring" are saved boards, each a filter. A card in two efforts appears on
both, because it's the same card.
**User sees:** a board switcher (All, then each effort, then saved filters),
and an effort's page opens its board.

### W7. Outreach that starts from a card

**Trigger:** the user is working a card and needs something from someone else.
**System:** from the card, **Ask someone…** composes a message with the card's
context. Its replies are linked to the card on arrival, and the card shows
"waiting on Bob since Tuesday".
**Model's part:** suggesting who to ask (people linked to the effort, people
who have answered similar things) and noticing when the reply arrives and
answers it, which can move the card.

### W8. A message that should be an event

As in the calendar plan's phase 8: structured bookings exactly, proposed times
from the text. The event, once added, links to the thread and to the thread's
efforts.

### W9. An event that needs work first

**Trigger:** the user creates or accepts an event, or the system notices one,
that implies preparation: a review meeting needs the document read, a
presentation needs slides.
**System:** suggests prep tasks due before the event ("Read the Q3 draft",
due the day before), linked to the event.
**User sees:** the event card lists its linked tasks with their state. **Show
on board** filters the board to that event's tasks. A reminder before the
event mentions anything still open ("2 prep tasks not done").

### W10. Efforts

Described above.

## More workflows worth having

### W11. Waiting on others, and the nudge

You asked; nobody answered. After a wait that depends on the person (their
usual reply time is already measured on the contact card), the conversation
surfaces in **Waiting on others** with a suggested nudge, short and specific,
that quotes what you asked. It is sent only when the user sends it.

### W12. Commitments you made

"I'll get back to you by Friday" in mail you sent is a promise. It becomes a
proposed card due Friday, linked to the thread. When a later message of yours
in that thread keeps the promise (the attachment, the answer), the card is
offered as done.

### W13. Meeting prep brief

Before an event, a one-screen brief:
- who's coming, with their contact cards, recent exchanges and open loops
  either way
- the thread that set it up, summarised
- files exchanged with those people recently
- tasks linked to the event or its effort
- what was agreed last time you met them

It's built from what's already linked, so it needs a model only for the
summaries.

### W14. After the meeting

When an event ends, a quiet prompt: "Anything to follow up from *Design
review*?" It takes notes, which become proposed tasks and, optionally, a
follow-up draft to the attendees: what was agreed and who is doing what.
Events with no attendees don't prompt.

### W15. Deadlines in mail

"Submissions close on the 30th", "renewal due 1 November", "please respond by
EOD". Dates that set a deadline become a due date on the related card, or a
reminder event if there's no card, with the sentence quoted. This is distinct
from W8 (an event at a time) and W12 (your own promise).

### W16. Documents that come back changed

A file arrives that is a new version of one sent before: same name, edited, or
similar content (the file entity and `similar:` already give this). It is
linked to the earlier version and to the card that was waiting for it, with
"*Contract v3* came back from Ada". That can move a card waiting on it.

### W17. The daily and weekly brief

**Morning:**
- today's events
- what needs a reply, oldest first
- tasks due
- what you're waiting on that's now overdue

**Friday:** per effort, what moved this week, what's stuck, and what's
waiting on whom. Both are built from stored state, assembled on request or
when the day starts, never per keystroke.

### W18. Status report for an effort

"Write this week's update for Acme": a draft built from the effort's
timeline (cards moved, decisions in threads, events held, files exchanged),
addressed to the effort's stakeholders. Editable, and sent by the user.

### W19. Delegating

Forwarding a message to someone with "can you handle this?" creates a card
assigned to them, in the original effort, with **Waiting on Bob**. Their reply
that finishes it offers to close it.

### W20. Time and money by effort

- **Time:** meetings linked to an effort are its time. `| sum hours by effort`
  is the timesheet, the time-tracking feature from the calendar plan.
- **Money:** receipts and invoices already extracted (`receipts`, `bills`) that
  belong to an effort are its expenses, with `| sum receipts.amount by effort`.

### W21. Key relationships going quiet

For contacts marked important, or the people most linked to an active effort:
"You haven't been in touch with Ada in 5 weeks; your last exchange was about
the renewal." Rule-based on contact activity. A model is needed only to say
what the last exchange was about.

### W22. Scheduling back and forth

A thread proposing times ("does Tuesday or Wednesday work?") is answered from
the calendar: free/busy checked, and a reply drafted with what works. When the
other side confirms, the event is suggested (W8). This joins triage, the
calendar plan's find-a-time, and event suggestions.

### W23. Requests for a specific thing

"Please send the signed copy", "can you share the deck". The task records
*what* is wanted. When the user later sends that person a message with a
matching attachment, the task is offered as done, with no model needed at
that point beyond matching names and similar files.

### W24. Trips and other bundles

Flight, hotel, car and restaurant confirmations for the same dates and place
are gathered into one effort (a trip), with the events, the confirmation
mails, and the documents (tickets, visas). A trip is an effort that assembles
itself; the user accepts the grouping.

### W25. Following up a sent proposal

Mail you sent with an attachment that matters (a quote, a proposal, a
contract), with no reply after a few days, surfaces in Waiting on others,
specifically: "Proposal to Acme, sent 6 days ago, not answered." It's W11 with
the file as the subject.

### W26. Noise, handled quietly

Conversations triaged as *nothing needed*, and rated *ignorable* by the
existing `importance` label, collect in a low-priority view. Once their pattern
is clear (the same sender, always ignored), the user is offered a rule. The
rule is the user's to make, and nothing is hidden by a guess.

## Linking, in one table

| From → to | How the link is made |
|---|---|
| message → conversation | derived (`References`), as now |
| conversation → card | suggested by triage (W2, W12), or made by hand |
| conversation → event | the invitation itself (derived), or suggested (W8) |
| card → event | suggested prep (W9), or made by hand |
| anything → effort | by hand, by rule (a saved query), inherited from the conversation, or suggested |
| file → file | new version (W16), from content similarity |
| card → person | who is waiting, or assigned (W7, W19) |

All links live in one table, with both ends, the kind of link, its source
(human, derived, rule or suggested), a confidence when suggested, and who
accepted it. One table means "everything linked to X" is one query, the
effort page and event card are views of it, and `linked:` can be a query term.

## The decisions these workflows need

This list is the hand-off to the next discussion: what has to be judged, by
anything, for the workflows to run. It records each decision's shape and
urgency, not which model makes it.

| # | Decision | Input | Output | When | Can it wait? |
|---|---|---|---|---|---|
| D1 | Does this conversation need anything from the user? | newest message, conversation summary, who sent last | none / reply / action / scheduling, can be several | on arrival | minutes |
| D2 | What is the task? | the asking sentence | imperative title, deadline, who's waiting | when D1 says action | minutes |
| D3 | Did the user make a commitment? | the user's sent mail | promise text, due date | on sending | minutes |
| D4 | Is this a deadline, an event, or a booking? | message text and structured data | date, time, zone, place; event or deadline | on arrival | minutes |
| D5 | Which effort does this belong to? | message, people, files, existing efforts | ranked efforts, or none | on arrival | hours |
| D6 | Does this reply close this card? | the draft, the card | done / progress / unrelated | while composing | **seconds**: the user is there |
| D7 | Does this incoming reply answer what we were waiting for? | reply, card or waiting item | answered / partly / no | on arrival | minutes |
| D8 | Write a short update, reply, nudge or status report | card, thread, effort timeline | draft text | on request | **seconds**: the user is there |
| D9 | Summarise for a brief | threads, events, people | short summaries | on request, or precomputed overnight | precomputable |
| D10 | Is this file a new version of that one? | file text and metadata | same document, or not | when a file arrives | minutes; mostly rules |
| D11 | What prep does this event need? | event, linked threads and files | proposed tasks | on accepting an event | minutes |
| D12 | Who should be asked? | card, effort, contacts' history | ranked people | on request | seconds, but rules can answer |

Only two of these have a person waiting on them: D6 and D8. Everything else
can run in the background, and most can be gated cheaply. D1 runs first and
decides whether D2–D5 run at all, as `purchased` gates `receipts` today, so
most mail costs one cheap judgement and nothing more.

## Two decisions for you before building

1. **Sending from the interface.** Today nothing in the interface sends mail;
   every message waits for `iql outbox approve` at a terminal. Several of these
   workflows are about sending: W3, W4, W7, W11, W14, W18, W22.
   - **Keep the rule as it is:** every reply lands in the outbox and is sent
     from a terminal. That's safe, and clumsy for a person working in the
     interface.
   - **Let a person send from the interface** when they wrote or reviewed the
     message and pressed Send. The terminal gate stays for anything an agent
     or automation drafted and nobody has reviewed. The rule becomes "a
     person's act", which is what the gate is protecting.

   I'd recommend the second. The workflows don't work well without it.
2. **Efforts as a first-class kind.** Building the hub (a new kind, links
   across all kinds, `effort:` in the language) is the larger change. The
   alternative, labels plus saved queries, is cheaper but can't hold
   time-tracking codes, stakeholders or an effort page. I'd recommend the
   first.

## Where this leaves things

- **Already present:**
  - conversations
  - `loops` and `importance` labels
  - tickets with sources and history, and a board that is a query
  - the calendar with proposals
  - contacts with responsiveness
  - files as entities, with versions findable by similarity
  - extractors gated by labels
  - the review and calibration loop
- **Missing:**
  - triage outcomes on conversations (D1)
  - the mailbox-wide Needs reply and Waiting on others lists
  - efforts, and the link table
  - card ↔ event links
  - the compose-time checks (D6)
  - drafting (D8)
  - the briefs
  - sending from the interface (a decision, above)

The next discussion, how AI makes the D1–D12 judgements cheaply in the
background, starts from the decision table.
