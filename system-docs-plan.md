# The system, from inside the application

*A phased plan. 10 October 2026.*

## Where it starts

- Settings has no view of the machine. Help → About is an `alert()` with a
  version in it.
- `settings.json` can only be edited by hand; nothing says what a change does or
  when it applies.
- Restarting or updating means a terminal.
- The page reloads itself after a server restart only in `--dev` mode.
- There is no Markdown renderer anywhere, and no user documentation: AGENTS.md
  is ~10,000 accurate words written to an agent.

## Phase 1 — the server knows, and says, what it is

`GET /api/system`: version, binary, mode (service / foreground / dev), PID,
uptime, mailbox and why, schema, database size, address, hostname, auth posture,
settings file, models, service status, power, AI provider and whether it is
remote, update availability.

## Phase 2 — settings that can be changed from the page

`GET/PUT /api/system/settings` with, per field, when it applies; a diff between
the file and what the running server started with, so "saved, restart to apply"
is visible. Binding beyond localhost needs an explicit confirmation. Switching
mailbox is its own action that checks the target, or creates one with `iql
init`. No settings file yet: one button creates it, adopting the current mailbox.

## Phase 3 — restart and update from the page

Restart by mode: the service manager for a service, `exec` (or a successor that
waits for the lock, on Windows) for foreground, an exit code the `--dev`
supervisor restarts on. Update runs as a detached `iql update --yes`, which
stops the service — or, in foreground and dev, asks the server to restart
through a token in the lock file. The page waits, reconnects to the new
instance and shows the update log. Restart detection works in every mode.

## Phase 4 — the System section and About

A Settings section that shows all of phase 1, edits phase 2, and runs phase 3.
Help → About opens it.

## Phase 5 — documentation people read

`docs/*.md`, the single source: guides written for a person. Embedded in the
binary, rendered by goldmark, searched through an index built from the same
files. Help → Documentation opens a searchable reader. `cmd/docsgen` renders the
same files, the same way, into the GitHub Pages site with the same search.

## Not doing

**Loading in-app docs from the website.** Offline, version skew, and telling
GitHub every time help is opened. Each page links to its website version.

---

## What was built

All five phases. Go suite green, 201 frontend tests, real typecheck; restart,
update and the panel checked end to end on a scratch mailbox, never the dev one.

### State — `GET /api/system`
`internal/api/systemapi.go`. `iql start` records a `Runtime` (mode, binary, PID,
mailbox and why, address and where it came from, auth posture, the settings it
started with, restart and update functions). Update checks are cached for an
hour; `?refresh=1` asks again.

### Settings — `GET/PUT /api/system/settings`
`internal/api/systemctl.go`. Unknown fields are refused. A non-loopback address
answers 409 until confirmed. The hostname applies at once (the displayed URL
changes); the hosts file is still a terminal command. Pending changes are
compared with what the server is *using*, and each says whether a restart
would apply it: a value outranked by `--addr`/`--data`/env says so.
`POST /api/system/mailbox` checks the target, and creates one only on request
(via `iql init` in a child process, password shown once).
`POST /api/system/settings/create` adopts the running mailbox and address.

### Restart and update — every mode
- service: a detached `iql service restart` (own session, so the manager's
  stop does not take it along).
- terminal: graceful shutdown, release lock and DB, then `exec` of the same
  binary and arguments — same PID, same terminal. Windows starts a successor
  that waits for the lock (`INBOXQL_RESTART_WAIT`).
- dev: exit 75; the supervisor restarts on it.
- update: detached `iql update --yes --data <mailbox>` logging to
  `~/.iql/logs/update.log` (a transient systemd unit on Linux service mode);
  in a terminal the server execs the new binary when the updater succeeds.
- `iql update` now stops the service only when the service runs the binary
  being replaced — updating a dev build no longer stops the installed service.
- The page's reload watcher works in every mode once a restart is asked for,
  and gives up after two minutes with what to check.

Verified for real: terminal restart (PID kept, new instance), dev restart,
and a full update through a local fake release server (download, checksum,
backup, swap, server came back as 0.0.99).

### Panel — Settings → System
State cards, restart, update with confirmation and live log, the settings
editor with "applies now / after restart" per field, mailbox switch, the
pending-restart banner. Help → About opens it.

### Documentation
- `docs/*.md`: 13 guides (~12k words) plus AGENTS.md as "For agents".
- `embed.go` (root package) embeds them; `internal/docs` renders with
  goldmark, rewrites guide links per reader, and cuts an index of 191 sections.
  Tests check every cross-link *and* every heading anchor resolves.
- `/api/docs`, `/api/docs/index`, `/api/docs/{slug}`; Help → Documentation (F1):
  grouped contents, word-prefix search with snippets, `/` to search, anchors,
  previous/next, "describes vX, the version running".
- `cmd/docsgen` / `make docs` renders the same into `site/docs` with the same
  search; Pages runs the link test then generates it. `site/docs/` is ignored.

### Found on the way
- `name:` in `in:attachments` was refused although documented — fixed with an
  entity-scoped alias, tested.
- AGENTS.md listed a `received:` contact field that does not exist — removed.

### Not done
- Linux and Windows restart/update paths are written and cross-compiled, not
  run on those systems.
- The service-mode restart was not exercised against the real LaunchAgent on
  this Mac (it would restart the installed service); it is the existing
  `iql service restart` launched detached.
