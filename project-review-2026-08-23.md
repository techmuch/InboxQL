# UEA Project Review & nexus-shell Assessment
*August 23, 2026*

## TL;DR

**UEA** is badly overextended relative to what's built. `requirements.md` describes an enterprise-grade product (encrypted credential vault, hybrid FTS5+vector search, a sync audit ledger, an AI agent execution engine, S3 backup, sentiment/LDA analysis). The actual codebase is 7 commits and ~3,450 lines of Go/TS implementing a basic IMAP sync + a plain-text login + a message list + a visual editor whose backend doesn't exist yet. Several of the "done" features are UI shells with no logic behind them, and there are two real security holes (plaintext credentials, disabled TLS verification) that should be fixed before anything else.

**nexus-shell** is in noticeably better shape than the "still finding its way" framing suggests — clean code, no `eval`/`dangerouslySetInnerHTML`, low `any` usage, real test coverage. I found three concrete, fixable issues (below), and the more urgent problem is that UEA is pinned to a version from six minor releases ago, so none of this actually affects UEA yet.

---

## Part 1: UEA — Requirements Compliance

Total implementation: 8 Go files (1,681 lines) + 5 frontend files (~2,000 lines), across 7 commits. `requirements.md` is 26.5KB. Section-by-section, against `go.mod` (only deps: `go-imap`, `go-message`, `uuid`, `mattn/go-sqlite3` — no vector library, no Prometheus/expvar, no S3 SDK, no LLM client, no Eino) and the actual code:

| Requirement | Status | Evidence |
|---|---|---|
| Per-host worker pool with rate limiting | **Partial** | `sync.go` does implement a per-host channel semaphore (`SyncManager.hostConnections`) — this part is real. |
| Worker state introspection API (expvar/Prometheus) | **Missing** | No such endpoint registered in `main.go`; no metrics library in `go.mod`. |
| Panic recovery / dead-letter per worker | **Missing** | `main.go:241` does `go syncManager.StartSync(acc)` with no `recover()`. A panic in any sync goroutine crashes the whole server, not just that worker. |
| Stateful incremental sync (UID/MODSEQ/UIDVALIDITY) | **Partial, and buggy** | Only `LastUID` is used to resume (`sync.go:119`). `LastMODSEQ` exists as a DB column but is never read or written by the sync code. `UIDVALIDITY` isn't tracked or checked at all, so a repacked mailbox (the explicit scenario in the requirement) will silently desync rather than trigger a re-sync. |
| Sync attempt ledger (`sync_history` table) | **Missing** | No such table in any of the 8 schema migrations in `store.go`. Only current state (`last_uid`) is kept — no history of individual sync attempts, durations, byte counts, or outcomes. |
| Dry-run mode | **Missing** | No flag or code path for it anywhere. |
| Content-aware hashing + MIME normalization pipeline | **Stubbed** | `hasher.go` is 19 lines: lowercase + trim, then SHA-256. No encoding normalization, no MIME boundary stripping. HTML bodies get a regex tag-strip (`sync.go:229`) before hashing, which is a reasonable stopgap but far short of "MIME normalization pipeline." |
| Hash explainability / collision logging | **Missing** | No debug dump of the normalized string; no collision `WARN` logging (there's a unique index on `content_hash`, so a collision will just fail the insert silently — `SaveMessage` uses `INSERT OR IGNORE`, so a real collision would be dropped without any log entry at all). |
| Credential vault (AES-256-GCM, Argon2id) | **Missing — plaintext credentials** | `account.go:11`: `Password string \`json:"password"\` // This will be encrypted later`. It hasn't been. Passwords go straight into the `accounts.password` SQLite column as plain text (`store.go:449`). This is the most important gap to close — see Security below. |
| Auth failure differentiation / lockout prevention | **Missing** | `ConnectIMAP` (`sync.go:249`) returns a single generic error either way; nothing distinguishes network timeout from bad credentials, and nothing throttles repeated bad-credential attempts. |
| Mock credential vault for CI | **N/A** | Moot until a real vault exists. |
| Hybrid search (FTS5 lexical + vector semantic + RRF) | **Missing entirely** | No `CREATE VIRTUAL TABLE ... fts5` anywhere in the 8 migrations. Search/filtering is plain `LIKE '%...%'` on `subject` (`store.go:536`) — the message body isn't searched at all, and there's no vector index, no embeddings, no RRF. |
| Topic discovery / LDA / clustering | **Missing, replaced with a placeholder** | `GetTopicStats` (`store.go:708`) just takes the *first word of the subject line*, lowercases it, and counts frequency, filtering a hardcoded ignore-word list. This is what's currently wired to the "Topic Trends" widget — there is no NLP/LDA anywhere in the codebase. |
| LLM Gateway / Ollama / OpenAI integration | **Missing** | No HTTP client for any LLM provider anywhere in `go.mod` or the code. |
| Bullet-to-Draft / SSE streaming | **Missing** | No SSE handler, no draft-generation endpoint. |
| S3-compatible encrypted backup | **Missing** | No AWS/S3 SDK dependency; no backup code. |
| `uea doctor` / `uea maintenance` / `uea backup` CLI | **Missing** | `main.go` only starts the HTTP server — there is no CLI subcommand dispatch at all yet. |
| AI Agent Builder (Eino) | **UI-only — see Part 3** | Frontend persists a ReactFlow graph as JSON; nothing executes it. |

**Net assessment:** of the roughly 20 backend capabilities specified, 2 are real (per-host concurrency limiting, basic UID-based incremental fetch), a handful are thin stand-ins, and the rest — including everything under "Hybrid Search," "LLM Gateway," "Credential Vault," and "Secure Cloud Backup" — don't exist yet.

## Part 2: UEA — Bugs and Security Issues Found in Code

These are concrete, not stylistic:

1. **TLS certificate verification is disabled unconditionally.** `internal/sync/sync.go:242`: `client.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})`. Every IMAP connection — carrying login credentials and full email content — is vulnerable to a trivial MITM regardless of the `ssl` setting a user picks. This should be a hard stop before anyone points this at a real mailbox.
2. **Credentials stored in plaintext.** Covered above — `accounts.password` is plain text in SQLite, and it's also sent back to the frontend unencrypted in `GET /api/accounts` responses (`account.Account.Password` has no `json:"-"` tag, unlike `store.User.PasswordHash` which correctly does).
3. **`handleMessage` is an empty stub.** `cmd/uea/main.go:280-282`:
   ```go
   func handleMessage(w http.ResponseWriter, r *http.Request) {
   // ... (no changes needed to handleMessage)
   }
   ```
   It's registered and routed (`/api/message`), but does nothing — no response is written at all. Anything that calls this (the Thread Focus View in `test.md` section 6) gets an empty 200. This looks like an artifact of an AI-assisted edit that dropped the implementation.
4. **No panic recovery in sync goroutines**, as noted above — a single malformed IMAP response that panics a parser will take down the whole server, not just that account's sync.
5. **A personal email address is hardcoded into a shared-code SQL query.** `internal/store/store.go:686`: `AND from_addr NOT LIKE '%david.d.fullmer@gmail.com%'`. The requirement ("Top Senders List... automatically excluding the user's own addresses") calls for deriving this from the configured account list, not a hardcoded literal. As written, this feature only works correctly for one specific email address, breaks for every other account UEA is supposed to support, and bakes a personal address into source that the project's own `readme.md` invites outside contributors to fork.
6. **Default admin account (`admin@uea.local` / `password123`) is created unconditionally on every startup** (`main.go:34`) with no forced-change mechanism and no way to disable it — fine for local dev, risky if this ever binds to anything but `localhost`.
7. **No SQL connection-pool tuning** (`db.SetMaxOpenConns`, etc.) on the shared `*sql.DB` — with WAL mode's single-writer model and concurrent sync goroutines writing messages, this is a `database is locked` error waiting to happen once more than one account syncs at a time (currently masked because there's exactly one dev account and no real load).
8. Minor: dead code — `apiMux.HandleFunc("/api/accounts", handleAccounts)` (`main.go:47`) is unreachable; the top-level exact-match registration on `mux` (`main.go:58`) always wins for that path first. Not a bug, but confusing enough to trip someone up later.

## Part 3: UEA — Where the Effort Actually Went

Commit history:
```
afacb2e feat: Enhance Visual AI Agent Builder and update project documentation
ab456b8 feat: Introduce Eino Visual Agent Builder
ecd1c63 fix: Resolve UI layout issues and finalize filter pillbox components
32c63e0 chore: Update documentation, add unit tests, and fix frontend build issues
3bad160 feat: Full stack UI integration with nexus-shell, authentication, and cross-filtering analytics
6206358 feat: Initial project setup, multi-account sync engine
37c4e23 init commit with requirements
```

The two most recent commits — the most recent work done — built the **Visual AI Agent Builder**: a ReactFlow canvas (`AgentManager.tsx`, 481 lines) with the full node palette from the spec (ChatModel, ToolsNode, Retriever, ReAct Agent, etc.). I checked what it actually does: `AgentManager.tsx` only calls `GET/POST/DELETE /api/agents`, which round-trip a JSON blob to a `schema_json` column (`store.go:301-339`). There is no `eino` import anywhere in `go.mod`, no execution endpoint, and no code path that ever runs a saved agent. **It's a graph editor that saves and loads JSON — the actual AI agent framework it's meant to expose has 0% backend implementation.**

That's the clearest sign the project is off course: the most recent, most complex feature work went into the UI for a capability whose backend doesn't exist, while foundational, load-bearing pieces are either broken (TLS verification off) or entirely missing (credential encryption, real search, sync ledger). If "off course" means effort isn't landing where it should, this is the concrete example — polishing a demo-able but non-functional visual builder is a natural trap (it's the most visually impressive thing to show), but it's built on a backend that can't do anything with what it produces yet.

**Suggested reprioritization**, in order:
1. Fix the two security issues (TLS verification, plaintext credentials) — both are one-file, contained fixes and both are actively dangerous if this is ever used against a real account.
2. Finish `handleMessage` and de-hardcode the "own address" filter — both are small, both are currently broken for anyone but the current single dev account.
3. Decide honestly whether Eino/agent-execution is in scope for the near term. If yes, it needs backend work before more frontend polish is worth doing. If the AI Agent Builder is a stretch goal, it's fine to freeze it and redirect effort to the sync ledger, real search (even "just FTS5, no vector yet" would be a big step up from `LIKE`), and MODSEQ/UIDVALIDITY handling — these are the parts every other feature depends on.

## Part 4: nexus-shell — Version & Dependency Status

- UEA's `frontend/package.json` pins `"nexus-shell": "^0.1.2"`. The actual repo at `../nexus-shell` is at **0.2.11** (134 commits total, most recent additions: global Cmd+K QuickSearch, a multi-theme selector, a graph auto-layout engine, property-panel components). UEA is roughly **six minor versions behind** what's being developed — none of the new-version risk below actually reaches UEA until it upgrades, and that upgrade itself is unassessed (no changelog was checked for breaking changes between 0.1.2 and 0.2.11, since the requirements.md phrasing suggested comparing versions was secondary to finding weaknesses in the library itself).
- **Dependency mismatch**: nexus-shell's `package.json` depends on `zustand@^4.5.2`; UEA depends on `zustand@^5.0.11`. These are different major versions with breaking API changes between them. If/when UEA upgrades nexus-shell, expect either two copies of zustand in the bundle (wasted size, and any code that tries to share a store instance across the boundary will not behave as expected) or a forced downgrade of UEA's own zustand — worth checking before the upgrade, not after.

## Part 5: nexus-shell — Code Quality (Overall)

Better than the "still finding its way" framing implies: 17,061 lines across 107 source files, 16 dedicated test files, zero `eval`/`new Function`/`dangerouslySetInnerHTML` in the whole `src` tree, zero `@ts-ignore`/`@ts-expect-error`, only 7 instances of `as any` in the entire codebase, no leftover `TODO`/`FIXME`/`HACK` markers. That's a genuinely clean baseline — most of what's below is refinement, not damage control.

### Concrete findings

1. **`PaneService.ts` reads `localStorage` synchronously at module load, with no guard.** (`src/core/services/PaneService.ts:56-67`, `81-83`). `createPaneStore` calls zustand's `create()` whose initializer runs immediately, and that initializer calls `localStorage.getItem(storageKey)` unconditionally. Because `useLeftPaneStore`/`useRightPaneStore`/`useBottomPaneStore` are created as module-level constants, this executes the moment the module is imported — before any component renders. In any environment where `localStorage` doesn't exist at import time (server-side rendering, e.g. a Next.js app importing nexus-shell, or a test file that doesn't set up jsdom), this throws immediately and takes the whole import down with it. It also never validates that a restored `activePanel` id still exists among the panels actually registered later via `setPanels`; if a panel is renamed or removed between app versions, the pane opens showing `ConnectedPane`'s "Panel content not found" fallback (`src/connected/ConnectedPane.tsx:41-48`) instead of just staying collapsed — not a crash, since that fallback path does exist, but a confusing first-paint state that's easy to reproduce (rename any default panel id and reload with an old value in storage). **Fix is small**: guard the `localStorage` calls with a `typeof window !== 'undefined'` check, and validate/clear a stale `activePanel` once `setPanels` runs.
2. **`ChatService.registerSlashCommand` has no matching unregister, and no de-duplication.** (`src/core/services/ChatService.ts:41-42`). It's an append-only array. Any component that registers a slash command in an effect and unmounts (or that runs twice under React 19 StrictMode in dev, which double-invokes effects) leaves duplicate or orphaned commands in the store for the rest of the session — there's no cleanup path at all, unlike the panel/pane APIs which at least have `setPanels` to fully replace state.
3. **`QuickSearch`'s combobox is missing `aria-activedescendant`.** (`src/components/widgets/QuickSearch.tsx`). It correctly uses `role="combobox"` / `role="listbox"` / `role="option"` and `aria-selected`, but the WAI-ARIA combobox pattern also requires the input to expose `aria-activedescendant` pointing at the id of the currently-highlighted option so screen readers announce keyboard navigation. Right now arrow-key highlighting is purely visual. This is a small, contained fix (give each option row a real `id` and wire it to the input's `aria-activedescendant`) and would be a clean, welcome first contribution — it doesn't touch any behavior, just accessibility.

None of these are severe — nothing here is a security hole or a data-loss bug, which is a meaningfully different risk profile than what's found in UEA above. They're the kind of thing you'd expect to find in a library that's shipped 134 commits in active development, and all three are small enough to send as a PR rather than just an issue.

## Sources
All findings are drawn directly from the project files at `/Users/david/projects/UEA` and `/Users/david/projects/nexus-shell` (git log, `requirements.md`, `go.mod`, `package.json`, and the source files cited inline above) as of 2026-08-23.
