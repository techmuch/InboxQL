# Labels, a full mirror, live two-way sync, and sending

*15 October 2026.*

Decisions taken:
- **One `label:`.** It covers your labels, Gmail's labels and annotator labels.
- **Labels replace folders.**
- **Full history**, attachments included.
- **The `outbox approve` gate is removed.**
- **Sending is built last.**

## Phase 0 — stop marking mail read

Sync fetched `BODY[]` on a read-write `SELECT`, which makes the server mark every
message it fetches as read, Gmail included. Fetch `BODY.PEEK[]` instead, and
use `EXAMINE` wherever nothing is written.

## Phase 1 — labels

A message carries a **set of labels**.

**System labels:**
- `inbox`, `sent`, `drafts`, `important`, `spam`, `trash`;
- starred and unread stay flags (`is:starred`, `is:unread`), as they are in
  Gmail and IMAP;
- archived means not in the inbox, spam or trash.

**Schema v43:**
- `mail_labels(name, system)`: the labels that exist.
- `message_labels(message_id, label, source, created_at)`: `source` is
  `server`, `import`, `human`, `agent` or `migration`.
- `message_locations(message_id, account_id, mailbox, uid_validity, uid)`:
  where a message is on the server, needed to change it there.
- `messages.gm_msgid` and `gm_thrid`: Gmail's permanent message and thread ids.
- `mailboxes` gains `uid_validity`, `low_uid` and `backfill_done`.
- `pending_changes`: the queue of local changes still to push (Phase 4).

**Backfill from what is stored now.** Each existing message gets labels from its
folder and flags:

| Comes from | Label |
|---|---|
| `\Deleted` | trash |
| junk flag or a Spam/Junk folder | spam |
| a Sent folder or sent from your address | sent |
| a Drafts folder | drafts |
| an INBOX folder, or no folder at all (synced before v14) that is not sent | inbox |
| Archive and All Mail | nothing, so archived |
| any other folder | a user label of that name, e.g. `Receipts`, `Work/Clients` |

**Query.** `label:x` is true when the message has mail label `x` **or**
annotator `x` said yes.
- `-label:x` is "no mail label `x`" and, when an annotator of that name
  exists, also "evaluated, and no". An annotator's labels stay three-valued.
- `unlabeled:` and `@confidence` still apply to annotators only.
- `has:label` covers both kinds.
- `folder:` stays as an alias: `folder:inbox` = `label:inbox`, and
  `folder:archive` = not inbox, spam or trash.
- `mailbox:` is still the raw folder a message was read from.
- An unknown name is still an error, with a suggestion.

**Store API:** `AddLabels`, `RemoveLabels`, `MessageLabels`, `LabelCounts`, and
`Archive`, `MoveToInbox`, `Trash`, `Untrash`, `Spam` and `NotSpam` as label
changes. Every local change also enqueues a pending change for accounts with a
server.

**API:**
- `GET /api/labels`: every label with total and unread.
- `POST /api/messages/labels` with `{ids, add[], remove[]}`.
- `POST /api/messages/archive`, `trash`, `spam`, `inbox`.
- A message's JSON gains `labels`.

**CLI:** `iql mail label|unlabel|archive|inbox|trash|untrash|spam|read|unread|star|unstar <id>…`
and `iql labels`.

**Desk:**
- **Rail:** Inbox, Starred, Sent, Drafts, All Mail, Spam, Trash, then a
  **Labels** section with your labels and annotator labels together.
- **Viewer:** Archive, Spam, Trash, Mark unread and a Label picker work (they
  were dead buttons). Label chips can be removed.
- **Keys:** `e` archive, `#` trash, `!` spam, `l` label.
- **List:** label chips on each row.

## Phase 2 — the full mirror

**Gmail**, detected by `X-GM-EXT-1`:
- Sync **All Mail**, plus **Spam** and **Trash**, which All Mail excludes.
- Fetch `X-GM-MSGID`, `X-GM-THRID` and `X-GM-LABELS`. The message id is the
  identity, so a message seen in Trash after All Mail is the same row.

**Other IMAP:** sync every selectable folder. Each folder is a label (special-use
folders map to the system labels), and copies of one message in several folders
are one row with several labels.

**Order:**
- New mail first, every pass (UID > high-water mark).
- Then history, newest first, in batches, from a UID list. A `low_uid` floor
  makes it resumable.
- Live mail is never stuck behind history.

**Matching imported mail.** A fetched message whose Message-ID is already stored
for the account is not stored again. It gains the server location and the
server's labels instead, which links the Apple Mail import to Gmail.

**Progress:** each account reports messages on the server, messages stored, and
whether history is complete.

## Phase 3 — live

**One worker per connected account, inside the server:**
- connect;
- run a pass;
- IDLE on All Mail (Gmail) or INBOX (others) until the server reports a change,
  a local change needs pushing, or 9 minutes pass;
- reconnect with backoff after errors and after sleep.

Other folders are checked every few minutes. The workers start with the server
and are restarted when an account is added, edited, disconnected or removed.

**New mail pushes a `mail` event to every open window**, and the Desk refreshes
its list and counts. **Sync now** wakes the worker.

## Phase 4 — two-way

**Gmail to here:**
- `CHANGEDSINCE` (CONDSTORE) brings flag and label changes.
- A UID list compared with what is stored finds messages that left All Mail.
- Messages moved to Trash or Spam turn up there by Gmail id.
- A message gone from the server entirely is kept locally, under Trash.

**Other servers to here:** flags fetched per folder; moves and deletions by UID
list.

**Here to the server:** every local change is queued and applied by the worker.
- Gmail: `UID STORE ±X-GM-LABELS`; trash and spam by `UID MOVE` into those
  folders.
- Others: `UID MOVE` between the special-use folders; user labels stay local.
- Flags: `UID STORE ±FLAGS`.
- Retried with backoff. The queue shows in the account card.

**Conflicts:** the server wins, except for labels with a change still queued.

**Never:** a permanent delete on the server.

## Phase 5 — sending

- **Send** in the compose window and on a draft: sends after an **Undo**
  window of a few seconds, then shows under Sent.
- Gmail files sent mail itself; for others InboxQL `APPEND`s a copy to Sent.
- `iql outbox approve <id>`:
  - asks interactively at a terminal;
  - otherwise takes `--yes`;
  - the exit-4 gate is gone.
- AGENTS.md, the iql skill and the docs say agents may send when asked, with
  draft → queue → approve as the pattern. `origin: agent` is still recorded.

## Not in this plan

- Writing annotator labels back to Gmail (a per-annotator switch). It builds on
  Phase 4, but it is a choice about what the models put on your phone.
- The calendar's approval gate for agent proposals. Unchanged until you say
  otherwise.

## Status

All six phases are built and tested. The work is uncommitted, and the version
is not bumped.

- [x] Phase 0: `BODY.PEEK[]` and `EXAMINE`.
- [x] Phase 1:
  - schema v43;
  - `label:` for both kinds;
  - `folder:` as views over labels;
  - the API and `iql mail`/`iql labels`;
  - rail, viewer, selection bar, label picker and `e # ! l s` keys.
- [x] Phase 2: the mirror (`internal/sync/mirror.go`) reads Gmail's All Mail,
  Spam and Trash, or every folder on other servers. It fetches metadata first,
  links mail already stored, and reads history newest first, resumably.
- [x] Phase 3:
  - one worker per account (`internal/sync/worker.go`) using IDLE;
  - a quick look every 2 minutes and a full pass every 10;
  - a `mail` event to windows;
  - the live status on the account card.
- [x] Phase 4:
  - `CHANGEDSINCE`;
  - deletions;
  - orphans kept in Trash;
  - the push queue (`internal/sync/push.go`), including label deletion on
    Gmail;
  - a whole folder is never expunged.
- [x] Phase 5:
  - Send with a 10-second Undo (`internal/api/sender.go`);
  - `internal/outbox` as the one way out;
  - a Message-ID on sent mail;
  - Sent filed on non-Gmail servers;
  - `outbox approve --yes`, with the terminal gate removed;
  - AGENTS.md, the skill and the docs updated.

Tests run against `internal/imaptest`, a fake Gmail and IMAP server with
labels, MODSEQ, IDLE, MOVE and the implicit `\Seen`, plus a fake SMTP server.
`go run ./internal/imaptest/devmail -data <dir>` runs InboxQL against them,
with a control port for changes made "on another device"; that is how it was
checked in a browser.

**Found and fixed while checking:**
- An idle worker counted its own EXAMINE as new mail and looped. A test now
  guards it.
- go-imap sent labels with spaces as two atoms.
- The fake did not report flag changes over IDLE.
- Changes queued by the CLI waited for the next pass.

**Not verified:** against real Gmail.
