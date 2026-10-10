# Accounts and importing mail

Mail reaches InboxQL in two ways. An **account** connects to a mail server
over IMAP and keeps syncing. An **import** reads mail that is already on this
computer — Apple Mail's own files, or a folder of `.eml` messages — once.

Both end up in the same mailbox and are queried the same way. The difference
is only where the mail came from, which `account:` and `mailbox:` still tell
you afterwards.

## Adding an account

**Settings → Mail Accounts → Add Account.**

| Field | What to put |
|---|---|
| IMAP server | e.g. `imap.gmail.com`, `imap.mail.me.com`, `outlook.office365.com` |
| Port | 993 almost everywhere — IMAP over TLS |
| Username | usually your full address |
| Password | for Gmail, iCloud and most providers with two-step sign-in, an **app password** |
| SMTP server | only needed to send replies; optional |

**Test Connection** before saving. A failure is classified rather than
reported as one opaque error: the password was rejected, the server name does
not exist, the network cannot reach it, or its certificate did not verify.
InboxQL always verifies a server's certificate; there is no setting to turn
that off.

Saving starts the first sync. On a large mailbox that takes a while, and you
can work with what has arrived while the rest comes in. After that, a sync
only asks for what is new.

From a terminal the same thing is:

```sh
INBOXQL_ACCOUNT_PASSWORD='app-password' iql account add \
  --name work --email me@example.com --host imap.example.com
iql account verify work     # test the connection, and say what is wrong
iql account sync work       # returns when the sync has finished
```

Passwords are never command-line flags, because those end up in your shell
history and are visible to every other program on the machine. They come from
an environment variable, standard input, or a prompt.

### What happens to the password

It is encrypted with AES-256-GCM using a key kept beside your database,
`vault.key`, and it is never sent back to the browser — the account form shows
that a password is stored, not what it is. Leave the field empty when editing
and the stored one is kept.

One exception: change an account's server, port or username and you must type
the password again. A credential for one server is not a credential for
another, and InboxQL will not present it to a host it was not given for.

### Removing an account

Removing an account **deletes every message stored for it**, including
anything imported into it. Your mail on the server is not touched — the next
time you add the account it syncs again — but imported mail has nowhere to come
back from. That is why imports belong in their own account, below.

## Importing from Apple Mail

**Settings → Import Mail**, or `iql import` in a terminal.

The import reads Mail's own files in `~/Library/Mail`. It is strictly
read-only: nothing in Mail is changed, moved or deleted, and Mail can stay
open while it runs.

### Full Disk Access

macOS protects `~/Library/Mail`, so the program reading it needs **Full Disk
Access**. When it is missing, the Import page says so and says which program
needs it — `~/.iql/bin/iql` for an installed copy — rather than failing with a
bare permission error.

Grant it in **System Settings → Privacy & Security → Full Disk Access**, add the
program with **+**, then restart InboxQL (**Settings → System → Restart**). The
usual mistake is granting it to the terminal or the browser instead of `iql`.

After every update, grant it again: macOS ties the grant to the exact file,
and an update is a new file.

### Choosing what to import

The Import page lists every mailbox Mail has, with how many messages each
holds. Pick the mailboxes, then how much:

- **Newest N messages**, across everything you picked — 100 across three
  mailboxes is 100, not 300.
- **Between two dates.**
- **Everything.**

**Import into** chooses the InboxQL account the mail will belong to. For an
archive you want to keep, make a dedicated account for it rather than using a
live one: imported mail goes when its account goes.

A **deep scan** of a mailbox parses every message to count attachments and
people and find the date range. It is thorough and slow on a large mailbox —
minutes — so it is never started for you.

### Try it first

Every import can be a **dry run**: it reads and parses everything and reports
exactly what would happen, without writing a row. Use one before importing
anything large.

The report always adds up:

| | |
|---|---|
| **imported** | new to this account |
| **duplicates** | already in this account — expected on a second run, and not an error |
| **skipped** | not imported on purpose, such as messages Mail never finished downloading |
| **failed** | could not be read; the log says which and why |

Run the same import twice and the second run imports nothing new: messages are
matched on their content, not their file names.

Attachments are off by default, because they can multiply the size of your
mailbox. Turn them on and they are stored once each, however many messages
carried them. See [Files](files.md).

### From a terminal

```sh
iql import sources                                  # is Mail there, and readable?
iql import mailboxes --source apple-mail            # the mailbox list, with counts
iql import run --mailbox <id> --account archive --limit 500 --dry-run
iql import run --mailbox <id> --account archive --limit 500
```

## Importing .eml files

The fallback that needs no permissions at all. Drag messages out of Mail —
or any client — into a folder in Finder, and import the folder:

```sh
iql import eml ~/Desktop/exported --account archive
```

Any mail that can be saved as a standard `.eml` message can come in this way.

## Where the mail lives afterwards

Everything is in your mailbox folder — `~/.iql/data` for an installed copy —
and **Settings → System** says which folder that is. Queries can still tell
the sources apart:

```
account:archive              everything imported into the archive account
mailbox:"Archive/2024"       the folder a message physically came from
folder:inbox                 the inbox, whatever account
```
