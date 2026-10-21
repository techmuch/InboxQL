# Backups you can see and manage

*21 October 2026.*

Backups existed only on the command line, and nothing ever removed one: the
update before 0.0.99 left two full copies of the mailbox (17 GB) on a disk
that was 97% full, and the next update would have tried to write a third
with 4.6 GB free. This makes backups something the page shows and manages,
in **Settings → Data Management**, above Erase All Local Data — and keeps
everything doable from the CLI.

## Decisions

- **A catalogue.** Every backup in `<data>/backups/` gets a small sidecar
  (`<name>.json`): why it was made (before an update, by you, scheduled,
  set aside by a restore), the version and schema that made it, whether the
  vault key and attachments came with it, and whether it was checked. Older
  backups are recognised by name; the `inboxql.db.<time>.bak` files a restore
  left in the data folder are listed too.
- **Keep-the-last-N, pruned automatically.** Per reason: 2 update backups,
  2 scheduled, 2 set aside by restores; yours are kept until you delete them.
  Pruning runs after a new backup has been written and checked, and never
  removes the newest backup.
- **Room first.** Before writing a backup, check the disk has the database's
  size plus a margin. If not, make the room the keep rule would make anyway
  once the new one exists (the oldest beyond N−1 of that reason) — never the
  newest backup, never one of yours — and if that is still not enough, refuse
  with the numbers before anything is written. `iql update` follows it, so an
  update never half-writes a backup onto a full disk.
- **Checked.** A new backup gets SQLite's `quick_check`, and its schema
  version is read back. The list says checked, failed, or not yet.
- **Back up now** is a maintenance job with progress; options to include the
  vault key and the attachments.
- **Scheduled** daily or weekly, off by default (each is a full copy); on
  battery it waits, like other heavy work.
- **Restore from the page.** The server cannot replace the database it has
  open, so the page asks for it, the server restarts, and the swap happens on
  startup before the database opens. The current database is set aside as a
  backup, not deleted. A backup made by a newer InboxQL is refused; an older
  one is upgraded on open. Restores read and write in streams, not 9 GB at
  once in memory.
- **CLI:** `iql backup [list|prune|verify|delete|policy]`, and `iql restore`
  accepts a backup's name and refuses while the server holds the mailbox.

## Phases

1. **The catalogue:** sidecars, listing (including legacy names), disk free
   space, keep rules, room-first, checking. `internal/backups`.
2. **CLI:** `iql backup` through the catalogue, its subcommands, `iql restore`
   by name and streaming, `iql update` with room-first and pruning.
3. **Data Management:** the list, space, Back up now (a job), keep rules,
   check, delete; Erase offers a backup first; System links to it.
4. **Scheduled backups.**
5. **Restore from the page:** staged restore applied at startup.
6. **Documented and checked:** settings.md, AGENTS.md, tests, the browser.

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5
- [x] Phase 6

## Checked

All Go tests (the catalogue, the API, the CLI, e2e under -race) and 295
frontend tests pass. Against a scratch mailbox, through the CLI and then the
page in Chrome:

- An older backup named `inboxql-pre-0.0.87-…` was listed as "Before an
  update to 0.0.87", unchecked; **Check it** marked it whole.
- **Back up now** ran as a job with progress, was checked, and its summary
  stayed on screen.
- **Restore…**, typed *restore*: the server restarted, swapped the 0.0.87
  backup in before opening the mailbox — the card added after it was gone —
  and kept what it replaced as "Set aside by a restore". The page reloaded
  onto the new server.
- Delete asks in its own row, saying how much it frees.
- Keep rules saved, including the weekly schedule, from both the page and
  `iql backup policy`; an ambiguous backup name is refused.

Found on the way: a backup being written was listed (0 bytes, "Check it")
while it was copied — backups are now written under a `.partial` name and
renamed when whole, which also means an interrupted one never looks real;
the progress line said "0 of 0 MB" for a small mailbox and is now in KB;
`iql restore` read the whole backup into memory (9 GB for this mailbox) and
now streams it; and the restore confirmation squeezed the table, so it has a
row of its own.
