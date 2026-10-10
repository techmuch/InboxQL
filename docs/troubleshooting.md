# Troubleshooting

Start with two commands. Between them they answer most questions:

```sh
iql where     # which mailbox a command will use, and why
iql doctor    # checks the mailbox, the key, the accounts and the models
```

In the application, **Settings → System** shows the same things for the
running server, and **Help → Log** shows what it has been doing — filter it
with `in:logs level>warn after:1d`.

## The page does not load

Run `iql service status`.

- **Not installed** — InboxQL is not running as your login service. Start it
  with `iql service install`, or run `iql start` in a terminal.
- **Installed, not running** — the reason is at the end of
  `~/.iql/logs/service.log`. The most common is another program already using
  port 8420. Pick another port in Settings → System (if you can reach it), or
  edit `"addr"` in `~/.iql/settings.json`, then `iql service restart`.
- **Running** — check the address. `iql where` prints the one it listens on,
  and a site name such as `inboxql.localhost` only works once the hosts-file
  command has been run.

## "InboxQL is already running"

One server runs per mailbox, so typing `iql start` while the login service is
running tells you where the running one is instead of starting a second copy
that would sync the same mail twice. Open the address it prints, or stop the
service first with `iql service stop`.

## The wrong mailbox opened

`iql where` says which mailbox and why. A command decides in this order:

1. `--data <folder>` on the command line;
2. the `INBOXQL_DATA` environment variable;
3. `~/.iql/settings.json`;
4. a `./data` folder in the current directory — only when there are no machine
   settings.

Settings → System shows the same answer for the server you are looking at.

## A restart or update did not come back

The System section waits two minutes, then says so. In a terminal:

```sh
iql service status
tail -50 ~/.iql/logs/service.log
cat ~/.iql/logs/update.log       # after an update started from Settings
```

An update that fails before replacing the program leaves the old version
installed and says so; one that fails after it has made a backup leaves that
backup in the mailbox's `backups` folder. `iql service start` starts the service
again.

## macOS says the program cannot be opened

It was downloaded through a browser, which marks it as quarantined, and
InboxQL is not notarised by Apple. Install with the `curl` command instead, or
clear the mark:

```sh
xattr -d com.apple.quarantine ~/.iql/bin/iql
```

## Importing from Apple Mail says it is blocked

The program needs Full Disk Access. Grant it to `~/.iql/bin/iql` — not to your
terminal or browser — in System Settings → Privacy & Security → Full Disk
Access, then restart InboxQL. After every update, grant it again: macOS ties
the grant to the exact file.

## An account will not sync

`iql account verify <id>` classifies the failure: the password was rejected,
the server name was not found, the network could not reach it, or the
certificate did not verify. For Gmail and iCloud a rejected password usually
means you need an *app password* rather than your sign-in password.

## Search does not find something I know is there

- Search is by **whole words**. `subject:invoice` does not match *invoiced*; use
  `subject:*invoice*`.
- Search is by **words, not meaning**. `invoice` does not find *billing*. Ask more
  than one way.
- A file's contents are only searchable once read: `in:attachments is:unread`
  lists the files nobody has read yet. See [Files](files.md).
- `-label:x` leaves out mail the annotator has not looked at. Use
  `-label:x OR unlabeled:x` for both.

## Queries are slow

The log records every slow query with its SQL: `iql log --slow`, or
`in:logs duration>1000` in the Desk. `iql doctor` also reports whether this
copy was built with the full-text index; one built without it still answers
correctly, but by scanning.

After an import of a lot of mail, `iql maintenance analyze` refreshes the
database's statistics.

## An annotator seems stuck

The first run of a local model loads it, which takes several seconds before
the first message is evaluated. On battery, the automatic pass after a sync
waits for mains power — Settings → System says when that is happening. A run
evaluates at most 200 messages at a time; the rest wait for the next.

## Reporting a problem

Include `iql version`, `iql doctor --json`, and the relevant lines of the log.
Be careful with the log: it can contain the text of your queries, and those can
name people.
