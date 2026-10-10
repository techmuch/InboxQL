# InboxQL as an installed service

*A phased plan. 7 October 2026. Decisions taken: no Apple Developer ID; port
8420; an optional named local address through the hosts file.*

## Where it starts

- The data directory is `--data`, then `$INBOXQL_DATA`, then **`./data` relative
  to wherever the command runs**. A background service and a terminal command can
  therefore open different mailboxes without either noticing.
- Nothing stops two servers on one database: no lock.
- A newer CLI migrates a database under an older running server.
- Model weights live inside each data directory (1–2 GB each time).
- Release builds exist for macOS universal, Linux amd64 and Windows amd64, with
  `SHA256SUMS`; they are unsigned, and the macOS universal binary is not re-signed
  after `lipo`.
- The only automatic heavy work is the annotation sweep started after a sync;
  nothing fires the declared `daily` trigger.

## Phase 1 — one machine, one mailbox

`~/.iql/settings.json` (or `$INBOXQL_HOME/settings.json`): `dataDir`, `addr`,
`models`, `hostname`, `heavyWorkOnBattery`. Lookup: `--data` > `$INBOXQL_DATA` >
settings > `./data`. `iql where` says which and why; a command run beside an
unused `./data` says so. `iql setup` writes the settings and initialises the data
directory. Models resolve to the machine folder, falling back to an existing
per-mailbox copy. Default port 8420.

A lock in the data directory: one server per mailbox, and `iql start` on a held
lock points at the running one instead. The lock records the server's schema
version, so a newer CLI refuses to migrate under an older server.

## Phase 2 — `iql service`

`install | uninstall | start | stop | restart | status`, in Go, for macOS
(LaunchAgent: at login, out at logout, restart on crash), Linux (`systemd
--user`) and Windows (a logon task). The service reads the settings, so there is
one source of truth for where the mail is.

## Phase 3 — a good citizen on a laptop

The automatic sweep waits for mains power unless the settings say otherwise. The
service does not start a local model server at login; it starts when first used.

## Phase 4 — `iql update`

Check the latest release, download the right archive, verify it against
`SHA256SUMS`, back up the database, stop the service, swap the binary, start it.
Never silent: `--check` only reports.

## Phase 5 — a named address

`iql hosts set inboxql.localhost` writes a marked block to the hosts file (needs
administrator rights, and says the exact command when it lacks them). `.local`
is refused: macOS resolves it over mDNS first, which makes every page load wait.

## Phase 6 — installing it

`install.sh` and `install.ps1`, a GitHub Pages site with per-OS instructions,
update, uninstall and troubleshooting, a Pages workflow, ad-hoc re-signing of the
macOS universal binary, and a Homebrew formula template.

## Not doing

**Notarisation** — no Developer ID. The consequences are documented: the `curl`
path avoids quarantine, and Full Disk Access must be re-granted after each update.

**Pushing, publishing or enabling Pages** — those are yours; the files and
workflows are ready.

---

## What was built

All six phases. 29 Go packages and 195 frontend tests green, race-clean; the
new packages vet clean for Linux and Windows as well.

### One machine, one mailbox — phase 1

`internal/machine` (settings, `$INBOXQL_HOME`, default port 8420),
`internal/modelpath` (weights once per machine, an existing per-mailbox copy
still used), `internal/serverlock` (a kernel lock — `flock` / `LockFileEx` on a
byte past the content, so the server's own description stays readable on
Windows). `iql setup`, `iql where`, the lookup order, the `./data` warning, and
a refusal to migrate under an older server. Both CLI and e2e tests now run with
an empty `$INBOXQL_HOME`, so a developer's installed mailbox is never touched.

### The service — phase 2, checked for real on this Mac

`internal/service`: launchd, systemd and schtasks, each selected by name and run
through an injectable runner, so all three are tested here. On macOS, in an
isolated home on port 8431: installed → served (HTTP 200) → `SIGKILL` → launchd
restarted it (pid 89833 → 89969) → `stop` stayed stopped → `start` served →
`uninstall` removed the plist and unloaded it. A second `iql start` pointed at
the running server and exited 0.

### A good laptop citizen — phase 3

`internal/power` (`pmset`, `/sys/class/power_supply`, `GetSystemPowerStatus`);
the after-sync sweep waits for mains unless `heavyWorkOnBattery`. In service
mode the local model server starts on first use, not at login. There is no wake
handling to do: sync is on demand, and nothing fires the declared `daily`
trigger.

### `iql update` — phase 4

`internal/update`: latest published release, asset per platform, refused
without a matching `SHA256SUMS` line, atomic swap that puts the old binary back
on failure. The command backs up through SQLite's online backup, stops the
service, swaps, restarts, and refuses a Homebrew-installed binary. Tested
against a fake release server; the swap itself on a temporary file, never on the
test binary.

### A named address — phase 5

`internal/hostsfile` writes only between its markers and leaves an unclosed
block alone; `.local` refused (mDNS). `iql hosts set` records the name as you,
then prints the exact `sudo` command when it cannot write the file; under sudo
it touches only the hosts file. Checked for real: a server with
`hostname: inboxql.localhost` prints that address, and both a page and a
same-origin POST succeed through it. `.localhost` names resolved here even
without a hosts entry — curl, and by design Chrome and Firefox, treat them as
loopback; the hosts file is what makes the name work for everything else.

### Installing — phase 6

`site/install.sh` (run end to end against a local fake release: installed and
set up; a tampered archive refused with nothing installed), `site/install.ps1`
(**not run** — PowerShell is not on this machine), `site/index.html`, a Pages
workflow that shellchecks the installer before publishing, ad-hoc `codesign`
after `lipo`, new release notes, and a Homebrew formula template — without a
licence line, because the repository does not declare one.

### Changed defaults to know about

- **Port 8420** everywhere 8080 was: server, `ui`, the Vite dev proxy (which
  now follows `$INBOXQL_ADDR`), the docs.
- **`./data` is no longer the default once `~/.iql/settings.json` exists.**

### Not done, and why

- **Running `iql setup` / `service install` on this machine for real.** That
  chooses your mailbox and adds a login item; it is yours to do.
- **Publishing**: enabling Pages, creating the tap repository, publishing a
  release (the workflow makes drafts, and `iql update` only sees published ones).
- **Windows and Linux on real hardware.** Tested through the injectable runner
  and cross-compilation; neither platform's service has run.
