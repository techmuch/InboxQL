# A calendar in InboxQL

*Feature list and solution design. 11 October 2026.*

## The goal, stated precisely

"Feature parity with Outlook and Google Calendar" means two different things,
and only one of them is possible for a program that runs on your own machine:

- **Parity for you, at your desk** — viewing, creating, editing, scheduling,
  inviting, answering invitations, reminders, time zones, recurrence, finding
  a time, several accounts at once. All of it is buildable, and this plan
  builds it.
- **Parity as a cloud service** — a booking page strangers can open, a phone
  app, push to a phone when the laptop is shut, colleagues opening your
  calendar as your delegate. These need a server on the internet that is
  always on. InboxQL is deliberately not that. For these, InboxQL uses the
  provider's own feature (Google's appointment schedules, Microsoft Bookings,
  the provider's delegation) and shows its results, rather than imitating it
  badly.

The feature list below marks each item **Build**, **Through provider** (works
when the calendar lives at Google or Microsoft, using their feature) or
**Not doing**, with the reason.

## Where it starts

- Invitations already arrive by mail as `text/calendar` parts. The dev mailbox
  holds **36 messages carrying one**. They are stored as attachments and shown
  as a "calendar" file type; nothing reads them as events.
- The query language, the entity model (`in:mail drafts contacts attachments
  tickets logs`), the login service, the approval gate on outgoing mail and the
  annotation engines are all in place. A calendar is a new kind of entity in an
  existing design, not a second application.

## What makes this different from Outlook and Google

Parity is the floor. These are the reasons to use it here rather than there:

1. **Mail and calendar are one database.** An invitation is linked to its
   conversation; a meeting's card shows the thread that arranged it, the files
   sent about it and the people in it, with their open loops.
2. **The calendar is queryable.** `in:events attendee:*@acme.com after:90d |
   sum duration by week` is "how much of my time Acme took this quarter".
   Google's Time Insights answers a fixed handful of such questions; here it
   is any question.
3. **It reads events out of mail.** Flight, hotel, restaurant and delivery
   confirmations, and "can we meet Tuesday at 3?", become *proposed* events, the
   way extractors propose tickets — never silently added.
4. **Every account in one place**, Google, Microsoft and iCloud side by side,
   offline, on this machine.

## Feature list

### Views and navigation

| Feature | Outlook | Google | InboxQL |
|---|---|---|---|
| Day, work week, week, month views | ✓ | ✓ | **Build** |
| Schedule / agenda list | ✓ | ✓ | **Build** — as a query result, so it filters like anything else |
| Year view | ✓ | ✓ | **Build** |
| Custom range (N days) | ✓ | ✓ | **Build** |
| Mini-month navigator | ✓ | ✓ | **Build** |
| Several calendars overlaid, colour-coded, toggled | ✓ | ✓ | **Build** |
| Calendars side by side (split view) | ✓ | — | **Build** |
| Go to date, today, keyboard navigation | ✓ | ✓ | **Build** — arrow keys like every list here |
| Secondary time zone column | ✓ | ✓ | **Build** |
| Week numbers, first day of week, hide weekends, working hours shading | ✓ | ✓ | **Build** |
| Declined events shown dimmed or hidden | ✓ | ✓ | **Build** |
| Print | ✓ | ✓ | **Build** — a print stylesheet for any view |
| Dark mode and themes | ✓ | ✓ | **Build** — inherits the app's themes |

### Events

| Feature | Outlook | Google | InboxQL |
|---|---|---|---|
| Create by click or drag; move and resize by drag | ✓ | ✓ | **Build** |
| Quick create, then full editor | ✓ | ✓ | **Build** |
| Natural-language create ("lunch with Ada Friday 1pm") | — | partial | **Build** — local parse; an LLM only if one is set up |
| All-day and multi-day events | ✓ | ✓ | **Build** |
| Title, location, description with formatting, links | ✓ | ✓ | **Build** |
| Attachments on events | ✓ | ✓ (Drive) | **Build** — the file entity, so the same file is one file |
| Show as busy / free / tentative / out of office / working elsewhere | ✓ | ✓ | **Build** |
| Private events | ✓ | ✓ | **Build** |
| Categories / colours per event | ✓ | ✓ | **Build** — categories are labels, queried as `label:` |
| Duplicate, copy to another calendar, move between calendars | ✓ | ✓ | **Build** |
| Undo | ✓ | ✓ | **Build** — every change is an entry in a change log |
| Default duration, "speedy meetings" (end early) | ✓ | ✓ | **Build** |
| Event from an email | ✓ | ✓ | **Build** — from any message, prefilled from it |
| Email the attendees | ✓ | ✓ | **Build** — a draft to everyone, through the outbox |

### Recurrence and time

| Feature | Outlook | Google | InboxQL |
|---|---|---|---|
| Daily, weekly, monthly, yearly; by weekday, by nth weekday, last day | ✓ | ✓ | **Build** — full RFC 5545 RRULE, RDATE, EXDATE |
| Edit or delete one occurrence / this and following / all | ✓ | ✓ | **Build** |
| Event in its own time zone; start and end in different zones | ✓ | ✓ | **Build** |
| Travel: display in local time when the machine's zone changes | ✓ | ✓ | **Build** |
| World clock | ✓ | ✓ | **Build** |
| Correct across daylight-saving changes | ✓ | ✓ | **Build** — IANA zones embedded; Windows zone names mapped |

### Invitations and scheduling

| Feature | Outlook | Google | InboxQL |
|---|---|---|---|
| Invite attendees; required and optional | ✓ | ✓ | **Build** |
| Accept / tentative / decline, with or without a note | ✓ | ✓ | **Build** |
| Propose a new time | ✓ | ✓ | **Build** (iTIP COUNTER) |
| Attendee responses tracked on the event | ✓ | ✓ | **Build** |
| Forward an invitation | ✓ | ✓ | **Build** |
| Updates and cancellations applied automatically | ✓ | ✓ | **Build** — and shown, never silent |
| Invitations from people on other systems (email invites) | ✓ | ✓ | **Build** — this works even with no calendar account at all |
| Guests can modify / invite others / see the list | — | ✓ | **Build** where the provider supports it |
| Free/busy of colleagues; scheduling assistant | ✓ | ✓ | **Through provider** — Graph `getSchedule`, Google `freebusy`, CalDAV scheduling |
| Find a time across attendees | ✓ | ✓ | **Build** on that free/busy |
| Rooms and resources, room finder | ✓ | ✓ | **Through provider** — needs the organisation's directory |
| Online meeting link added automatically (Teams, Meet, Zoom) | ✓ | ✓ | **Through provider** for Teams and Meet; a link field and detection for anything else |
| Join button from the event | ✓ | ✓ | **Build** |
| Auto-decline during out of office | ✓ | ✓ | **Build** — as a proposal you confirm, see the agent rule below |

### Your time

| Feature | Outlook | Google | InboxQL |
|---|---|---|---|
| Working hours and working location | ✓ | ✓ | **Build**, synced where the provider has the field |
| Out of office | ✓ | ✓ | **Build** |
| Focus time | ✓ | ✓ | **Build** as an event type; DND is the OS's business |
| Time insights (how time was spent) | Viva | ✓ | **Build** — every question the query language can ask |
| Holidays calendars | ✓ | ✓ | **Build** — subscribed ICS |
| Birthdays from contacts | ✓ | ✓ | **Build** — from contacts that carry a birthday |
| Tasks on the calendar | To Do | Tasks | **Build** — tickets with a due date appear on it; provider tasks later |

### Reminders

| Feature | Outlook | Google | InboxQL |
|---|---|---|---|
| Reminders, several per event, default per calendar | ✓ | ✓ | **Build** |
| Notification on this computer | ✓ | ✓ | **Build** — browser notification, and the OS notification from the login service when no window is open |
| Snooze, dismiss | ✓ | ✓ | **Build** |
| "Time to leave" from location | ✓ | ✓ | **Not doing** — needs live traffic data from a third party |
| Notifications on a phone | ✓ | ✓ | **Through provider** — the phone app of the calendar the event lives in |
| Email reminders | ✓ | ✓ | **Through provider** |

### Calendars and accounts

| Feature | Outlook | Google | InboxQL |
|---|---|---|---|
| Several accounts at once | ✓ | partial | **Build** — Google, Microsoft, iCloud, Fastmail, any CalDAV, side by side |
| Local calendars with no server | ✓ | — | **Build** |
| Subscribe to an ICS URL | ✓ | ✓ | **Build** |
| Import and export .ics | ✓ | ✓ | **Build** |
| Share a calendar, set permissions, delegate access | ✓ | ✓ | **Through provider** — sharing is something other people's servers act on |
| Open someone else's shared calendar | ✓ | ✓ | **Build** — it is a calendar the account can already see |
| Publish a public calendar | ✓ | ✓ | **Through provider** |
| Booking page / appointment schedule for outsiders | Bookings | ✓ | **Through provider** — strangers cannot reach a server on your laptop, and should not |
| Offline | partial | — | **Build** — it is all local; changes queue until online |
| Search events | ✓ | ✓ | **Build** — the query language |

### Beyond both

| Feature | InboxQL |
|---|---|
| Invitation linked to its mail thread, and the thread to the meeting | **Build** |
| Meeting card: the thread, the files, each attendee's contact card and open loops | **Build** |
| Events proposed from mail — travel, reservations, "can we meet Tuesday" | **Build** — proposed, accepted by you, like tickets |
| Queryable time — `\| sum duration by attendee`, `\| series duration by week` | **Build** |
| An agent that can see the calendar and draft changes | **Build** — drafts only; see below |

## How it is built

### The entity

A calendar event is a new kind, `in:events`, beside mail, contacts, files and
tickets.

- **iCalendar is the source of truth.** Each event keeps its raw `VEVENT`
  (with its `VTIMEZONE`s), the way a message keeps its raw RFC 822. Everything
  a provider sends survives a round trip, including fields InboxQL does not
  understand — the most common way calendar clients corrupt each other's data
  is by dropping what they don't model.
- **Derived columns for querying**: start, end, all-day, time zone, status,
  transparency, organiser, calendar, location, conference link, recurrence
  master/override.
- **Occurrences are materialised** for a rolling window (a year back, two
  ahead) from the recurrence rules, and re-expanded when a rule or exception
  changes. Views and queries read occurrences; editing reads the master.
- **Attendees are participant edges**, the same mechanism that makes contacts:
  every attendee is a contact, so a contact card can say "next meeting
  Thursday" and "met 14 times this year".
- Schema: `calendars`, `events`, `event_occurrences`, `event_attendees`,
  `event_alarms`, `calendar_changes` (the outgoing queue and undo log). One
  migration, with the usual backup first.

### Where events come from

| Source | Protocol | Notes |
|---|---|---|
| Invitations in mail | iMIP (RFC 6047) over the existing sync | Works with no calendar account. REQUEST, REPLY, CANCEL, COUNTER update the event. |
| iCloud, Fastmail, Nextcloud, any CalDAV | CalDAV (RFC 4791), sync-collection (RFC 6578), scheduling (RFC 6638) | App-specific passwords, like IMAP today. |
| Microsoft 365 and Outlook.com | Microsoft Graph, delta queries | OAuth 2.0 (device-code flow). EWS is being retired; Graph is the only future-proof path. |
| Google Calendar | Google Calendar API, sync tokens | OAuth 2.0. The API rather than Google's CalDAV, because Meet links, working location, focus time and event types only exist there. |
| Subscriptions | ICS over HTTPS | Read-only, refreshed on a schedule. |
| Local | none | Calendars that live only in the mailbox. |

**OAuth is the largest new piece.** InboxQL authenticates with passwords today;
Google and Microsoft calendars cannot be reached that way. The plan uses the
device-code flow (open a URL, enter a code — no redirect to a local server
needed), stores refresh tokens in the existing vault, and needs an OAuth
client registered with each of Google and Microsoft. Google treats calendar
access as a sensitive scope: an app used by more than a hundred people must
pass Google's verification. Until then, users can supply their own client ID.
**Registering those clients is yours to do**, like publishing releases.

### Two-way sync

- Pull with each provider's incremental mechanism (CalDAV sync tokens, Graph
  delta, Google sync tokens); full resync only when a token expires.
- Local changes go into `calendar_changes` first and are pushed with the
  provider's ETag. When the server changed meanwhile, the change is not forced:
  the event shows both versions and you choose. Silent last-write-wins is how
  calendars lose meetings.
- Sync rides the existing sync scheduler and the battery rule: polling slows on
  battery; reminders never do.

### Invitations, and the approval gate

Answering an invitation, or sending one, tells other people something. The
existing rule is that **mail an agent wrote goes nowhere until a person
approves it**, and the calendar keeps that rule exactly:

- **You, in the interface**: Accept, Decline, Send invites — they happen, as in
  any calendar. You pressed the button.
- **An agent**: can read the calendar, find times, and *draft* an event, an
  RSVP or a reschedule. Drafts land in a **proposed** state, shown on the
  calendar hatched, with `origin: agent`. Nothing reaches another person until
  you accept the draft — the same shape as proposed tickets and the outbox.
  `iql calendar approve` refuses without a terminal, like `outbox approve`.
- **Automatic rules** (auto-decline during out of office) are proposals too,
  until you switch a rule to automatic yourself.

### The query language

```
in:events after:today before:7d                  this week
in:events attendee:*@acme.com after:90d | sum duration by week
in:events status:needs-action                    invitations waiting on you
in:events organizer:me() -has:conference         meetings I set up with no link
in:events calendar:work during:2026-10-15T14:00  what is on at that moment
in:events | count by attendee                    who I meet most
thread:<message-id>                              mail, and now the meeting it set up
```

New fields: `calendar:` `attendee:` `organizer:` `status:` (accepted, tentative,
declined, needs-action), `show:` (busy, free, tentative, oof), `location:`
`has:` (conference, attendees, reminder, recurrence) `during:` `duration>`.
Stages already work: `count`, `top`, `series`, and `sum duration`. The
calendar views are queries too, so a saved query can be a calendar: "client
meetings" as a view of its own.

### The interface

- **A Calendar tab** in the shell: views, the mini-month, the calendar list with
  colours and toggles, drag to create, move and resize, keyboard throughout.
- **An event card** like the contact card: details, attendees with their
  responses and contact cards, the mail thread, the files, Join, and the
  history of changes.
- **Invitations in the Desk**: a message carrying an invitation shows it as a
  card with Accept / Tentative / Decline / Propose time and the clash with your
  calendar, so you rarely need to leave the mail.
- **The rail** gets "Invitations" (`in:events status:needs-action`) and
  "Today".
- **Find a time**: attendees' free/busy as rows, suggested slots, working hours
  respected.

### Reminders

A scheduler in the server fires alarms from the occurrences table. With a
window open it uses browser notifications; with none open, the login service
posts an OS notification (macOS, Windows toast, Linux notify-send). A laptop
asleep fires nothing, and on waking shows what it missed rather than a
stampede of stale alerts.

### Events from mail

The annotation system already proposes tickets from mail. The same path
proposes events:

- **Structured** confirmations first — schema.org markup that airlines, hotels
  and booking sites embed in their mail, which is exact and free.
- **Text** next — "can we meet Tuesday at 3?" — a GLiNER extractor for dates,
  times and places, gated by a Laya label ("this message proposes a meeting"),
  as `receipts` is gated by `purchased`.
- Either way the result is a **proposed** event linked to the message; you accept
  or reject it, and the rejection is kept as evidence the extractor was wrong.

### What it uses

- `github.com/emersion/go-ical` and `go-webdav` (CalDAV client), from the same
  author as the IMAP library already in use.
- An RRULE engine (`github.com/teambition/rrule-go`), tested hard against
  RFC 5545's own examples and Outlook's and Google's edge cases.
- Go's embedded IANA zone database, plus the CLDR Windows-to-IANA map for
  Outlook's zone names.
- Frontend: views built here on the existing shell, not a calendar widget
  library. Every widget library brings its own theme and its own keyboard
  model, and both would fight the rest of the app.

## Phases

| Phase | Delivers | Needs |
|---|---|---|
| **1. Events from what is already here** | `in:events`; invitations in mail parsed into events; ICS import, export, subscriptions; local calendars; day/week/month/agenda views; the event card linked to its thread | Nothing new from you |
| **2. Recurrence and time zones, properly** | Full RRULE with exceptions, "this and following", Windows zones, secondary zone, travel | — |
| **3. CalDAV two-way** | iCloud, Fastmail, Nextcloud: create, edit, delete, conflicts | App passwords, as for IMAP |
| **4. Invitations out** | Invite, RSVP, propose time, cancellations, through the provider or by iMIP mail; the agent-proposal gate | — |
| **5. Microsoft and Google** | OAuth device flow; Graph and Calendar API sync; Teams/Meet links; free/busy and find a time; rooms | **OAuth clients registered by you** |
| **6. Reminders** | Alarm scheduler, browser and OS notifications, snooze | — |
| **7. Your time and intelligence** | Working hours, OOO, focus time, holidays, birthdays, tickets on the calendar; events proposed from mail; insights; agent tools and skill update | — |

Phase 1 is useful on its own, and it is the one only InboxQL can do: everyone
who has ever been sent an invitation has a calendar in their mailbox already.

## Decisions that are yours

1. **Which accounts first.** If your calendar is Google or Microsoft, phase 5
   matters most to you, and it is the one gated on registering OAuth clients.
2. **OAuth clients.** Register a Google Cloud and a Microsoft Entra app, or
   have each user bring their own client ID until the Google verification is
   done.
3. **Whether a human RSVP from the interface should also go through the
   outbox.** This plan says no — pressing Accept is the person acting — and
   keeps the gate for agents and automatic rules.

## Not doing

- **Booking pages for outsiders**, a phone app, push to a phone, acting as
  someone's delegate host: they need an always-on public server. Use the
  provider's.
- **Time to leave**: needs live traffic from a third party.
- **Exchange on-premises via EWS**: Microsoft is retiring it; Graph only.


---

# Execution plan — Google first, then Exchange Server

*11 October 2026. Supersedes the phase table above for order.*

Two facts checked before starting:

- **Google's device-code flow does not allow calendar scopes** (its allowlist is
  sign-in, two Drive scopes and YouTube). The desktop-app flow does: a redirect
  to a loopback address with PKCE, on any port. InboxQL is already a server on
  loopback, so the redirect comes back to it — no second listener. The client
  secret of a desktop client is not confidential by Google's own description.
- **EWS is retiring only in Exchange Online** (phased from 1 October 2026, off
  1 April 2027). Exchange Server on-premises keeps EWS, fully supported. So
  "Exchange Server" is EWS; Exchange *Online* needs Microsoft Graph and is a
  separate, later piece.

## Phase A — the entity
Schema v41: `calendars`, `events` (raw iCalendar kept), `event_occurrences`,
`event_attendees`, `calendar_changes`. `internal/calendar`: iCalendar parsing,
RRULE expansion with exceptions and time zones (Windows zone names mapped).
A local calendar always exists.

## Phase B — what is already in the mailbox
Invitations in mail (iMIP REQUEST / CANCEL / REPLY) become events, linked to
their message; ICS import and export. Backfilled from stored mail.

## Phase C — `in:events`
Query fields, stages (`sum duration`), `iql calendar` commands, API.

## Phase D — the interface
Calendar tab: day, week, month, agenda; calendar list; create/edit/delete; the
event card with its thread; the invitation card in the message viewer; rail
entries.

## Phase E — Google Calendar
OAuth (loopback + PKCE, client ID supplied in Settings, tokens in the vault);
calendar list; incremental sync with sync tokens; create, edit, delete with
ETags and conflict surfacing; RSVP; Meet link on request.

## Phase F — Exchange Server
EWS: Autodiscover-free URL configuration, Basic and NTLM auth over TLS,
FindItem/CalendarView sync with SyncFolderItems, create/update/delete, RSVP.

## Phase G — reminders and the agent gate
Alarm scheduler, browser notifications; agent-made changes are proposals that
only a person turns into real ones.

## Designed for, not built: time tracking
People who bill time need to say **who each block of time is charged to**. The
event model carries what that needs from the start, so it is a feature to add,
not a migration to fight:
- a `charge` on an event occurrence (not the series — a recurring meeting can
  be billed to different clients week to week), stored with who set it and
  whether it was suggested or confirmed;
- charge codes as their own small entity (client, project, code, rate,
  active), so `| sum duration by charge` is the timesheet;
- suggestions from what is already known: the attendees' domains, the
  contact's labels, the mail thread's labels — proposed, confirmed by the
  person, like tickets.
Not built in this plan; the columns and the query field are reserved.

---

# What was built

All of phases A–G, Google first, then Exchange Server. Go suite green (race-clean
on the new packages), 210 frontend tests, real typecheck; the calendar checked
in a browser on a scratch mailbox — never the dev one, which is still at v40.

### A — the entity (schema v41)
`calendar_accounts`, `calendars`, `events` (raw iCalendar kept), `event_attendees`,
`event_occurrences` (with `charge` / `charge_source` reserved for time tracking),
`calendar_changes`, `draft_calendar`. `internal/calendar`: parsing and writing
iCalendar, IANA and Windows zone names (the full CLDR default map), custom
`VTIMEZONE` fallback, RRULE/RDATE/EXDATE expansion with overrides, a generated
`VTIMEZONE` on export (US and EU rules checked), `RemainingCount` for splitting
a counted series.

### B — invitations already in the mailbox
Every `text/calendar` part, inline or attached, applied by method: REQUEST
(stale sequences ignored), CANCEL (kept, marked), REPLY (attendee status). A
cursor means only new mail is read after the first pass. An invitation also in
a synced calendar is shown once, from the synced copy.

### C — `in:events`
One row per occurrence. Fields `next: during: calendar: attendee: organizer:
status: show: title: location: duration has: is: charge:`; `| count by`,
`| top`, `| sum hours by week|attendee|calendar|domain`. Proposals excluded
unless `is:proposed`. `duration>1000` alone still means the log.

### D — the interface
Calendar tab (Ctrl+Shift+Y): day/week/month/agenda, mini-month, calendar
list, click-to-create, drag to move (across days) and resize, the
this / following / all question for series, event card with RSVP, Join,
attendees and the mail thread, Waiting-for-you with Approve/Reject. The
invitation card in the message viewer, with clashes. `in:events` results on the
Desk.

### E — Google Calendar
Desktop-app OAuth with PKCE (the device flow does not allow calendar scopes),
redirect to `127.0.0.1` on InboxQL's own port, the callback outside the auth
middleware and guarded by a single-use state; refresh token in the vault.
Calendar list (free/busy-only calendars skipped), sync tokens with 410 →
full resync, recurring masters + instances, create/update/delete with
`If-Match`, a single occurrence found through `instances?originalStart`
(Google does not document instance ids), RSVP sending the whole attendee list.
Tested against a fake Google; **not yet against real Google** — that needs the
OAuth client registered (yours to do).

### F — Exchange Server (EWS, on-premises)
NTLM or Basic over TLS; GetFolder/FindFolder; SyncFolderItems + GetItem with
MimeContent (iCalendar), so series and exceptions come through the same
parser; CreateItem in schema order with RRULE → EWS patterns (refusing what
EWS cannot express), UpdateItem with ChangeKey conflicts, DeleteItem with
cancellations, Accept/Tentative/Decline items; an occurrence found by its
`OriginalStart` in a CalendarView. Tested against a fake Exchange; **not yet
against a real server.** Exchange Online is not supported (EWS retiring there).

### G — reminders and the agent gate
Alarms delivered to every open window (in-app, and a browser notification once
allowed), the OS notification when none is open (macOS, Linux), nothing older
than ten minutes after sleep. An agent's change is a proposal; approval is the
Calendar tab or `iql calendar approve`, which exits 4 without a terminal; the
HTTP approve refuses a request that did not come from the page. Answering a
mail-only invitation queues an iMIP REPLY through the outbox.

### Found and fixed on the way
- `| count by week` labelled most days with the *previous* week's Monday —
  for mail too. Fixed in the shared bucket expression, tested.
- `SEQUENCE;VALUE=TEXT` and `URL;VALUE=TEXT` written by the encoder; fixed.
- A stored event's recurrence slice was shared with its copies; "this and
  following" rewrote the rule it was copying. `Model()` now copies.
- One unreadable calendar stopped every calendar after it from syncing; each
  is now pulled independently.
- Invitation clashes came back as JSON `null` and crashed the message viewer.

### Not done
- Real-provider testing (needs your Google OAuth client and an Exchange server).
- Exchange Online / Microsoft Graph; CalDAV (iCloud, Fastmail); free/busy and
  find-a-time; rooms; working hours / OOO; holidays and birthdays calendars;
  events proposed from mail by extractors; Windows OS notifications.
- **Time tracking** — designed for (occurrence-level `charge`, `charge:` in the
  language, `| sum hours by charge`), not built, as planned.
