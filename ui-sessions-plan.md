# Windows the backend can see and point at

*A phased plan. 3 October 2026.*

## It is a remote control, not a sync

"Syncing" would mean replication: canonical state somewhere, conflict
resolution, merge rules, reconciliation after a disconnect. Nothing asked for
needs any of it. What is wanted is two things:

- the backend **observes** which frontends exist and what each is showing;
- the backend **commands** one of them — *window 2, run this query*.

A registry and a command channel. No conflicts, because only one side ever
writes a given fact: the frontend owns what it is showing, the backend owns the
list of who is connected.

**A command does not take ownership.** Set a window's query from the CLI, reload
the window, and it returns to its own persisted state. The CLI points; it does
not drive.

## The vocabulary already exists

`lib/tabs.ts` exports six openers, and one of them is already the whole feature:

```ts
openQuery = (query) => { useQueryStore.getState().set(query); openTool(DESK_TAB, 'Desk'); }
```

So a command is `{verb, args}` dispatched to `openQuery`, `openTool`,
`openMessageByID`, `openContact`, `openAttachment` or `openErrorLog`. **Nothing
serialises a layout tree** — the layout belongs to `nexus-shell` and stays
there.

## Transport

SSE down, POST up. There are already three SSE endpoints here, it is one-way
server-to-client which is exactly a command channel, and it needs no new
dependency and no second auth path.

## Two things that decide whether this is pleasant

**The name has to be typeable.** A UUID in `iql ui list` is useless. Short
server-assigned names — `1`, `2` — so `iql ui query 2 "from:stripe"` can be
typed, and the same name shows in the window so you can tell which is which.

**Liveness is where it rots.** SSE drops silently: a laptop sleeps, a proxy
times out, a tab closes without unloading. Without heartbeat and expiry the
list fills with ghosts and a command to a dead window *appears to succeed*.

---

## Phase 1 — The registry

`internal/uisession`: register, heartbeat, expire, list. A window reports what
it is showing; the server assigns the short name.

In memory, not the database. A connection is not a fact that should outlive the
process that holds it — a registry restored from disk is a list of windows that
are not there.

## Phase 2 — The channel and the commands

SSE for delivery, POST for registration and state. Four verbs: `query`, `open`,
`notice`, `ping`.

`notice` is not decoration. A UI that reorganises itself with no explanation is
alarming rather than helpful, so an agent that moves somebody's screen can say
why in the same message that moves it.

## Phase 3 — The CLI

```
iql ui list                      who is connected, and what each is showing
iql ui query <id> <query>        point one at something
iql ui open <id> <what>          desk, log, settings, a message id
iql ui notice <id> <text>        say why
```

Targeting a window that has gone must fail and say so, not exit 0.

## Phase 4 — The UI

An indicator: the window's own name, whether its channel is live, and what it
last received.

**And a panel listing every connected frontend and what each is showing** — the
same answer `iql ui list` gives, in the place somebody is already looking. It
is the half that makes this useful to a person rather than only to an agent:
two windows open on two screens, and one of them can tell you what the other is
doing.

## Not doing

**Arbitrary state setting.** "Set that window's threading preference" is a long
tail needing a command each. The six openers and a query cover what was asked.

**Persisting commands.** A command is an instruction to a live window, not a
new source of truth about it.

**Cross-origin anything.** The channel inherits the existing refusal: a page on
another origin must not be able to watch, or drive, somebody's mail client.

---

## What was built

| Phase | Where |
|---|---|
| 1 — the registry | `internal/uisession` |
| 2 — the channel | `internal/api/uiapi.go`, SSE down and POST up |
| 3 — the CLI | `iql ui list\|query\|open\|notice\|close` |
| 4 — the UI | the Windows tab, the status-bar indicator, the note |

### It works, on two real browser windows

```
$ iql ui list --addr 127.0.0.1:8099
ID        SHOWING                  TABS                                     SEEN
2   live  label:purchases          welcome desk annotators settings errors  1s ago
3   live  folder:inbox | timeline  welcome desk annotators settings errors  3s ago
  2 was last told: query label:purchases
```

`iql ui query 2 "label:purchases" --note "I found 19 purchases — have a look"`
changed that window's query bar, its pills, its results and its rail highlight,
and put the note on screen beside them.

Closing a tab removed it from the listing within seconds, and a command to it
then **failed with exit 3** rather than reporting success — which is the one
failure this whole surface exists to avoid.

### The UI lists them too, which was the point

The Windows tab shows every open window: its name, whether its channel is live,
what it is showing, which tabs it has, and what it was last told. The row for
the window you are reading it in is marked **this window**, because otherwise
the names are labels nobody can map to a screen.

It is also a remote control — typing a query into another window's box and
pressing Send moved it, verified between two tabs.

### Three things that would have gone wrong

**The listing going stale.** Heartbeat and state are one request, because "is it
alive" and "what is it showing" are the same question about the same window.
Split, a live window could show a minute-old query.

**Ghost windows.** Expiry is 35 seconds against a 10-second heartbeat — one
missed beat is not a death, and a sleeping laptop has not closed its window.
A clean close says so immediately rather than waiting to expire.

**The layout walk.** The first version guessed at the shell's shape and reported
no tabs at all. It is a FlexLayout model and `visitNodes` is the way through it —
the same walk `openTool` already does. Found by looking at the listing rather
than at the code.

### Deliberately not done

**Arbitrary state setting.** The six existing openers plus a query cover what
was asked; "set that window's threading preference" is a long tail needing a
command each.

**Persistence.** A command is an instruction to a live window. Reload it and it
returns to its own state. Anything else would make the CLI a second owner of
what the frontend shows, which is where conflicts and merge rules come from.

**Nothing hidden from the person.** Everything an agent does through this is
visible in the Windows tab afterwards, and `--note` means it is explained at the
time. That is the intended property, not an oversight.
