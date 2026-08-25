# InboxQL - Comprehensive Test Plan

This document outlines the core features of the InboxQL workbench and provides step-by-step instructions for manual verification.

---

## Running Automated Tests

To run the full suite of automated tests for both the backend and frontend, use the provided `Makefile`:

```bash
make test
```

This will:
1.  Execute all Go tests in the project (`go test ./...`).
2.  Run the frontend test suite using Vitest in non-interactive mode.

---

## 1. Authentication & Security
**Feature:** Secure access to the application and protected API endpoints.
- **Steps to Test (Login):**
  1. Open `http://localhost:5173`.
  2. Verify the Login screen appears with the "InboxQL" logo.
  3. Enter Username: `admin@inboxql.local` and Password: `password123`.
  4. Click **Sign In**.
  5. Verify successful entry into the workbench.
- **Steps to Test (Logout):**
  1. Click the user profile in the top-right corner.
  2. Select **Logout**.
  3. Verify redirection back to the Login screen.
- **Steps to Test (API Protection):**
  1. While logged out, attempt to visit `http://localhost:5173/api/accounts`.
  2. Verify the response is "unauthorized" (or status 401).

## 2. Branding & Layout
**Feature:** Customized workbench UI.
- **Steps to Test:**
  1. Verify the top-left title is **"InboxQL"**.
  2. Verify the left sidebar and activity bar are hidden.
  3. Verify the menu order: **Tools** -> **View** -> **Help**.

## 3. Tool Management
**Feature:** Launching unique tool tabs via menu and shortcuts.
- **Steps to Test:**
  1. Open **Tools -> Analytics Dashboard**.
  2. Open **Tools -> Mail Client**.
  3. Verify both tabs are open.
  4. Click **Tools -> Analytics Dashboard** again.
  5. Verify the existing "Dashboard" tab is focused instead of opening a new one.
- **Shortcuts:**
  - `Ctrl+Shift+D` (Dashboard)
  - `Ctrl+Shift+M` (Mail)
  - `Ctrl+Shift+F` (Search)
  - `Ctrl+,` (Settings)

## 4. Mail Account Management
**Feature:** CRUD operations for email connections.
- **Steps to Test (Add):**
  1. Open **Settings -> Mail Accounts**.
  2. Click **+ Add Account**.
  3. Fill in details and click **Save Account**.
  4. Verify the account appears in the list.
- **Steps to Test (Edit):**
  1. Click the **Gear icon** on an account card.
  2. Change the name and click **Update Account**.
  3. Verify the change persists.
- **Steps to Test (Connectivity):**
  1. In the account form, click **Test Connection**.
  2. Verify the loading pulse and the success/failure message.

## 5. Live Synchronization & Stats
**Feature:** Real IMAP syncing and live data reporting.
- **Steps to Test:**
  1. Click the **Refresh (Sync)** icon on an account card.
  2. Observe the status bar "Sync" status change to "Syncing" (simulated duration).
  3. Verify **Messages** count and **Storage** size update on the card.
  4. Verify the **Unread Count** in the status bar updates.

## 6. Mail Client UI (Gmail Clone)
**Feature:** Standard email browsing experience.
- **Steps to Test:**
  1. Open the **Mail Client**.
  2. Verify the list view shows Sender, Subject, and Date.
  3. Click a message to open the **Detail View**.
  4. Click the **Back button** to return to the list.

## 7. User Profile & Theming
**Feature:** Customizing the user experience.
- **Steps to Test:**
  1. Open **Settings -> User Profile**.
  2. Update "Display Name" and "Profile Image URL".
  3. Verify the top-right header widget updates.
  4. Open **Settings -> Appearance**.
  5. Switch between Light, Dark, and Georgia Tech themes.

## 9. Analytics Dashboard Filter Bar
**Feature:** Active filters are visible in the dashboard and can be cleared.
- **Steps to Test:**
  1. Open **Tools -> Analytics Dashboard**.
  2. Click on a Top Sender or Topic Trend to apply a filter.
  3. Verify the **Active Filters** bar appears below the "Analytics Pulse" header, displaying the selected filter.
  4. Click the **X** on the specific filter tag to clear it.
  5. Verify the filter is cleared and the dashboard data resets.
  6. Apply another filter, then click the **Clear All** button in the filter bar.
  7. Verify all filters are cleared and the filter bar disappears.

## 10. AI Agent Builder
**Feature:** Users can visually build and save Eino AI Agents.
- **Steps to Test:**
  1. Open **Tools -> AI Agents**.
  2. Click **Create New Agent**.
  3. Verify the React Flow canvas opens with default nodes (Input, Chat Model, Output).
  4. Change the agent name to `Test Eino Agent` in the header.
  5. Drag a connection (edge) between nodes to verify interactivity.
  6. Click **Save Agent** and confirm the success dialog.
  7. Click the back arrow to return to the list.
  8. Verify the `Test Eino Agent` appears in the saved agents list.
