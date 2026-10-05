# Conversations open or closed by default

*A phased plan. 4 October 2026.*

## What the mailbox says about the question

Measured on `../uea-test`:

```
25,006 conversations
19,400 of them one message          78%
     1 of them 1,088 messages
  mean 1.74
```

"Expand everything" fails at both ends. Most conversations are a single message
whose sender and subject are already in the collapsed header, so expanding them
adds a row of nothing. And one conversation would render 1,088 rows from one
chevron — a stall that reads as a bug.

Expansion costs no fetch: entries already ship with the list. The cost is DOM.

## Phase 1 — the preference

`lib/threadExpansion.ts`: `collapsed | expanded`, in localStorage beside the
threading and viewer preferences, defaulting to `collapsed` — today's behaviour.

## Phase 2 — the row obeys it, and keeps your hand

`ThreadRow` did `useState(defaultOpen)`, which reads its argument once. Rows are
keyed by thread and survive across queries, so a changed preference would have
changed nothing on screen.

Instead: the preference is the source, and a click records an override on top.
Flip the setting and every row you have not touched follows; the ones you opened
or closed by hand stay as you left them.

A result with **one** conversation still always opens. That was never a
preference — it is the thing you asked to read.

## Phase 3 — the cap

An open conversation renders its first 25 entries and says how many more there
are, with a button for the rest. Applied to every open thread, not only ones
opened by default: clicking the 1,088-message chevron was already a stall.

## Phase 4 — the setting

Settings → General, directly under Inbox Layout. It only means something when
the inbox is one row per conversation, and says so rather than sitting there
inert.

## Not doing

**Expanding only "small" conversations.** The setting asked for is a default,
not a heuristic; the cap is what makes the plain version safe.

---

## What was built

All four phases, verified in a browser against a copy of `../uea-test`.

- `lib/threadExpansion.ts` — the preference, `ENTRY_CAP = 25`, and `isOpen`,
  which states the precedence once: what you did to the row, then "alone on
  screen", then the preference.
- `ThreadRow` keeps an override (`boolean | null`) instead of
  `useState(defaultOpen)`. Tested: flipping the setting opens every untouched
  row, and a row closed by hand stays closed through two flips.
- The cap, with the remainder named. On the real 1,088-message conversation:
  25 rendered, **"and 1,063 more — show all"**; show all rendered 1,088 rows.
- Settings → General → **Conversations**, under Inbox Layout, saying when it does
  not apply.

### A correction to the plan's premise

I called rendering the 1,088-message thread "a stall". Measured, *show all*
took **151 ms** — noticeable, not a freeze. The cap is still worth having, for a
different reason than the one I gave: with "Open" as the default, a page of 50
conversations is already 50+ rows of repeated headers before any long one, and
the arrow keys walk all of it. The cap keeps a long conversation from turning
the list into that conversation.

The cost of "Open" on singletons was visible as predicted: each one-message
conversation repeats its own sender and subject as its only entry.

178 frontend tests, real typecheck (`-p tsconfig.app.json`) clean, Go suite
green, no console errors.
