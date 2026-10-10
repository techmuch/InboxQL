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
| 4 | a person has to do this, at a terminal |
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
| `send` | queue a draft. **It does not send.** |
| `outbox` | `list` `show` — and `approve`, which a person runs at a terminal and confirms by typing `yes` |

### Mail in

| | |
|---|---|
| `account` | `add` `list` `remove` `verify` `sync` |
| `import` | `sources` `mailboxes` `scan` `run` `eml` — read-only, from Apple Mail or `.eml` files |
| `export` | messages out, as `.eml` or JSON |

### Labels, contacts, tickets

| | |
|---|---|
| `annotate` | labels and extractors — `list` `create` `run` `plan` `correct` `score` `starters` `enable` `disable` `sweep` |
| `laya` | the local judgement model — `install` `status` `calibrate` `remove` |
| `gliner` | the local extraction model — `install` `status` `remove` |
| `contact` | `list` `show` `set` `label` `note` `responsiveness` `classify` |
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
returns, what the exit codes mean, and why `send` does not send.
