# Calendar

InboxQL has a calendar, and you may already have one in it. Every invitation
you have ever been sent arrived as mail with a calendar part attached; InboxQL
reads those into an **Invitations** calendar on its own, with no account to
connect. Connect Google Calendar or an Exchange Server, and their calendars
appear beside it.

Open it from **Go → Calendar** (**g c**). **File → New event** starts one from
anywhere, and **File → Import calendar** and **Export calendar** move `.ics`
files in and out of your own calendar.

## The views

**Day**, **week**, **month** and **agenda**, with today, the previous and the
next period in the toolbar. On the keyboard: **t** for today, **←** and **→**
to move, **d w m a** for the views, **n** for a new event.

- **Click an empty slot** to start an event there.
- **Drag an event** to move it — to another day in the week view — and drag its
  bottom edge to change its length. Everything snaps to a quarter of an hour.
- **Click an event** for its card.

Each calendar has a colour, and the list on the left shows or hides it. Some
events look different on purpose:

| Looks | Means |
|---|---|
| striped | an invitation you have not answered, or a proposal waiting for you |
| faded and struck through | you declined it |
| no colour block in the month view | an all-day event |

**Cards due** is the board's due dates, drawn as all-day entries: click one to
open the card, drag it to another day to change when it is due, and hide them
like any calendar. See [Your board](tickets.md#on-the-calendar).

## An event's card

When and where, how it repeats, the **Join** button for a Meet, Teams, Zoom or
Webex link, who is invited and how each has answered, and — for an invitation
— **Yes**, **Maybe** and **No**. If the event came by mail, the card links to
the message, and the message shows the event: **an invitation can be answered
from the message it came in**, with what else is on at that time beside it.

## Creating and editing

**New event**, a click on an empty slot, or **n**. Title, times (or all day),
how it repeats, which calendar, people, where, a meeting link, a description,
a reminder, whether it shows you as busy, and whether it is private.

Changing a repeating event asks what the change is for, because these are three
different edits:

- **Only this one** — this occurrence changes; the rest stay.
- **This and the ones after** — the series ends before this one, and a new
  series carries on from it with your change.
- **All of them** — the whole series.

The same question comes when you drag an occurrence of a repeating event, or
delete one.

## Answering invitations

**Yes**, **Maybe** or **No** on the card, or on the message.

- For an event in a **Google** or **Exchange** calendar, the answer goes through
  that provider, which tells the organizer.
- For an invitation that only arrived **by mail**, the answer goes back by mail,
  as a calendar reply the organizer's calendar understands. It waits in the
  outbox until you send it, from **Intelligence → [Waiting for approval](#open/approvals)** or
  with `iql outbox approve <draft-id>`.

## Connecting Google Calendar

Google lets an app read and change your calendar only through a sign-in with
an *OAuth client*. Until InboxQL has a verified client of its own, you use one
from your own Google Cloud project — about five minutes, once:

1. In the [Google Cloud console](https://console.cloud.google.com/), create a
   project, and enable the **Google Calendar API** for it.
2. Under **OAuth consent screen**, choose *External*, fill in the names, and add
   your own Google address as a **test user**.
3. Under **Credentials**, choose **Create credentials → OAuth client ID**, of
   type **Desktop app**.
4. In InboxQL, **Settings → Calendars → [Connect Google](#setting/google-calendar)**, paste the client ID
   and secret, and sign in.

Google signs you in in a new tab and sends you back to this computer. InboxQL
keeps a refresh token, encrypted in your mailbox's vault, and talks to Google
directly — nothing goes through anyone else.

From a terminal:

```sh
INBOXQL_GOOGLE_CLIENT_SECRET=… iql calendar connect google --client-id 1234-abcd.apps.googleusercontent.com
```

Every calendar you can read appears: your own, shared ones, and subscriptions
like holidays. Ones shared with you as *free/busy only* do not, because there
is nothing in them to show.

## Connecting Exchange Server

For an Exchange Server your organisation runs itself. **Settings → Calendars →
Connect Exchange**: the EWS address (usually
`https://mail.example.com/EWS/Exchange.asmx`), your username (`DOMAIN\user` or
your address), password, your address, and the sign-in method — *Windows sign-in
(NTLM)* for most servers, *Basic* if your administrator says so. InboxQL tests
the sign-in before saving anything.

```sh
INBOXQL_EXCHANGE_PASSWORD=… iql calendar connect exchange \
  --url https://mail.example.com/EWS/Exchange.asmx --username 'CORP\me' --email me@example.com
```

**Exchange Online (Microsoft 365) is not supported this way.** Microsoft is
switching EWS off there — from October 2026, entirely by April 2027 — and an
Exchange Online calendar needs a different connection, which is planned.

## Sync

Connected calendars sync every five minutes, after every mail sync, and
whenever you press **Sync now**. Changes you make go up on the next pass —
straight away in practice.

If the same event was changed somewhere else before your change arrived,
InboxQL does not overwrite it. The calendar shows the other version, and
**Waiting for you** lists your change as a conflict, so you can make it again
if you still want it.

An invitation that is in both your mail and a connected calendar shows once,
from the calendar, which is the copy that can change.

## Reminders

Set one on any event. When it comes due, every open InboxQL window shows it,
with **Join** and **Open**; press **Allow notifications** once and the browser
shows it as a system notification too, even when the tab is in the background.
With no window open, InboxQL asks your operating system to show it (macOS and
Linux).

A computer that was asleep does not wake to a pile of old reminders: only ones
that came due in the last ten minutes are shown.

## Asking questions

The calendar is a query like everything else — one row per occurrence:

```
in:events next:7d                                  what is on this week
in:events status:needs-action                      invitations you have not answered
in:events attendee:*@acme.com after:90d | sum hours by week
in:events organizer:me() -has:conference           meetings you set up with no link
in:events | sum hours by attendee                  who your time goes to
in:events during:2026-10-15T14:00                  what is on at that moment
```

[The query language](query-language.md#calendar-events) has every field.

## Agents and the calendar

An AI agent driving InboxQL can read the calendar and find times, and it can
**propose** changes — a new event, a move, a cancellation, an answer to an
invitation. A proposal changes nothing and tells nobody: it shows on the
calendar, striped, and under **Waiting for you**, until you **Approve** or
**Reject** it. From a terminal, `iql calendar approve <id>` — which works only
at a terminal, typed by a person.

## On the command line

```sh
iql calendar agenda --next 7d
iql calendar create --title "Planning" --start "2026-10-15 10:00" --duration 90m --repeat "FREQ=WEEKLY;BYDAY=MO"
iql calendar respond <event-id> accepted
iql calendar changes
iql calendar import holidays.ics
iql calendar export --calendar local --out mine.ics
```

`iql help calendar` lists everything.

## Coming next: time tracking

For people who bill their time, the calendar is most of a timesheet already.
The next piece lets you say **who each meeting is charged to** — per
occurrence, since a weekly call can be billed to a different client each week —
with suggestions from the people in the meeting and the mail it came from,
and `| sum hours by charge` as the timesheet. The data model already has a
place for it.
