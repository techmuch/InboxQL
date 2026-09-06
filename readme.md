# InboxQL

**Email for Engineers**

**InboxQL** is a powerful, self-hosted web application for deep-dive email analytics. It connects to your existing email accounts via IMAP and provides a comprehensive dashboard to explore your data, discover trends, and gain insights from your communication history.

## Key Features

*   **Unified Dashboard**: Aggregate and analyze email data from multiple accounts in a single professional-grade interface powered by the `nexus-shell` framework.
*   **Visual AI Agent Builder** *(design preview)*: Draft agent topologies on an interactive node-based canvas (`reactflow`). Graphs are saved as definitions; the Eino execution runtime is not implemented yet.
*   **Interactive Analytics**: Explore your data with interactive heatmaps (powered by `@nivo/calendar`), dynamic donut charts, and topic treemaps. Drill down with deep cross-filtering between dates, senders, and topics.
*   **Desk**: One surface over mail, drafts and tickets. Folders are queries, the rail is a list of them, and the same view renders a message list, a chart or a count depending on what you asked.
*   **Privacy-First**: Your data is stored locally, and no email content is ever sent to a third party without your explicit consent. Account passwords are encrypted at rest with AES-256-GCM.
*   **Cross-Platform**: InboxQL is available for Windows, macOS, and Linux.

## Status

InboxQL is in active early development. The table below is the honest state of play — several items described in `requirements.md` are specified but not yet built:

| Area | Status |
| :--- | :--- |
| IMAP sync (incremental, per-host concurrency limits) | Working |
| Message storage, dedup hashing, analytics dashboard | Working |
| Authentication, encrypted credentials at rest | Working |
| Cross-filtering (date / sender / topic) | Working |
| Query language (`iql query`) | Working — filters, negation, aggregation pipeline |
| Search | FTS5 full-text index; lexical, not semantic. No embeddings or vector search |
| Threading | Follows `References` headers |
| Labels and extraction (`iql annotate`) | Working — rule engine and LLM engine |
| Tickets and board (`iql ticket`) | Working — proposed by extractors, owned by you |
| Drafts as a queryable entity (`in:drafts`) | Working |
| Desk — merged mailbox and query surface | Working |
| Topic discovery | Placeholder (first word of subject); no LDA or clustering |
| Visual AI Agent Builder | **Design preview only** — topologies can be drawn and saved, but there is no Eino runtime, so agents cannot execute |
| LLM gateway | Working — Ollama and any OpenAI-compatible endpoint |
| Sentiment analysis | Not built in; definable as an annotator |
| CLI suite (`doctor`, `maintenance`, `backup`) | Working |
| Encrypted cloud backup | Not implemented |

## Running it

```bash
iql init  --data ~/.inboxql
iql start --data ~/.inboxql
```

InboxQL listens on `localhost` only and does not ask for a password there. It
is your machine; you are already the only one who can reach it.

That changes as soon as you serve it to anyone else:

| How you start it | Password |
|---|---|
| `iql start` | not required — localhost only |
| `iql start --addr :8080` | required — reachable from the network |
| behind a reverse proxy | required — the request was relayed |
| `iql start --require-password` | required — always |

The reverse-proxy row is not configurable away, and it is the one worth
understanding: a proxy on this host relays every request over loopback, so the
connection *looks* local no matter who sent it. InboxQL refuses passwordless
access for any request carrying `X-Forwarded-For`, `X-Real-Ip`, `Forwarded` or
`X-Forwarded-Host`.

Neither is the other one: **a page on another site cannot drive the API.**
Passwordless access authenticates a request with no cookie, so `SameSite` does
not help — any page you visited could otherwise have reached `localhost:8080`.
InboxQL refuses passwordless access, and refuses writes outright, for any
request a browser marks as coming from elsewhere (`Sec-Fetch-Site`, or an
`Origin` that does not match the address asked for). Command-line tools send
neither header and are unaffected, which is the point: a browser cannot
suppress them and a script never sets them.

`--trust-local` forces passwordless access on despite a public listen address.
It prints a warning, and you should not need it.

## Security Notes

*   **Credentials at rest**: IMAP/SMTP passwords are sealed with AES-256-GCM using a machine-local key stored at `data/vault.key` (mode `0600`). **Back this file up alongside your database** — without it, stored passwords cannot be recovered. Passwords are never returned to the browser by the API.
*   **Transport**: IMAP TLS connections verify the server certificate. There is no option to disable this.
*   **Changing an account's server**: editing an account's IMAP host, port or username requires entering the password again. A credential stored for one server is not carried across to another.
*   **Default account**: `iql init` creates `admin@inboxql.local` with a randomly generated password, printed once and never recoverable. Set `INBOXQL_ADMIN_PASSWORD` (and optionally `INBOXQL_ADMIN_USER`) before running init if you want to choose it yourself, or reset it later with `iql user passwd`.

## Getting Started

InboxQL is distributed as a single, zero-dependency binary.

### Download Release

Download the latest release for your operating system, prepare a data directory,
then start the server:

```bash
./iql init      # creates ./data with the database and encryption key
./iql start
```

`./iql` on its own lists every subcommand — it is an administrative CLI as well
as a server. You can then access the InboxQL dashboard by opening your web browser and navigating to `http://localhost:8080`.

### Build from Source

To build InboxQL from source, ensure you have Go 1.21+ and Node.js 20+ installed, then run:

```bash
make build
```

This will produce the `bin/iql` executable. You can start the server in the foreground with:

```bash
make start --foreground
```

## CLI Management

InboxQL also includes a powerful command-line interface (CLI) for managing your accounts and performing other administrative tasks.

*   `iql account`: Add, list, remove, or verify connections to your email accounts.
*   `iql doctor`: Run diagnostics to check the health of your InboxQL installation.
*   `iql maintenance`: Perform maintenance tasks such as re-indexing your data.
*   `iql backup`: Create and manage backups of your InboxQL data.
*   `iql query`: Search and aggregate with the query language.
*   `iql sql`: Run a read-only SQL statement against your mailbox.
*   `iql annotate`: Define labels and extractors, and run them over your mail.

## The query language

InboxQL's name is not decorative. `iql query` takes a filter expression with an
optional pipeline:

```bash
iql query "from:stripe after:2026-01-01 has:attachment"
iql query "is:unread -from:*@acme.com"
iql query "| count by week"
iql query "| top domain 10"
```

Three match modes, because they are three different questions:

```bash
iql query "from:acme"          # contains — also matches notacme@x.com
iql query "from:=a@acme.com"   # exactly that address
iql query "from:*@acme.com"    # glob
```

Negation is `-` (or `NOT`) and composes over anything: `-(from:alice
after:2026-01)`. Over recipients it means *no recipient is x* rather than *some
recipient is not x*, which is the reading that would otherwise match nearly
everything.

Whatever the language cannot express, `iql sql` will — read-only, against a
documented schema. `iql sql --schema` lists the tables.

## Labels and extracted data

An annotator is a named, versioned instruction applied to your mail. A label
answers yes or no; an extractor pulls structured records out of a body.

A rule annotator is just a query, so it needs no model and runs over a whole
mailbox in milliseconds:

```bash
iql annotate create billing --engine rule \
  --instructions "from:*@stripe.com OR subject:invoice"
iql annotate run billing
iql query "label:billing | count by month"
```

An LLM annotator takes a prompt instead. Extractors turn recurring
machine-generated mail into a queryable series:

```bash
iql annotate create metrics --kind extract --engine llm --time-field day \
  --instructions "Extract each day's signup count from the weekly digest."
iql annotate run metrics --dry-run     # what would be sent, and where
iql annotate run metrics
iql query "| extract metrics | series signups by week"
```

**Labels are three-valued.** `-label:billing` means *the annotator looked and
said no* — it does not include mail the annotator has never seen. Ask for those
with `unlabeled:billing`. Without that distinction, a negative query on a
partly-labelled mailbox returns everything nobody has looked at yet and
presents it as an answer.

A run with a remote provider sends message bodies to that provider, so it
refuses without recorded consent (`--allow-remote`) and `--dry-run` reports
what would leave the machine. With Ollama, nothing does.

Corrections outrank the machine: `iql annotate correct billing <id> --yes`
survives every re-run and every version bump.

## Tickets and the board

An extractor that pulls actions out of your mail can raise tickets, which are
entities with their own state rather than another view of the annotation:

```bash
iql ticket propose actions --dry-run       # what would be raised
iql ticket propose actions --auto-accept 0.9
iql ticket list --query "status:proposed"  # the queue, for you to rule on
iql ticket board
```

**Extraction proposes; it does not create.** Anything below the confidence bar
waits in `status:proposed`. A task list you cannot trust is worse than none.

Ticket fields are query terms, so one language filters both, and a message field
inside a ticket query asks about the ticket's *evidence*:

```bash
iql query "status:todo due:7d"
iql query "status:* from:*@acme.com"     # tickets whose mail came from Acme
iql query "status:* | count by status"
```

**A re-run never rewrites what you set.** Editing an extractor's prompt bumps
its version and invalidates every annotation — and changes nothing on your
board. Tickets are seeded by annotations and owned by you; `ticket_sources`
records which messages are the evidence, and `iql ticket show` prints them.

Run `iql --help` for the full list, and `iql help <command>` — or
`iql <command> --help`, which is the same page — for detail on any one of them.

Global flags (`--data`, `--json`, `--verbose`, `--no-color`) may appear before
or after the command, so `iql doctor --data ./data` and
`iql --data ./data doctor` are the same invocation.

### Shell completion

```bash
iql completion bash > /usr/local/etc/bash_completion.d/iql   # bash
iql completion zsh  > "${fpath[1]}/_iql"                     # zsh
iql completion fish > ~/.config/fish/completions/iql.fish    # fish
```

Completion covers command and subcommand names, and resolves real values where
it can — `iql account sync <TAB>` offers your configured account ids.

### Output

Human output is a plain aligned table, coloured only when writing to a
terminal. Redirect it or pipe it and you get unstyled text, so
`iql search --query invoice | awk '{print $1}'` gives you message ids. Set
`NO_COLOR` or pass `--no-color` to turn colour off at a terminal too. For
anything programmatic, prefer `--json`.

## Contributing

We welcome contributions from the community! If you're interested in contributing to InboxQL, please see our [Development Guide](development.md) for more information.
