# A threaded inbox by default

*A phased plan. 3 October 2026.*

## What the setting is, and what it is not

**A default, not a force.** It decides where the Desk starts; the Threads
button still decides what any one query does.

Literal "always" would break three things, and the first is silent:

| | |
|---|---|
| **Aggregates** | only one terminal stage is allowed, and `WithStage` *replaces* the existing one — so forcing `timeline` onto `\| count by week` deletes the count |
| **Non-mail kinds** | `in:contacts`, `in:attachments`, `in:tickets`, `in:drafts` have no conversations |
| **The Threads button** | if every query is forced, pressing it to get a flat list does nothing |

A default avoids all three and, because of how the rail already composes, *is*
always-threaded in ordinary use.

## Most of "always" is already true

`ReplaceField` — what a rail folder click does — preserves pipeline stages. So
once `| timeline` is in the query, Inbox → Starred → Sent keeps it. And the
entity entries (`in:contacts`, saved queries) replace the whole query, so they
are naturally unthreaded without a special case.

That leaves two things to build: where the query starts, and what happens when
somebody changes their mind.

## `timeline`, not `thread`

`| timeline` is one row per conversation — what the Threads button already
writes, and what "threaded" means in a mail client. `| thread` expands to the
rest of each conversation as a flat list, which is a different feature.

## Browser-local

Beside `ViewerModeSetting`, stored the same way. The General panel says what it
is: *"stored here rather than on the server, so each machine you use can
differ."* A reading preference is a per-machine thing in the same way the theme
and the tab layout are.

---

## Phase 1 — The preference

`frontend/src/lib/threading.ts`: a store with the same shape as the viewer-mode
one — read from `localStorage` at startup, written on change, and a blocked
store costs persistence rather than the switch.

## Phase 2 — The setting

A section in General beside Viewer Tabs, two options, matching that component
so the page does not grow a third way of asking the same kind of question.

## Phase 3 — Where it applies

**At the start**, in the query store's initial value, so there is no flash of a
flat list while an async check decides.

**When it changes**, on the query in the bar — a setting that does nothing until
the next reload reads as broken.

## Phase 4 — Where it must not

A guard both callers share: never add the stage to a query carrying a terminal
aggregate, and never to one about something other than mail.

## Not doing

**Removing the Threads button.** It is the override that makes a default safe.

**A server-side preference.** If this should follow the account rather than the
browser it belongs on `/api/settings`, which is a different setting in a
different section.

---

## What was built

| Phase | Where |
|---|---|
| 1 — the preference | `frontend/src/lib/threading.ts` |
| 2 — the setting | `ThreadingSetting` in General, beside Viewer Tabs |
| 3 — where it applies | `openingQuery()` in `lib/filters.ts`; a preference effect in Desk |
| 4 — where it must not | `canThread()`, and the tests that prove each guard earns its place |

### Verified in the browser, on the real mailbox

```
default                    folder:inbox                     flat
switch it on               folder:inbox | timeline          58 conversations, live
reload                     folder:inbox | timeline          persisted
Inbox → Starred            folder:starred | timeline        stage carried
People                     in:contacts kind:person          left alone
press Threads              folder:inbox                     and it stays off
```

### The guards are real, and tested as such

Each was checked by removing it and watching the test fail:

- drop `canThread` → "leaves a counting query alone" and "leaves a query about
  something other than mail alone" both fail;
- make the effect react to the query as well as the preference → "does not
  fight the Threads button" fails, which is the bug where the stage comes
  straight back the instant the button removes it.

A test that passes either way is not evidence, and this plan had one of those
in an earlier session.

### Something the feature exposed

Clicking **Inbox** from **People** produced `in:contacts kind:person
folder:inbox` — still a contacts query, now carrying a folder term that means
nothing there, and unthreaded whatever the preference said.

Pre-existing: a rail folder click composes onto the current query so a
cross-filter survives changing folder, and nothing stopped it composing across
kinds. The preference made it visible, because "always threaded" plainly was
not.

A folder is a statement about mail, so switching kind now starts over. Within
mail it still composes, which is what keeps both the cross-filter and the
thread stage.

### Not done

**A server-side preference.** This is per-browser, like the theme and the tab
layout, and the General panel says so. If it should follow the account it is a
different setting in a different place.

**`| thread`.** `| timeline` is one row per conversation, which is what the
Threads button already wrote and what "threaded" means in a mail client.
Expanding to the rest of each conversation as a flat list is a different
feature.
