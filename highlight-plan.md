# The row that stays outlined

*A phased plan. 4 October 2026.*

## What is actually missing

Arrowing the message list already previews without pressing Enter
(`previewMessage`, `lib/tabs.ts`). It is wired to one list out of six, and the
highlight it leaves behind is the browser's focus ring — which is lost the
moment focus goes anywhere else.

So two separate gaps, and they are not the same work:

```
previewAttachment / previewContact      five lists never got the gesture
aria-current                            no row records "this is the one shown"
```

## Three states, not two

| State | Scope | Survives focus leaving |
|---|---|---|
| `aria-selected` | bulk selection, many rows | yes |
| focus | one row, where the keyboard is | **no** |
| `aria-current` | **what the viewer is showing** | yes — the point |

`aria-current` is **derived, not stored.** `useViewerStore` already holds
`messageId`, `contact` and `file` as separate slots; a row is current iff the
slot for its kind points at it. Inventing a parallel `currentId` would be a
second source of truth for a question the store already answers.

**Per-kind is what makes the cascade work.** One global "current" would
un-highlight the file the moment a message inside it was selected — and the file
is still on screen in its own tab. Separate slots keep both lit.

## Reuse mode cannot cascade, and that is correct

`clearsOthers` is the whole difference between the two viewer modes. In `reuse`,
`setMessage` clears `file`, so picking a message from a file's occurrence list
blanks the File tab — *the tab holding the list being arrowed through*.

Not special-cased. That function answers one question and an exception for
"unless the message came from this file" erodes it. The two-level cascade is a
**split-mode** feature; reuse mode keeps working one subject at a time, which is
what it means.

---

## Phase 1 — `aria-current` on every row kind

A row asks the viewer store whether it is the subject, and draws an outline that
does not depend on focus. Six kinds: message, thread, ticket, draft, contact,
file.

Distinct from the selection fill, because they co-occur: a row can be ticked for
a bulk action *and* be the one on screen, and if they render the same somebody
will read a selection of one as a preview.

## Phase 2 — the gesture, for the other lists

`previewAttachment` and `previewContact`, mirroring `previewMessage`: set the
slot, do not call `openTool`. Without this, arrowing the file list switches tab
away from Desk on the first keystroke and the second goes nowhere — the exact
bug `previewMessage`'s comment describes, still live for files.

**And a gate.** The roving tab stop remembers a row, so `focusin` fires on it
when the list regains focus — which would preview it, yanking the viewer away
from whatever was being read in another tab. Preview belongs to keyboard
navigation, not to focus arriving.

## Phase 3 — the cascade

`useRovingFocus` inside `AttachmentOccurrences`, so a file's messages arrow and
preview the way the file list does. Its own root: the panel is in another
FlexLayout tab and the hook is scoped to one ref.

The rows are `<button>`s. Safari does not focus a button on click, so the click
handler stays — an `onFocus`-only preview would be dead to the mouse there.

## Not doing

**A global current-row store.** Derived from the viewer store or it is a second
truth to keep in sync.

**Marking read on preview.** The viewer does not set `\Seen` today. Arrowing
fifty messages must not mark fifty read, so this stays out.

**Special-casing reuse mode.** Argued above.

---

## What was built

All three phases, verified against the real mailbox in a browser.

### A correction to the plan's premise

The plan said no row recorded what the viewer was showing. **Desk's own inbox
list already did** — `openMessageId = useViewerStore(s => s.messageId)`, drawn as
a left bar, hand-rolled in `Desk/index.tsx`. The idea existed; what was missing
was that it had a name, that it reached the other lists, and that a screen reader
could tell.

So Phase 1 was less "invent a state" than "promote one list's local trick to
`lib/rowState.ts` and give it `aria-current`".

### Three kinds, not six

The plan said six row kinds. The code says **three**, and the difference is the
honest part: a kind can only be *current* if it is a viewer subject.

| Kind | Opens | Current? |
|---|---|---|
| message, contact, file | a viewer slot | yes — one per `ViewerKind` |
| ticket | drills the query down to its conversation | no |
| draft | nothing at all | no |

`aria-current` therefore lands on message rows (both the inbox list and the query
result table), Timeline entries, file rows, contact rows and the occurrence rows.

### `lib/rowState.ts`

`useIsCurrent(kind, id)` reads the viewer store; nothing new is stored.
`rowStateClasses` computes the ring **once**, which is not stylistic:

```
ring-1 ring-2   →  Tailwind emits both; the later rule in the stylesheet wins,
                   whichever order the caller wrote them in
```

Appending a selected row's classes to a current row's would have drawn an
unpredictable ring. Selection fills, current outlines, and a row that is both
does both — because a selection of exactly one row must not read as a preview.

### The gate

`movedByKeyboard()` in `rovingFocus.ts`, set around the `el.focus()` inside
`focusRow` and read by every row's `onFocus`. `focus()` dispatches focusin
synchronously, so the flag is exact — no timer, no window.

Without it the roving tab stop is a trap: tabbing back into a list focuses the
row it remembers, which would have previewed it and pulled the viewer off
whatever the user had opened elsewhere. That is precisely what the cascade asks
them to do, so the feature would have broken itself.

One existing test asserted `row.focus()` previews. It was standing in for an
arrow key; it now presses one, because direct focus is no longer that gesture.

### `previewAttachment`, `previewContact`, `previewMessageByID`

The first two mirror `previewMessage`: set the slot, never call `openTool`.
Asserted against a fake layout model — `addTab` and `doAction` are never called —
so a future edit swapping `preview` back to `open` fails rather than silently
teleporting the user out of the list.

`previewMessageByID` is for a list that holds ids and not messages. It fetches,
so it carries a **sequence guard**: arrowing issues one request per keystroke,
they land in any order, and without it a slow response for an abandoned row
overwrites the row the user is now on. `previewMessage` bumps the same counter,
so a fetch in flight from the occurrence list cannot land on top of a direct
preview from the message list.

### The cascade, and the mode that cannot have it

`AttachmentOccurrences` gets its own `useRovingFocus` root — the hook is scoped
to one ref and this panel is in a different FlexLayout tab.

Previewing from it is **split-mode only**, as argued above: in reuse mode setting
a message clears the file, which blanks the tab the list is inside. The arrows
still move and still outline; opening stays Enter's job. Encoded as `previews`
rather than discovered later.

`openedFrom` and `aria-current` are kept apart on those rows. The first means
*this panel is embedded in that message's viewer* and reads "this message"; the
second means *the viewer is showing it now*. They coincide inline and diverge the
moment somebody arrows, which is when it matters.

### A bug the tests found

`FileSilences` read `usage.files.toLocaleString()` off whatever
`/api/attachments/usage` returned. A response missing the field threw **during
render**, taking down the whole results pane — "the file list is broken" rather
than "the size is unknown". Found because a Desk test mocked the endpoint with
`{}`. Each field is now defaulted, and a usage of zero files renders nothing
rather than "0 B in 0 files".

### Verified in the browser, on the real mailbox

```
file row clicked, focus moved away   → still outlined (current 0, focused -1)
two Down keys on the file list       → outline and focus follow, Desk stays selected
                                       (preview, not open — the File tab did not steal focus)
File tab, Down on its occurrences    → second message outlined, File tab stays selected
                                       and the file row in Desk is STILL outlined (13)
Viewer tab                           → holds "Re: Bitwarden Support", 2/13/2024
```

That last pair is the whole point: two rows lit at once in two different tabs,
because they are two different viewer slots.

No console errors. 153 frontend tests (up from 131), `tsc --noEmit` clean, Go
suite green.

### Not documented in AGENTS.md

Deliberately. That file is the contract for driving InboxQL from the CLI and the
API; keyboard behaviour in the web UI is not part of it, and padding it with
things an agent cannot invoke makes the parts it can harder to find.
