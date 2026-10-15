# Mail accounts, reworked

*14 October 2026.*

## What was wrong

- **Test Connection was simulated.** It waited 1.5 s and reported success
  unless the host contained "error" or the password was empty. Nothing was
  contacted.
- **One click deleted an account and all of its mail.** No confirmation, no
  count, and the button appeared only on hover.
- **Saves failed silently.** A 409 (name taken) or 400 (server changed, so the
  stored password no longer applies) closed the form as if it had worked.
- **Sending was guessed.** SMTP port fixed at 587, no separate login for the
  outgoing server.
- **"Last Sync" was the date of the newest message**, not when a sync ran.
- **IMAP with SSL off logged in over plaintext**, never trying STARTTLS, and
  connections had no timeout, so a dead server hung a verify forever.

## Phase 1 — make it honest

- A real verify, `POST /api/accounts/verify`: logs in to IMAP and to SMTP
  (connect, TLS, authenticate, quit; nothing sent) and returns a classified
  verdict for each — password rejected, host not found, no answer, untrusted
  certificate. It works on the form's unsaved values and on a saved account
  (whose stored password is used only if the server has not changed).
- One classifier, shared by the API, `iql account verify` and the sync status.
- IMAP: 20-second connect timeout; with SSL off, STARTTLS is required, as it
  already was for SMTP.
- Schema v42: `smtp_user`, `smtp_password` (encrypted; blank means "same as
  incoming"), `last_synced_at`, `disconnected`.
- The form shows the server's error on a failed save, and has SMTP port and an
  optional separate SMTP login.
- Remove asks first and says what it deletes.

## Phase 2 — start from the address

`GET /api/accounts/discover?email=` works out the servers, nearest source
first, and says which it used:

1. a built-in table: Gmail, Google Workspace (by MX), iCloud, Outlook.com and
   Microsoft 365 (by MX), Yahoo, AOL, Fastmail, Proton Bridge, Zoho, GMX;
2. the domain's own autoconfig file (`autoconfig.<domain>` and
   `<domain>/.well-known/autoconfig`);
3. RFC 6186 SRV records (`_imaps._tcp`, `_submission._tcp`);
4. **only when the person presses the button**, Mozilla's ISP database —
   because asking it tells Mozilla the domain.

The first three ask only the mail domain itself and DNS — places the account
is about to be connected to anyway — so they run without asking.

Each result carries the provider's note when there is one: Gmail, iCloud and
Yahoo need an app password; Microsoft has switched off passwords for IMAP on
Outlook.com and Microsoft 365, which is said rather than discovered by failing.

The add flow becomes: address and password → found settings, shown → verify
both → save. "Enter settings manually" opens the full form.

## Phase 3 — the account card

- Status as a sentence: "Synced 4 min ago", "Password rejected — update it",
  "Disconnected — mail kept".
- Sync, Test, Edit, Disconnect and Remove always visible, labelled.
- **Disconnect**: forget the login and stop syncing; the mail stays and stays
  searchable. For an account you have left — a job, a provider.
- **Remove**: says exactly what goes ("12,408 messages, 1.2 GB") and needs the
  account's name typed.

## Phase 4 — Sign in with Google for Gmail

Gmail over IMAP and SMTP with XOAUTH2, so no app password: the same loopback
PKCE sign-in the calendar uses, asking for `https://mail.google.com/`, with
the OAuth client already saved for the calendar offered again. The refresh
token is kept in the vault like a password; access tokens are refreshed as
needed.

## Phase 5 — the rest

CLI parity (`iql account discover`, `iql account verify` checking SMTP too,
`iql account disconnect`), docs, settings index, tests.

## Status

Phases 1–5 are built and tested: Go tests (real IMAP and SMTP sign-ins
against local servers with TLS, a fake Google for the sign-in and XOAUTH2,
discovery against fake DNS and HTTP) and frontend tests (`MailAccounts.test.tsx`).
Schema v42 is applied on first open.

Checked in a browser on a scratch mailbox: the migration, Gmail found from
the built-in list with its note and the Google sign-in box, the manual form's
verify naming an unknown server, saving starting the first sync, the card's
status after a failed sync, Disconnect, and Remove with its typed-name check.

Not done: a sign-in against real Gmail, which is tested against fakes only,
as the calendar was.
