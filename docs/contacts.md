# Contacts and open loops

A contact is anyone — or anything — you have exchanged mail with. You never
add one: every message makes a contact of every address it touches. So the
contact list is not an address book you maintain; it is a view of your
mailbox, arranged by who is in it.

Open **Tools → Contacts** (Ctrl+Shift+C), or click a name on any message.

## What a contact card shows

Everything on the card is worked out from your mail when you open it, never
stored as a separate count, so it cannot disagree with the mailbox.

- **Who they are** — name, organisation, and what kind of sender they are.
- **How much** — messages, how many each way, first and last seen.
- **Turnaround** — the median time *you* take to reply to them, and they to
  you.
- **Addressing role** — how often you put them in To rather than Cc, which says
  whether they are someone you write to or someone you keep informed.
- **When their mail arrives** — a 24-hour histogram.
- **Open loops** — below.
- **Files** — what they sent you, or everything on any message they were on.
- **Notes** — your own, private, in Markdown.

## People and systems

A mailbox is mostly not people. Receipts, notifications, newsletters and
alerts come from addresses nobody types at, and a contact list that mixes them
with your colleagues is hard to use. Each contact has a **kind**: *person*,
*organization*, *system* or *unknown*.

Sorting them takes two passes:

```sh
iql contact classify                 # the free pass: headers and addressing
iql contact classify --model laya    # then Laya, for what the first pass left
```

The first pass reads what is already in the mail — mailing-list headers,
`noreply` addresses, bulk-mail markers — and settles most contacts for
nothing. The second asks the Laya model about each sender still unknown,
reading their last few subject lines, and writes a kind only where the answer
is clear. Add `--dry-run` to either to see what would change.

When it is wrong, fix it on the card: choosing a kind there records it as
yours, and your choice outranks every automatic one. The rail's **People**,
**Systems** and **Unclassified** entries are queries over this field —
`in:contacts kind:person` and so on.

## Labels and notes

Give a contact any labels you like — `client`, `vip`, `family` — on the card,
or:

```sh
iql contact label alice@example.com client
iql contact unlabel alice@example.com client
iql contact note alice@example.com "Prefers a call for anything urgent."
```

Then they are query terms: `in:contacts label:client`. (Older versions called
these *tags*; `tag:` still works.) Notes are searchable with `notes:` and
never leave this machine.

## Open loops

The most useful part of a card is two short lists: **Awaiting your reply** and
**Awaiting their reply**. Each is a conversation where somebody is waiting.

The naive way to build them — whoever sent the last message is waiting on the
other — counts every "thanks!" and every newsletter as waiting on you. So when
the [`loops` label](labels.md#somewhere-to-start) has looked at a
conversation, its judgement decides: a conversation `loops` says is closed is
left out, and the card says how many it left out. Where `loops` has not looked
yet, the row is marked, and the card says the list is built on who sent last.

Rule on `loops` where it is wrong — right-click its chip on the message — and
the list follows your ruling.

Across your whole mailbox:

```
in:contacts awaiting:me              people waiting on you
in:contacts awaiting:them            people you are waiting on
in:contacts label:client awaiting:me
```

## Querying contacts

`in:contacts` makes a query about contacts rather than mail.

| Term | Matches |
|---|---|
| `kind:` | `person` `organization` `system` `unknown` |
| `label:` | a label you gave them (`tag:` is the old name) |
| `has:` | `phone` `name` `org` `notes` `label` `awaiting` |
| `notes:` | words in your notes |
| `awaiting:` | `me` or `them` |
| `messages>` `sent>` | how much mail, e.g. `messages>10` |
| `email:` `name:` `org:` `phone:` | what is known about them |

```
in:contacts kind:person messages>20 | sort last desc
in:contacts | top domain 10
in:contacts kind:unknown messages>5
```

From a terminal, `iql contact show <address>` prints the card and
`iql contact responsiveness <address>` the turnaround and open loops.
