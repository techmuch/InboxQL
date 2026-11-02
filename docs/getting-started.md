# Getting started

InboxQL is a mailbox you can ask questions of. It keeps a copy of your mail on
your own computer, in a database you can query, and puts a mail client, an
analytics dashboard and a query language on top of it.

It is built for people who would rather type `from:*@stripe.com after:2026 |
sum receipts.amount by month` than scroll. You do not have to start there —
it reads mail like any other client — but everything it shows you is a query,
and every query is something you can change.

## What it is, and what it is not

**It is local.** Your mail is copied into a database on this machine. Nothing
is sent anywhere unless you set up a remote AI provider and then ask it to
read something — and even then, InboxQL asks first. See [Privacy](privacy.md).

**It is a server you open in a browser.** InboxQL runs in the background and
you use it at `http://localhost:8420`. That makes it one program on every
platform, and it means an agent or a script can use the same mailbox you do.

**It is your mail client.** It keeps a full copy of each account, current both
ways: new mail arrives in seconds, and archiving, labelling or reading here
does the same on the server. It sends mail, with a few seconds to undo. Mail
has labels, as in Gmail, rather than folders.

**It is early.** The mail client, the query language, labels, contacts, files
and tickets all work. Some things are deliberately not there yet — there is no
calendar, and search is by words, not by meaning.

## Install it

One command. It downloads the release for your computer, checks it against
the published checksums, installs it in `~/.iql`, and starts InboxQL when you
log in.

On macOS or Linux:

```sh
curl -fsSL https://techmuch.github.io/InboxQL/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://techmuch.github.io/InboxQL/install.ps1 | iex
```

Then open **http://localhost:8420** — or type `iql open`, which opens whatever
address the running server is at. [Installing, updating and
removing](installing.md) has the details, including what each file is for.

## Finding your way around

Every screen has the same three parts:

- **On the left, the places.** *Needs me* — [Today](#open/today), Ahead,
  Replies I owe, To review, Drafts and outbox — then your mail (Inbox,
  Everything, Ask in words), your **Subjects** (saved queries and topics), and
  the other places: Calendar, Board, Files, People, Agent activity, Settings,
  Help. A number beside one is how many are waiting.
- **In the middle, the screen** you went to. Back and Forward work, and each
  place has its own address (`#/today`), so a bookmark works too.
- **On the right, the evidence**: why the agent concluded something, how a
  list was made, the message under the cursor. Drag its edge to make it wider
  or narrower, or press **.** to hide it.

At the top, the **query bar** asks the mail anything (press **/** to get to
it), **Go to…** (**Ctrl+K** / **⌘K**) finds any place, command or setting by
name, and **Write** (**c**) starts a message.

**?** on any screen says what that screen is for, what is on it and which keys
work there. A link marked **↗** in a guide opens the place in the application it
describes. On a phone the places become a bar along the bottom
([On your phone](installing.md#on-your-phone)).

## Your first hour

### 1. Add an account

Open **Settings → [Mail Accounts](#setting/mail-accounts)** and choose **Add account**. Type your
address: InboxQL works out the servers. Then the password — for iCloud, Yahoo
and most providers with two-step sign-in an *app password*, not the one you
sign in with; Gmail can use **Sign in with Google** instead. InboxQL signs in
before saving and says exactly what went wrong if something does: a rejected
password, a server name that does not exist, a server that did not answer.
[Accounts](accounts-and-import.md#adding-an-account) has the details.

Saving the account connects it, and it stays connected: you never need to
press Sync. The first time, it reads the whole account, newest first, so this
week's mail is there in minutes and older mail fills in behind it — a large
Gmail account can take a few days. The account's card says how far it has got.

Already have years of mail in Apple Mail? [Importing](accounts-and-import.md#importing-from-apple-mail)
reads it straight from Mail's own files, without changing them.

### 2. Start from Today

**Today** is the home screen: the replies you owe, what is waiting for you to
review, what arrived since yesterday grouped so a sender's newsletters can be
archived at once (with Undo), the dates you marked important, and the next 48
hours. **Inbox** is the whole mailbox as a list; move through it with the arrow
keys and each message reads on the right. [The Desk](desk.md) covers the list.

### 3. Ask it something

Type into the query bar:

```
from:me() after:30d | count by week
```

That is how much mail you sent in each of the last four weeks. Replace
`count by week` with `top to 10` for the ten people you wrote to most. The
[query language](query-language.md) has the full list.

### 4. Let it sort your mail

Install the **elements** — receipts, travel, calendar events, tasks, who is
waiting, importance, what kind of sender someone is, topics — with
`iql elements install`, then ask the AI you already use (Claude Code,
Antigravity) to run `iql work guide` and read your newest conversations.
[Reading with your AI harness](reading.md) explains the loop, and
[Labels](labels.md) how to correct an answer when it is wrong.

### 5. See how it is running

**Settings → System and updates** says which version
this is, where your mail is stored and why, whether it is running as your
login service, and whether there is an update. [Settings](settings.md)
describes all of it.

## Where to next

- [The Desk](desk.md) — reading, conversations, keyboard.
- [The query language](query-language.md) — the part worth learning.
- [Contacts](contacts.md) — who is waiting for you, and whom you are waiting on.
- [Troubleshooting](troubleshooting.md) — when something is not right.

Everything here is also in the application, under **Help**,
and describes the version you are running.
