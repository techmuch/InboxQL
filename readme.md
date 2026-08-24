# Universal Email Analytics (UEA)

**Universal Email Analytics (UEA)** is a powerful, self-hosted web application for deep-dive email analytics. It connects to your existing email accounts via IMAP and provides a comprehensive dashboard to explore your data, discover trends, and gain insights from your communication history.

## Key Features

*   **Unified Dashboard**: Aggregate and analyze email data from multiple accounts in a single professional-grade interface powered by the `nexus-shell` framework.
*   **Visual AI Agent Builder** *(design preview)*: Draft agent topologies on an interactive node-based canvas (`reactflow`). Graphs are saved as definitions; the Eino execution runtime is not implemented yet.
*   **Interactive Analytics**: Explore your data with interactive heatmaps (powered by `@nivo/calendar`), dynamic donut charts, and topic treemaps. Drill down with deep cross-filtering between dates, senders, and topics.
*   **Intelligent Mailbox**: Seamlessly pivot from high-level analytics to a high-performance email feed filtered precisely by your dashboard selections.
*   **Privacy-First**: Your data is stored locally, and no email content is ever sent to a third party without your explicit consent. Account passwords are encrypted at rest with AES-256-GCM.
*   **Cross-Platform**: UEA is available for Windows, macOS, and Linux.

## Status

UEA is in active early development. The table below is the honest state of play — several items described in `requirements.md` are specified but not yet built:

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
*   **Default account**: on first run UEA creates `admin@uea.local` with a well-known development password. Set `UEA_ADMIN_PASSWORD` (and optionally `UEA_ADMIN_USER`) before running UEA anywhere other than localhost.

## Getting Started

UEA is distributed as a single, zero-dependency binary.

### Download Release

Download the latest release for your operating system, prepare a data directory,
then start the server:

```bash
./uea init      # creates ./data with the database and encryption key
./uea serve
```

`./uea` on its own lists every subcommand — it is an administrative CLI as well
as a server. You can then access the UEA dashboard by opening your web browser and navigating to `http://localhost:8080`.

### Build from Source

To build UEA from source, ensure you have Go 1.21+ and Node.js 20+ installed, then run:

```bash
make build
```

This will produce the `bin/uea` executable. You can start the server in the foreground with:

```bash
make start --foreground
```

## CLI Management

UEA also includes a powerful command-line interface (CLI) for managing your accounts and performing other administrative tasks.

*   `uea account`: Add, list, remove, or verify connections to your email accounts.
*   `uea doctor`: Run diagnostics to check the health of your UEA installation.
*   `uea maintenance`: Perform maintenance tasks such as re-indexing your data.
*   `uea backup`: Create and manage backups of your UEA data.

For more information on the CLI, run `uea --help`.

## Contributing

We welcome contributions from the community! If you're interested in contributing to UEA, please see our [Development Guide](development.md) for more information.
