# The command line

`iql` is both the server and a command line over the same mailbox. Anything
the application does, a script can do — and most of what is in these guides
has a command beside it.

```sh
iql                       # every command, grouped
iql help <command>        # one command in detail; same as iql <command> --help
```

## Global flags

These work anywhere on the line, before or after the command:

| Flag | |
|---|---|
| `--data <folder>` | use this mailbox rather than the machine's |
| `--json` | machine-readable output on stdout, nothing else; messages go to stderr |
| `--verbose` | include the database's routine logging |
| `--no-color` | plain text, even at a terminal (or set `NO_COLOR`) |

Output to a terminal is an aligned table, coloured; piped or redirected it is
plain text, so `iql search --query invoice | awk '{print $1}'` gives message
ids. For anything a program will read, use `--json`.

## Which mailbox

Every command except `init` and `setup` needs an existing mailbox, chosen in
this order:

1. `--data <folder>`;
2. `INBOXQL_DATA`;
3. `dataDir` in `~/.iql/settings.json`;
4. `./data` in the current directory — only when there are no machine settings.

`iql where` prints the answer and the reason. A command never creates a
mailbox by accident: with none to use, it stops and says how to make one.

`INBOXQL_HOME` moves `~/.iql` itself, settings and all.

## Exit codes

| Code | Means |
|---|---|
| 0 | it worked |
| 1 | something failed — read stderr |
| 2 | the command was written wrong, and did not run |
| 3 | the account, message or draft does not exist |
| 4 | a person has to do this, at a terminal (approving an agent's calendar change) |
| 5 | something it needs is missing — stderr names it |

## The commands

### Getting started

| | |
|---|---|
| `setup` | make this machine's mailbox and settings |
| `where` | which mailbox, and why |
| `service` | `install` `uninstall` `start` `stop` `restart` `status` — the login service |
| `update` | install the newest release, with a backup first; `--check` only looks |
| `hosts` | `set <name>` `remove` `show` — a name for the site |
| `init` | prepare a mailbox folder: database, key, administrator |
| `start` | run the server in this terminal (`--addr`, `--open`, `--dev`) |
| `open` | open the running server in your browser; `--print` just prints the address |
| `version` | which version, built from what |
| `doctor` | health checks; non-zero when any fail |

### Asking questions

| | |
|---|---|
| `query` | the [query language](query-language.md); `--explain` shows the SQL, `--count` just the number |
| `saved` | `list` `show` `save` `delete` `move` `icon` — named queries and the rail |
| `sql` | read-only SQL for anything the language cannot say; `--schema` lists tables |
| `search` | the older flag-based search |
| `read` | a message, or `--thread` for its conversation |
| `analyze` | summarise a thread, with an AI provider — or emit it for you to read |

### Mail out

| | |
|---|---|
| `draft` | `create` `list` `show` `delete` — `--reply-to <id>` fills in the rest |
| `send` | queue a draft in the outbox. **It does not send.** |
| `outbox` | `list` `show` `reject` — and `approve`, which sends: at a terminal it shows the message and asks, and `--yes` sends without asking |

### Changing mail

| | |
|---|---|
| `mail` | `archive` `inbox` `trash` `untrash` `spam` `notspam` `label <name>` `unlabel <name>` `read` `unread` `star` `unstar`, each on one or more message ids |
| `labels` | every label with its counts; `create <name>`, `delete <name>` |

A change is made here at once and sent to the account's server by the running
server within seconds.

### Mail in

| | |
|---|---|
| `account` | `add` `list` `discover` `verify` `sync` `disconnect` `remove` — `sync` waits for all of the mail; the running server keeps accounts live without it |
| `import` | `sources` `mailboxes` `scan` `run` `eml` — read-only, from Apple Mail or `.eml` files |
| `export` | messages out, as `.eml` or JSON |

### Labels, contacts, tickets

| | |
|---|---|
| `annotate` | labels and extractors — `list` `create` `run` `plan` `correct` `score` `starters` `enable` `disable` `sweep` |
| `laya` | the local judgement model — `install` `status` `calibrate` `remove` |
| `gliner` | the local extraction model — `install` `status` `remove` |
| `contact` | `list` `show` `set` `label` `note` `responsiveness` `classify` |
| `calendar` | `agenda` `create` `update` `delete` `respond` `changes` `approve` `import` `export` `connect` `sync` — see [Calendar](calendar.md) |
| `ticket` | `list` `show` `new` `move` `accept` `reject` `merge` `propose` `board` |

### Looking after it

| | |
|---|---|
| `backup` / `restore` | a consistent copy of the database, safe while running |
| `maintenance` | `attachments` `text` `reindex` `analyze` `vacuum` `integrity` `prune-log` |
| `ocr` | read scanned files with a vision model |
| `log` | what InboxQL did; `log level` sets how much it records |
| `llm` | the AI provider: `status` `configure` `test` `disable` |
| `user` | dashboard logins; `user passwd` |
| `vault` | the key account passwords are encrypted with |
| `ui` | the open browser windows — see what each shows, point one at a query |
| `skills` | `list` `install` `remove` — teach AI coding tools how to use iql |

Two commands are destructive and cannot be undone: `account remove` deletes
every message stored for that account, and `vault rotate` re-encrypts every
stored password.

## Passwords are never flags

Anything secret is read from an environment variable, from standard input, or
from a prompt — never from the command line, which ends up in shell history
and is visible to other programs. `INBOXQL_ACCOUNT_PASSWORD` for `account add`,
`INBOXQL_NEW_PASSWORD` for `user passwd`.

## Completion

```sh
iql completion zsh  > "${fpath[1]}/_iql"
iql completion bash > /usr/local/etc/bash_completion.d/iql
iql completion fish > ~/.config/fish/completions/iql.fish
```

Completion knows real values where it can: `iql account sync <Tab>` offers your
account ids.

## Driving it from an agent

The command line is designed to be used by an AI agent as well as a person.
[For agents](agents.md) is the contract it follows — what every command
returns, what the exit codes mean, and that it sends mail only when you ask it
to. `iql help agents` prints the same contract from the program itself.

An agent can do anything you can on this computer, the command line included,
so the safeguard is what it is told: the contract and the skill tell it to
draft and queue, and to send only when asked. A message it queues waits in
**Waiting for approval**, marked as written by an agent.

To give the AI tools you use that knowledge up front, install the iql skill:

```sh
iql skills list             # which tools are here, and what is installed
iql skills install claude   # Claude Code: ~/.claude/skills/iql
iql skills install codex    # Codex and Gemini CLI: ~/.agents/skills/iql
```

The skill is short — the rules that stop an agent doing harm, the commands it
will use most, and a pointer to `iql help agents` for everything else.
Codex and Gemini CLI read the same folder, so one install serves both. Only a
skill iql wrote is ever replaced or removed, and a new version of iql keeps
installed ones current. Settings → System → **AI assistants** does the same
with buttons.
