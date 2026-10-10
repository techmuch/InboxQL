# The Desk

The Desk is where mail is read, and it is built on one idea: **a folder is a
query.** The Inbox is `folder:inbox`. A saved search is a query with a name.
A label is `label:importance`. They all sit in the same rail, and clicking any
of them puts its query in the bar at the top — so the Desk teaches the query
language simply by being used.

Open it from **Tools → Desk**, or **Ctrl+Shift+M**.

## The rail

The column on the left has three kinds of row, kept apart because they behave
differently.

**Folders** are the mailbox itself: Inbox, Starred, Sent, Archive, Drafts,
Spam, Trash and All Mail, with live unread counts. You can hide one you never
use; you cannot delete it, because it *is* the mail. **Reset** brings hidden
folders back.

The folders divide the mailbox between them with nothing counted twice, and
**Inbox is what is left**: everything not sent, archived, deleted or junk. On
Gmail that matters — Gmail's archive is the absence of a label InboxQL does
not record, so archived Gmail mail still appears in the Inbox here.

**Saved queries** are yours. Six arrive with a new mailbox — Tickets,
Proposed, Files, People, Systems and Unclassified — and they are ordinary
saved queries: rename them, change their icons, reorder them, or remove them.
**Arrange** (the button at the top of the rail) shows the controls for moving,
editing and removing; editing a query never moves it.

To save the query in the bar, use **Save** beside it and give it a
name. A saved query is also a term — `saved:clients after:7d` is "the clients
query, this week" — so saved queries are building blocks, not just bookmarks.

**Annotations** lists the labels and extractors that have found something.
It is derived, so it cannot be arranged: it would be out of date the next time
one of them ran.

## The query bar

Type a query and press **Enter**. While you type, the bar suggests fields,
values — real addresses from your mailbox, your labels, your folders — and
says what a term means. **Tab** accepts a suggestion.

A query in the bar can be shown as text or as **pills**, one per term, which
you can remove with a click. Either way it is the same query. The
[query language](query-language.md) guide covers what you can write.

What you get back depends on what you asked:

| You asked for | You see |
|---|---|
| a filter, such as `from:alice` | a list of messages |
| `in:contacts`, `in:attachments`, `in:drafts`, `status:todo` | contacts, files, drafts or tickets |
| `\| count by week`, `\| top from 10` | groups, with a chart |
| `\| series receipts.amount by month` | a time series |
| a bare `\| count` | one number |

The list loads more as you scroll; there is no page size to choose.

## Reading

Move through any list with the **↑** and **↓** arrow keys. The row you are on
stays outlined, and the message opens in the viewer beside the list as you
arrive on it. **Enter** opens it properly; clicking does the same.

Select several with **Shift**-click or **⌘/Ctrl**-click. A bar appears above
the list with what you can do to all of them at once — mark them read, unread
or starred — and **Narrow to these**, which turns the selection into a query.

A message's header shows its labels as chips under the sender. Right-click a
chip (or press the menu key on it) to say whether the label is right; see
[teaching an annotator](labels.md#teaching-it-rulings).

## Conversations

Settings → General → **Inbox Layout** chooses between one row per message,
newest first, and **one row per conversation**: messages grouped into the
conversation they belong to, together with any tickets and drafts it produced.

With conversations on, **Conversations** chooses whether each one starts
**closed** — one line each, which is quickest to scan with the arrow keys — or
**open**, showing every message, ticket and draft beneath it. Open or close any
one by hand and it stays that way while you work. A very long conversation
shows its 25 most recent entries and says how many more there are.

Conversations follow the `References` header that mail clients write, so two
unrelated messages that happen to share a subject are not merged. A client
that leaves that header out can split one conversation into several, which is
the safer mistake.

## Viewer tabs

Messages, contacts and files open in viewer tabs. Settings → General →
**Viewer Tabs** chooses between **one shared tab**, where opening anything
replaces what was there, and **a tab for each kind**, so opening a file leaves
the message you were reading where it was.

**Attachment Previews** decides where a file you click on opens: **in the
message**, below its attachment chips; **in the File tab**, on its own, with
every message that carried it; or **both at once**.

## Keyboard

| Keys | |
|---|---|
| ↑ ↓ | move through a list |
| Enter | open the row |
| Ctrl+Shift+M | the Desk |
| Ctrl+Shift+F | search |
| Ctrl+Shift+C | Contacts |
| Ctrl+Shift+K | the Ticket Board |
| Ctrl+Shift+L | Annotators |
| Ctrl+Shift+D | the Analytics Dashboard |
| Ctrl+Shift+E | the Log |
| Ctrl+, | Settings |
| Ctrl+I | the chat panel |

In the query bar: **Enter** runs the query, **Tab** takes a suggestion, **↑ ↓**
move through suggestions, **Escape** closes them.

## Everything is a tab

The Desk, Contacts, the Board, Settings and the rest are tabs in one window,
and you can split and rearrange them. The layout is remembered per browser.
**Help → Windows** lists every window you have open on this mailbox and what
each is showing — useful when an agent has pointed one of them at something,
because what it did is recorded there.
