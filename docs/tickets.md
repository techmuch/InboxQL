# Tickets

A message is something that happened: it arrived, and it will never change. A
**ticket** is something to do: it starts as an idea, gets worked, and gets
done. InboxQL keeps the two apart on purpose. A ticket points at the mail it
came from, and you own its state; the machine can suggest one, but it never
changes a status, title or date you set.

## Where tickets come from

**By hand**, from a terminal:

```sh
iql ticket new "Send Acme the signed contract" --message <id> --due 2026-10-17
```

With `--message`, it is filed in that message's conversation, so it
appears beside the mail on the Desk when conversations are shown.

**Proposed from your mail.** An extractor that finds actions in mail — one
you have made, say `actions` (see [Labels](labels.md#making-your-own)) — can
propose tickets. It does not create them:

```sh
iql ticket propose actions --dry-run           # what would be proposed
iql ticket propose actions --auto-accept 0.9   # propose; accept the very confident ones
```

Anything below the bar waits with status *proposed*. The rail's **Proposed**
entry is that queue. Accept a proposal to put it on the board; reject it and
it is kept, marked rejected, because a rejection is the evidence that the
extractor was wrong. A task list you cannot trust is worse than none, so
nothing goes on the board without you.

There is one ticket per conversation per extractor. Running it again adds
newer mail to the ticket's evidence and never makes a duplicate. Two tickets
that are really one can be merged — always by you, never guessed.

## The board

**Tools → Ticket Board** (Ctrl+Shift+K). Columns are statuses: *todo*,
*doing*, *done*. Drag a card to move it. The filter above the board decides
which tickets appear; the columns do not change with it, which is what keeps
dragging unambiguous — dropping a card into *doing* sets its status to doing,
and nothing else.

Each card lists the messages behind it. If a ticket cannot be traced to its
mail, something is wrong.

## Asking about tickets

Ticket fields are query terms, so the same language that filters mail filters
tickets:

| Term | Matches |
|---|---|
| `status:` | `proposed` `todo` `doing` `done` `rejected`; `status:*` is every ticket |
| `priority:` | its priority |
| `due:` | due on or before — `due:7d` is the next seven days, looking forward |
| `ticket:` | words in the title |
| `raised:` | `human` or `annotator` |

A mail field inside a ticket query asks about the ticket's **evidence**:

```
status:todo due:7d
status:* from:*@acme.com          tickets whose mail came from Acme
status:* | count by status
status:todo | timeline            each conversation, and what it caused, in order
```

On the command line: `iql ticket list`, `iql ticket show <id>` — which prints
the `iql read` command for each message behind the ticket — `iql ticket move
<id> doing`, and `iql ticket board`.
