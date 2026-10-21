# Your board

A message is something that happened: it arrived, and it will never change. A
**card** — a ticket — is something to do: it starts as an idea, gets worked,
and gets done. The board is where your to-dos and personal projects live, with
what Trello has for one person, and one thing it has not: a card can point at
the mail it came from. You own every card; the machine can suggest one, but it
never changes a list, title or date you set.

## Where cards come from

**By hand.** "Add a card" at the bottom of any list, **File → New task**
(**t**), or from a terminal:

```sh
iql ticket new "Paint the fence" --label home --due 5d
iql ticket new "Send Acme the signed contract" --message <id> --due 2026-10-17
```

With `--message`, the card is filed in that message's conversation, so it
appears beside the mail on the Desk when conversations are shown, and it
starts with that conversation's topics as labels.

**Proposed from your mail.** When your AI harness reads your mail
([Reading your mail](reading.md)), the tasks it finds arrive as proposals.
An extractor of your own can propose them too (see
[Labels](labels.md#making-your-own)):

```sh
iql ticket propose actions --dry-run           # what would be proposed
iql ticket propose actions --auto-accept 0.9   # propose; accept the very confident ones
```

Anything below the bar waits in **Proposed**. Accept one to move it to your
first open list; reject it and it is kept, marked rejected, because a
rejection is the evidence that the extractor was wrong. There is one card per
conversation per extractor: running it again adds newer mail to the card and
never makes a duplicate. Two cards that are really one can be merged — always
by you, never guessed.

**Made by a rule** when mail arrives — see [Automation](#automation).

## The board

**Go → Board** (**g b**). Lists run left to right; you start with
*Proposed*, *To do*, *Doing* and *Done*.

- **Drag a card** to any place in any list. Where you drop it is where it
  stays: order within a list is yours, and a new card goes to the bottom.
- **Open a card** by clicking it, or with **Enter**.
- **Add a card** at the bottom of a list; it stays open, so a run of cards is
  a run of Enters.
- **Narrow the board** with the box above it — `label:home`, `due:14d`,
  `from:*@acme.com`, or plain words. The lists stay put while you narrow it,
  which keeps dragging unambiguous: dropping a card in a list puts it there,
  and nothing else.

With a card focused (Tab to it):

| Key | Does |
|---|---|
| ↑ ↓ ← → | move between cards and lists |
| Shift + ↑ ↓ | move the card up or down its list |
| Shift + ← → | move the card to the next list |
| Enter | open it |
| c | archive it |

### Boards

The name at the top left is the **board**. A board is a named filter over the
same lists: "Home" might be `label:home`, "This week" `due:7d`. **New board**
in that menu makes one; the pencil beside a board edits or deletes it. A card
added on a board is given the labels its filter names, so it does not vanish
from the board you added it on. **All cards** is every card.

### Your own lists

**Add another list** at the right of the board. A list's **⋯** menu renames
it (or click its title), moves it left or right, archives it, or deletes it.
Deleting a list with cards asks where they go; a card is never deleted as a
side effect.

A list is **open** (work to do) or **finished** — Done is finished. Moving a
card into a finished list marks it complete: its due date turns green and
`state:closed` finds it. Make any list finished from its menu: *Cards here are
finished*. There is always at least one open list and one finished list, and
exactly one Proposed list, where proposals land.

A list's name can change freely; what `status:` matches is its id, which
does not. A list made as "Waiting on vendor" is `status:waiting-on-vendor`
for good, whatever you rename it to.

## A card

Click a card to open it.

- **Title** — click to edit.
- **List** — move it from the menu under the title.
- **Labels** — the **+** picks from the labels in use and the topics on your
  mail, or makes a new one. The palette beside each sets its colour.
- **Start and due dates** — a due date shows on the card: yellow when it is
  within a day, red when it has passed, green once the card is in a finished
  list.
- **Priority** — high, medium or low.
- **Description** — in Markdown: `**bold**`, `- lists`, and links, which
  open in your browser. ⌘↵ saves.
- **Checklists** — as many as you like, each with its own progress bar. The
  card's front shows how many items are done.
- **From this mail** — the messages behind the card. If a card cannot be
  traced to its mail, something is wrong.
- **Activity** — your comments, and the card's history: when it moved, what
  was added and ticked, and who did it — you, the AI that proposed it, or a
  rule.
- **Archive** takes it off the board; **Delete** removes it. Its mail is
  untouched either way.

### Labels are topics

A card's labels and the topics on your mail are one vocabulary. A card raised
from mail starts with its conversation's topics; after that they are the
card's own. In a query about cards, `label:` and `topic:` both mean the card's
labels — `in:tickets label:home` — while `topic:home` on its own is still
about mail.

### Archive

Archived cards and lists are off the board, not gone. **Archived** above the
board lists them, to open or send back. Queries about cards leave archived
ones out unless they say `archived:` — the way Trash is left out of mail.

## On the calendar

Cards with a due date appear on the [Calendar](calendar.md) as all-day
entries under **Cards due**, which you can hide like any calendar. Click one
to open the card; drag it to another day to change its due date.

## Automation

**Automation** above the board holds rules. A rule is a trigger and some
actions:

- **When a card enters a list** — tick every checklist item, add or remove a
  label, set or clear the due date, set the priority, or archive it. "When a
  card enters Done, tick everything and archive it."
- **When mail arrives matching a query** — make a card in a list, one per
  conversation, with labels, a due date or a priority. "Mail from my landlord
  makes a card in To do, labelled home, due in 3 days." A rule acts on mail
  that arrives after it was made; ▶ runs it over older mail too. Mail rules
  run after every sync.

Nothing a rule does moves a card between lists, so one rule can never set off
another. What a rule did is in the card's activity, credited to the rule.

## Asking about cards

Card fields are query terms, so the same language that filters mail filters
cards:

| Term | Matches |
|---|---|
| `status:` / `list:` | the list a card is in, by id: `todo`, `doing`, one of yours; `status:*` is every card |
| `state:` | `open`, `closed` (in a finished list), `proposed`, `rejected` |
| `label:` / `topic:` | a label on the card — a whole word; `label:home*` for a prefix |
| `due:` | due on or before — `due:7d` is the next seven days, looking forward |
| `start:` | starting on or before |
| `priority:` | its priority |
| `ticket:` | words in the title |
| `comment:` | words in its comments |
| `checklist:` | words in its checklist items |
| `archived:` | `yes`, `no`, or `*` for both |
| `raised:` | `human`, `annotator`, `agent` or `rule` |
| a bare word | the card's title, description, comments and checklist, or its mail |

A mail field inside a card query asks about the card's **evidence**:

```
state:open due:7d
in:tickets label:home -state:closed
status:* from:*@acme.com          cards whose mail came from Acme
status:* | count by status
status:todo | timeline            each conversation, and what it caused, in order
```

## From a terminal

Everything the board does, `iql ticket` does, so an AI harness can run the
board too:

```sh
iql ticket board [--board home]                 the lists and their cards
iql ticket show <id>                            everything on a card
iql ticket edit <id> --due 2026-11-02 --body -  change it (the description from stdin)
iql ticket move <id> doing [--after <id> | --top]
iql ticket label <id> home,garden               unlabel takes them off
iql ticket checklist <id> add "Buy primer"      done, undo, edit, remove…
iql ticket comment <id> "Rang, no answer"
iql ticket archive <id>                         restore puts it back
iql ticket lists add "Waiting on vendor" --after todo
iql ticket boards add Home --filter "label:home"
iql ticket rules add --when enters:done --do check-all --do archive
iql ticket rules add --when mail --query "from:landlord@example.com" --list todo --do add-label=home
```

An id can be the first eight characters `iql ticket list` prints.
`iql ticket show` prints the `iql read` command for each message behind the
card.
