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

**It does not send mail on its own.** You can write and queue replies in it,
and an agent can draft them, but a message leaves only when a person approves
it at a terminal.

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

Then open **http://localhost:8420**. [Installing, updating and
removing](installing.md) has the details, including what each file is for.

## Your first hour

### 1. Add an account

Open **Settings → Mail Accounts** and choose **Add Account**. You need the
IMAP server, your username and a password — for Gmail and iCloud that is an
*app password*, not the one you sign in with. **Test Connection** says
exactly what went wrong if something does: a rejected password, a server
name that does not exist, a network that cannot reach it.

Saving the account starts the first sync. A large mailbox takes a while; the
status bar at the bottom of the window shows how far it has got, and you can
use everything that has arrived while the rest comes in.

Already have years of mail in Apple Mail? [Importing](accounts-and-import.md#importing-from-apple-mail)
reads it straight from Mail's own files, without changing them.

### 2. Find your way around the Desk

The **Desk** is where mail is read. The rail on the left lists your folders,
your saved queries and the labels that have found something. Clicking one puts
its query in the bar at the top, so you learn the language by watching it.

Use the arrow keys to move through the list; the message opens beside it.
[The Desk](desk.md) covers the rest.

### 3. Ask it something

Type into the query bar:

```
from:me() after:30d | count by week
```

That is how much mail you sent in each of the last four weeks. Replace
`count by week` with `top to 10` for the ten people you wrote to most. The
[query language](query-language.md) has the full list.

### 4. Let it sort your mail

**Tools → Annotators** has a starter pack of labels and extractors —
`importance`, `loops` (conversations waiting for a reply), `purchased`,
`receipts` and a few others. They run on this machine, with models InboxQL
downloads once. Switch on the ones you want and run them over the last few
months. [Labels](labels.md) explains what each engine is good at, and how to
correct one when it gets something wrong.

### 5. See how it is running

**Help → About InboxQL** opens the System section of Settings: which version
this is, where your mail is stored and why, whether it is running as your
login service, and whether there is an update. [Settings](settings.md)
describes all of it.

## Where to next

- [The Desk](desk.md) — reading, conversations, keyboard.
- [The query language](query-language.md) — the part worth learning.
- [Contacts](contacts.md) — who is waiting for you, and whom you are waiting on.
- [Troubleshooting](troubleshooting.md) — when something is not right.

Everything here is also in the application, under **Help → Documentation**,
and describes the version you are running.
