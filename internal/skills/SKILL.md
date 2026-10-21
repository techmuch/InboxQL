---
name: iql
description: Use when the user asks about their email, their calendar, or anything InboxQL holds — finding or summarising mail, archiving and labelling it, what is on and when people are free, who someone is or who is waiting on whom, attachments and files, receipts and other extracted values, tickets, or drafting and sending a reply. InboxQL is a local mailbox driven by the `iql` command. Send mail only when the user asks you to.
---

<!-- Installed by iql {{VERSION}}. `iql skills remove` deletes this folder; `iql update` refreshes it. -->

# Using InboxQL (`iql`)

InboxQL keeps the user's mail in a local database and answers questions about it
through the `iql` command. This is the short version. **The full contract is
`iql help agents`** — read it before anything not covered here, and when an
answer here seems not to fit.

## Rules that are not negotiable

- **Send only when the user asks you to.** Compose with `iql draft create
  --origin agent`, queue with `iql send <id>` — which sends nothing — and tell
  the user it is waiting in their outbox. When they ask you to send it,
  `iql outbox approve <id> --yes`. Mail cannot be recalled.
- **Always `--origin agent`** when you create a draft or change mail.
- **Always pass `--json`**, and read the exit code: 2 your command was wrong,
  3 not found (do not retry), 4 needs a person (calendar approvals), 5
  something is not set up (stderr says what).
- **Do not change the machine unasked**: no `service install|start|stop`,
  `update` (without `--check`), `hosts set`, `setup`, `account remove`,
  `vault rotate`, `annotate delete`, and no `POST /api/system/restart` or
  `/api/system/update`. Suggest them; the person runs them.
- **Mail leaving the machine is the user's call.** Check `iql --json llm status`
  before `analyze` or an `llm` annotator; if the provider is remote, say so first.

## First, which mailbox

```
iql --json where            the mailbox in use, and why
```

## Asking questions: `iql query`

```
iql --json query "from:*@stripe.com after:2026-01-01 has:attachment"
iql --json query "is:unread to:me() after:7d | count by from"
iql --json query "in:contacts awaiting:me | sort last desc"
iql --json query "in:attachments filetype:pdf content:invoice"
iql --json query "extract:receipts after:2026 | sum receipts.amount by month"
```

- `from:acme` contains, `from:=a@acme.com` exact, `from:*@acme.com` glob.
- `me()` is the user's own addresses — use it instead of asking.
- Words match **whole words**, not meaning: `invoice` does not find *billing* or
  *invoiced*. Ask several ways; `subject:*invoice*` for a substring.
- `-to:x` means *no* recipient is x.
- `in:` picks the kind: `mail` (default) `drafts` `contacts` `attachments`
  `tickets` `logs`.
- Stages after `|`: `count [by f]`, `top f n`, `sort f`, `limit n`, `thread`,
  `participants`, `sum|avg|min|max a.f [by bucket]`, `series a.f by bucket`.
- A malformed query exits 2 with the position; an unknown field suggests the
  nearest real one. Fix it rather than retrying.
- `iql --json sql "SELECT …"` is read-only SQL for anything the language
  cannot say; `iql --json sql --schema` lists tables.

## Labels, two kinds, one `label:`

Mail carries labels, as in Gmail, not folders: `inbox`, `sent`, `spam`,
`trash` and the user's own (`label:Receipts`). Archived is not in the inbox.
`folder:inbox` still works and means the same. `iql --json labels` lists them.

An **annotator** label is the other kind, and `label:` answers for both. For
an annotator it is three-valued: `label:x` said yes, `-label:x` **looked and
said no**, `unlabeled:x` never looked. Before concluding "there are no more",
check coverage with `iql --json annotate show x`. Scores on the local models
rank rather than measure unless calibrated: `label:x@0.9` is "the more
confident ones", not "90% right".

## Changing mail

```
iql --json mail archive <id>... --origin agent
iql --json mail label Receipts <id>... --origin agent
iql --json mail trash|spam|inbox|read|unread|star <id>...
```

Changes are made here and on the user's mail server (Gmail included) within
seconds. Archive and label when asked; trash only when asked.

## Reading and replying

```
iql --json read <message-id> --thread
iql --json analyze <message-id> --prompt "what is still outstanding?"
iql --json draft create --reply-to <message-id> --origin agent --body -   (body on stdin)
iql --json send <draft-id>                     queues; does not send
iql --json outbox show <draft-id>              the exact bytes, to check your work
iql --json outbox approve <draft-id> --yes     sends — only when the user asked
```

`analyze` with `"mode": "context"` means no AI provider is set up: there is no
`answer`, so read the messages it returns and answer yourself. That is normal.

## Triaging and extracting: `iql work`

To process, label, and extract structured records (`receipts`, `travel`,
`events`, `tasks`, `waiting`, `importance`, `sender`, `topics`) from a batch of
conversations in **two commands**:

```
iql elements install                                                (once)
iql work next --limit 10 --template /tmp/answers.json --model <id>
iql --json work submit /tmp/answers.json
```

- `work next --template <file>` prints a readable digest of all leased
  conversations to stdout (with element schemas, message IDs, sender addresses,
  and clean text) and writes a pre-scaffolded JSON array to `<file>` with all
  IDs, leases, sender guesses, and default `"none"` entries pre-filled.
- Edit `<file>` to fill in verbatim `quote` substrings, `tags`, optional mail
  `"labels": ["receipts"]` (or `"archive": true`), and any extracted records,
  then run `iql --json work submit <file>` (pass `--accept-tickets` or set
  `"acceptTickets": true` when the user asked to create task cards on the board).
- `events` and `tasks` answers automatically create proposed calendar events and
  tickets (returning `eventChangeIds` and `ticketIds` in the submit result).

## The calendar

```
iql --json query "in:events next:7d"
iql --json query "in:events status:needs-action"
iql --json calendar create --title "…" --start "2026-10-15 09:00" --duration 30m --origin agent
```

One row per occurrence. **Every calendar change you make is a proposal**
(`--origin agent`): it shows, marked, until a person approves it —
`iql calendar approve` exits 4 for you. Tell them the command. Answering an
invitation that came by mail queues mail, sent with `iql outbox approve`.

## Contacts, files, tickets

- `iql --json contact show <address>` — who they are, turnaround, open loops.
  Open loops are the `waiting` element's judgements only; a conversation nobody
  has judged is in neither list (`unlabeled:waiting`), so an empty list is not
  "nobody is waiting".
- `in:attachments` lists each file once however many times it was sent;
  `attached:<name|hash>` finds the mail that carried one. A file not yet read
  (`is:unread`) answers nothing to `content:` — that is not "no file says so".
- The board is the user's to-dos and projects: `iql --json ticket board`,
  `ticket show <id>`, and `ticket new|edit|move|label|checklist|comment|archive`
  — everything the page does. Lists are the user's own (`ticket lists`); do
  not assume `todo` exists. Labels are topics: reuse ones in use. Comment with
  `--origin agent`. Make rules (`ticket rules`) only when asked.
- Proposals wait in `state:proposed`: surface them, let the user accept or
  reject. Never accept for them.

## Showing the user something

If InboxQL is open in their browser, `iql --json ui list` shows each window, and
`iql ui query <window> "<query>" --note "<why>"` puts a result in front of them.
Always give the note. `iql open` just opens a tab; prefer `ui query`, which says
why the screen changed.

## When something is not here

`iql help agents` (everything), `iql help <command>`, and `iql --json doctor`
for what is wrong with the installation.
