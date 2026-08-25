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
| `./bin/iql` | Lists every CLI subcommand. The server is `iql serve`. |
| `make stop` | Stops any running backend instances. |
| `make restart` | Restarts the backend. |
| `make clean` | Removes build artifacts (`bin/`, `frontend/dist/`, etc.). |

### 3.2. Local Development (Hot Reloading)

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

### 3.3. Backend Development

The backend code is located in `internal/`.

*   **API Endpoints**: Defined in `internal/auth/` and other relevant packages.
*   **Sync Engine**: Implemented in `internal/sync/`.
*   **Storage**: Database logic is in `internal/store/`.

### 3.4. Frontend Development

The frontend code is located in the `frontend/src` directory.

*   **Agent Manager**: `AgentManager.tsx` handles the visual builder logic.
*   **Global State**: Managed via `App.tsx` and related components.

## 4. Testing

Testing is handled via `make test`, which triggers:
- **Backend**: `go test ./...`
- **Frontend**: `npm test` (Vitest)

For more details on manual verification, see [test.md](test.md).

## 5. CI/CD

The project uses GitHub Actions for CI/CD. The workflow is defined in `.github/workflows/ci.yml`. The workflow is triggered on every push to the `main` branch and on every tagged release. It builds the application for all target architectures and runs the tests.

## 6. Contributing

We welcome contributions from the community! If you're interested in contributing to InboxQL, please follow these steps:

1.  Fork the repository.
2.  Create a new branch for your feature or bug fix.
3.  Make your changes and commit them with a descriptive commit message.
4.  Push your changes to your fork.
5.  Open a pull request to the `main` branch of the original repository.
