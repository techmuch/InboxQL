package cli

import (
	"flag"
	"strings"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/sync"
)

func init() {
	register(&Command{
		Name:    "account",
		Summary: "manage email accounts",
		Usage: `iql account <add|list|remove|verify|sync> [flags]

  add      connect a mailbox
  list     show configured accounts
  remove   delete an account and its messages
  verify   test the IMAP connection and credentials
  sync     fetch new mail now

add flags:
  --id <id>            identifier (defaults to a slug of --name)
  --name <name>        display name
  --email <address>    the account's own address, excluded from Top Senders
  --host <host>        IMAP host
  --port <n>           IMAP port (default 993)
  --user <user>        IMAP username (defaults to --email)
  --no-ssl             connect without TLS (refused unless you mean it)
  --smtp-host <host>   SMTP host, required to send
  --smtp-port <n>      SMTP port (default 587)

The password is never taken as a flag: it is read from INBOXQL_ACCOUNT_PASSWORD,
from stdin when piped, or prompted for without echo. A password in argv is
visible in shell history and to anyone running ps.`,
		Run: runAccount,
	})
}

func runAccount(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	switch sub {
	case "add":
		return accountAdd(ctx, rest)
	case "list", "":
		return accountList(ctx, rest)
	case "remove", "rm":
		return accountRemove(ctx, rest)
	case "verify":
		return accountVerify(ctx, rest)
	case "sync":
		return accountSync(ctx, rest)
	default:
		return Fail(ExitUsage, "unknown subcommand %q (want add, list, remove, verify or sync)", sub)
	}
}

func accountAdd(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("account add", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	id := fs.String("id", "", "account identifier")
	name := fs.String("name", "", "display name")
	email := fs.String("email", "", "the account's own email address")
	host := fs.String("host", "", "IMAP host")
	port := fs.Int("port", 993, "IMAP port")
	user := fs.String("user", "", "IMAP username")
	noSSL := fs.Bool("no-ssl", false, "connect without TLS")
	smtpHost := fs.String("smtp-host", "", "SMTP host")
	smtpPort := fs.Int("smtp-port", 587, "SMTP port")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if *host == "" {
		return Fail(ExitUsage, "--host is required")
	}
	if *email == "" && *user == "" {
		return Fail(ExitUsage, "one of --email or --user is required")
	}
	if *user == "" {
		*user = *email
	}
	if *name == "" {
		*name = *email
	}
	if *id == "" {
		*id = slug(*name)
	}
	if *id == "" {
		return Fail(ExitUsage, "could not derive an id; pass --id")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	password, err := ctx.ReadSecret("INBOXQL_ACCOUNT_PASSWORD", "IMAP password")
	if err != nil {
		return err
	}

	acc := &account.Account{
		ID: *id, Name: *name, Email: *email,
		Host: *host, Port: *port, User: *user,
		Password: password, SSL: !*noSSL,
		SMTPHost: *smtpHost, SMTPPort: *smtpPort,
		LastSyncStatus: "idle",
	}

	if err := store.SaveAccount(acc); err != nil {
		return Fail(ExitError, "failed to save account: %v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(acc.Redacted())
	}
	ctx.Printf("Added account %s (%s@%s)\n", acc.ID, acc.User, acc.Host)
	ctx.Printf("The password is encrypted at rest with the vault key.\n\n")
	ctx.Printf("Verify it with: iql account verify %s\n", acc.ID)
	return nil
}

func accountList(ctx *Context, args []string) error {
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	accounts, err := store.ListAccounts()
	if err != nil {
		return Fail(ExitError, "failed to list accounts: %v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(account.RedactAll(accounts))
	}

	if len(accounts) == 0 {
		ctx.Printf("No accounts configured. Add one with `iql account add`.\n")
		return nil
	}

	ctx.Printf("%-20s %-28s %-28s %s\n", "ID", "EMAIL", "IMAP", "LAST SYNC")
	for _, a := range accounts {
		imap := a.Host
		if a.Port != 0 {
			imap = a.Host + ":" + itoa(a.Port)
		}
		status := a.LastSyncStatus
		if status == "" {
			status = "never"
		}
		if a.LastSyncError != "" {
			status += " (" + truncate(a.LastSyncError, 30) + ")"
		}
		ctx.Printf("%-20s %-28s %-28s %s\n", a.ID, truncate(a.Email, 28), truncate(imap, 28), status)
	}
	return nil
}

func accountRemove(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("account remove", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if fs.NArg() != 1 {
		return Fail(ExitUsage, "usage: iql account remove <id> [--yes]")
	}
	id := fs.Arg(0)

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	acc, err := store.GetAccount(id)
	if err != nil {
		return Fail(ExitError, "failed to load account: %v", err)
	}
	if acc == nil {
		return Fail(ExitNotFound, "no account with id %q", id)
	}

	// The accounts -> messages foreign key cascades, so this is not just
	// forgetting a password: it deletes every synced message too.
	if !*yes {
		stats, _ := store.GetAccountStats(id)
		count := 0
		if stats != nil {
			count = stats.TotalMessages
		}
		if !ctx.Confirm(sprintf("Remove account %s and delete its %d stored message(s)?", id, count)) {
			return Fail(ExitError, "cancelled")
		}
	}

	if err := store.DeleteAccount(id); err != nil {
		return Fail(ExitError, "failed to remove account: %v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]string{"removed": id})
	}
	ctx.Printf("Removed account %s\n", id)
	return nil
}

func accountVerify(ctx *Context, args []string) error {
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	var accounts []*account.Account
	if len(args) == 1 {
		acc, err := store.GetAccount(args[0])
		if err != nil {
			return Fail(ExitError, "failed to load account: %v", err)
		}
		if acc == nil {
			return Fail(ExitNotFound, "no account with id %q", args[0])
		}
		accounts = []*account.Account{acc}
	} else {
		var err error
		if accounts, err = store.ListAccounts(); err != nil {
			return Fail(ExitError, "failed to list accounts: %v", err)
		}
	}

	type verdict struct {
		ID     string `json:"id"`
		OK     bool   `json:"ok"`
		Kind   string `json:"kind,omitempty"`
		Detail string `json:"detail,omitempty"`
	}
	var results []verdict
	failed := false

	for _, acc := range accounts {
		c, err := sync.ConnectIMAP(acc)
		if err == nil {
			c.Logout()
			results = append(results, verdict{ID: acc.ID, OK: true})
			if !ctx.JSON {
				ctx.Printf("[  ok  ] %-20s %s:%d\n", acc.ID, acc.Host, acc.Port)
			}
			continue
		}

		failed = true
		// requirements.md 2.1.4 asks for this distinction so the sync engine can
		// stop hammering a server with credentials it has already rejected.
		kind := classifyConnectError(err)
		results = append(results, verdict{ID: acc.ID, OK: false, Kind: kind, Detail: err.Error()})
		if !ctx.JSON {
			ctx.Printf("[ FAIL ] %-20s %s:%d — %s\n", acc.ID, acc.Host, acc.Port, kind)
			ctx.Printf("                              %v\n", err)
		}
	}

	if ctx.JSON {
		if err := ctx.EmitJSON(results); err != nil {
			return err
		}
	}
	if failed {
		return &Error{Code: ExitError, Err: errString("one or more accounts failed verification")}
	}
	return nil
}

// classifyConnectError separates credential rejections from everything else.
//
// IMAP servers signal a bad login with a NO or BAD response, which go-imap
// surfaces as an error string rather than a typed error — hence the string
// matching. A wrong classification is not harmful here; it only changes the
// wording of the advice.
func classifyConnectError(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "authenticat"),
		strings.Contains(msg, "invalid credentials"),
		strings.Contains(msg, "login failed"),
		strings.Contains(msg, "password"),
		strings.Contains(msg, "auth"):
		return "authentication rejected — check the username and password"
	case strings.Contains(msg, "no such host"),
		strings.Contains(msg, "dns"):
		return "host not found — check the IMAP hostname"
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "deadline"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "unreachable"):
		return "network unreachable — the server did not answer"
	case strings.Contains(msg, "certificate"),
		strings.Contains(msg, "x509"),
		strings.Contains(msg, "tls"):
		return "TLS verification failed — the server's certificate was not trusted"
	default:
		return "connection failed"
	}
}

func accountSync(ctx *Context, args []string) error {
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql account sync <id>")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	acc, err := store.GetAccount(args[0])
	if err != nil {
		return Fail(ExitError, "failed to load account: %v", err)
	}
	if acc == nil {
		return Fail(ExitNotFound, "no account with id %q", args[0])
	}

	// Run inline rather than in a goroutine: a CLI invocation that returned
	// before the work finished would be useless in a cron job.
	manager := sync.NewSyncManager(1)
	manager.StartSync(acc)

	updated, err := store.GetAccount(acc.ID)
	if err != nil {
		return Fail(ExitError, "sync finished but the account could not be re-read: %v", err)
	}
	stats, _ := store.GetAccountStats(acc.ID)

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{
			"id":     updated.ID,
			"status": updated.LastSyncStatus,
			"error":  updated.LastSyncError,
			"stats":  stats,
		})
	}

	ctx.Printf("Sync %s: %s\n", updated.ID, updated.LastSyncStatus)
	if updated.LastSyncError != "" {
		ctx.Printf("  %s\n", updated.LastSyncError)
	}
	if stats != nil {
		ctx.Printf("  %d message(s) stored\n", stats.TotalMessages)
	}
	if updated.LastSyncStatus == "error" {
		return &Error{Code: ExitError, Err: errString("sync failed")}
	}
	return nil
}
