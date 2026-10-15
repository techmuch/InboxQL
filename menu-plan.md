# The main menu, reorganised

*14 October 2026. Released as v0.0.87.*

## Where it starts

- **"Tools" mixes places with occasional tools.** Desk, Calendar, Board and
  Contacts sit beside Annotators, AI Agents and the Dashboard.
- **"Help" holds Settings, Windows, Log and Sign Out.**
- **"View" has one item.**
- **Nothing can be created from the menu.**
- **Some places are unreachable.** Files is reachable only through the Desk
  rail; Search is a command in no menu.
- **Names disagree:** Ticket Board, Tickets, cards. "Annotators" is internal
  vocabulary.
- **AI Agents looks like a working feature** but cannot run anything.
- **Shortcuts are uneven and clash with the browser.** Board has none, and
  Ctrl+Shift+C, A, D and E are browser shortcuts on Windows and Linux.
- **Found while planning:** the Desk's Compose button has no handler, the Board
  cannot create a card, and drafts have no API.
- **What the shell already offers:** submenus, a CommandPalette component, and
  a UserProfile dropdown that takes actions.

## Phase 1 — the menus

| Menu | Contents |
|---|---|
| File | New message · New event · New task — Import mail… · Import calendar (.ics)… · Export calendar… — Settings… |
| Go | Desk · Calendar · Board · Contacts · Files — Dashboard — Find anything… |
| View | Chat · Conversations ▸ · Viewer tabs ▸ · Theme ▸ — Reset layout — Windows · Log |
| Intelligence | Waiting for approval · Review queue · Labels & extractors · AI assistants · AI Agents (Preview) |
| Help | Documentation · Keyboard shortcuts · For agents — Check for updates · About InboxQL |
| Your name | Profile · Settings · Sign out |

Renames: Ticket Board → **Board**; Annotators → **Labels & extractors**; AI
Agents → **AI Agents (Preview)**.

## Phase 2 — making "New …" real
- **New message:** a compose dialog (account, To, Cc, Subject, body) that saves
  a draft through the new `POST /api/drafts`. Sending stays in the outbox until
  sending from the interface is built.
- **New task:** a dialog creating a card through `POST /api/tickets`.
- **New event:** opens the calendar's editor.
- **Import calendar (.ics):** a file picker posting to the existing import
  endpoint.
- **Export calendar:** downloads your own calendar as `.ics`.
- The Desk's Compose button opens the same dialog.

## Phase 3 — Find anything (Ctrl+K / ⌘K)
The shell's CommandPalette, offering every command, every place and every
saved query, matched as you type.

## Phase 4 — keyboard
- **Two-key sequences:** g d Desk, g c Calendar, g b Board, g p People, g f
  Files, g a Waiting for approval.
- **Single keys:** c new message; ? the keyboard page.
- **Typing is never taken over:** keys pressed in a field are left alone.
- **Legacy shortcuts:** the Ctrl+Shift combinations keep working but are no
  longer shown.
- **Help → Keyboard shortcuts:** a page listing all of it.

## Phase 5 — Waiting for approval
One view of everything proposed and waiting for a person: proposed cards,
calendar proposals, and drafts queued in the outbox (with the approve command).

## Phase 6 — docs, tests, release
Update the guides that name menu paths. Tests for the key sequences, the
palette and the new endpoint. Bump the patch version to 0.0.87, commit, tag,
and push.

## What shipped in 0.0.87

All six phases, with these departures from the plan:

- **No dividers in the menus.** The shell's menu bar (nexus-shell 0.2.11) has
  no separator; the old Help menu was drawing `---` as an item. Groups are
  kept by order instead. A separator belongs in the shell.
- **No "Review queue" item.** Review is per label, inside Labels & extractors,
  so a menu entry would have opened the same tab under a second name.
- **Theme ▸ uses the shell's own theme commands**, so it lists every bundled
  theme (Texas A&M included) rather than a copy that would drift.
- **The user menu's dropdown is right-aligned by a stylesheet rule.** The shell
  left-aligns it whenever the name is shown and ignores `className`; the fix
  belongs upstream.
- **AI assistants** and **Check for updates** open Settings → System and scroll
  to that section.

Tests: `keySequences.test.ts` (4), `AppDialogs.test.tsx` (compose queues a
draft and shows the approve command; a new task lands in To do; Find anything
lists commands and saved queries), `TestDraftFromTheInterface` in Go. Checked
in a browser on a scratch mailbox: every menu and submenu, `?`, `g a`, `t`,
Ctrl+K, File → New event, Compose, the user menu, and Waiting for approval
with a proposed card.
