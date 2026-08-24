# Mail Import — Design

*Draft, 2026-08-23. Supersedes the CLI-only import sketch.*

Import mail from a desktop client's local store into UEA, non-destructively, driven
either from Settings → Import in the UI or from `uea import` on the command line.
Apple Mail is the first source; the design is pluggable so Thunderbird and Outlook
are additions rather than rewrites.

---

## 1. What this has to get right

Four things drive nearly every decision below.

**Reading is privileged.** `~/Library/Mail` is TCC-protected on macOS. Whichever
process reads it — the CLI, or `uea serve` when the UI drives the scan — needs Full
Disk Access, and without it the failure is a bare `EPERM`. A web UI makes this
worse, not better: the user clicks a button in a browser and a background process
they never think about is the thing that lacks permission.

**Scanning and importing are different operations.** Scanning is read-only and
answers "what is there". Importing writes. Conflating them means a user who wanted
to look at their mailbox has already imported 40,000 messages.

**Importing a mailbox is not an HTTP request.** A full mailbox can be six figures of
messages and tens of gigabytes of attachments. This needs a job model with progress,
cancellation and survivable state, not a request that hangs for twenty minutes.

**The browser must never name a filesystem path.** A `POST` that accepts
`{"path": "..."}` and reads it is a directory-traversal hole that hands any page on
localhost the ability to read arbitrary files as the UEA user. The server enumerates
sources itself; the client selects from that list by opaque id.

---

## 2. Architecture

### 2.1 `internal/importer` — the source abstraction

```go
// Source is one mail client UEA can read from.
type Source interface {
    ID() string    // "apple-mail" — stable, used by the API
    Name() string  // "Apple Mail" — shown to a person

    // Detect reports whether this client's store is present and readable.
    // A present-but-unreadable store is the TCC case and must be
    // distinguishable from an absent one.
    Detect() (Detection, error)

    // Mailboxes enumerates the folder tree. Cheap: directory walk only.
    Mailboxes() ([]Mailbox, error)

    // Scan produces statistics for a mailbox at the requested depth.
    Scan(ctx context.Context, mailboxID string, depth ScanDepth, progress func(Progress)) (Stats, error)

    // Open iterates raw RFC822 messages. The iterator is the only path that
    // touches message bodies, and it is strictly read-only.
    Open(ctx context.Context, mailboxID string, sel Selection) (MessageIter, error)
}

type Detection struct {
    Available  bool
    Root       string // e.g. /Users/david/Library/Mail/V10
    Readable   bool   // false + Available true == needs Full Disk Access
    Reason     string // human-readable explanation when unavailable
    Remedy     string // actionable fix, e.g. the Full Disk Access instructions
}
```

Implementations, in the order they are worth building: `applemail`, `eml`
(a directory of `.eml` files), `mbox`, then Thunderbird and Outlook.

`eml` is deliberately second rather than last. Dragging messages out of Mail.app into
Finder exports them as `.eml`, which is a zero-permission path to a working import on
day one, and it is the permanent fallback whenever a macOS update moves Apple's
furniture.

### 2.2 Apple Mail specifics

Layout:

```
~/Library/Mail/V<n>/<account-uuid>/<Mailbox>.mbox/<uuid>/Data/<shard>/<shard>/Messages/<id>.emlx
```

`V<n>` tracks the macOS release; detect the highest `V*` present rather than
hardcoding. `.mbox` directories nest, so mailbox discovery is a tree
(`Archive.mbox/2024.mbox` → `Archive/2024`).

The `.emlx` container is simple:

```
1847                      ← byte count of the payload, ASCII, on its own line
Return-Path: <...>        ← exactly that many bytes of raw RFC822
<?xml version="1.0"...>   ← Apple plist: flags, and denormalised metadata
```

Also present, and both need handling: `.partial.emlx` holds a message whose body was
never downloaded — importing one produces an empty message, so skip and count them
separately — and `.emlxpart` files hold attachment parts stored outside the message.

The plist `flags` integer encodes read/flagged/deleted as bits. That mapping is
reverse-engineered rather than documented, so decode only `\Seen` and `\Flagged` and
leave the rest alone. `~/Library/Mail/V<n>/MailData/Envelope Index` is a SQLite
database with authoritative flags and mailbox names, but its schema shifts between
macOS releases; treat it as a later enhancement, never a dependency.

### 2.3 One parser, shared

`internal/sync.parseIMAPMessage` already walks MIME for plain and HTML parts. Extract
it as `message.ParseRFC822(raw []byte) (*message.Message, error)` and have both sync
and every import source call it. Two MIME parsers will drift, and the import path
would inherit none of the fixes the sync path gets.

---

## 3. Schema changes

### 3.1 Fix the content-hash collision first — migration v10

`hasher.NormalizeAndHashSHA256("")` returns `e3b0c442…` for every body-less message,
and `idx_messages_content_hash` is **globally** unique. The first calendar invite
inserts; every subsequent one is silently discarded by `INSERT OR IGNORE`. Import is
the workload that hits this hardest — calendar invites, attachment-only mail, and
HTML-only mail whose tag-strip leaves whitespace all land in that bucket.

Two changes, both wanted:

- Hash over `Message-ID + Date + From + Subject + normalized body` rather than the
  body alone, so distinct empty-bodied messages get distinct hashes.
- Make the index `UNIQUE(account_id, content_hash)`. Cross-account dedup is a
  *reporting* concern, not a storage constraint — the same message legitimately
  exists in two mailboxes, and the database should be able to say so.

This is a live bug in IMAP sync today, not only in import.

### 3.2 Attachments — migration v11

```sql
CREATE TABLE attachments (
  id            TEXT PRIMARY KEY,
  message_id    TEXT NOT NULL,
  filename      TEXT,
  mime_type     TEXT,
  size          INTEGER NOT NULL,
  content_hash  TEXT NOT NULL,   -- sha256 of the decoded bytes
  storage_path  TEXT,            -- NULL when skipped (too large / not imported)
  inline        BOOLEAN NOT NULL DEFAULT 0,
  content_id    TEXT,            -- for inline images referenced by cid:
  skipped       TEXT,            -- reason, when storage_path is NULL
  FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);
CREATE INDEX idx_attachments_message ON attachments(message_id);
CREATE INDEX idx_attachments_hash    ON attachments(content_hash);
```

**Blobs live on disk, not in SQLite**, content-addressed under
`<data>/attachments/<hash[0:2]>/<hash>`. Three reasons: a 25 MB BLOB column destroys
the "backup is a fast `sqlite3_backup`" property; content addressing dedupes the same
PDF sent to five people automatically; and a file on disk can be served, exported and
virus-scanned without a round trip through the database.

The consequence must be documented loudly: `uea backup` copies the *database*. Once
attachments exist, a backup is no longer self-sufficient — either `uea backup` learns
to archive the blob directory too, or the docs say plainly that `<data>/attachments/`
must be backed up alongside. I would do the former.

Rows are written even when the blob is skipped, so "this message had a 400 MB video
we did not store" is recorded rather than invisible.

### 3.3 Import jobs — migration v12

```sql
CREATE TABLE import_jobs (
  id           TEXT PRIMARY KEY,
  source       TEXT NOT NULL,      -- "apple-mail"
  account_id   TEXT NOT NULL,
  mailboxes    TEXT NOT NULL,      -- JSON array of mailbox ids
  selection    TEXT NOT NULL,      -- JSON: limit / date range / all
  status       TEXT NOT NULL,      -- pending|running|paused|done|failed|cancelled
  dry_run      BOOLEAN NOT NULL DEFAULT 0,
  total        INTEGER,            -- known after scan; NULL while counting
  imported     INTEGER NOT NULL DEFAULT 0,
  duplicates   INTEGER NOT NULL DEFAULT 0,
  failed       INTEGER NOT NULL DEFAULT 0,
  attachments  INTEGER NOT NULL DEFAULT 0,
  bytes        INTEGER NOT NULL DEFAULT 0,
  started_at   INTEGER,
  finished_at  INTEGER,
  last_error   TEXT,
  cursor       TEXT                -- resume point
);
```

Persisted rather than in-memory so a server restart mid-import leaves a record
instead of a mystery, the UI can reattach after a page reload, and `cursor` makes
resume possible rather than start-over.

---

## 4. API

```
GET  /api/import/sources                     → available clients + detection state
GET  /api/import/sources/:id/mailboxes       → mailbox tree, fast stats
POST /api/import/sources/:id/scan            → deep scan job   {mailboxIds, depth}
POST /api/import/jobs                        → import job      {sourceId, mailboxIds, accountId, selection, attachments, dryRun}
GET  /api/import/jobs                        → history
GET  /api/import/jobs/:id                    → status snapshot
GET  /api/import/jobs/:id/events             → SSE progress stream
POST /api/import/jobs/:id/cancel             → stop cleanly
```

SSE for progress, matching requirements.md 2.3 which already specifies it for the AI
gateway — one streaming mechanism in the codebase, not two. Polling `GET /:id` stays
as the fallback for clients that cannot hold a stream.

Note what is absent: no endpoint accepts a filesystem path. `:id` is a source
identifier from the server's own list, and `mailboxIds` are opaque handles the server
minted during discovery.

---

## 5. Statistics — and what they cost

The requested stats split sharply by cost, and the UI has to be honest about which
it is showing.

| Stat | Cost | How |
|---|---|---|
| Message count | Instant | Count `.emlx` files |
| Total size on disk | Instant | Sum file sizes |
| Mailbox tree | Instant | Directory walk |
| Date range | Cheap | First/last file mtime, refined by header scan |
| Unread count | Moderate | Trailing plist of every file |
| Attachment count / bytes | Expensive | Parse MIME structure of every message |
| Distinct contacts | Expensive | Parse From/To/Cc of every message |
| Already in UEA | Moderate | Header-only Message-ID read, checked against the DB |

So: **fast scan is the default and returns instantly** — counts, sizes, tree, rough
date range. A **Deep scan** button then parses headers and MIME structure for
attachments, contacts and the duplicate preview, reporting progress as it goes.

The duplicate preview is the most valuable number on the screen and the one nobody
asks for: *"of 12,481 messages, 3,204 are already in UEA."* It turns a scary import
into an informed one. Match on Message-ID rather than content hash — header-only
reads, an order of magnitude cheaper, and accurate enough for a preview.

"Contacts" is computed as distinct participant addresses across the scanned messages.
UEA has no contacts table, and this design does not add one — it is a derived
statistic, and a real contacts feature is separate work.

---

## 6. UI — Settings → Import

Settings already renders a category sidebar (`accounts`, `profile`, `appearance`,
`ai`, `security`); this adds `import`.

```
┌ Import Mail ─────────────────────────────────────────────────────┐
│                                                                   │
│  Source   ( • Apple Mail )  ( Thunderbird — not yet )             │
│                                                                   │
│  ✓ Apple Mail detected — ~/Library/Mail/V10, 4 accounts           │
│                                                                   │
│  ┌ Mailboxes ────────────────────────────────────────────────┐   │
│  │ ☐ ▾ david@gmail.com                                        │   │
│  │   ☐   INBOX            12,481 msgs   2.1 GB                │   │
│  │   ☐   Sent Messages     3,902 msgs   410 MB                │   │
│  │   ☐ ▾ Archive                                              │   │
│  │     ☐   2024            8,110 msgs   1.4 GB                │   │
│  └────────────────────────────────────────────────────────────┘   │
│  [ Deep scan selected ]  — adds attachments, contacts, duplicates │
│                                                                   │
│  Import into   [ Apple Mail Archive ▾ ]                           │
│  Scope         (•) Newest [  100 ]   ( ) Date range   ( ) All     │
│  Attachments   ☑ include, skip over [ 25 ] MB                     │
│                ☐ Dry run — report only, write nothing             │
│                                                                   │
│  [ Start import ]                                                 │
└───────────────────────────────────────────────────────────────────┘
```

The permission failure gets a first-class state, not a toast:

```
⚠ Apple Mail found, but UEA cannot read it.
  macOS restricts ~/Library/Mail. Grant Full Disk Access to the program
  running UEA, then restart it:
    System Settings → Privacy & Security → Full Disk Access
  Currently running as: /Users/david/projects/UEA/bin/uea
  [ Re-check ]
```

Naming the actual binary matters — the usual mistake is granting access to Terminal
when the server was launched by something else.

During a run, progress replaces the form: a bar, live
`imported / duplicates / failed / attachments`, current mailbox and message, and a
Cancel button. On completion, a summary that stays put and is reachable later from a
job history list, because "what did that import actually do" is asked afterwards.

**Where the code goes.** `App.tsx` is 1,288 lines and `SettingsView` is a large slice
of it. This panel should not go in there. Extract `src/views/Settings/ImportPanel.tsx`
and, ideally, split the other Settings categories out at the same time — this feature
is a good excuse for a refactor that is overdue anyway.

The mailbox tree is a natural fit for nexus-shell's `TreeWidget`, and the job history
for its `DataGrid`. Worth using deliberately: it is a real consumer exercising the
library, which is the fastest way to find its rough edges.

---

## 7. CLI parity

Everything the UI does is the same importer package behind `uea import`, and the CLI
lands first because it is testable without a browser:

```
uea import sources                                    # what is detected
uea import mailboxes --source apple-mail              # the tree, fast stats
uea import scan --source apple-mail --mailbox INBOX   # deep stats
uea import run --source apple-mail --mailbox INBOX \
    --account archive --limit 100 --attachments --dry-run
uea import eml ~/Desktop/exported --account archive   # the no-permission path
```

All support `--json`, so an agent can drive an import the same way it drives search —
and AGENTS.md gains a section.

---

## 8. Phasing

| Phase | Contents | Rough size |
|---|---|---|
| 0 | Fix the content-hash collision (migration v10) | small — do this first regardless |
| 1 | `internal/importer` skeleton, `ParseRFC822` extraction, `eml` source, `uea import eml` | half a day — **imports your 100 messages** |
| 2 | Apple Mail source: discovery, `.emlx`, TCC detection, fast + deep scan, `uea import` CLI | ~1 day |
| 3 | Attachments: migration v11, content-addressed blob store, `uea backup` learns about it | ~1 day |
| 4 | Job model (v12), API endpoints, SSE progress | ~1 day |
| 5 | Settings → Import UI, plus the SettingsView split | ~1–2 days |
| 6 | mbox, Thunderbird, Envelope Index for authoritative flags | later |

Phases 0–2 give a working, tested importer with no UI. That is the point at which
the feature is real; everything after is reach and ergonomics.

---

## 9. Decisions I would want confirmed

1. **Import target account.** `messages.account_id` is `NOT NULL` with
   `ON DELETE CASCADE`, so imported mail is deleted along with whatever account owns
   it. I would default to a dedicated host-less "archive" account that sync and
   verify skip, rather than importing into a live IMAP account where
   `uea account remove` would take the archive with it.

2. **Attachment default.** Include attachments by default with a 25 MB per-file cap,
   or exclude by default and make it opt-in? Including is friendlier; excluding keeps
   the first import fast and the data directory small.

3. **Whether `uea backup` archives blobs.** Once attachments are on disk the database
   backup is no longer complete. I think `backup` should produce a bundle, but that
   changes its output format from a single `.db` file.
