# Universal Email Analytics (UEA) - Gemini Context

This file serves as a comprehensive guide for Gemini CLI to understand the UEA project's architecture, technologies, and development workflows.

## Project Overview
**Universal Email Analytics (UEA)** is a high-performance, self-hosted web application for deep-dive email analytics. It centralizes data from multiple IMAP accounts into a local-first environment, enabling interactive dashboards, trend discovery, and AI-assisted workflows while maintaining strict user privacy.

### Key Technologies
- **Backend:** Go (Golang)
- **Frontend:** React (TypeScript, Vite), based on the **nexus-shell** internal npm package (inspired by VS Code and Jupyter Lab).
- **Database:** SQLite (with WAL mode, FTS5 for lexical search, and planned vector extensions for semantic search)
- **Protocols:** IMAP (for email synchronization)
- **AI Integration:** Support for local (Ollama/llama.cpp) and remote (OpenAI, Gemini) LLM backends.

## Project Structure
- `cmd/uea/`: Contains the main entry point (`main.go`). The binary serves as both the web server and the CLI administrative tool.
- `internal/`: Core backend logic.
    - `account/`: Account configuration and credential management.
    - `vault/`: AES-256-GCM encryption at rest for account passwords (machine-local key in `data/vault.key`).
    - `hasher/`: Content-aware hashing algorithms for message deduplication.
    - `message/`: Message data structures and processing logic.
    - `store/`: SQLite persistence layer, including schema migrations and optimized queries.
    - `sync/`: Multi-account IMAP sync engine with intelligent worker pools and stateful incremental sync.
    - `embed/`: Handles embedding of the compiled frontend assets into the Go binary.
- `frontend/`: Modern React application.
    - `src/`: React components, views, and API client.
    - `public/`: Static assets for the frontend.

## Building and Running

The project uses a `Makefile` to manage the build lifecycle.

### Key Commands
- `make all`: Builds both the frontend and the backend.
- `make build`: Builds both the frontend and the backend.
- `make frontend`: Installs dependencies, builds the React application, and copies assets to `internal/embed/static/`.
- `make backend`: Compiles the Go application into `bin/uea`.
- `make start`: Builds and runs the backend in the background (default).
- `make start --foreground`: Runs the backend in the foreground.
- `make test`: Runs both backend and frontend tests.
- `make run`: Alias for `make start`.
- `make restart`: Stops and then starts the server.
- `make stop`: Stops any running backend instances.
- `make clean`: Removes build artifacts and logs.

### Development Workflow
1. **Frontend Development:**
   - Navigate to `frontend/`.
   - Run `npm run dev` to start the Vite development server (defaults to `http://localhost:3000`).
2. **Backend Development:**
   - Run `go run ./cmd/uea/main.go` to start the backend server (defaults to `http://localhost:8080`).
3. **Testing:**
   - All tests: `make test`
   - Backend: `go test ./...`
   - Frontend: `npm --prefix frontend test` (Vitest).

## Development Guidelines
- **Observability:** When the application is running, always open a browser tab (using the `new_page` tool) to the local development URL (typically `http://localhost:8080`) to observe the tool operating and validate UI changes.
- **Frontend Framework:** The UI must be built using the **nexus-shell** internal npm package, following its VS Code and Jupyter Lab-inspired design patterns.
- **Incremental Sync:** Always respect the IMAP UID and MODSEQ to avoid redundant data fetching.
- **Concurrency:** Use the `SyncManager` in `internal/sync` to manage connection limits per host.
- **Database:** Perform schema changes via the migration logic in `internal/store/store.go`. Ensure `PRAGMA user_version` is updated.
- **Error Handling:** Follow standard Go error handling patterns; wrap errors with context where appropriate.
- **Credentials:** Never write an account password to the database directly — `store.SaveAccount` encrypts via `internal/vault`, and `GetAccount`/`ListAccounts` decrypt. Never serialise a password to an API client; use `account.Redacted()` / `account.RedactAll()` on any handler that returns accounts.
- **TLS:** IMAP connections must verify certificates. Do not reintroduce `InsecureSkipVerify`.
- **Long-running goroutines:** Anything launched with `go` must recover from panics; a crashed sync worker must not take down the server.
- **Styling:** Prefer Vanilla CSS for maximum flexibility and performance.
- **Performance:** Use React Virtualization for the message feed to handle large datasets with zero lag.

## CLI Commands
The `uea` binary supports several administrative commands:
- `uea account`: Manage email accounts (add, list, remove, verify).
- `uea doctor`: Run diagnostics on the installation.
- `uea maintenance`: Re-index vectors or reclaim disk space.
- `uea backup`: Create and manage encrypted backups.

## Implementation Status

Do not assume a feature described in `requirements.md` exists. As of the latest commit:
- **Working:** IMAP sync with per-host concurrency limits and UID-based incremental fetch, message storage/dedup, auth + sessions, encrypted credentials, analytics dashboard with cross-filtering.
- **Placeholder:** search is `LIKE` on subject only (no FTS5, no vectors); "topics" is the first word of the subject line (no LDA/NLP).
- **Not implemented:** LLM gateway, Bullet-to-Draft/SSE, sentiment analysis, sync history ledger, MODSEQ/UIDVALIDITY handling, CLI subcommands (`doctor`/`maintenance`/`backup`), S3 backup.
- **Design preview:** the Visual AI Agent Builder saves graph JSON but has no Eino runtime — there is no `eino` dependency in `go.mod` and no endpoint that executes an agent.

## Future Roadmap (Inferred)
- **Semantic Search:** Full integration of vector embeddings for message bodies.
- **Social Network Mapping:** Visualization of sender/recipient relationships.
- **Bullet-to-Draft:** AI-assisted email composition from bullet points.
- **Secure Cloud Backup:** Encrypted streaming to S3-compatible storage.
