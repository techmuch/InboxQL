# Calendar, next: availability, rooms, time away, holidays, birthdays, suggestions

*A phased plan. 12 October 2026. Follows `calendar-plan.md`.*

## What was checked first

- **Google working hours are not in the API.** Out-of-office, focus time and
  working location are, as event types, but only on primary calendars and only
  for some users; out-of-office and focus time must be timed, not all-day.
- **Google rooms can only be listed by an administrator.** The Directory API's
  calendar-resource scope is an admin scope. A non-admin can still book a room
  whose address they have, and see its free/busy if the organisation shares it,
  which most do.
- **Exchange Server does all of it for a regular user:** `GetRoomLists` and
  `GetRooms` list rooms, and `GetUserAvailability` returns free/busy and working
  hours for people, rooms and resources. It only takes whole days, starting
  and ending at midnight.
- **What exists here today:** contacts have no birthday field. Messages keep
  their HTML (`html_body`), which is where airline and hotel confirmations carry
  their schema.org data. Occurrences already know busy or free, and declined or
  not. The suggestion pattern (a model proposes, a person rules, a rejection is
  kept) is the one tickets and agent proposals already use.

## The two principles this plan keeps

1. **Availability, never details.** Finding a time uses free/busy blocks:
   busy, tentative, away, or unknown. It never uses other people's titles,
   attendees or notes, and it never shows yours to anyone.
2. **Anything that tells another person something is a person's act.** Booking
   a room, setting an auto-reply, declining meetings while away, and adding a
   suggested event to a shared calendar all follow the existing rule. You
   pressing the button does it. An agent or an automatic rule proposes it.

## Phase 1: working hours

The base everything else stands on. "Find a time" means within working hours.

- **The setting.** Hours for each weekday, with more than one range allowed
  (9–12, 13–17), a home time zone, and an optional lunch break. Stored in the
  mailbox (schema v42: `calendar_settings`), one per calendar account, with a
  default for the user.
- **From the provider where possible.** Exchange gives your own working hours,
  and other people's, through `GetUserAvailability`. Google does not expose
  them, so for Google they are set here.
- **In the views.** Hours outside working time are shaded. The week view
  starts its scroll at the start of the working day.
- **In the query language.** `is:afterhours` lists meetings outside your
  working hours, and `| sum hours by …` works over that set. "How much of my
  week happens after six?" becomes one query.
- **Not doing:** writing working hours back to Google, which has no API for it.

## Phase 2: free/busy

Answers "when is each of these people busy?" for a range.

| Source | How |
|---|---|
| You | your own occurrences: busy, tentative or free, skipping declined and proposed events |
| Colleagues on Google | `freebusy.query`; answers for anyone whose calendar the organisation shares |
| Colleagues on Exchange | `GetUserAvailability`, which also returns their working hours. Requests are padded to whole days, as EWS requires, then trimmed. |
| Anyone else | unknown, said as unknown: never treated as free |

- **One merged answer per person.** A list of `{start, end, kind}` blocks,
  where kind is busy, tentative, away or unknown. That one shape feeds find-a-time
  and the scheduling grid.
- **Cached for about ten minutes per person and range.** Typing an attendee's
  address then reopening the grid shouldn't ask the provider again.
- **Command line and agents:** `iql calendar busy --who a@x.com,b@y.com --from
  … --to …`. It only reads, so it's safe for an agent to call, and it returns
  blocks, never event details.

## Phase 3: find a time

- **The search.** For the people and the length asked for, step through the
  range in 15-minute increments. Keep a slot only if it's inside everyone's
  working hours, as far as they're known, and nobody is busy. Rank the slots:
  - everyone known to be free outranks a slot where someone is unknown
  - sooner beats later
  - avoid lunch and the first and last half hour of the day
  - a meeting adjacent to another beats one that splits a free afternoon in two
  - prefer times inside your own working hours
  - and, as phase 4 adds, a slot where a suitable room is free
- **When nothing fits,** it says why ("Ada is busy every afternoon this week")
  and offers the best partial slots with who can't make each.
- **In the editor:** a **Find a time** panel. Each attendee is a row with
  their busy blocks, a column cursor follows the proposed slot, and the top
  suggestions sit above as chips. Choosing one sets the start and end.
- **Command line and agents:** `iql calendar find-time --with a@x.com,b@y.com
  --duration 30m --within 5d`. It only reads; booking the slot is a separate,
  proposed `create`.
- **Offer times by email.** For people whose calendars can't be seen, InboxQL
  writes a draft listing three times in the recipient's time zone where it's
  known (yours otherwise) and queues it in the outbox. Choosing one later
  creates the event. This is how people actually schedule with someone outside
  their organisation.

## Phase 4: rooms

- **Finding rooms:**
  - **Exchange:** `GetRoomLists`, then `GetRooms`, which a regular user can
    call.
  - **Google, as an administrator:** the Directory API, with the
    calendar-resource scope added at sign-in. That needs the client to request
    it and the Workspace to allow it.
  - **Google, everyone else:** rooms already used in past events (Google marks
    resource attendees with `resource: true`), plus any room address typed in.
    They're remembered, so the list grows with use.
- **A room is an attendee** with the role `resource`. It shows in its own row
  in find-a-time, and "any free room in Building 2" is a constraint the search
  can satisfy.
- **Booking** is inviting the room. The room's calendar accepts or declines on
  its own, and the event card shows which. Booking is a person's act, or a
  proposal when an agent does it.
- **Room details:** capacity, building and floor where the provider gives them.
  Find-a-time can then filter on "seats ≥ 6".
- **Not doing:** listing a Google Workspace's rooms for a non-admin through the
  Directory API, which Google doesn't allow.

## Phase 5: out of office

"Out of office" means three different things, and the plan keeps them apart:

1. **A block on the calendar** showing you as away.
   - Local calendars: a new **away** status alongside busy and free.
   - Google: an `outOfOffice` event, on primary calendars only, timed, and only
     where Google offers it. Otherwise a normal event shown as busy, with a
     note saying so.
   - Exchange: `LegacyFreeBusyStatus=OOF`.
2. **Declining meetings that fall in that time.**
   - Google's outOfOffice event can do this itself, with a decline message.
   - Elsewhere, InboxQL lists the clashing invitations and *proposes* a decline
     for each, which you approve together or one by one. It never declines on
     its own unless you switch the rule to automatic.
3. **An automatic email reply.**
   - Exchange: `SetUserOofSettings`, with the time window and the internal and
     external messages.
   - Gmail: its own vacation responder, which needs a Gmail scope this app
     doesn't ask for. The plan points you to Gmail's settings and doesn't
     request the scope.
   - Plain IMAP accounts: there's no standard way, and the settings page says
     so.

- **The Away dialog** sets all three at once: from, until, the message, and
  whether to decline. Anything that would send mail or decline meetings shows
  exactly what will happen before it's saved.
- **Agents:** may propose time away. They may never set an auto-reply or
  decline meetings directly.

## Phase 6: holidays

- **Worked out on this machine, not downloaded.** Public holidays follow rules
  (fixed dates, nth weekdays, Easter-relative dates), and a rules library
  covers most countries and their regions. That avoids a request to anyone and
  works offline. The library will be chosen by checking its coverage and
  licence, not assumed.
- **Settings:** pick a country and optionally a region; more than one is
  allowed, for people who work across borders. Each becomes a read-only
  **Holidays** calendar, generated for the years in view rather than stored.
- **Google's holiday calendars,** for people who already subscribe to them,
  keep arriving with the calendar list as they do now. The holidays setting
  offers to hide the duplicates.
- **Used elsewhere:** find-a-time treats a public holiday in someone's country
  as away, where their country is known.

## Phase 7: birthdays

- **A birthday on a contact** (schema v42: `contacts.birthday`). It stores a
  full date or just a month and day, since people rarely share the year.
  Sources, in order of trust:
  - typed on the contact card
  - imported from a vCard: `iql contact import contacts.vcf`
  - Exchange contacts, via EWS `FindItem` on the Contacts folder
  - Google contacts, via the People API, which needs an extra scope. That's
    asked for only when you turn it on.
- **A read-only Birthdays calendar,** generated each year from contacts:
  - all-day events titled "Ada Lovelace's birthday", with the age when the year
    is known
  - a birthday on 29 February shows on 28 February in other years
  - an optional morning reminder, set once for the whole calendar
- **On the contact card:** the birthday, and "in 12 days" when it's close.
- **Not doing:** guessing birthdays from mail ("happy birthday!" messages). It's
  wrong often enough to be embarrassing, and a wrong birthday is worse than
  none.

## Phase 8: events suggested from mail

The same shape as proposed tickets: a suggestion is shown, a person rules on
it, and a rejection is kept as evidence the source was wrong.

- **Structured data first, because it's exact and free.** Airlines, hotels,
  restaurants, ticketing sites and car hires embed schema.org data in their
  mail (`FlightReservation`, `LodgingReservation`, `FoodEstablishmentReservation`,
  `EventReservation`, `RentalCarReservation`), as JSON-LD or microdata. That
  gives the exact times, places, and booking and flight numbers:
  - a flight becomes an event from departure to arrival, each in its own
    airport's time zone
  - a hotel stay becomes an all-day span
  - all of it runs with no model at all
- **Then text, for mail that proposes a time.** "Can we meet Tuesday at 3?":
  - **The gate:** a Laya label, "this message proposes a meeting time or
    confirms a booking". It's the same pattern as `purchased` gating `receipts`.
  - **The extraction:** a GLiNER extractor on what the gate passes, finding
    the date, time, place and length.
  - **The resolution:** relative dates are worked out against the message's
    own date and time zone, not today's ("Tuesday" in a Friday email means the
    Tuesday after it). Times with no zone use the sender's, read from the
    `Date` header.
  - **Answers:** "Tuesday at 3 works for me" in a reply attaches to the thread's
    existing suggestion rather than creating a new one.
- **Where suggestions show:**
  - a card on the message: "Add to calendar", "Not an event"
  - a **Suggested** entry in the rail
  - ghosted blocks in the week view, only when you turn them on
- **Not duplicated:** a suggestion matching an existing event (same booking
  number, or the same title near the same time) is linked to it, not
  suggested again.
- **Changes to a booking update its event.** A later confirmation with the same
  booking reference updates the times; a cancellation email proposes removing
  the event.
- **Stored separately** (schema v42: `event_suggestions`), not as events.
  Nothing is in your calendar until you add it, and a rejected suggestion
  stays, marked.
- **Querying:** `in:events is:suggested`, and `extract:` on the extractor's
  fields as for any annotator.
- **Agents:** may read suggestions and accept them only as proposals, as with
  everything else.

## Order, and why

| Phase | Depends on | Needs from you |
|---|---|---|
| 1 Working hours | — | — |
| 2 Free/busy | 1 | a connected account to test colleagues |
| 3 Find a time | 1, 2 | — |
| 4 Rooms | 2, 3 | for Google rooms as an admin, the extra scope on your OAuth client |
| 5 Out of office | 1 | — |
| 6 Holidays | — | which countries you want first |
| 7 Birthdays | — | for Google contacts, the People API enabled on your Cloud project |
| 8 Suggestions from mail | — | about fifty review rulings on the gate label, to measure it |

Phases 6 to 8 are independent of 1 to 5 and could run alongside them. Phase 8's
structured half (schema.org) needs no model and could ship first on its own.

## Schema v42, in one migration

- `calendar_settings`: working hours, time zone, holiday countries, birthday
  options; per account, with a user default.
- `contacts.birthday`, `contacts.birthday_source`.
- `event_suggestions`: the source message, kind, the proposed event, confidence,
  state, the accepted event's id, and when it was created.
- `rooms`: the address, name, capacity, building, floor, provider, and when it
  was last seen.
- `freebusy_cache`: by person and range, with an expiry. It's disposable, so it
  could live only in memory.

As always, the live database is backed up before the migration runs.

## What each phase is checked against

- **Working hours and find-a-time:** table tests over hand-built busy patterns,
  including time zones either side of a daylight-saving change and someone
  whose working day crosses midnight in yours.
- **Free/busy and rooms:**
  - the fake Google and fake Exchange servers extended with `freebusy.query`,
    `GetUserAvailability`, `GetRoomLists` and `GetRooms`
  - then a real account each, before calling the phase done
- **Holidays:** known dates for each supported country, checked against the
  official published lists for two years. That includes Easter-relative
  holidays and observed-day rules (a holiday on a Saturday observed on the
  Friday).
- **Birthdays:** vCard variants (with and without a year, `--MMDD`), and the
  29 February rule.
- **Suggestions:**
  - real confirmation emails: an airline, a hotel and a restaurant, from the
    mailbox where they exist, with personal details removed
  - the text extractor measured on reviewed rulings before it's switched on by
    default
