# Release notes

What changed in each release, newest first. Click the version number under
**InboxQL** at the top left to open this page at the release you are running.

**How releases are named.** A release is named for the day it was made and
how many came before it that day: `2026.10.30.2` is the second release of
October 30, 2026. Releases before that were numbered `0.0.1` to `0.0.113`
and have been renamed this way; each entry gives its old number, so a backup
named for one (`inboxql-pre-0.0.112-…`) can still be matched to its release.
`iql update` treats the new names as newer than every old number.

## 2026.10.30.3 {#v2026-10-30-3}

*October 30, 2026*

- **Dates say the year when it is not this one.** A list shows a time for
  today (**9:08 PM**), the day and month earlier this year (**Oct 24**), and
  the year as well before that (**Mar 1, 2025**) — so last March and this
  March are no longer both "Mar 1". Hover over a date for the whole date and
  time. The Desk, conversations, search results, files, people, cards and
  the board all follow the same rule, and a screen reader hears the same
  short form the eye sees.
- The count above the list could keep the previous search's total ("1–17
  of 0") when both answers fit on one page; it now says 17 of 17.

## 2026.10.30.2 {#v2026-10-30-2}

*October 30, 2026*

- **Releases are named by date.** `2026.10.30.2` rather than `0.0.114`, so a
  version says how old it is. Every earlier release was renamed the same way;
  each is listed below with its old number.
- **Release notes, here and in the app.** This page is in Help →
  Documentation, and clicking the version number opens it at your release.
  After an update a dot beside the version says there is something new,
  until you have looked.
- **`iql version --notes`** prints what changed in the release you are
  running.
- The Desk's screen-reader behaviour is now checked on every change: what a
  row is called, the announcements when you focus on a sender or go back,
  and the landmarks.

## 2026.10.30.1 {#v2026-10-30-1}

*October 30, 2026 · was 0.0.113*

- **Readable in every theme.** Indigo text in the dark theme and gold text
  in the gt theme were too faint to read comfortably (2.9:1 and 1.7:1 against
  the page). Text now uses its own shade in those themes; buttons keep the
  theme's colour.
- **Landmarks and quieter status.** The status bar is a labelled landmark
  rather than something a screen reader announces every time it changes, the
  workspace is the page's main area, and your avatar is no longer read out
  next to your name (nexus-shell 0.2.14).
- The Work loop's daily chart shows today in the morning too; before noon it
  left today off.

## 2026.10.29.2 {#v2026-10-29-2}

*October 29, 2026 · was 0.0.112*

- **Folder counts are instant.** Counting the inbox, the account's totals
  and the rail no longer reads every message: 0.5 s to 0.05 s for the folder
  counts and 0.65 s to 0.02 s for the account's on a real mailbox. The
  mailbox gains an index the first time this version opens it.

## 2026.10.29.1 {#v2026-10-29-1}

*October 29, 2026 · was 0.0.111*

- **One person or topic at a time.** **Clear** at the end of the query bar,
  **Escape** in the list, or clicking the folder you are in returns to all of
  it. **Back** (or **u**, **Alt+←**) returns to the view before — its query,
  its grouping and where you had scrolled. **f** shows everything from the
  sender of the row you are on, **m** messages like it, a row's label
  everything with that label, and **Group by → Topic** lists your topics.
  See [One person or topic at a time](desk.md#one-person-or-topic-at-a-time).
- **Accessible rows.** Each message is one item a screen reader reads in one
  breath — unread, starred, sender, subject, labels, when — and the list says
  which keys it takes. Dimmed rows and counts stay readable.
- **"Who is waiting" is fast.** People waiting on you went from minutes to
  under half a second, and waiting mail from 11 seconds to a few
  milliseconds. Counts nobody is waiting for any more are stopped.
- A click right after a group's **Show** no longer loses to it.

## 2026.10.22.5 {#v2026-10-22-5}

*October 22, 2026 · was 0.0.110*

- **Measuring the work loop.** Intelligence → Work loop shows how much your
  AI harness reads and how often it is right: untouched guesses are kept
  apart from answers, a second reader checks a sample blind, each element has
  a target, and where the two readers disagree it waits for you to say which
  was right. See [Measuring the loop](reading.md#measuring-the-loop).

## 2026.10.22.4 {#v2026-10-22-4}

*October 22, 2026 · was 0.0.109*

- The work loop recognises bulk mail, lists and calendar invitations from the
  message itself; overrides can be sent as just the changes; an element's
  instructions can be tuned (`iql elements tune`); long messages are trimmed
  to the parts worth reading.

## 2026.10.22.3 {#v2026-10-22-3}

*October 22, 2026 · was 0.0.108*

- `make install` replaces the binary in place, beside the old one, so a
  running service picks it up cleanly.

## 2026.10.22.2 {#v2026-10-22-2}

*October 22, 2026 · was 0.0.107*

- Every shade of the theme's colours works: the dashboard's bars, selection
  rings, warning text and dividers were silently missing in some themes.

## 2026.10.22.1 {#v2026-10-22-1}

*October 22, 2026 · was 0.0.106*

- A proposed event opens the calendar on its day. The work loop's digest
  starts with an index of what can be acted on, and sending the same answers
  twice does no harm.

## 2026.10.21.5 {#v2026-10-21-5}

*October 21, 2026 · was 0.0.105*

- Sync fetches again any mail the server has that could not be stored the
  first time.

## 2026.10.21.4 {#v2026-10-21-4}

*October 21, 2026 · was 0.0.104*

- Released builds include the full-text index again, so new mail can be
  stored and searched. Dashboard cards load on their own.

## 2026.10.21.3 {#v2026-10-21-3}

*October 21, 2026 · was 0.0.103*

- The dashboard follows the folder you are in, clicks land on exactly what
  was clicked, and it gains importance and sender cards and a one-year view.

## 2026.10.21.2 {#v2026-10-21-2}

*October 21, 2026 · was 0.0.102*

- **The rail previews the click.** Each label, annotator and folder counts
  what clicking it would show inside what you are looking at.

## 2026.10.21.1 {#v2026-10-21-1}

*October 21, 2026 · was 0.0.101*

- **The folder frame.** The query bar sits inside a folder, quick filters
  add one term each, every match can be selected and acted on at once (with
  undo), the folder can be grouped by sender or domain, and All Items
  searches mail, files, cards, people and events together.

## 2026.10.20.5 {#v2026-10-20-5}

*October 20, 2026 · was 0.0.100*

- **Backups in the app.** Settings → Data Management lists every backup, the
  room it takes and what is kept, makes and checks backups, runs them on a
  schedule, and restores from the page.

## 2026.10.20.4 {#v2026-10-20-4}

*October 20, 2026 · was 0.0.99*

- **Board:** Trello-level board with your own lists, card panel, checklists, labels as topics, boards, calendar due dates and automation rules.

## 2026.10.20.3 {#v2026-10-20-3}

*October 20, 2026 · was 0.0.98*

- **Work loop:** Compact digest URLs, idempotent sender answers, and --accept-tickets.

## 2026.10.20.2 {#v2026-10-20-2}

*October 20, 2026 · was 0.0.97*

- **Work loop:** Batch triage digest, template scaffolding, and unified mail labeling.
- **Build:** Fixed: pin Go 1.27.1 to match go.mod.
- **Build:** Fixed: start transactions as writers; move actions to Node 24 releases.

## 2026.10.20.1 {#v2026-10-20-1}

*October 20, 2026 · was 0.0.96*

- **Work loop:** Agent element extraction engine, work harness protocol, and reading guide.

## 2026.10.19.4 {#v2026-10-19-4}

*October 19, 2026 · was 0.0.95*

- **Query language:** Omit Trash and Spam from results unless mentioned.

## 2026.10.19.3 {#v2026-10-19-3}

*October 19, 2026 · was 0.0.94*

- **Contacts:** Communication intensity activity calendar on contact cards.

## 2026.10.19.2 {#v2026-10-19-2}

*October 19, 2026 · was 0.0.93*

- **Help:** In-app contextual help, deep links into settings/tabs, screenshot gallery.

## 2026.10.19.1 {#v2026-10-19-1}

*October 19, 2026 · was 0.0.92*

- **Viewer:** Secure link handling, remote content blocking, CID inline images.

## 2026.10.15.3 {#v2026-10-15-3}

*October 15, 2026 · was 0.0.91*

- **Labels:** Unified mail labels, full mirror, two-way sync, and live sending.

## 2026.10.15.2 {#v2026-10-15-2}

*October 15, 2026 · was 0.0.90*

- **Sync:** Fixed: use UID fetch and UIDNEXT comparison for incremental IMAP sync.

## 2026.10.15.1 {#v2026-10-15-1}

*October 15, 2026 · was 0.0.89*

- **Accounts:** Rework mail accounts with verification, auto-discovery, Sign in with Google.

## 2026.10.14.2 {#v2026-10-14-2}

*October 14, 2026 · was 0.0.88*

- **Settings:** Find settings in palette, direct setting anchors.

## 2026.10.14.1 {#v2026-10-14-1}

*October 14, 2026 · was 0.0.87*

- **Interface:** Reorganise the main menu; New message/task/event, Find anything, key sequences, Waiting for approval.
- Ci(pages): disable SC2016 for literal PATH in installer.
- Build: update embedded static assets for v0.0.86.

## 2026.10.11.1 {#v2026-10-11-1}

*October 11, 2026 · was 0.0.86*

- **Calendar:** Complete phases A through G, full calendar engine, sync, and UI.

## 2026.10.10.4 {#v2026-10-10-4}

*October 10, 2026 · was 0.0.85*

- **Interface:** Fixed: unlayered nexus-shell with split Tailwind imports for multi-theme support.
- Ci: publish releases automatically on tag push (draft: false).

## 2026.10.10.3 {#v2026-10-10-3}

*October 10, 2026 · was 0.0.84*

- Open-avatars-skills plan phases 1 to 5, iql open, TCC disk access, Gravatar avatars, and agent skills.

## 2026.10.10.2 {#v2026-10-10-2}

*October 10, 2026 · was 0.0.83*

- System-docs plan phases 1 to 5, system settings panel, embedded markdown docs, cmd/docsgen.

## 2026.10.10.1 {#v2026-10-10-1}

*October 10, 2026 · was 0.0.82*

- Deploy plan phases 1 to 6, service mode, auto-update, site, hosts file, single-server locking.

## 2026.10.6.1 {#v2026-10-6-1}

*October 6, 2026 · was 0.0.81*

- Annotation plan phases 1 to 9, human rulings, review queue, contact labelling and classification.

## 2026.10.4.12 {#v2026-10-4-12}

*October 4, 2026 · was 0.0.80*

- Single occurrence file logic inline and in file tab tracking.

## 2026.10.4.11 {#v2026-10-4-11}

*October 4, 2026 · was 0.0.79*

- Phase 1 to 4 of thread expansion plan, default thread open states and capped rendering length.

## 2026.10.4.10 {#v2026-10-4-10}

*October 4, 2026 · was 0.0.78*

- Perf plan phases 1 to 6, schema v38 dropping coalesces and thread key backfill, proper pagination and async folder counts.

## 2026.10.4.9 {#v2026-10-4-9}

*October 4, 2026 · was 0.0.77*

- Phase 1 to 3 of highlight plan, current row aria state and cross-list preview tracking.

## 2026.10.4.8 {#v2026-10-4-8}

*October 4, 2026 · was 0.0.76*

- Phase 1 to 4 of files plan, attached queries and occurrences tracking.

## 2026.10.4.7 {#v2026-10-4-7}

*October 4, 2026 · was 0.0.75*

- Maintenance only: tests, formatting and documentation brought up to date; nothing you would notice.

## 2026.10.4.6 {#v2026-10-4-6}

*October 4, 2026 · was 0.0.74*

- Phase 1 to 4 of logging plan, duration tracing for queries and api.

## 2026.10.4.5 {#v2026-10-4-5}

*October 4, 2026 · was 0.0.73*

- Phase 1 million-message scale split out message_raw table.

## 2026.10.4.4 {#v2026-10-4-4}

*October 4, 2026 · was 0.0.72*

- Maintenance only: tests, formatting and documentation brought up to date; nothing you would notice.

## 2026.10.4.3 {#v2026-10-4-3}

*October 4, 2026 · was 0.0.71*

- Ci: fix typecheck step and release artifacts download.

## 2026.10.4.2 {#v2026-10-4-2}

*October 4, 2026 · was 0.0.70*

- **AI:** Fixed: replace Unix-only syscall.Kill with os.Process.Kill for cross-platform build.

## 2026.10.4.1 {#v2026-10-4-1}

*October 4, 2026 · was 0.0.69*

- **Interface:** A rail you can arrange.

## 2026.10.3.4 {#v2026-10-3-4}

*October 3, 2026 · was 0.0.68*

- **Interface:** Windows the backend can see and point at.

## 2026.10.3.3 {#v2026-10-3-3}

*October 3, 2026 · was 0.0.67*

- **Log:** A log you can query via in:logs.

## 2026.10.3.2 {#v2026-10-3-2}

*October 3, 2026 · was 0.0.66*

- **Interface:** A threaded inbox by default.

## 2026.10.3.1 {#v2026-10-3-1}

*October 3, 2026 · was 0.0.65*

- **Annotators:** Laya classification engine.

## 2026.10.2.2 {#v2026-10-2-2}

*October 2, 2026 · was 0.0.64*

- **Query language:** Extracted values as grouping keys and aggregate paths.

## 2026.10.2.1 {#v2026-10-2-1}

*October 2, 2026 · was 0.0.63*

- **Annotators:** A starter pack you can switch off.

## 2026.10.1.1 {#v2026-10-1-1}

*October 1, 2026 · was 0.0.62*

- **Annotators:** Fixed: a progress bar that moves, and the starter pack in Settings.

## 2026.9.20.4 {#v2026-9-20-4}

*September 20, 2026 · was 0.0.61*

- **Annotators:** Triggers — when to drain a queue that already exists.
- **Annotators:** A starter pack, and annotators that remember their scope.
- **Viewer:** Run an annotator on the message you are reading.

## 2026.9.20.3 {#v2026-9-20-3}

*September 20, 2026 · was 0.0.60*

- **Annotators:** Extract from attachments, not just the body.
- **Annotators:** Correct one extracted value, and make corrections actually survive.
- **Viewer:** Show extracted values where they were found.

## 2026.9.20.2 {#v2026-9-20-2}

*September 20, 2026 · was 0.0.59*

- Maintenance only: tests, formatting and documentation brought up to date; nothing you would notice.

## 2026.9.20.1 {#v2026-9-20-1}

*September 20, 2026 · was 0.0.58*

- **Span extractor:** Check the weights against the checksum the repository publishes.
- **Annotators:** Create span extractors from the UI.
- **Annotators:** A plan says where a span extractor sends the mail.
- **Span extractor:** Record which weights produced each annotation.
- **Annotators:** Extract by scoring spans of the message, not by asking for JSON.

## 2026.9.19.2 {#v2026-9-19-2}

*September 19, 2026 · was 0.0.57*

- Fixed: support legacy charsets in email parser.

## 2026.9.19.1 {#v2026-9-19-1}

*September 19, 2026 · was 0.0.56*

- **Interface:** Add copy to clipboard button in error log.
- **Maintenance:** Run the health checks and their fixes from the UI.
- **Viewer:** Choose whether the Viewer tab is reused or split by kind.
- **Files:** A Files entry in the rail.
- Build: update embedded static frontend assets.
- **Files:** Similar: across files, over what is inside them.
- **Files:** Read scanned files with a vision model.
- **Files:** Read what is inside files, and search it.
- **Files:** Files on contact cards, tickets and in Desk results.
- **Files:** Preview files in the viewer, with their other copies.
- **Files:** Serve file bytes, with the posture that requires.
- **Files:** In:attachments, a first-class entity over files.
- **Files:** Extract on every ingestion path, and recover what was missed.
- **Contacts:** Private notes, custom tags, and responsiveness analytics.
- Build: update embedded static frontend assets for v0.0.50.

## 2026.9.6.1 {#v2026-9-6-1}

*September 6, 2026 · was 0.0.30*

- Build: update embedded static frontend assets for v0.0.30.
- **Interface:** Reorder settings tabs to prioritize profile and appearance.
- **Query language:** Implement query language, annotators, and fts5 search.
- Build: update embedded static frontend assets for v0.0.29.

## 2026.8.25.4 {#v2026-8-25-4}

*August 25, 2026 · was 0.0.29*

- **Settings:** Wire up data deletion button to clear local databases.
- Build: update embedded static frontend assets for v0.0.28.

## 2026.8.25.3 {#v2026-8-25-3}

*August 25, 2026 · was 0.0.28*

- **Settings:** Add new data management settings page.
- Build: update embedded static frontend assets for v0.0.27.

## 2026.8.25.2 {#v2026-8-25-2}

*August 25, 2026 · was 0.0.27*

- Build: update embedded static frontend assets for v0.0.26.

## 2026.8.25.1 {#v2026-8-25-1}

*August 25, 2026 · was 0.0.26*

- **Interface:** Implement infinite lazy scroll for mailbox list.
- Build: update embedded static frontend assets for v0.0.25.

## 2026.8.24.19 {#v2026-8-24-19}

*August 24, 2026 · was 0.0.25*

- **Interface:** Add keyboard navigation and shift multi-selection to message list.
- Build: update embedded static frontend assets for v0.0.24.

## 2026.8.24.18 {#v2026-8-24-18}

*August 24, 2026 · was 0.0.24*

- **Interface:** Display application version dynamically in UI components.

## 2026.8.24.17 {#v2026-8-24-17}

*August 24, 2026 · was 0.0.23*

- **Command line:** Add --dev flag to watch executable and auto-restart server on changes.
- Build: update embedded static frontend assets for v0.0.22.

## 2026.8.24.16 {#v2026-8-24-16}

*August 24, 2026 · was 0.0.22*

- **Interface:** Unify active toggle button style to standard primary background.
- Build: update embedded static frontend assets for v0.0.21.

## 2026.8.24.15 {#v2026-8-24-15}

*August 24, 2026 · was 0.0.21*

- **Interface:** Enhance toggle button selection background contrast in message viewer.
- Build: update embedded static frontend assets for v0.0.20.

## 2026.8.24.14 {#v2026-8-24-14}

*August 24, 2026 · was 0.0.20*

- **Interface:** Add distinct background colors and mode indicators for message view modes.
- Build: update embedded static frontend assets.

## 2026.8.24.13 {#v2026-8-24-13}

*August 24, 2026 · was 0.0.19*

- **Interface:** Add html, text, and raw view toggles to message viewer.

## 2026.8.24.12 {#v2026-8-24-12}

*August 24, 2026 · was 0.0.18*

- **Sign-in:** Refine password requirement and security warning flags.

## 2026.8.24.11 {#v2026-8-24-11}

*August 24, 2026 · was 0.0.17*

- **Sign-in:** Add --trust-local flag and INBOXQL_TRUST_LOCAL env var for explicit local trust.

## 2026.8.24.10 {#v2026-8-24-10}

*August 24, 2026 · was 0.0.16*

- Add e2e test suite, GitHub Actions CI/release workflows, and local auth refinements.

## 2026.8.24.9 {#v2026-8-24-9}

*August 24, 2026 · was 0.0.15*

- **Command line:** Add --open flag to iql start to launch browser tab.

## 2026.8.24.8 {#v2026-8-24-8}

*August 24, 2026 · was 0.0.14*

- **Sign-in:** Auto-authenticate localhost connections and enforce passwords for external interfaces.

## 2026.8.24.7 {#v2026-8-24-7}

*August 24, 2026 · was 0.0.13*

- **Storage:** Fixed: automatically migrate legacy iql.db database to inboxql.db.

## 2026.8.24.6 {#v2026-8-24-6}

*August 24, 2026 · was 0.0.12*

- **Command line:** Rename serve command to start with serve alias.
- **Command line:** Add ASCII art banner on server startup.

## 2026.8.24.5 {#v2026-8-24-5}

*August 24, 2026 · was 0.0.11*

- Maintenance only: tests, formatting and documentation brought up to date; nothing you would notice.

## 2026.8.24.4 {#v2026-8-24-4}

*August 24, 2026 · was 0.0.10*

- **Command line:** Add shell completion, structured logging, and interactive UI styling.
- Build: update embedded frontend static bundle for 0.0.9.

## 2026.8.24.3 {#v2026-8-24-3}

*August 24, 2026 · was 0.0.9*

- Rebrand UEA to InboxQL (iql) across codebase, CLI, and documentation.

## 2026.8.24.2 {#v2026-8-24-2}

*August 24, 2026 · was 0.0.8*

- **Command line:** Support --version and -v global flags.
- Build: update embedded frontend static bundle for 0.0.7.

## 2026.8.24.1 {#v2026-8-24-1}

*August 24, 2026 · was 0.0.7*

- Support folder hierarchy sync, message folders, and fix login input alignment.
- **Interface:** Render full Top Senders list as bar graph and update static assets.

## 2026.8.23.6 {#v2026-8-23-6}

*August 23, 2026 · was 0.0.6*

- **Interface:** Display full sender list with interactive bar graph counts.
- Build: update embedded frontend static bundle for 0.0.5.

## 2026.8.23.5 {#v2026-8-23-5}

*August 23, 2026 · was 0.0.5*

- Add error logging, iql errors CLI, and message tab viewer.
- Build: update embedded frontend static bundle for 0.0.4.

## 2026.8.23.4 {#v2026-8-23-4}

*August 23, 2026 · was 0.0.4*

- Add attachment blobstore, import job manager, and web import UI.
- Build: update embedded frontend static bundle for 0.0.3.

## 2026.8.23.3 {#v2026-8-23-3}

*August 23, 2026 · was 0.0.3*

- Add desktop email importer suite and upgrade nexus-shell integration.

## 2026.8.23.2 {#v2026-8-23-2}

*August 23, 2026 · was 0.0.2*

- **Command line:** Implement full CLI command suite, agent tools, LLM gateway, and mailer.
- Build: add install and uninstall targets to Makefile.

## 2026.8.23.1 {#v2026-8-23-1}

*August 23, 2026 · was 0.0.1*

- **Interface:** Clarify visual agent builder status as design preview and update password editing UX.
- **Security:** Fixed: implement credential vault, enforce IMAP TLS verification, and recover from sync worker panics.
- Enhance Visual AI Agent Builder and update project documentation.
- Introduce Eino Visual Agent Builder.
- Fixed: resolve UI layout issues and finalize filter pillbox components.
- Full stack UI integration with nexus-shell, authentication, and cross-filtering analytics.
- Initial project setup, multi-account sync engine (core logic, deduplication, db schema).
- Init commit with requirements.
