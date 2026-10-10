# Installing, updating and removing

InboxQL is one program, `iql`. The same file is the server you open in a
browser and the command line you can script. Installing it means putting that
file somewhere, telling it which mailbox is yours, and asking your operating
system to start it when you log in.

## The one-line install

On macOS or Linux:

```sh
curl -fsSL https://techmuch.github.io/InboxQL/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://techmuch.github.io/InboxQL/install.ps1 | iex
```

In order, it:

1. downloads the release for this computer and the release's `SHA256SUMS`;
2. refuses to go on unless the download matches its published checksum;
3. installs `iql` into `~/.iql/bin` and adds that folder to your `PATH`;
4. runs `iql setup`, which writes `~/.iql/settings.json` and makes your mailbox;
5. runs `iql service install`, which starts InboxQL now and at every login.

Run it again on a machine that already has InboxQL and it updates instead.

Release builds exist for macOS (Apple Silicon and Intel, in one file), and for
Linux and Windows on x86-64. Anything else builds from source with `make build`.

Environment variables change what the installer does: `INBOXQL_HOME` installs
somewhere other than `~/.iql`, `INBOXQL_VERSION=v0.1.0` picks a release, and
`INBOXQL_NO_SERVICE=1` installs without starting it at login.

## Where everything goes

| Path | What it is |
|---|---|
| `~/.iql/bin/iql` | The program. |
| `~/.iql/settings.json` | Which mailbox this machine uses, the address, the models folder. |
| `~/.iql/data` | Your mailbox: the database, its key, attachments, backups. |
| `~/.iql/models` | Model weights, downloaded once per machine when first needed. |
| `~/.iql/logs/service.log` | What the login service printed. |
| `~/.iql/logs/update.log` | What the last update started from Settings did. |

On Windows `~` is your user folder, so the program is
`C:\Users\you\.iql\bin\iql.exe`.

**Back up `~/.iql/data`**, and above all keep `vault.key` with the database.
Account passwords are encrypted with that key; without it they cannot be read
back. `iql backup --include-key` copies both.

## The login service

InboxQL runs as *your* service, not a system one. It starts when you log in,
stops when you log out, and it never keeps a laptop awake: closing the lid
sleeps the machine and InboxQL with it.

| | How it is installed | After a crash |
|---|---|---|
| macOS | a LaunchAgent, `io.github.techmuch.inboxql` | restarted |
| Linux | a systemd user unit, `inboxql.service` | restarted |
| Windows | a logon task, `InboxQL`, with no window | start it again |

On Linux a user unit runs while you are logged in. To have it start at boot
instead, run `loginctl enable-linger $USER` once.

```sh
iql service status      # installed? running? at what address?
iql service stop        # until the next login
iql service start
iql service restart     # after editing settings.json by hand
iql service uninstall   # stop it and remove the definition; mail is untouched
```

**Settings → System** shows the same state and has a **Restart** button.

### On battery

On a laptop running on battery, InboxQL holds back the heavy work nobody asked
for: the pass of labels and extractors that follows a sync waits for mains
power. Mail still syncs; what you start by hand still runs. To let the
automatic pass run on battery anyway, switch on **Heavy work on battery** in
Settings → System.

## Updating

From the application: **Settings → System → Updates**. It says whether a newer
release exists, and **Update** installs it.

From a terminal:

```sh
iql update            # install the newest release
iql update --check    # only say whether there is one
```

Either way the update:

1. downloads the new release and checks it against `SHA256SUMS` — a mismatch,
   or no checksum at all, stops here with nothing changed;
2. **backs up your mailbox** into its `backups` folder;
3. stops the server;
4. replaces the program;
5. starts it again.

Your mailbox's database is upgraded when the new version first opens it, after
the backup. The page you started the update from reconnects by itself.

**Installed with Homebrew?** Then Homebrew owns the program, and `iql update`
refuses rather than disagreeing with it. Use `brew upgrade inboxql`.

**On a Mac**, importing from Apple Mail needs Full Disk Access for the program.
InboxQL is not signed with an Apple Developer ID, so macOS ties that grant to
the exact file, and a new version is a new file: grant it again in System
Settings → Privacy & Security → Full Disk Access after each update.

## A name instead of a port

`http://localhost:8420` works. If you would rather type a name:

```sh
iql hosts set inboxql.localhost
```

That records the name in your settings and adds two lines to your hosts file,
between marker comments so nothing else in it is touched. The hosts file
belongs to the administrator, so the command prints exactly what to run to
finish — `sudo iql hosts set inboxql.localhost` on macOS and Linux, or the same
command in a PowerShell opened with *Run as administrator* on Windows. Then
open `http://inboxql.localhost:8420`.

Use a name ending in `.localhost` or `.test`. Names ending in `.local` are
refused: macOS looks those up on the network before it reads the hosts file,
so every page would wait. `iql hosts remove` takes the lines out again.

You can also set the name in Settings → System; the hosts-file step is still
one command in a terminal, because a web page asking for an administrator's
password is exactly what an attack looks like.

## More than one mailbox

A mailbox is a folder. The one in `~/.iql/settings.json` is this machine's, and
every command and the login service use it. Any other is a `--data` away:

```sh
iql start --data ~/Mail/archive-2019 --addr 127.0.0.1:8421
iql query --data ~/Mail/archive-2019 "from:alice"
iql where                       # which mailbox a command would use, and why
```

One server runs per mailbox. Starting a second on the same folder tells you
where the first is instead of fighting over the database.

To make another folder the machine's mailbox, use **Use a different mailbox…**
in Settings → System, or `iql setup --data <folder> --force`. Nothing is moved:
both mailboxes stay where they are.

## Removing it

```sh
iql hosts remove          # if you set a name
iql service uninstall
rm -rf ~/.iql/bin
```

That leaves your mailbox in `~/.iql/data`. Delete `~/.iql` to remove
everything. The installer also added one line to your shell's profile, marked
`# added by the InboxQL installer`, which you can delete.

## Building from source

```sh
git clone https://github.com/techmuch/InboxQL
cd InboxQL
make build          # the frontend, then bin/iql with the frontend inside it
./bin/iql setup
./bin/iql service install
```

You need Go and Node.js. `make build` builds the web interface first, because
the program serves the copy embedded in it.
