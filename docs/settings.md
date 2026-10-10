# Settings and the System section

**Settings** (Ctrl+, or Help → Settings) is a tab with a section for each kind
of setting. They are stored in three different places, and knowing which is
the difference between a setting that follows you and one that does not:

| Where it is stored | What it means | Sections |
|---|---|---|
| this browser | another browser, or another computer, has its own | General, Browser Storage |
| the mailbox | moves with the mailbox folder; applies to every window | Mail Accounts, AI Configuration, Logging, Profile |
| the machine, `~/.iql/settings.json` | this computer, whichever mailbox | System |

## System

The section to open when you want to know what is going on. **Help → About
InboxQL** opens it directly.

### This server

**Running as** says how the server was started, because that decides how it
restarts, when it stops, and which settings it obeys:

| | |
|---|---|
| **Login service** | Started by the operating system when you logged in; stops when you log out. The installed way. |
| **In a terminal** | Started by hand with `iql start`. Stops when that terminal closes. |
| **Development** | Started with `iql start --dev`, which restarts it whenever the program is rebuilt. |

It also shows the version, how long it has been running, its process number,
and the path of the program file — the one to grant Full Disk Access to on a
Mac.

**Restart server** restarts it in the way its mode needs: through the service
manager, in the same terminal, or through the development supervisor. The page
waits, then reloads by itself when the new server answers. If it does not come
back within two minutes, the page says so and what to check.

### Mailbox

Where your mail is stored, **and why it is that folder**:

| Because | |
|---|---|
| named by the machine settings | the normal case for an installed copy |
| named with `--data` | the server was started with a folder given explicitly |
| named by `$INBOXQL_DATA` | an environment variable chose it |
| the `./data` folder where it was started | there are no machine settings, so it used the folder it was started in |

Also the database's size and schema version, and when the newest backup in its
`backups` folder was made — worth a glance before updating.

### Address

The address to open, what the server is listening on, whether it asks for a
password (and why — see [Privacy](privacy.md#when-a-password-is-asked-for)),
and, if you set a site name, whether your hosts file has it yet.

### Login service, Power, AI and models

Whether the login service is installed and running. Whether this computer is on
battery, and so whether background work is waiting for mains power. Which AI
provider is configured and whether it is on this computer or another — the one
fact on the page about mail leaving the machine — and which local models are
installed, and where.

### Updates

Says whether a newer release exists; **Check now** asks again, otherwise it
checks at most hourly. **Update to …** shows what will happen, then downloads
the release, verifies it, backs up your mailbox, replaces the program and
restarts the server, showing its progress as it goes. The page reconnects on
the new version.

A copy installed with Homebrew says so and leaves updating to `brew upgrade
inboxql`. [Installing](installing.md#updating) has the details.

### Machine settings

The editor for `~/.iql/settings.json`. Each field says whether it **applies
now** or **after a restart**:

| Setting | Applies | |
|---|---|---|
| Mailbox | after restart | changed with **Use a different mailbox…**, below |
| Listen address | after restart | `127.0.0.1:8420` by default; keep it on 127.0.0.1 unless you mean other machines to reach it |
| Models folder | after restart | where model weights are kept, once per computer |
| Site name | now | e.g. `inboxql.localhost`; the hosts file still needs one command in a terminal, which it shows you |
| Heavy work on battery | now | let the annotation pass after a sync run on battery |

When saved settings differ from what the running server is using, a banner
lists each difference — *Listen address: 127.0.0.1:8420 → 127.0.0.1:9000* —
with **Restart now**. If the server was started with a flag that outranks the
file, such as `--addr`, the banner says that a restart will *not* apply that
setting, rather than letting you restart and wonder.

Changing the listen address to anything other than this machine asks you to
confirm first: anyone on the network could then reach the port, a password
becomes required, and InboxQL has no TLS of its own, so it belongs behind a
reverse proxy.

**Use a different mailbox…** takes the full path of a mailbox folder. If there
is a mailbox there, the settings point at it and the server switches when it
restarts. If there is not, it says so and offers to create an empty one — never
silently, because a mistyped path is not a setting, it is a new, empty
mailbox. Nothing is moved or copied; the old mailbox stays where it was.

A server running with no machine settings at all offers **Create settings for
this mailbox**, which records the mailbox and address it is already using — so
nothing changes now, but every command and the login service will agree from
then on.

## General

How the Desk looks and behaves in this browser: the colour theme, **Inbox
Layout** (a row per message or per conversation), **Conversations** (start
closed or open), **Viewer Tabs** (one shared tab or one per kind), and
**Attachment Previews** (in the message, in the File tab, or both). The Desk's
default rail entries can be re-added here too. [The Desk](desk.md) explains
each.

## Mail Accounts

Add, edit, test and remove IMAP accounts, with per-account message counts.
[Accounts](accounts-and-import.md) covers the fields.

## Import Mail

Bring in mail from Apple Mail, read-only. See
[Importing](accounts-and-import.md#importing-from-apple-mail).

## AI Configuration

Which model providers InboxQL may use — Ollama or Swama on this machine, or any
OpenAI-compatible service — as named profiles, with a **Remote** or **Local**
badge on each. It also holds the starter annotators, and the defaults for
ticket proposals, similarity and topic extraction. Nothing here is used until
you run something that needs it, and a remote provider is never sent a whole
mailbox without your consent per annotator. See [Labels](labels.md).

## Logging

What InboxQL writes in its own log, which you can read under **Help → Log** or
query with `in:logs`: how slow a query must be to be recorded as slow, which
noisy categories to include, how many lines to keep, and whether the text of
your queries is written down. How *much* is logged — the level — is set in the
Log tab itself.

## Maintenance

Jobs that work through the whole mailbox, with progress: **Extract
attachments**, **Read file contents**, **Read scans (OCR)**, **Embed file
text** and **Rebuild indexes**. Anything that sends file contents to a model
says so before it starts.

## Data Management

**Erase All Local Data** deletes every stored message and attachment from this
mailbox. Your mail on the server is untouched and will sync again; imported
mail will not come back. There is no undo.

## Browser Storage

What this browser has stored for InboxQL — the window layout, a cached profile —
with **Reset Layout** for a layout that has got into a muddle, and **Clear All
Browser Storage**. None of it is your mail.
