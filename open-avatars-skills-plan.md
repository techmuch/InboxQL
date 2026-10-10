# `iql open`, button padding, disk access, Gravatar, agent skills

*A phased plan. 10 October 2026.*

## Phase 1 — buttons get their padding back
Find why `px-5` on Save Profile has no effect (suspected: an unlayered reset
in nexus-shell's stylesheet beating Tailwind v4's layered utilities), fix the
cascade once, check in a browser that padded buttons are padded everywhere.

## Phase 2 — `iql open`
Open the running server's URL from the lock file. Not running: say what to
run (service installed → `iql service start`; otherwise `iql start --open`),
never start anything. `--print` for scripts and SSH. `--json`. AGENTS.md: a
command for people; agents use `iql ui query`.

## Phase 3 — Full Disk Access, as easy as macOS allows
There is no prompt to trigger. Instead:
- name the right program: the iql binary under the login service, the
  terminal app when started from a terminal (macOS checks the responsible
  process) — verified on this Mac;
- `POST /api/import/disk-access/open` opens the exact Settings pane;
  `…/reveal` shows the program in Finder;
- "I've granted it — restart and check": restart, then re-detect.

## Phase 4 — pictures from Gravatar, on request, stored
- One button on the profile, one per contact. Fetched by the server
  (SHA-256 of the address, `d=404`, 256px), never by the page; the URL is
  built by the server from the address, never taken from the browser.
- Stored in the mailbox (`<data>/avatars/`), served locally; "no Gravatar"
  remembered; Refresh and Remove. Shown on the card, the contact list and
  the message header, initials otherwise. No bulk fetch.

## Phase 5 — skills for AI tools
- `iql help agents` prints AGENTS.md (embedded already).
- A short skill embedded in the binary, version-stamped.
- Detect tools by their home folders. Adapters only for formats confirmed:
  Claude Code (`~/.claude/skills/iql/SKILL.md`); others only once their
  documentation confirms where global instructions live, written as a
  marked block in a shared file.
- `iql skills list|install|remove`, the same in Settings → System; the
  endpoint takes no content. `iql update` refreshes installed skills.

## Not doing
- Bulk Gravatar fetches. Each one tells Gravatar you know that address.
- An MCP server — a separate decision.

---

## What was built

All five phases. Go suite green, 206 frontend tests, real typecheck; each
feature checked in a browser against a scratch mailbox, with a stand-in
Gravatar server.

### Buttons
nexus-shell ships Tailwind v3's reset unlayered, and unlayered CSS beats every
layered Tailwind v4 utility: `px-5` lost to `button { padding: 0 }` unless
nexus-shell's own bundle happened to contain the same class. It is now
imported from index.css as `layer(nexus)`, between Tailwind's base and its
utilities. Save Profile measures 20px a side (was 0); the shell's tabs and rail
look as before. Every button in the app benefits, not just this one.

### `iql open`
Opens the URL the running server recorded in its lock file (so a site name is
used). Starts nothing: says `iql service start` or `iql start --open`.
`--print` for scripts/SSH, `--json`. Tested with a held lock.

### Full Disk Access
`internal/tcc`. macOS has no prompt for it; the page now:
- names the right program — the iql binary under the login service (certain:
  the server knows it is the service), the .app found walking up the process
  tree (Terminal, iTerm, VS Code), or says honestly when it cannot tell
  (tmux/screen/SSH reparented to launchd);
- opens the exact Settings pane, shows the program in Finder (the body names
  `grantee` or `binary`, never a path), and "restart and check" returns to the
  Import section after the reload.
- The private `responsibility_get_pid_responsible_for_pid` was tried and
  answered "itself" for every process here, so it is not used.
- Not exercised live: this machine can read ~/Library/Mail, and pressing the
  buttons would put System Settings/Finder on your screen. Covered by tests.

### Gravatar
`internal/avatar`, files in `<data>/avatars/` named by SHA-256 of the address
with a JSON sidecar — no schema change, moves with the mailbox. Server builds
the URL; SVG and over-1 MB refused; "none" remembered. Profile: **Use my
Gravatar** sets the profile picture to the local copy. Contacts: **Get picture
from Gravatar / Ask again / Refresh / Remove** on the card; pictures (or
initials) on the card, the contact list and the message header. The page asks
`/api/avatars/stored` once rather than a request per row. Profile save's
`alert()` replaced by an inline note; the "CHANGE" overlay that did nothing
removed.

### Skills
Formats confirmed from each tool's documentation: all use `SKILL.md` with
`name` + `description`. Claude Code reads `~/.claude/skills`; Codex
`~/.agents/skills`; Gemini CLI `~/.gemini/skills` or the `~/.agents/skills`
alias — so two locations serve three tools, without Gemini seeing two copies.
- `iql help agents` prints AGENTS.md from the binary.
- `internal/skills/SKILL.md` (~800 words), version-stamped with a marker; a
  folder named `iql` without the marker is never touched.
- `iql skills list|install|remove`, `/api/system/skills` (takes a location,
  never content), the **AI assistants** card. Each server start refreshes
  installed skills written by an older version.
- Nothing installed into your real ~/.claude or ~/.agents: that is yours to do.

### Also
- AGENTS.md: `received:` (not a contact field) removed again — the earlier
  removal had not survived.
- Guides updated: command line, installing, getting started, importing (the
  grantee table), contacts (pictures), settings, privacy (the two other
  outbound requests, each on a button).
