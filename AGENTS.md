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

### `search` — find messages

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

**This is substring matching, not relevance ranking.** There is no full-text
index and no semantic search. A single-word query on a large mailbox is a table
scan and will be slow — narrow with `--account` or a date range. Because it is
literal, a search for "invoice" will not find "billing"; issue several queries
with different wordings rather than assuming one returned everything.

### `read` — get one message or a whole thread

```
iql --json read <message-id>
iql --json read <message-id> --thread
```

Without `--thread`, returns a single message object with its full `body`. With
`--thread`, returns `{threadOf, count, messages[]}` oldest first.

Threading groups by **normalised subject** (`Re:`/`Fwd:` prefixes stripped), not
by `References` headers. An unrelated message that happens to share a subject
line can appear in a thread. Sanity-check participants and dates before relying
on a thread being one conversation.

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

## Administrative commands

You will not usually need these, but they are available and all support
`--json`:

`init` (prepare a data directory), `doctor` (health checks, non-zero on
failure), `account` (add/list/remove/verify/sync), `user`, `vault`
(status/rotate), `llm` (status/configure/test/disable), `maintenance`,
`backup` / `restore`, `export`, `version`, `serve`.

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

**Sync is synchronous.** `iql account sync <id>` returns only when the sync has
finished, so it is safe in a script. It can take a while on a large mailbox.

---

## What is not implemented

Do not promise the user any of this; none of it exists:

- Semantic or vector search, FTS5, relevance ranking — `search` is `LIKE`.
- Topic modelling or clustering. The dashboard's "topics" is the first word of
  the subject line.
- Sentiment analysis.
- Attachment extraction over IMAP. Sync stores bodies only. `iql import
  --attachments` does extract and store attachments from a desktop client, and
  `import scan --deep` counts them, but a synced mailbox has none.
- Agent execution. The Visual AI Agent Builder in the web UI saves graph JSON
  and cannot run it — there is no Eino runtime.
- Reading mail as HTML. `body` is the plain-text part; `htmlBody` exists in the
  database but `search` and `read` return plain text.

## Privacy

With no LLM provider configured, nothing leaves the machine. With `ollama`
against localhost, nothing leaves the machine. With a remote provider, thread
contents are sent to it whenever `analyze` or `draft --bullets` runs — check
`iql --json llm status` before assuming either way, and tell the user if you
are about to send their mail to a third party.
