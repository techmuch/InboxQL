# InboxQL redesign — vision, objectives and values

*Draft for review, October 31, 2026. Nothing in the mockups is drawn until
this is agreed.*

## Why redesign

The interface grew one capability at a time, and each was given a place to
live: a Desk, a Board, a Calendar, a Dashboard, Intelligence, a Work loop,
Annotators, Windows, Settings. Each is reasonable alone. Together they are
an IDE-shaped workspace of tabs and panes, where the person decides where to
go and what to look at, and every new capability is another destination.

That is backwards from how the mail is actually worked. The way it is used
is: *take one person or one topic, deal with everything about it, return to
the whole, pick the next*. Most of the reading and sorting is now done by an
agent through `iql work`, and the person's job has become **reviewing,
deciding and replying** rather than triaging. The interface still treats the
person as the one doing the triage.

## Vision

> **A mailbox you and your agents work through together, where the person
> decides and the machine does the reading.**

InboxQL holds all of your mail on your own machine as data you can ask
anything of. Agents read it, sort it, extract what is worth knowing and
propose what to do. The interface exists so that a person can see the state
of their correspondence at a glance, settle what needs a human — a reply, an
approval, a correction — one subject at a time, and trust that nothing was
done in their name that they did not see.

## Who it is for

1. **The owner of the mailbox** — technical, keyboard-first, wants to reach
   inbox-clear on what matters without reading what doesn't. Uses it daily,
   in short sessions.
2. **The owner's agents** — Claude Code, Codex and others driving `iql`.
   They are first-class users of the same data, but they act through
   proposals, not through the screen.
3. **The owner on a bad day** — tired, on a laptop, using a screen reader
   or a magnifier, or on a phone. The design is judged by this person, not
   by the best case.

## Objectives

What the redesign must achieve. Each is something a mockup can be checked
against.

1. **One subject at a time.** Any person, conversation, topic, label or
   query can become *the subject*, and everything about it — mail, files,
   cards, events, open loops, what the agent concluded — is on one screen.
   Leaving it returns you exactly to where you were.
2. **What needs me, first.** Opening the app answers "what needs a human
   today?" before "what arrived?": replies owed, proposals to approve, agent
   answers to check, drafts waiting to send. The inbox is one input to that,
   not the home screen.
3. **Agents' work is visible and reviewable.** Everything an agent did or
   proposes — a label, an extracted receipt, a card, a calendar event, a
   draft — is shown with who did it, why, and its evidence (the quote), and
   can be accepted, corrected or undone in one action.
4. **The query is the interface, not a power-user corner.** Every view is a
   query you can see, edit, save and hand to an agent. Clicking builds the
   query; typing it is never required and never hidden.
5. **Fewer places.** The number of top-level destinations goes down, not up.
   New capabilities appear *inside* a subject (a receipt on its message, a
   card on its conversation), not as a new tab.
6. **Accessible by construction.** WCAG 2.2 AA in every theme, complete
   keyboard operation, a sensible screen-reader reading order, 11px minimum
   text, works at 200% zoom and at phone width.
7. **Calm.** Colour means something or is absent. Counts are there when they
   change a decision. Nothing animates to get attention.

## Values

How to decide when objectives conflict. Each comes with what it costs,
because a value that costs nothing doesn't decide anything.

**1. The person decides; the machine proposes.**
Agents can label, extract, draft and propose freely. Sending, accepting a
calendar change, deleting and anything that reaches another person waits for
a human. *Cost:* more approval steps; the design must make approving fast
(batch, keyboard), never remove the step.

**2. Nothing happens silently.**
Every change says what it did, who did it (you, an agent, a rule), and how
to undo it. An empty result says why it is empty. A screen that changes
itself says why it changed. *Cost:* more words on screen; they must be short.

**3. Your mail stays yours.**
Local by default; anything leaving the machine is named before it goes.
No anti-agent gates either: the owner's agents are trusted the way the owner
is, within value 1. *Cost:* features that need a cloud service are opt-in
and visibly so.

**4. Show the evidence.**
A conclusion — "waiting on you", "a receipt for $42", "important" — links to
the words in the mail that support it. Confidence scores are shown as rank
or not at all, never as false precision. *Cost:* every derived value needs
a source to point at.

**5. Legible over clever.**
Plain words over icons alone; one way to do a thing over three; state you
can read over state you must remember. *Cost:* some density is given up.

**6. Keyboard and screen reader are primary, not compatible.**
Designed for them first, then for the mouse. *Cost:* every custom control
needs keyboard and ARIA semantics designed alongside its look, not after.

**7. Honest about what exists.**
Mockups may show what is not built, but are labelled so; the shipped UI
never shows a control that does nothing.

## Re-examining the approach

Questions the mockups will explore rather than assume:

- **Workspace or flow?** Replace the tab-and-pane workspace (flexlayout)
  with a fixed three-part layout — *what needs me* / *the subject* /
  *the evidence*. Free-form tabs are removed; there is no expert mode.
- **Home screen.** Replace the inbox as home with a *Today* view built from
  queries: owed replies, approvals, agent checks, upcoming events.
- **Subject pages.** A person, topic or conversation as a page you land on,
  with mail, files, cards and events in one stream, rather than four tabs
  filtered to the same thing.
- **Agent activity as a first-class stream** — a review queue and a log of
  what agents did, rather than scattered "proposed by agent" chips.
- **Command palette as the main navigation**, with the rail reduced to saved
  subjects.
- **Phone width** as a real layout, not a squeezed desktop.

## Non-goals

- Becoming Gmail. Familiar where it helps; not a clone.
- An agent that sends, accepts or deletes without a person.
- Charts for their own sake. The dashboard earns space only where it changes
  a decision.
- Building any of this before the mockups are reviewed.

## How the mockups will be delivered

Static HTML in `docs/mockup/redesign/`, no build step, openable from disk:

- `index.html` — the vision summary and a map of every screen and workflow.
- One file per screen, with real-shaped (made-up) data, in light and dark.
- Each workflow documented step by step beside the screens it uses: the
  goal, who starts it, each step and its keyboard path, what the screen
  reader hears, and what is undoable.
- Mockups only — nothing wired to the running server.

## Decided in review (2026-11-01)

- The inbox keeps its own place in the navigation.
- Topics (such as "Kitchen remodel") get their own page from the start.
- No free-form tabs and no expert mode.
- "Ask in words" is in scope, answered by an agent CLI the person already uses
  (Claude Code, agy). Its place in the UI is being designed.
- The phone is a real target: an app installed from Chrome, reached over the
  person's Tailscale network. Its details are being designed.
- Important dates notify the person in the days leading up to them.
- Birthdays and anniversaries come from contacts.
