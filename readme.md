# InboxQL

**Email for Engineers**

**InboxQL** is a powerful, self-hosted web application for deep-dive email analytics. It connects to your existing email accounts via IMAP and provides a comprehensive dashboard to explore your data, discover trends, and gain insights from your communication history.

## Key Features

*   **Unified Dashboard**: Aggregate and analyze email data from multiple accounts in a single professional-grade interface powered by the `nexus-shell` framework.
*   **Visual AI Agent Builder** *(design preview)*: Draft agent topologies on an interactive node-based canvas (`reactflow`). Graphs are saved as definitions; the Eino execution runtime is not implemented yet.
*   **Interactive Analytics**: Explore your data with interactive heatmaps (powered by `@nivo/calendar`), dynamic donut charts, and topic treemaps. Drill down with deep cross-filtering between dates, senders, and topics.
*   **Intelligent Mailbox**: Seamlessly pivot from high-level analytics to a high-performance email feed filtered precisely by your dashboard selections.
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
| Search | Basic `LIKE` on subject only — no FTS5, no semantic/vector search yet |
| Topic discovery | Placeholder (first word of subject); no LDA or clustering |
| Visual AI Agent Builder | **Design preview only** — topologies can be drawn and saved, but there is no Eino runtime, so agents cannot execute |
| LLM gateway, Bullet-to-Draft, sentiment analysis | Not implemented |
| CLI suite (`doctor`, `maintenance`, `backup`) | Not implemented |
| Encrypted cloud backup | Not implemented |

## Security Notes

*   **Credentials at rest**: IMAP/SMTP passwords are sealed with AES-256-GCM using a machine-local key stored at `data/vault.key` (mode `0600`). **Back this file up alongside your database** — without it, stored passwords cannot be recovered. Passwords are never returned to the browser by the API.
*   **Transport**: IMAP TLS connections verify the server certificate. There is no option to disable this.
*   **Default account**: on first run InboxQL creates `admin@inboxql.local` with a well-known development password. Set `INBOXQL_ADMIN_PASSWORD` (and optionally `INBOXQL_ADMIN_USER`) before running InboxQL anywhere other than localhost.

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
