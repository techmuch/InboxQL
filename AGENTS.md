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
| `account: folder: mailbox:` | where the message lives |
| `is:` | `unread read starred deleted draft answered junk reply` |
| `has:` | `attachment file label reply` |
| `after: before: on:` | `2026-08-15`, `2026-08`, `2026`, `today`, `7d` |
| `larger: smaller:` | `5mb`, `500kb`, a byte count |
| `label: unlabeled: conf>` | annotator results — see below |
| `extract:name.field>10` | extracted structured data |
| `thread:<message-id>` | every message in that conversation |
| `saved:<name>` | everything a saved query matches |
| `in:<kind>` | what the query is about: `mail`, `drafts`, `tickets` |
| a bare word | full-text search |

**A query is about one kind of thing.** Mail is the default. `in:` says
otherwise, and a field only one kind has says it for you — `due:` means
tickets, `origin:` means drafts. Where a name belongs to several kinds it names
none of them: **`status:` means tickets** unless the query says `in:drafts`.
`in:` cannot be negated; it selects the source rather than filtering it.

A field that exists for another kind says so rather than reporting itself
unknown, and a field the kind cannot answer says why — a draft has no flags,
attachments or thread, because it has never been mail.

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
| extract <annotator>               read structured output
| series <field> by <bucket>        an extracted value over time
| sum|avg|min|max <field> [by <bucket>]
```

Group and bucket names: `from domain to cc account mailbox label thread
subject`, and `hour day week month year`.

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

## `contact` / `in:contacts` — people, systems, notes, tags & responsiveness

A contact is an address you have corresponded with. Every message creates one
for every address it touches. Counts (messages, sent, received, first and last
seen) are derived on every read from participant edges, never cached.

```
iql --json contact list [--query "tag:client"]
iql --json contact show <address>
iql --json contact tag <address> <tag>
iql --json contact untag <address> <tag>
iql --json contact note <address> [text]
iql --json contact responsiveness <address>
iql --json query "in:contacts tag:vip awaiting:me"
iql --json query "in:contacts has:notes"
```

| Term | Matches |
|---|---|
| `kind:` | `person organization system unknown` |
| `tag:` | custom tag assigned to contact |
| `has:` | `phone name org notes tag awaiting` |
| `notes:` | prose search in private contact notes |
| `awaiting:` | `me` (contact sent last message) or `them` (user sent last message) |
| `messages:` `sent:` `received:` | interaction thresholds, e.g. `messages>10` |

`contact responsiveness <address>` returns communication dynamics including
median reply turnaround time for me vs. them, open loops (`awaitingMyReplyCount`
and `awaitingTheirReplyCount` with pending thread subjects and snippets), role
ratio (`toRatio`), and a 24-hour histogram of incoming messages.

---

## `annotate` — labels and extracted data

An annotator is a named, versioned instruction applied to messages. A **label**
answers yes or no; an **extractor** pulls structured records out of a body.
Same mechanism, so they version, re-run and query the same way.

```
iql --json annotate list
iql --json annotate show <name>
iql --json annotate plan <name> [--scope <query>]
iql --json annotate run  <name> [--scope <query>] [--limit n] [--dry-run]
```

Two engines:

- **`rule`** — the instruction is a query expression. Evaluated by the database
  in one pass, deterministic, no provider needed. Prefer this whenever the
  question can be asked as a query.
- **`llm`** — the instruction is a prompt, evaluated one message at a time.

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
iql --json query "from:analytics@acme.com | extract saas-metrics | series signups by week"
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

### Corrections

`annotate correct <name> <message-id> --yes|--no` records a human ruling. It
outranks the machine result, survives version bumps and re-runs, and removes
that message from the pending queue. You may suggest corrections; the user
makes them.

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
(status/rotate), `llm` (status/configure/test/disable), `maintenance`,
`backup` / `restore`, `export`, `version`, `start`.

Two to avoid unless explicitly asked: `account remove` deletes every stored
message for that account, and `vault rotate` re-encrypts every credential.
Both are destructive and neither is reversible.

`iql account verify <id>` is genuinely useful for diagnosis — it classifies
failures as authentication rejected, host not found, network unreachable, or
TLS verification failed, rather than returning one opaque error.

---

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

**The data directory is explicit.** Pass `--data <dir>` or set `INBOXQL_DATA`. No
command except `init` will create one; the rest exit 5 with instructions. This
is intentional — the old behaviour silently made an empty database wherever the
process happened to be running.

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

- Semantic or vector search, embeddings, relevance ranking. `iql query` uses an
  FTS5 index for token and phrase matching, which is lexical: it will not find
  "billing" from "invoice". `search` remains plain `LIKE`.
- Topic modelling or clustering. The dashboard's "topics" is still the first
  word of the subject line. An LLM annotator is the way to get real topics, and
  it has to be run first.
- Sentiment analysis, except as an annotator someone defines.
- Attachment extraction over IMAP. Sync stores bodies only. `iql import
  --attachments` does extract and store attachments from a desktop client, and
  `import scan --deep` counts them, but a synced mailbox has none.
- Agent execution. The Visual AI Agent Builder in the web UI saves graph JSON
  and cannot run it — there is no Eino runtime.
- Reading mail as HTML. `body` is the plain-text part; `htmlBody` exists in the
  database but `search` and `read` return plain text. Extractors read
  `htmlBody` directly, because the structure behind a chart is the data.
- Running an annotator over HTTP. `/api/annotators` lists them and their
  coverage; defining and running one is CLI-only, because a run can send the
  mailbox to a provider and that decision belongs at a terminal.
- Ingesting anything but mail. Any source renderable as RFC822 can enter
  through `import`, but there is no calendar, webhook or chat connector.

## Authentication

`iql start` listens on `127.0.0.1:8080` and serves it without a password, so a
local tool driving the API needs no credentials in the default configuration.

A password is required whenever the audience widens:

- the listen address is not loopback (`--addr :8080`, a LAN address);
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
