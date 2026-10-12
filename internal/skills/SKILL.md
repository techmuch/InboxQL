---
name: iql
description: Use when the user asks about their email, their calendar, or anything InboxQL holds — finding or summarising mail, what is on and when people are free, who someone is or who is waiting on whom, attachments and files, labels, receipts and other extracted values, tickets, or drafting a reply. InboxQL is a local mailbox driven by the `iql` command. Not for sending mail without the user, which iql cannot do.
---

<!-- Installed by iql {{VERSION}}. `iql skills remove` deletes this folder; `iql update` refreshes it. -->

# Using InboxQL (`iql`)

InboxQL keeps the user's mail in a local database and answers questions about it
through the `iql` command. This is the short version. **The full contract is
`iql help agents`** — read it before anything not covered here, and when an
answer here seems not to fit.

## Rules that are not negotiable

- **You can compose mail. You cannot send it.** `iql send` only queues. A person
  delivers it with `iql outbox approve <id>` at a terminal; for you that command
  exits 4, and there is no flag or variable around it. Do not look for one.
  After queueing, tell the user the exact command to approve it.
- **Always `--origin agent`** when you create a draft.
- **Always pass `--json`**, and read the exit code: 2 your command was wrong,
  3 not found (do not retry), 4 needs a person, 5 something is not set up
  (stderr says what).
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

## Labels are three-valued

`label:x` said yes, `-label:x` **looked and said no**, `unlabeled:x` never
looked. `-label:x` does not include unevaluated mail. Before concluding "there
are no more", check coverage with `iql --json annotate show x`. Scores on the
local models rank rather than measure unless calibrated: `label:x@0.9` is "the
more confident ones", not "90% right".

## Reading and replying

```
iql --json read <message-id> --thread
iql --json analyze <message-id> --prompt "what is still outstanding?"
iql --json draft create --reply-to <message-id> --origin agent --body -   (body on stdin)
iql --json send <draft-id>                     queues; does not send
iql --json outbox show <draft-id>              the exact bytes, to check your work
```

`analyze` with `"mode": "context"` means no AI provider is set up: there is no
`answer`, so read the messages it returns and answer yourself. That is normal.

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
  Open-loop lists built on who-sent-last alone count newsletters as "waiting on
  you"; `loopsJudged` says whether the `loops` label refined them.
- `in:attachments` lists each file once however many times it was sent;
  `attached:<name|hash>` finds the mail that carried one. A file not yet read
  (`is:unread`) answers nothing to `content:` — that is not "no file says so".
- Tickets are proposed, not created: surface `status:proposed`, let the user
  accept or reject. Never accept for them.

## Showing the user something

If InboxQL is open in their browser, `iql --json ui list` shows each window, and
`iql ui query <window> "<query>" --note "<why>"` puts a result in front of them.
Always give the note. `iql open` just opens a tab; prefer `ui query`, which says
why the screen changed.

## When something is not here

`iql help agents` (everything), `iql help <command>`, and `iql --json doctor`
for what is wrong with the installation.
