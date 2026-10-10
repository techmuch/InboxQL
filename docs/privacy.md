# Privacy and security

InboxQL is built on one assumption: your mail is yours, and it should stay on
your computer unless you decide otherwise, knowingly, each time it matters.

## What stays on this machine

Everything, by default.

- **Your mail** is copied into a database in your mailbox folder. InboxQL does
  not have a server of its own, and there is no account to make with anyone.
- **The local models** — Laya for labels, GLiNER for extraction — run inside
  InboxQL. The only network request either ever makes is downloading it, once,
  checked against a published checksum.
- **Ollama or Swama** on this computer are local too.

Mail leaves the machine in exactly two ways, and both are your choice:

1. **Syncing**, which talks to *your* mail server, over TLS, with the server's
   certificate verified.
2. **A remote AI provider**, if you configure one, and only when you run
   something that uses it: analysing a thread, expanding notes into a draft,
   reading scans, or an annotator on the `llm` engine. A remote provider is
   marked **Remote** in AI Configuration and in Settings → System.

An annotator that would send mail to a remote provider refuses to run until you
have agreed to it, for that annotator, and its **Plan** says how many messages
and roughly how much text would be sent, and where.

## When a password is asked for

InboxQL listens on `127.0.0.1` — this computer only — and there it does not ask
for a password. Reaching that address means being on this machine already, and
asking its owner for a password protects nothing.

That changes as soon as the audience is wider than you:

| How it is reached | Password |
|---|---|
| from this computer, the default | not asked for |
| listening beyond this computer (`--addr :8420`, a LAN address) | required |
| through a reverse proxy | required, always |
| started with `--require-password` | required |

The proxy row cannot be switched off, and it is the subtle one: a proxy on this
computer relays every request over the local connection, so every request
*looks* local whoever sent it. A request that carries proxy headers never gets
passwordless access.

**A web page on another site cannot use InboxQL.** Any page you visit could
otherwise send requests to `localhost:8420`. Browsers mark where a request came
from, and InboxQL refuses passwordless access to — and refuses any change from —
a request marked as coming from another site. Command-line tools and scripts
send no such marks and are unaffected.

`iql init` creates an administrator account with a random password, printed
once. You need it only if you ever serve InboxQL beyond this computer;
`iql user passwd` sets a new one.

## Account passwords

IMAP and SMTP passwords are encrypted with AES-256-GCM, using a key kept in
your mailbox folder as `vault.key`, readable only by you. They are never sent
back to the browser, never accepted as command-line arguments, and never
presented to a server they were not given for: change an account's server and
you must type its password again.

**Keep `vault.key` with your backups.** Without it, the stored passwords cannot
be decrypted — though your mail can still be read, and you can re-enter the
passwords. Together, the key and the database are as good as the passwords
themselves, so keep them somewhere as private as this computer.

## Backups

```sh
iql backup                                     # into the mailbox's backups folder
iql backup ~/Backups/inboxql.db --include-key  # and the key beside it
iql backup --include-attachments               # and the stored files
iql restore <file>
```

A backup is safe to take while InboxQL is running. Updates make one
automatically before changing anything, and Settings → System shows when the
newest one was made.

Files are stored outside the database, so a plain backup leaves them out — and
says so when there are files it is leaving behind.

## Agents

InboxQL can be driven by an AI agent from the command line, with the same
abilities you have — with one exception that is not negotiable: **an agent can
write and queue mail, but cannot send it.** Delivery needs a person at a
terminal to read the message and type `yes`. Drafts an agent wrote are marked
as such when you review them. [For agents](agents.md) is the full contract.

When an agent moves one of your windows to show you something, the window says
what it was told and why, and **Help → Windows** keeps the record.

## The log

InboxQL's log lives in the mailbox and records what it did, including — by
default — the text of queries you run, because those make problems
diagnosable. A query can name a person, so you can switch that off in
Settings → Logging; the timings are kept and the words are not.
