# Developer On-ramp: InboxQL — Email for Engineers

This document provides a comprehensive guide for developers who want to contribute to the InboxQL project.

## 1. Architecture Overview

InboxQL is a web application with a Golang backend and a ReactJS frontend.

### 1.1. Backend (Golang)

The backend is responsible for:

*   **Multi-Account Sync Engine**: A sophisticated worker pool architecture that manages concurrency on a per-host basis. It uses a stateful incremental sync to fetch new headers or flags, and a content-aware hashing algorithm for deduplication.
*   **Data Persistence & Hybrid Search**: A hybrid search architecture that combines a lexical layer (FTS5) and a semantic layer (Vector Index) for fast and accurate search results.
*   **API & AI Gateway**: A unified interface for multiple AI backends, with support for streaming responses using Server-Sent Events (SSE). Integrates the Eino framework for orchestrating AI agent workflows.
*   **CLI Management Suite**: A powerful administrative tool for managing accounts, running diagnostics, and performing maintenance tasks.

### 1.2. Frontend (ReactJS)

The frontend is a modern ReactJS application built with Vite, TypeScript, and Tailwind CSS. It leverages the `nexus-shell` framework to provide a professional, VS Code-inspired Master-Detail-Filter layout. It utilizes `zustand` for high-performance global state management (especially for cross-filtering logic), `@nivo/calendar` for rich data visualizations, and `reactflow` for the Visual AI Agent Builder.

## 2. Getting Started

### 2.1. Prerequisites

*   Go 1.21+
*   Node.js 20.x and npm
*   A C compiler (for `sqlite-vss`)

### 2.2. Installation

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/your-username/iql.git
    cd iql
    ```

2.  **Build the entire project (Frontend & Backend):**

    The simplest way to get started is to use the provided `Makefile`:

    ```bash
    make build
    ```
    This command installs frontend dependencies, builds the React application, embeds the assets into the Go binary, and compiles the backend into `bin/iql`.

### 2.3. Running the application

The project provides several ways to run the application using `make`:

1.  **Run in Background (Default):**
    ```bash
    make start
    ```
    This stops any running instances, rebuilds the backend, and starts it in the background, logging to `iql.log`.

2.  **Run in Foreground:**
    ```bash
    make start --foreground
    ```
    Use this if you want to see the logs directly in your terminal.

3.  **Access the Dashboard:**
    Once running, open your browser to `http://localhost:8080`.

## 3. Development Workflows

### 3.1. Makefile Reference

The `Makefile` is the primary tool for managing the development lifecycle.

| Command | Description |
| :--- | :--- |
| `make all` | Alias for `make build`. |
| `make build` | Builds both the frontend and the backend. |
| `make frontend` | Installs npm dependencies and builds the React application. |
| `make backend` | Compiles the Go backend into `bin/iql`. |
| `make test` | Runs both backend (`go test`) and frontend (`vitest`) tests. |
| `make start` | Runs the backend in the background (logs to `iql.log`). |
| `make start --foreground` | Runs the backend in the foreground. |
| `./bin/iql` | Lists every CLI subcommand. The server is `iql start`. |
| `./bin/iql <cmd> --help` | Detail on one command; identical to `iql help <cmd>`. |
| `make stop` | Stops any running backend instances. |
| `make restart` | Restarts the backend. |
| `make clean` | Removes build artifacts (`bin/`, `frontend/dist/`, etc.). |

**`make start` runs passwordless**, because that is the default: InboxQL binds
`127.0.0.1` and does not ask for a password there. Run
`make start AUTH=--require-password` to exercise the login flow.

The auth model lives in `trustDecision` in `internal/cli/serve.go`, decided
once at startup from the listen address, plus the per-request forwarded-header
check in `internal/auth`. `TestTrustDecision` is the table version of it;
change one without the other and it fails.

### 3.2. How the CLI is assembled

Commands register themselves into `cli.Commands` from each file's `init`, and
`internal/cli/root.go` builds a Cobra tree from that registry. Cobra owns the
command tree, help routing, completion and typo suggestions; it does **not**
parse subcommand flags. Each command still parses its own with the stdlib
`flag` package, which is why every command sets `DisableFlagParsing`.

Two consequences worth knowing before editing:

*   Global flags are stripped by `splitGlobals`, not by Cobra, so they work in
    any position. Anything after a bare `--` is left alone.
*   Because Cobra does no flag parsing for these commands, it also hands the
    globals to completion functions as if they were positional arguments.
    `positional()` in `completion.go` exists for exactly that.

Human output goes through `internal/cli/ui`. Do not hand-roll column widths
with `%-20s`; build a `Printer` from the context and use `NewTable`, whose
columns size themselves. Colour is decided once, from whether the destination
is a terminal, and a table styled through `Cell`/`Emphasise` is guaranteed to
lay out identically with colour on or off — there is a test that asserts it.

Adding a command means: register it, put its name in `commandOrder` and
`commandGroup` (a test fails otherwise), and add its subcommand verbs to
`subcommands` so completion offers them.

### 3.3. Tests and CI

Three layers, run by `.github/workflows/ci.yml` on every push:

| Layer | Command | What it covers |
|---|---|---|
| Unit | `make test` | Go packages and the frontend suite. |
| End-to-end | `make e2e` | The real binary: CLI contract, HTTP auth, outbox gate, backup round trip. |
| Build | CI only | Native compile on Linux, macOS and Windows. |

`make test-all` runs the first two together.

End-to-end tests live in `e2e/` behind a `//go:build e2e` tag, so `go test
./...` does not pay for them. They compile the binary once, run each case
against its own temp data directory, and start servers on free loopback ports —
so they are safe to run in parallel with a live install.

**Why the build matrix has three runners.** `mattn/go-sqlite3` is cgo, and
`iql backup` uses SQLite's online backup API, which exists only in the cgo
build. `CGO_ENABLED=0` fails to compile rather than degrading, so `GOOS=windows
go build` from Linux is not an option without a cross C toolchain. Building
natively on each platform is simpler. The rest of the tree is portable: with
that one call excluded, every package compiles for all three.

Releases are tag-driven (`.github/workflows/release.yml`): push `v0.1.0` and it
builds all three platforms, checks each binary reports the tagged version,
and opens a draft GitHub release with checksums. macOS builds are unsigned for
now; notarisation is the next step if InboxQL is distributed beyond your own
machines.

### 3.4. Local Development (Hot Reloading)

For a faster development loop with hot-module replacement (HMR), you can run the components separately:

1.  **Start the Backend:**
    ```bash
    go run ./cmd/iql/main.go
    ```
    (Defaults to `http://localhost:8080`)

2.  **Start the Frontend Dev Server:**
    ```bash
    cd frontend
    npm run dev
    ```
    (Defaults to `http://localhost:3000`)

The frontend dev server will proxy API requests to the backend.

### 3.5. Backend Development

The backend code is located in `internal/`.

*   **API Endpoints**: Defined in `internal/auth/` and other relevant packages.
*   **Sync Engine**: Implemented in `internal/sync/`.
*   **Storage**: Database logic is in `internal/store/`.

### 3.6. Frontend Development

The frontend code is located in the `frontend/src` directory.

*   **Agent Manager**: `AgentManager.tsx` handles the visual builder logic.
*   **Global State**: Managed via `App.tsx` and related components.

## 4. Testing

Testing is handled via `make test`, which triggers:
- **Backend**: `go test ./...`
- **Frontend**: `npm test` (Vitest)

For more details on manual verification, see [test.md](test.md).

## 5. CI/CD

Two GitHub Actions workflows. See §3.3 for what each layer covers and why the
build matrix needs three runners.

| Workflow | Trigger | Jobs |
|---|---|---|
| `ci.yml` | every push, every PR | `test` (vet, gofmt, `go test -race`), `frontend` (typecheck, vitest, build), `build` (Linux / macOS / Windows), `e2e` |
| `release.yml` | tags matching `v*` | three-platform build, version check against the tag, draft release with checksums |

`build` and `e2e` depend on `test` and `frontend`, so a failing unit test stops
the run before anything is compiled three times.

## 6. Contributing

We welcome contributions from the community! If you're interested in contributing to InboxQL, please follow these steps:

1.  Fork the repository.
2.  Create a new branch for your feature or bug fix.
3.  Make your changes and commit them with a descriptive commit message.
4.  Push your changes to your fork.
5.  Open a pull request to the `main` branch of the original repository.
