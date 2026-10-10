# AGENTS.md — driving InboxQL from an LLM agent

InboxQL's CLI is designed to be used by an autonomous agent as well as by a person.
This file is the contract. Everything here is stable; if something is not
described here, do not depend on it.

```
iql [--data <dir>] [--json] <command> [flags]
iql <command> [flags] [--data <dir>] [--json]
```

Both forms are the same invocation: `--data`, `--json`, `--verbose` and
`--no-color` are global and may appear anywhere, before or after the command.

Always pass `--json`. Every command listed under **Agent tools** emits a JSON
object or array on stdout and nothing else; diagnostics go to stderr.

Routine database logging is suppressed unless you pass `--verbose`. Schema
migrations and warnings are still written to stderr, because those change the
user's database or need attention.

---

## The one rule that matters

**You can compose email. You cannot send it.**

`iql send` does not send. It moves a draft into an outbox and stops. Delivery
happens only when a person runs `iql outbox approve <id>` from an interactive
terminal, reads the rendered message, and types `yes`.

That gate is enforced by checking whether stdin is a TTY. When you invoke
`approve`, it will refuse and exit **4**. There is no `--force`, no `--yes`, and
no environment variable that bypasses it. This is deliberate — do not look for
a way around it, and do not tell the user one exists.

The correct pattern when a reply is wanted:

1. Research with `search`, `read`, `analyze`.
2. Compose with `iql draft create --origin agent`.
3. Queue with `iql send <draft-id>`.
4. **Tell the user the draft is queued and that they must approve it**, giving
   them the exact command: `iql outbox approve <draft-id>`.

Always pass `--origin agent` when you compose. It is recorded on the draft and
shown to the approver, who should read agent-written mail more carefully than
their own.

---

## Exit codes

| Code | Meaning | What to do |
|---|---|---|
| 0 | Success | Continue. |
| 1 | Something failed | Read stderr. Usually retryable only after a fix. |
| 2 | Bad arguments | Your invocation was wrong. The command did not run. |
| 3 | Not found | The account, message or draft id does not exist. Do not retry. |
| 4 | Needs human approval | Expected from `outbox approve`. Stop and ask the user. |
| 5 | Not configured | A prerequisite is missing. stderr names it. |

Exit 5 has two common causes worth distinguishing: no data directory (the user
must run `iql init`), and no LLM provider (fine — see below).

`iql doctor` follows the same split: **5** means the data directory was never
initialised, so the answer is `iql init`; **1** means it exists and one or more
checks failed, so the answer is to read the findings.

---

## Agent tools

### `query` — the query language

```
iql --json query "from:stripe after:2026-01-01 has:attachment"
iql --json query "is:unread -from:*@acme.com | count by week"
```

This is the general tool; `search` below is the older flag-based form, kept
working for existing callers. Prefer `query`: it composes, it negates, and it
aggregates, so one invocation answers questions that would otherwise be
several.

A query is a **filter**, optionally followed by **pipeline stages** after `|`.

| Term | Matches |
|---|---|
| `from: to: cc: bcc: anyone:` | addresses, substring by default |
| `subject: body:` | words in the text |
| `account: folder: mailbox:` | where the message lives — `folder:` is `inbox starred sent archive drafts spam trash all` |
| `is:` | `unread read starred deleted draft answered junk reply` |
| `has:` | `attachment file label reply` |
| `attached: carries:` | mail that carried *this* file, by content hash or name |
| `after: before: on:` | `2026-08-15`, `2026-08`, `2026`, `today`, `7d` |
| `larger: smaller:` | `5mb`, `500kb`, a byte count |
| `label: unlabeled: conf>` | annotator results — see below |
| `extract:name` `extract:name.field>10` | extracted structured data |
| `thread:<message-id>` | every message in that conversation |
| `saved:<name>` | everything a saved query matches |
| `in:<kind>` | what the query is about: `mail`, `drafts`, `tickets`, `contacts`, `attachments`, `logs` |
| a bare word | full-text search |

**A query is about one kind of thing.** Mail is the default. `in:` says
otherwise, and a field only one kind has says it for you — `due:` means
tickets, `origin:` means drafts. Where a name belongs to several kinds it names
none of them: **`status:` means tickets** unless the query says `in:drafts`.
`in:` cannot be negated; it selects the source rather than filtering it.

A field that exists for another kind says so rather than reporting itself
unknown, and a field the kind cannot answer says why — a draft has no flags,
attachments or thread, because it has never been mail.

**The folders partition the mailbox**, so the six counts add up to it exactly
and no message is in two. `inbox` is defined as the remainder — not sent, not
archived, not deleted, not junk — which is worth knowing before reasoning from
it.

`folder:archive` is matched on the mailbox name's trailing segment: the
RFC's special-use `Archive`, Apple Mail's `Archive.mbox`. **Gmail's "All Mail"
is deliberately not archive** — it contains the inbox too, and Gmail archiving
is the absence of the Inbox label, which InboxQL does not record. On a Gmail
account, archived mail is therefore still in `folder:inbox`; say so rather than
reporting an empty archive as "nothing is archived".

Where a message physically sat is `mailbox:`, which is the raw folder name and
not one of these seven.

**Shorthands** worth knowing, because they save several terms each:

```
from:(alice OR bob)   a group scoped to one field
from:me()             the configured accounts' own addresses
has:cc                somebody was copied; -has:cc means nobody was
```

`me()` is the one to reach for rather than asking the user their address — it
resolves from the accounts they already configured.

**Match modes.** These are different questions and the language keeps them
apart:

```
from:acme          contains "acme" — also matches notacme@x.com
from:=a@acme.com   exactly that address
from:*@acme.com    glob; * matches any run of characters
```

Prose fields (`subject:`, `body:`, bare words) are **token** matches through
the full-text index, so `subject:invoice` finds the word, not a fragment inside
another word. Use `subject:*invoice*` for a substring.

**Negation** is `-` or `NOT`, and it composes over anything:

```
-from:alice
-(from:alice after:2026-01)
from:*@acme.com -to:bob@acme.com
```

`-to:x` means *no recipient is x*, not *some recipient is not x*. This is worth
knowing because the wrong reading is the plausible one and it would match
almost every message with more than one recipient.

**Pipeline stages.** A query with a stage returns groups rather than messages;
the response's `kind` field says which.

```
| count [by <field>]                totals, or totals per group
| top <field> [n]                   the n largest groups
| sort <field> [asc|desc]           reorder messages
| limit <n>                         cap the rows
| sample <n>                        a random draw, not the newest n
| thread                            expand to whole conversations
| participants                      who appears, and how often
| series <ann>.<field> by <bucket>  an extracted value over time
| sum|avg|min|max <ann>.<field> [by <bucket>]
```

Group and bucket names: `from domain to cc account mailbox label thread
subject`, and `hour day week month year` — plus **any extracted value**, as
`<annotator>.<field>`.

**An extracted value is named the same way everywhere**, which is the one thing
worth remembering here:

```
extract:receipts.amount>100        filter on it
| sum receipts.amount by month     add it up
| series receipts.amount by week   over time
| count by receipts.merchant       how many messages per value
| top receipts.merchant 10         the commonest
```

A field whose name contains a space is quoted: `| count by "receipts.order
number"`.

`| count by` counts **messages**, like every other grouping key — a message
naming two merchants adds one to each. `| sum` and `| series` add up
**records**, because a weekly digest with seven bars is seven of them and
collapsing those to the message is what would destroy the series.

`| extract <annotator>` still parses and still supplies a default extractor to
a later aggregate, so old saved queries keep working. **Do not write new ones
with it.** On its own it filters nothing — `| extract receipts` returns the
whole mailbox — and its position does not matter, so it reads as a step that
happens and is not one. Use `extract:receipts` to filter and the path form to
aggregate.

Responses are `{query, kind, count, ...}` where `kind` is `"messages"`,
`"groups"` or `"count"`. Only one aggregate stage per query.

`--explain` returns the compiled SQL instead of running it. `--count` returns
just the number. A malformed query exits **2** with the position of the
problem, so fix the expression rather than retrying it. An unknown field
suggests the nearest real one, so read the error before guessing again.

`--complete <pos>` reports what may be typed at a cursor position, which is
also how the web editor offers completions. It answers on incomplete text and
never fails, so it is safe to call while assembling a query:

```
iql --json query "from:al" --complete 7
→ {"context":"value","field":"from","prefix":"al",
   "candidates":[{"value":"alice@acme.com","detail":"142 messages"}, …]}
```

### `saved` — name and reuse a query

```
iql --json saved list
iql --json saved show <name>
iql saved save "Acme invoices" --query "from:*@acme.com subject:invoice"
iql saved delete <name>
```

A saved query is a **building block**, not a bookmark: `saved:<name>` is a term,
so `saved:acme-invoices after:7d` composes. The name is a slug of the title.

### It is also the rail

The rail's entries **are** saved queries. Six of them used to be hardcoded in a
component — Tickets, Proposed, Files, People, Systems, Unclassified — and are
seeded rows now, so they can be renamed, reordered and removed like anything
else.

```
iql saved move <name> up|down|top|bottom|<n>    where it sits
iql saved icon <name> [icon]                    which icon it draws; no icon lists them
iql saved defaults [--install] [--only a,b]     entries to add, and what each matches here
iql saved folders [hide|show <folder>|reset]    the mailbox rows
```

**Position, not alphabetical.** Ordering used to be `pinned DESC, name ASC`,
so the only way to move something was to rename it. `pinned` still works and no
longer decides the order.

**Editing does not move.** `saved save` leaves position alone; only `move`
changes it.

**`defaults` reports what each entry matches in this mailbox.** Zero is the
useful answer — an entry that finds nothing is a row that teaches somebody the
feature does not work — so check it before suggesting one.

**Folders are hidden, never deleted.** Inbox and its siblings carry live unread
counts and are the mailbox itself; `saved folders reset` is the way back.

**The Annotations section is derived and not arrangeable.** It lists the
annotators that found something, and a hand-ordering would be stale the next
time one runs.

Two rules: the query is compiled before it is stored, so an invalid one is
refused where it is written; and saved queries **do not nest** — one cannot
reference another. Deleting a saved query makes references to it fail rather
than silently match nothing.

### `sql` — the escape hatch

```
iql --json sql "SELECT from_addr, COUNT(*) FROM messages GROUP BY 1 ORDER BY 2 DESC LIMIT 10"
iql --json sql --schema
```

Read-only: the connection is opened `mode=ro`, so nothing here can write
whatever the statement says. Use it when `query` cannot express something.
`--schema` lists the tables. `messages.date` is epoch **milliseconds**.

### `search` — find messages (older form)

```
iql --json search --query "invoice" --since 2026-08-01 --limit 20
```

| Flag | Meaning |
|---|---|
| `--query <text>` | substring match on subject, body and sender |
| `--account <id>` | restrict to one account |
| `--from <address>` | sender contains this |
| `--since` / `--until` | `YYYY-MM-DD`, inclusive |
| `--unread` | only messages without `\Seen` |
| `--limit` / `--offset` | paging; default limit 25 |
| `--full` | include full bodies (omitted by default) |

Returns `{query, count, results[]}`. Each result has `id`, `accountId`, `from`,
`to`, `subject`, `date`, `unread`, `snippet`.

**This is substring matching, not relevance ranking**, and it stays that way
for compatibility. `iql query` uses the FTS5 index instead and is the better
tool for text.

Neither is semantic: a search for "invoice" will not find "billing". Issue
several queries with different wordings rather than assuming one returned
everything.

### `read` — get one message or a whole thread

```
iql --json read <message-id>
iql --json read <message-id> --thread
```

Without `--thread`, returns a single message object with its full `body`. With
`--thread`, returns `{threadOf, count, messages[]}` oldest first.

Threading follows the **`References` header**, so an unrelated message that
merely shares a subject line is no longer pulled into a thread.

One limit remains: a client that sends `In-Reply-To` without `References`
starts a new conversation key at each reply, so a thread from such a client can
split into pairs. Threads over-split rather than over-merge, which is the safer
direction — but do not assume a short thread is the whole exchange.

### `analyze` — summarise, or get context to reason over

```
iql --json analyze <message-id> --prompt "what is still outstanding?"
```

Check the **`mode`** field in the response:

- **`"mode": "llm"`** — a provider is configured. The `answer` field holds
  generated prose. The thread is still included.
- **`"mode": "context"`** — no provider is configured. There is no `answer`.
  You get `subject`, `participants[]` with per-sender counts, `span`
  (first/last/days), and `messages[]` with full bodies. **This is not an
  error.** Analyse the payload yourself and answer the user directly.

`--max-messages <n>` caps the thread (default 20); when it trims, it keeps the
most recent messages and sets `truncated: true`.

### `draft` — compose

```
iql --json draft create --reply-to <message-id> --origin agent --body -
iql --json draft create --to a@x.com --subject "..." --body "..." --origin agent
iql --json draft list [--status draft|queued|sent|failed]
iql --json draft show <draft-id>
iql --json draft delete <draft-id>
```

`--reply-to <message-id>` is the usual path: it fills in the recipient, the
`Re:` subject and the threading headers from the stored message, and picks the
account the original arrived on.

Body input: `--body "text"`, `--body-file <path>`, or `--body -` to read stdin.
Prefer stdin for anything multi-line — it avoids shell quoting entirely.

`--bullets "..."` expands notes into prose, but **only with an LLM provider
configured**; without one it exits 5 rather than storing your bullets as the
message. If you get exit 5, write the prose yourself and pass `--body`.

Creating a draft transmits nothing.

### `send` — queue for approval

```
iql --json send <draft-id>
```

Validates the draft and the account's SMTP settings, then sets status to
`queued`. The response includes a `note` restating that nothing was sent.

Exit 2 means the draft is malformed (no recipients, empty subject) or already
sent. Exit 5 means the account has no SMTP host configured.

### `outbox` — review

```
iql --json outbox list
iql --json outbox show <draft-id>
```

`show` returns the exact bytes that would go on the wire, under `rendered`.
Use it to check your own work before telling the user it is ready.

`outbox approve` will exit 4 for you. `outbox reject <id> --reason "..."`
returns a draft to `draft` status and is safe for you to call.

---

## `import` — bring in mail from a desktop client

```
iql --json import sources
iql --json import mailboxes --source apple-mail
iql --json import scan --mailbox <id> --deep
iql --json import run --mailbox <id> --account <id> --limit 100 --dry-run
iql --json import eml <folder> --account <id>
```

Sources are **read-only**. Nothing is written, moved or deleted in the user's mail
client, and you should say so if they ask.

`sources` reports each client as ready, blocked or absent. **Blocked is the common
one on macOS**: `~/Library/Mail` needs Full Disk Access, and the `remedy` field
carries the exact instructions including which binary needs the grant. Relay that
verbatim rather than paraphrasing — the usual mistake is granting access to the
wrong program.

`scan` is fast by default and returns only counts, sizes and the mailbox tree. Pass
`--deep` for attachments, contacts and the date range; that parses every message and
takes minutes on a large mailbox, so tell the user before starting one. The `depth`
field in the response says which you got — a `0` after a fast scan means *not
measured*, not *none*.

`run` reports `scanned / imported / duplicates / skipped / failed`, and those buckets
sum to `scanned`. Duplicates are messages already in that account, matched on content
hash; they are expected on a re-run and are not an error. `partial` counts messages
the client never finished downloading, which are skipped rather than stored empty.

**Always offer `--dry-run` first** for anything but a small `--limit`. It parses
everything and reports exactly what would happen without writing a row.

`--limit` spans the whole run, not each mailbox: 100 across three folders is 100.

Imported mail belongs to `--account` and cascades on account deletion, so an archive
belongs in its own account rather than a live IMAP one. If the user has no suitable
account, say so rather than picking one of their real mailboxes.

`import eml` needs no special permission and is the fallback whenever `sources`
reports blocked: the user drags messages out of Mail.app into a folder, and you
import that.

---

## `in:drafts` — drafts are their own kind

A draft is outgoing and unsent, and deliberately not a row in `messages` so it
cannot be deduplicated against real mail. It is queried like anything else:

```
iql --json query "in:drafts status:queued"
iql --json query "in:drafts origin:agent after:7d"
iql --json draft list --query "origin:agent"
```

| Term | Matches |
|---|---|
| `status:` | `draft queued sent failed` — needs `in:drafts` |
| `origin:` | `human` or `agent`; unique to drafts, so it implies `in:drafts` |
| `to: cc: bcc: subject: body: account: after: before: on:` | as for mail |

`folder:drafts` still works and means `in:drafts`.

Results come back under `drafts` with `kind: "drafts"`, not under `messages` —
a draft has no id in the message store, so do not pass one to `read`.

---

## `contact` / `in:contacts` — people, systems, notes, labels & responsiveness

A contact is an address you have corresponded with. Every message creates one
for every address it touches. Counts (messages, sent, received, first and last
seen) are derived on every read from participant edges, never cached.

```
iql --json contact list [--query "in:contacts label:client"]
iql --json contact show <address>
iql --json contact label <address> <label>
iql --json contact unlabel <address> <label>
iql --json contact note <address> [text]
iql --json contact responsiveness <address>
iql --json contact classify [--model laya] [--limit n] [--dry-run]
iql --json query "in:contacts label:vip awaiting:me"
iql --json query "in:contacts has:notes"
```

| Term | Matches |
|---|---|
| `kind:` | `person organization system unknown` |
| `label:` | a label on the contact — needs `in:contacts` |
| `tag:` | the same, under its old name, and it implies `in:contacts` on its own |
| `has:` | `phone name org notes label awaiting` |
| `notes:` | prose search in private contact notes |
| `awaiting:` | `me` (contact sent last message) or `them` (user sent last message) |
| `messages:` `sent:` | interaction thresholds, e.g. `messages>10` |

**Contact tags are labels now.** Same word as on mail, stored with who set it.
`tag`, `untag`, `tag:` and `/api/contacts/tags` still work. One thing to get
right: **`label:` without `in:` is a message label**, as it always was — a name
two kinds claim names neither, so the query stays about mail. Write
`in:contacts label:vip`, or the old `tag:vip`.

### People and systems

`contact classify` sets `kind` from evidence, cheapest first: the free header
pass (`List-Unsubscribe`, `Auto-Submitted`, `noreply@` and so on), then with
`--model laya` the decision model for senders the headers left unknown, reading
each one's address and last three subject lines.

- **Only senders are judged.** A contact that never sent anything has no text of
  its own; on a real mailbox that was 7,410 of 10,591. They stay `unknown`.
- **The model writes a kind only when decisive**, and leaves the rest unknown.
  Its cut-offs rank rather than measure, and on its first real run it called a
  travel company's mailer a person. Say so if the user asks how reliable it is;
  their ruling on the contact card wins.
- `--dry-run` with `--model` over-counts: nothing from the header pass is
  written, so the model is shown senders the headers would have settled.

`contact responsiveness <address>` returns communication dynamics including
median reply turnaround time for me vs. them, open loops (`awaitingMyReplyCount`
and `awaitingTheirReplyCount` with pending thread subjects and snippets), role
ratio (`toRatio`), and a 24-hour histogram of incoming messages.

**Open loops read the `loops` label where it has looked.** Who sent the last
message says which way a loop points; the label says whether it is open at all.
`loopsJudged` says whether the label exists, `closedByJudgement` how many
conversations who-sent-last would have listed that it (or the user) closed, and
each thread's `judged` whether it was looked at. Without the label the lists are
who-sent-last alone, which counts every newsletter as waiting on the user — say
that rather than presenting the list as a to-do list.

---

## `in:attachments` — files are their own kind

A file is an entity, and the entity is the **bytes**, not the arrival. The same
attachment sent to five people is **one** file with five occurrences, so a
listing of files does not repeat it and `messages>1` finds the ones that went
round.

```
iql --json query "in:attachments filetype:pdf larger:1mb"
iql --json query "in:attachments content:invoice from:*@acme.com"
iql --json query "in:attachments is:shared | count by from"
```

| Term | Matches |
|---|---|
| `filename:` `file:` `name:` | the name it arrived under — *any* of them |
| `filetype:` `type:` `mime:` | `pdf image photo audio video doc sheet slides`, or a MIME prefix |
| `content:` `inside:` | words **inside** the file, once it has been read |
| `size:` `larger:` `smaller:` | the file's own size |
| `messages:` | how many messages carried it — `messages>1` is a circulated document |
| `id:` | the content hash; a prefix is enough |
| `similar:<hash>` | files near it in meaning, once embedded |
| `is:` | `stored missing inline attached shared read unread scanned searchable` |
| `has:text` | something was extracted from it |

**Any message field also works** and asks about the mail the file arrived on:
`in:attachments from:*@acme.com after:7d` is "files that came from Acme last
week". Over *every* occurrence — a document that arrived from two people is
from both.

A bare word searches the filename, the file's text, and the carrying message,
because nothing distinguishes which was meant.

### The states that look like absence

Three separate silences, and they are different answers:

```
is:unread     nobody has looked inside this file yet
is:scanned    read, and it holds no text — an image of a page. iql ocr reads these
is:missing    recorded, but the bytes are not on disk
```

A mailbox where nothing has been extracted returns nothing for `content:`, and
that means *nobody has read them*, not *no file says that*. `iql doctor` says
which, and the fix is `iql maintenance attachments` then
`iql maintenance text`.

### Going the other way: `attached:`

`in:attachments` answers *which files*. `attached:` answers *which mail*, and it
is an ordinary message term, so it composes with everything in the mail
language:

```
iql --json query "attached:128072f0de5e"        the mail that carried this file
iql --json query "attached:*.pdf after:7d"      mail with a PDF on it, last week
iql --json query "attached:contract -from:me()" somebody sent me the contract
```

The value is a **content-hash prefix** when it looks like one — eight hex
characters or more — and a **filename** otherwise, with `=` and `*` available as
everywhere else. `carries:` is an alias.

It is deliberately **not** called `file:`. That name already belongs to an
attachment query, where it means the name a file arrived under, and one term
quietly answering about two different kinds depending on an `in:` elsewhere in
the expression is the kind of thing nobody debugs twice.

Negation reads as it does for recipients: `-attached:x` is *no attachment of
this message is x*, not *some attachment is not x*.

### Sends are not rows

A file's occurrences are the rows that reference it, and **a row is not a
send**. The same part can be attached twice to one message — a signature image
referenced once in the body and once as a trailer, a client that duplicates on
forward — and counting rows would report a delivery that never happened.

On a real mailbox, of the files appearing more than once, eight were in
genuinely different messages and two were attached twice to a single one. So:

- **count over distinct messages.** `messages:` on an attachment query, and the
  `messages` field in the API, are distinct carrying messages.
- **list over rows.** `/api/attachments/occurrences` returns every reference,
  because each one is a real part of a real message with its own position and
  its own name.

The viewer names the difference only when the two disagree — *"1 message ·
attached 2 times"* — so the common case stays quiet and the odd one is legible.

---

## `ui` — the open windows, and pointing them at things

A browser with InboxQL open registers itself with the running server. You can
see what somebody is looking at, and put something in front of them.

```
iql --json ui list                     who is open, and what each is showing
iql ui query <id> <query> --note why   point one window at a query
iql ui open <id> <what>                desk, log, settings, annotators, windows, or a message id
iql ui notice <id> <text>              say something, changing nothing
iql ui close <id>                      forget one that is gone
```

**This command talks to the server, not the data directory.** Every other one
reads the database; the list of open windows lives in the memory of the process
serving them, so `iql start` must be running and `--addr` must point at it
(default: `$INBOXQL_ADDR`, then `addr` in the machine settings, then `127.0.0.1:8420`).

### Why this is the useful half

`ui list` answers *what is the user looking at* without asking them. Each window
reports its current query and its open tabs, so you can see that somebody is on
`label:purchases` before suggesting anything about it.

The other direction is a handoff: when you have found something, put it on their
screen rather than describing it.

```
iql ui query 2 "from:*@stripe.com after:7d" --note "the invoices you asked about"
```

### Always say why

**`--note` is not decoration.** A screen that reorganises itself with no
explanation reads as a fault, not as help. The note arrives with the command and
is shown beside what changed. Use it every time you move somebody's view.

### What a command is, and is not

An instruction to a live window, **not a new source of truth about it**. Point a
window at a query, let the person reload it, and it returns to its own state.
You are pointing, not driving.

A command to a window that has closed **fails** — exit 3, "no window named …".
It does not quietly succeed, so do not report that you showed somebody something
unless the command returned 0.

`connected: false` in a listing means the window is open but its channel has
dropped; it cannot be commanded until it reconnects.

### The person can see all of this

The Windows tab in the UI lists every open window, what each is showing, which
tabs it has, and what it was last told — including by you. There is no way to
move somebody's screen without it being visible to them afterwards, which is the
intended property rather than an oversight.

## `in:logs` — what the application did

A log line is not mail. It has a level, a subsystem, the run it belongs to and
some words, and it is queried like every other kind:

```
iql --json query "in:logs level>warn after:7d"
iql --json query "in:logs | count by category"
iql --json query "in:logs job:<import-id>"
```

| Term | Matches |
|---|---|
| `level:` | `debug info warn error` — **ordered**, so `level>warn` is the errors |
| `category:` | the subsystem: `import`, `sync`, `app`, or whatever wrote it |
| `job:` | everything one import or maintenance run recorded |
| `account:` | which account it was about |
| `duration:` | how long it took, in milliseconds — `duration>1000` |
| `message: context: reference:` | the text, where it happened, what it was about |
| `after: before: on:` | against the line's own time |

Group by `level`, `category`, `job`, `account`, or `hour day week month year`.

```
iql --json log [--level warn] [--category import] [--job <id>] [--limit n] [--clear]
iql --json log level [debug|info|warn|error]
```

`iql log` is the quick read; `in:logs` composes. `iql errors` still works and
means `iql log`.

### What is in it, and what is not

**Everything the process printed.** The standard library's logger is routed
here, so migrations, syncs, annotator runs and model calls all land at `info`
under category `app` unless they said otherwise. Before this they went to
stderr and vanished.

**Nothing from before the database opened.** The log is a table, so the lines
written while opening and migrating it reach stderr only. That is a real gap
rather than a bug to report.

**stderr still gets everything.** The database copy is the one you can query
later; stderr is the one that works when the database does not.

### Timings

Every query is recorded with how long it took, in a column rather than in
prose — so this is a question the log can answer:

```
iql --json query "in:logs duration>1000 after:1h | count by category"
iql --json log --slow
```

**A slow query is a warning and carries its SQL.** Anything under the threshold
is a `debug` line without it: "took 2.2 s" says something is wrong, the
statement says what, and on every row it would be bloat. Paste it into
`iql sql --explain`.

Also timed: HTTP requests, syncs, annotator runs and model loads — `laya.Open`
takes about four seconds and said nothing before, which is the obvious suspect
when the first annotation of a run seems to hang.

### What is recorded is separate from how much

| | Where | What it is |
|---|---|---|
| **Level** | the Log tab, `iql log level` | how much — the floor |
| **Everything else** | Settings → Logging | what, and how slow is slow |

Settings → Logging holds four things: the slow-query threshold, which of the
noisy categories are on, how many lines to keep, and whether query text is
written down.

**Categories sit on top of the level, not inside it.** Debug is otherwise
all-or-nothing — turn it up to chase one thing and ten thousand request lines
bury it. `http` is off by default for that reason.

**Query text is user content.** `from:solicitor@…` is as sensitive as the mail
it finds, and the log outlives the query. Recorded by default because this is a
local database; when it is switched off the words are omitted entirely rather
than redacted, and the timings and counts survive. Check
`iql --json log level` before assuming either way, and do not quote a logged
query back to a user who has turned it off.

### The level is a volume control

`iql log level debug` changes it now and remembers it, because logging is
turned up after something has already gone wrong.

**Debug is loud** — a sync writes thousands of lines a minute into the same
database the mail is in. Tell the user that before suggesting it, and suggest
turning it back down. The log is capped at 50,000 lines;
`iql maintenance prune-log` trims it, keeping the newest.

A line is never worth stalling work for, so a full buffer drops rather than
blocks. Dropped lines are reported in the log itself and by `iql log level`, so
a gap is visible rather than silent — if you see one, the answer is a lower
level, not a bigger buffer.

## `annotate` — labels and extracted data

An annotator is a named, versioned instruction applied to messages. A **label**
answers yes or no; an **extractor** pulls structured records out of a body.
Same mechanism, so they version, re-run and query the same way.

```
iql --json annotate list
iql --json annotate show <name>
iql --json annotate plan <name> [--scope <query>]
iql --json annotate run  <name> [--scope <query>] [--limit n] [--dry-run]
iql --json annotate starters [--install] [--only a,b]
iql --json annotate sweep [--trigger after-sync|daily] [--dry-run]
iql --json annotate enable <name>
iql --json annotate disable <name>
```

### Somewhere to start

`annotate starters` is a pack of seven worth beginning from, meant to be
edited. It reports how much of *this* mailbox each one reaches before it is
installed.

**The mix is the lesson.** Every span extractor ships behind a cheaper
judgement that gates it — `receipts` is scoped to `label:purchased`, a decision
label at about three seconds a message. Copy that pattern rather than making
everything an extractor: the gate narrows a 20-second-per-message job to the
mail that could possibly match.

Creating them runs nothing. They arrive with coverage at zero.

### Scope lives on the annotator

`--scope` still wins when given, but an annotator remembers its own, so
`iql annotate run receipts` covers `label:purchased` without being told. That is
also what a triggered run reads, since nobody is there to pass a flag.

### Triggers say when, never what

| Trigger | Means |
|---|---|
| `manual` | the default: it runs when told |
| `after-sync` | drain what is pending once new mail has landed |
| `daily` | drain on a timer |

A trigger is **not a second filter** — scope is the only "what". It is a policy
for when to drain the queue `PendingMessages` already computes, so an
annotator with nothing pending is skipped rather than started.

`annotate sweep` runs everything waiting on a trigger, and `--dry-run` reports
what it would do. A pass is capped at 200 messages per annotator: what is left
stays pending and the next pass takes it, so a short run is not a failure.

**Over HTTP, a sync starts the sweep as a maintenance job.** `iql account sync`
does not — it is synchronous and safe in a cron job, and silently gaining
twenty minutes of extraction would make that false, so it prints what is
waiting instead.

Four engines:

- **`rule`** — the instruction is a query expression. Evaluated by the database
  in one pass, deterministic, no provider needed. Prefer this whenever the
  question can be asked as a query.
- **`llm`** — the instruction is a prompt, evaluated one message at a time.
- **`gliner`** — the instruction is a set of labels, and the answer is spans of
  the message itself. Extractors only. Runs on this machine.
- **`laya`** — the instruction is a statement, and the answer is how likely it
  is true. Labels only. Runs on this machine, about three seconds a message.

### Which engine to label with

**`rule` whenever a query can say it.** It is free, exact and re-runs in one
pass. `subject:invoice OR from:*@stripe.com` is a better label than any model,
because it is checkable and costs nothing.

**`laya` when the question needs judgement.** "Is this *actually* a purchase, or
a newsletter that mentions a price" is not a query, and the alternative was an
LLM — the engine that invents. A decision annotator emits no tokens: it scores
the two answers and returns a probability, so there is nothing to hallucinate.

```
iql annotate create purchased --kind label --engine laya \
  --instructions "this message is a receipt, an invoice, an order confirmation or a payment"
```

Write the instruction as a **statement about the message**, not a question and
not a prompt — the model is scoring whether it holds.

It needs weights on disk (`iql laya install`, ~620 MB becoming ~1.2 GB), reads
the subject, sender and the first few hundred words, and **cannot extract**:
`--kind extract --engine laya` is refused, as `--kind label --engine gliner` is.

### Its confidence is an ordering, not a frequency

**This is the one thing to get right about this engine.** The published
checkpoint ships uncalibrated — every temperature 1.0, and its authors measure
its expected calibration error at 0.466. On this mailbox a software newsletter
scored 0.993 on "is this a receipt".

So `label:purchased@0.9` is a **ranking cut**, not "ninety percent of these are
right". Do not tell the user otherwise, and do not present a high score as
evidence the answer is right.

`iql laya calibrate <name>` fits a temperature from the user's **reviewed**
rulings and makes the number mean what it looks like. It needs at least 20 and
refuses below that. Rulings made while reading do not count: they are mostly
corrections, and a set of corrections says the model is never right.
`--include-inflow` overrides that on the record. Until it has run, say that the
scores order and do not measure. `iql laya status` reports the state.

Calibration cannot change an answer — a temperature divides the logits, which
cannot reorder them — so it never makes the model more accurate, only more
honest about itself.

### Which engine to extract with

**Prefer `gliner` over `llm` for extraction.** An LLM composes its answer, so
it can return a value that is not in the message. Asked for five fields on an
order confirmation, a local model returned

```json
{"amount":"$675.00","order number":"109870",
 "due date":"N/A","invoice number":"N/A","account number":"N/A"}
```

at confidence 1.0. Three of those are fabrications, and
`extract:x.due_date` matches a record whose due date is the string `"N/A"`. A
span model has no way to do that: the only thing it can return is a pair of
offsets, so every value is a substring of the message.

Reach for `llm` when the field needs *understanding* rather than *locating* —
"the sentiment of this complaint", "what the sender is actually asking for" —
because a span model knows nothing about a field beyond the words of its name.

A span extractor also:

- **cannot label.** `--kind label --engine gliner` is refused.
- **takes its labels from the schema's field names.** There is no separate
  label list, and changing the schema bumps the version, because it is a
  different question.
- **scores at most 12 fields at once.** Split a wider schema into two
  annotators, which also lets them re-run separately.
- **reads the first ~1300 words** of a message and stops. Long threads and
  marketing mail are read in part.
- **reads the attachments too.** The subject, the body, and every file whose
  text has been extracted — on a mailbox of receipts most of the values are
  inside the PDFs, not the bodies. A file nobody has read yet is skipped, not
  read as empty; `iql maintenance text` is what reads them.
- **stores offsets.** Each record carries the value, plus `field` and byte
  offsets into that field. `field` is `"subject"`, `"body"`, or
  `"attachment:<content hash>"` — the hash, because the same document sent to
  five people is one file.

  Byte offsets, not character offsets. Mail is full of things like the narrow
  no-break space in `9:50 PM`, and indexing by character lands a byte or two
  short on exactly the messages that matter. `GET /api/messages/{id}/annotations`
  returns the text pre-cut into marked runs so a reader never has to do this
  arithmetic at all.
- **carries a real confidence per record**, which is that span's own score
  rather than one number the model volunteered about its whole reply.

### The model it needs

`gliner` needs weights on disk, which are not in the binary:

```
iql --json gliner status      installed or not, and which model
iql gliner install            download (~800MB) and prepare, once
```

`annotate run` on a `gliner` annotator with no model exits **5** and names the
command. `iql doctor` reports it as a failure, but only once an annotator
actually asks for the engine — a mailbox with no span extractors is not
missing anything.

**It sends nothing anywhere.** The model runs in this process; the one network
request the feature ever makes is the download above, which is checked against
the checksum the repository publishes. So unlike an `llm` annotator, there is
no consent question and no endpoint to warn the user about. Say so plainly if
they ask — this is the extractor that does not involve a third party.

Budget roughly **15–20 seconds per message** on a laptop CPU, against about 40
for a local generative model. Scope the first run rather than starting on a
whole mailbox.

### Labels are three-valued, and this will trip you up

A label has three states, not two: the annotator said yes, the annotator said
no, or **the annotator has never seen this message**.

```
label:invoice        evaluated, yes
-label:invoice       evaluated, no      ← NOT the unevaluated ones
unlabeled:invoice    never evaluated
```

`-label:x` deliberately excludes messages the annotator has not reached. If an
annotator has covered 10k of 200k messages, the other reading would return
190k messages and present them as a negative result. When you want the loose
reading, ask for it: `-label:x OR unlabeled:x`.

Before reasoning from a negative label, check coverage with `annotate show` —
`evaluated` against `total`. A label that has only seen a tenth of the mailbox
supports "these are invoices", not "these are all the invoices".

`label:invoice@0.9` sets a confidence floor. LLM labels carry one; rule labels
are always 1.0.

### Extracted data

An extractor's records are queryable and aggregatable:

```
iql --json query "extract:saas-metrics.signups>1000"
iql --json query "from:analytics@acme.com | series saas-metrics.signups by week"
iql --json query "| count by saas-metrics.plan"
```

One message can yield several records — a weekly digest with a bar per day is
seven — and an extractor may declare which extracted field holds a record's own
date. That matters: a digest sent on Monday reports the previous week, so
bucketing by the message date shifts the whole series while still looking
plausible.

### Running one

**Always `--dry-run` first.** It reports how many messages would be evaluated
and, for a remote provider, roughly how much text would leave the machine.

A run over a whole mailbox with a hosted provider sends **every message in
scope** to that provider. That is a different act from `analyze` on one thread,
and an annotator without recorded consent refuses rather than doing it — the
error names the endpoint. Do not suggest working around it; tell the user what
would be sent and where, and let them decide.

`--scope <query>` narrows a run to matching messages, which is the cheap way to
try a prompt before committing to the mailbox.

### Switched off, which is not deleted

An annotator carries `enabled`. Off means **it will not run**: triggers skip it,
the message viewer stops offering it, and `annotate run` on it exits 1 with the
command to switch it back on. Nothing else changes.

**Everything it has already said still counts.** `label:purchased` keeps
matching while `purchased` is off, and `extract:receipts.amount` keeps returning records.
Those are facts about messages that really were observed — the same reason a
version bump keeps the old answers rather than deleting them. So a negative
label still means *evaluated, no*, and coverage still reads as it did; do not
tell the user their results were cleared, because they were not.

This exists because `annotate delete` cascades. Deleting an annotator deletes
every annotation it ever wrote, including the human corrections, and those are
not rebuildable at any price. If a user wants an annotator to stop, `disable`
is the answer; reach for `delete` only when they say they want the results
gone too, and say what that costs first — `annotate show` reports it as
`holds`.

Two numbers that are not the same thing, and the mistake is easy:

- `progress.matched` and `progress.humanCorrections` count **messages**. They
  are coverage, which is what a progress bar wants.
- `holds` counts **annotation rows**, which is what a delete would take. One
  message can hold many: a receipt with an amount, a date and three references
  is five rows. On a worked mailbox the two differ by roughly nine to one.

Quote the second when you are telling somebody what deleting would cost.

`enabled` is not `trigger`. `trigger: manual` means *only when told*; off means
*not even then*.

### Corrections, rulings and scores

`annotate correct <name> <message-id> --yes|--no|--level <l>|--clear` records a
human ruling. It outranks the machine result, survives version bumps and
re-runs, and removes that message from the pending queue. You may suggest
corrections; the user makes them.

**A ruling says how it was made**, and the two kinds are different evidence:

| `via` | Made | Used for |
|---|---|---|
| `inflow` | while reading — usually because something looked wrong | outranking the machine on that message |
| `review` | on a random draw from the review queue | that, plus accuracy and calibration |

`correct` defaults to `inflow`. **Do not pass `--via review`** for a ruling the
user did not make from the review queue: it launders a correction into the set
the model is measured on.

In the interface, a label on a message is a chip with a context menu — Right,
Wrong, or for a levelled label *Should be …* — and the Annotators tab has
**Review**, which draws ten messages across the whole score range and hides the
model's answer until the user has given theirs.

```
iql --json annotate score <name>
```

reports accuracy on reviewed rulings only, and for a yes/no label the cut-off
that would have been right most often — an uncalibrated model can rank
correctly and still put 0.5 in the wrong place. **Two annotators asking the same
question on different engines, scored on the same rulings, is how an engine is
chosen for a question.** Below 20 reviewed rulings the number is shown with a
warning; repeat it.

### Conversations and levels

A label can judge a **whole conversation** (`"unit":"thread"` in its schema). It
is evaluated once, on the newest message, and a reply makes the conversation
pending again. The decision model reads a one-line summary of the conversation
— how many messages, who sent the last, whether the user has replied — then the
newest message; the LLM engine reads the whole conversation. Either way the
stored record carries `lastFromMe`, which is what says which way an open loop
points.

A label can have **levels** (`"levels":[{"label":…,"describe":…}]`, best first).
It then answers which level rather than yes or no — the decision model as a
score question, the LLM by picking one — and an invented level is refused.

The starter pack is built around two of these: `loops` (does the newest message
expect a reply) and `importance` (important / normal / low / ignorable), both
scoped to the last 90 days. It ships **no rule labels**; rules can still be
written, the pack just does not start anyone on them.

### Deleting a gate

`annotate delete <name>` is refused when another annotator's scope reads it
(`label:<name>`), because a broken scope makes a run cover the whole mailbox.
`--disable-dependents` switches those off in the same step — off, not deleted.

---

## `ticket` — work derived from mail

A ticket is an **entity with state that changes**; a message is an **immutable
event**. That distinction is the design, and it matters to you:

- Tickets are *seeded* by extractors and *owned* by the user. A re-run attaches
  more evidence; it never rewrites a status, title or due date someone set.
- Editing an extractor's instructions bumps its version and invalidates every
  annotation — and changes nothing on the board.

```
iql --json ticket list [--query "status:todo due:7d"]
iql --json ticket show <id>
iql --json ticket board [--query <filter>]
iql --json ticket propose <extractor> [--auto-accept 0.9] [--dry-run]
iql ticket move <id> <status>
iql ticket accept <id> | reject <id>
```

**Ticket fields are query terms**, so the same language filters both:

| Term | Matches |
|---|---|
| `status:` | `proposed todo doing done rejected`; `status:*` means every ticket |
| `priority:` | a ticket's priority |
| `due:` | on or before this date — **forward-looking**, so `due:7d` is the next week |
| `ticket:` | words in the title |
| `raised:` | `human` or `annotator` |

A query naming any of these is answered from the tickets table. A **message**
field inside such a query asks about the ticket's *evidence*:
`status:todo from:*@acme.com` is "tickets whose mail came from Acme", not
"tickets that are from Acme". Negation asks whether *any* source matches.

**Extraction proposes; it does not create.** `ticket propose` puts results below
`--auto-accept` into `status:proposed` for a person to accept or reject. Do not
accept on the user's behalf — surface the queue and let them rule. A rejection
is kept rather than deleted, because it is the evidence that the extractor was
wrong.

Identity is **one ticket per conversation per annotator**, keyed on the thread.
Re-running is therefore idempotent. Threads that should be one ticket, or
tickets that should be several, are `ticket merge` — an explicit human action,
never inferred.

Every ticket carries the messages it came from; `ticket show` prints them with
the `iql read` command for each. If you cannot trace a ticket to its mail,
something is wrong.

## Administrative commands

You will not usually need these, but they are available and all support
`--json`:

`init` (prepare a data directory), `doctor` (health checks, non-zero on
failure), `account` (add/list/remove/verify/sync), `user`, `vault`
(status/rotate), `llm` (status/configure/test/disable), `maintenance`
(attachments/text/reindex), `ocr` (read scans with a vision model),
`gliner` (status/install/remove — the span-extraction model),
`laya` (status/install/calibrate/remove — the decision model),
`log` (read what the application did; `log level` sets how much is recorded),
`ui` (the open browser windows; needs a running server — see above),
`backup` / `restore`, `export`, `version`, `start`, and the machine commands
below: `setup`, `where`, `service`, `update`, `hosts`.

**The System section of Settings** (`GET /api/system`) reports what `iql where`
and `iql service status` do, for the running server: its mode (`service`,
`foreground` or `dev`), the mailbox and why it is that one, the address and
auth posture, saved settings still waiting for a restart, power, and whether
the AI provider is remote. `POST /api/system/restart` and
`POST /api/system/update` exist for the page's buttons. **Do not call them
unasked**: both end the process serving the user's open windows, and an update
replaces the binary. Suggest them, and let the person press the button.

**The user guides** are embedded in the binary and served at `/api/docs` — the
same Markdown as `docs/*.md`, plus this file. When a user asks how something
works, `GET /api/docs/index` is a searchable copy of everything they can read
under Help → Documentation, and pointing them at a guide by name is better
than paraphrasing it.

Two to avoid unless explicitly asked: `account remove` deletes every stored
message for that account, and `vault rotate` re-encrypts every credential.
Both are destructive and neither is reversible.

`iql account verify <id>` is genuinely useful for diagnosis — it classifies
failures as authentication rejected, host not found, network unreachable, or
TLS verification failed, rather than returning one opaque error.

---

## An installed machine

InboxQL is normally installed as a per-user background service, with one
mailbox per machine named in `~/.iql/settings.json`:

```
iql --json where                  which mailbox, and why — read this first
iql --json service status         installed? running? at what address?
iql --json update --check         is there a newer release?
iql --json hosts show             a named address, if one is set
```

| Command | Does |
|---|---|
| `setup [--data <dir>] [--addr …]` | writes the settings and prepares the mailbox; `--data` adopts an existing one where it is |
| `where` | the data directory in use, and whether it came from `--data`, `$INBOXQL_DATA`, the settings or this folder |
| `service install\|uninstall\|start\|stop\|restart\|status` | a LaunchAgent / systemd user unit / logon task, from login to logout |
| `update [--check] [--yes]` | verify, back up the mailbox, stop the service, replace the binary, start it |
| `hosts set <name>\|remove\|show` | a name such as `inboxql.localhost` in the hosts file |

**Only `where`, `service status`, `update --check` and `hosts show` are yours to
run unasked.** The rest change the machine — a login item, the binary, the
hosts file, the mailbox's schema — and need the person to ask for them.

**One server per mailbox.** A running server holds a lock in its data
directory. `iql start` on a held mailbox prints where the running one is and
exits 0. A command from a newer `iql` refuses to open a mailbox an older server
is serving — it would migrate the database underneath it — and says to stop
the service or update.

**On battery**, the automatic annotation sweep after a sync waits for mains
power unless `heavyWorkOnBattery` is set in the settings. Nothing is lost:
what is pending stays pending. A sweep you run yourself is not deferred.

**The service starts local models on first use**, not at login: a model
server held from login to logout costs gigabytes whether or not it is used.
The first request that needs one waits for it to start.

**macOS builds are not notarised.** Full Disk Access — needed to import from
Apple Mail — is tied to the exact binary and lost at each update. If an import
reports `blocked` after an update, that is why; relay the `remedy`.

## Things that will trip you up

**Passwords are never flags.** `account add` and `user passwd` read secrets
from an environment variable, from stdin, or from an interactive prompt —
never from argv, because argv is visible in shell history and to `ps`. Set
`INBOXQL_ACCOUNT_PASSWORD` / `INBOXQL_NEW_PASSWORD`, or pipe the value in.

**Passwords are never returned.** `account list` omits the password field
entirely. When updating an account, omitting the password preserves the stored
one; sending an empty string does not clear it.

**Except when the server changes.** An update that alters the IMAP host, port
or username must send the password again — a credential for one server is not a
credential for another, and InboxQL will not present it to a host it was not
given for. Over HTTP that is a **400**; the fix is to supply the password, not
to retry.

**Creating an account will not overwrite one.** `POST /api/accounts` without an
`id` derives one from the name and answers **409** if that collides. Pass the
`id` explicitly to update an existing account.

**Which mailbox, first match wins:** `--data <dir>`, then `$INBOXQL_DATA`,
then `~/.iql/settings.json` (`$INBOXQL_HOME` moves that folder), then `./data` —
the last only when there are no machine settings. **Run `iql --json where`
before reasoning about which mailbox you are reading**; it says which and why.
No command except `init` and `setup` will create one; the rest exit 5 with
instructions. A settings file that will not parse also exits 5, rather than
falling back to `./data` — which would be a different mailbox.

**Flags may follow positionals.** `iql read m1 --thread` and
`iql read --thread m1` are equivalent. Use `--` before an argument that starts
with a dash; everything after `--` is passed through untouched, including
anything that looks like a global flag.

**Global flags work anywhere.** `iql doctor --data ./data` and
`iql --data ./data doctor` are identical. This was not true before 0.1.0: the
globals parsed only ahead of the command name, so the second form was the only
one that worked.

**`-flag` and `--flag` are both accepted** for multi-character names, as they
always have been. `-v` remains the shorthand for `--version`.

**The full-text index may be absent.** FTS5 is a compile-time option. Released
binaries have it; one built without the `sqlite_fts5` tag falls back to
substring matching — correct answers, table scans. `iql doctor` reports which,
and `/api/query/fields` returns `fullText`. If text queries are slow, check
that before assuming the mailbox is large.

**Sync is synchronous.** `iql account sync <id>` returns only when the sync has
finished, so it is safe in a script. It can take a while on a large mailbox.

---

## What is not implemented

Do not promise the user any of this; none of it exists:

- Relevance ranking, and semantic search *by default*. `iql query` uses an
  FTS5 index for token and phrase matching, which is lexical: a search for
  "invoice" will not find "billing". `search` remains plain `LIKE`.

  The exception is `similar:<id>`, which does compare meaning — but only
  between things that have been embedded, and only after
  `iql annotate embed` has run with an embedding profile configured. It
  answers "what else is like this one", not "find me things about billing".
  Nothing is embedded until somebody asks, so `similar:` on a fresh mailbox
  returning nothing means *not embedded*, not *nothing alike*.
- Topic modelling or clustering. The dashboard's "topics" is still the first
  word of the subject line. An LLM annotator is the way to get real topics, and
  it has to be run first.
- Sentiment analysis, except as an annotator someone defines.
- Reading a file InboxQL cannot parse. Text is extracted from PDFs and plain
  formats; a scan holds no text layer and needs `iql ocr`, and some formats
  have no reader at all. `is:scanned` and `is:unread` say which is which.
- Agent execution. The Visual AI Agent Builder in the web UI saves graph JSON
  and cannot run it — there is no Eino runtime.
- Reading mail as HTML. `body` is the plain-text part; `htmlBody` exists in the
  database but `search` and `read` return plain text. Extractors read
  `htmlBody` directly, because the structure behind a chart is the data.
- Defining or running an annotator **without a person present**. The API can
  do both now — `POST /api/annotators` and `/api/annotators/run`, which the
  web UI uses — but a run on the `llm` engine can send a mailbox to a
  provider, so consent is still recorded per annotator and still refused
  without it. Surface the queue; let the user rule.
- Ingesting anything but mail. Any source renderable as RFC822 can enter
  through `import`, but there is no calendar, webhook or chat connector.

## Authentication

`iql start` listens on `127.0.0.1:8420` and serves it without a password, so a
local tool driving the API needs no credentials in the default configuration.

A password is required whenever the audience widens:

- the listen address is not loopback (`--addr :8420`, a LAN address);
- the request arrived through a proxy — any of `X-Forwarded-For`, `X-Real-Ip`,
  `Forwarded`, `X-Forwarded-Host`. This one cannot be turned off;
- the request came from a browser page on another origin. Neither can this one.
- `--require-password` or `INBOXQL_REQUIRE_PASSWORD=1` is set.

**Cross-origin requests get nothing.** A request carrying `Sec-Fetch-Site:
cross-site` or `same-site`, or an `Origin` that does not match the `Host` it
asked for, never gets passwordless access, and is refused outright (**403**) if
it would change anything. This does not affect you: `curl`, the CLI and any
local script send neither header, and the check is there because a browser
cannot suppress them. If you are proxying requests from a browser and see a
403, strip the `Origin` header rather than asking the user to disable
something — there is no flag for it.

**JSON endpoints require `Content-Type: application/json`** and answer **415**
without it.

Then authenticate by posting credentials to `/api/login` and keeping the
`session_id` cookie.

`--trust-local` forces passwordless access on despite a public listen address.
Do not suggest it as a fix for a 401 without saying what it exposes: with a
reverse proxy in front, everyone reaching the proxy is signed in as the
administrator.

---

## Privacy

With no LLM provider configured, nothing leaves the machine. With `ollama`
against localhost, nothing leaves the machine. With a remote provider, thread
contents are sent to it whenever `analyze` or `draft --bullets` runs — check
`iql --json llm status` before assuming either way, and tell the user if you
are about to send their mail to a third party.
