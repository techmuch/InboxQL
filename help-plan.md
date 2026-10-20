# Help that knows the application

*19 October 2026.*

The guides and the application did not point at each other. A guide could say
"Settings → General → Color theme" but not take you there; a screen could not
say which guide explains it, except in three places written by hand; and there
were no pictures.

The idea throughout is a stable id that a test checks against the thing it
names, so a renamed setting or heading fails the build instead of quietly
leaving a dead link.

## Phase 1 — guides link into the application

- `[Color theme](#setting/theme)` opens Settings at that setting, through
  `openSetting`; `[Annotators](#open/annotators)` opens a tab.
- On the website, where neither can open anything, the link renders as bold
  text, so the written path ("Settings → General → **Color theme**") still
  reads.
- A test fails on a `#setting/` that names no setting, or an `#open/` that
  names no place.

## Phase 2 — "?" marks, and the registry behind them

- `frontend/src/lib/helpTopics.ts` lists every place that has help: where it
  is in the application, and the guide and heading that explain it. It is the
  inventory.
- `<Help topic="…" />` draws a small "?" that opens that section.
- Tests: every topic names a guide and heading that exist (Go, against the
  real renderer); every topic is used, and every `<Help>` names a topic.
- The three hand-written links move onto it.

## Phase 3 — the inventory, placed

One "?" per panel or section, not per control. Where a place had no section
to point at, the guide got one: **Links and images**, **Windows**, **The
Log** and **The Dashboard** in the Desk guide.

| Where | Topic | Guide section |
|---|---|---|
| Desk, the rail | `rail` | desk#the-rail |
| Desk, what Trash and Spam held back | `trash-and-spam` | query-language#trash-and-spam-stay-out-unless-you-ask |
| Desk, the query bar | `query-bar` | query-language |
| Message viewer, the Load images bar | `load-images` | desk#links-and-images |
| Message viewer, labels from annotators | `message-labels` | labels#reading-the-answers |
| Waiting for approval | `approvals` | desk#waiting-for-approval |
| Contact card | `contact-card` | contacts#what-a-contact-card-shows |
| File viewer | `file-tab` | files#the-file-tab |
| Calendar | `calendar` | calendar#the-views |
| Calendar, an event | `event-card` | calendar#an-events-card |
| Board | `board` | tickets#the-board |
| Annotators | `annotators` | labels |
| Log | `log` | desk#the-log |
| Windows | `windows` | desk#windows |
| Dashboard | `dashboard` | desk#the-dashboard |
| Settings, User Profile | `settings-profile` | settings#user-profile |
| Settings, General | `settings-general` | settings#general |
| Settings, System | `settings-system` | settings#system |
| Settings, Mail Accounts | `settings-accounts` | accounts-and-import#adding-an-account |
| Settings, Calendars | `settings-calendars` | calendar#connecting-google-calendar |
| Settings, Import Mail | `settings-import` | accounts-and-import#importing-from-apple-mail |
| Settings, AI Configuration | `settings-ai` | settings#ai-configuration |
| Settings, Logging | `settings-logging` | settings#logging |
| Settings, Maintenance | `settings-maintenance` | settings#maintenance |
| Settings, Data Management | `settings-data` | settings#data-management |
| Settings, Browser Storage | `settings-storage` | settings#browser-storage |
| Settings, Mail Accounts, adding Gmail | `gmail-app-password` | accounts-and-import#gmail |
| Settings, AI Configuration, Starters | `ai-starters` | labels#somewhere-to-start |

Left out, on purpose:

- **Review** and **New message** are dialogs over the whole window, so a guide
  opened from them would open underneath. The Annotators "?" reaches Review's
  section in the same guide.
- **Security** in Settings is a placeholder with nothing to explain yet.
- In Settings the "?" sits beside the open section's name in the sidebar, not
  on the page, where it landed on the sections' own buttons.

## Phase 4 — screenshots

- `make screenshots` (scripts/screenshots.sh) starts the devmail harness on a
  new mailbox in a temporary folder, seeds it — a year of made-up mail, an
  order confirmation with a logo carried in the message, a phishing-shaped
  link, a reply an agent queued — and drives Chrome with
  frontend/scripts/screenshots.mjs (playwright-core: no browser download).
- The harness runs with a **home folder of its own**. The first run showed the
  System section reading this machine's settings file and login service; the
  System picture is also cropped above the Login service card, because
  launchctl answers for the machine whatever the home folder says.
- Twelve scenes, light and dark, WebP: 24 files, about 640 KB. Sparse screens
  are cropped to what matters so they read at the guide's width.
- Embedded with the guides (`docs/images/*.webp`), served at
  `/api/docs/images/…`, copied beside the pages by docsgen. A guide writes
  `![…](images/desk.webp)`; both themes are rendered and CSS shows the one
  matching the application's theme, or the reader's on the site.
- Tested: every picture a guide shows exists in both themes, and every file
  is shown somewhere.

## Phase 5 — checked

In Chrome against devmail: the Load images "?" opens the Desk guide at Links
and images; `#setting/theme` opens Settings at the theme; the Settings
sidebar "?" opens the guide at General; `#open/log` opens the Log; light shows
the light pictures and dark the dark ones, each loaded. Every test passes.

## Status

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5
