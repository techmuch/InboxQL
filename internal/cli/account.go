package cli

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/autoconfig"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/mailer"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/sync"
)

func init() {
	register(&Command{
		Name:    "account",
		Summary: "manage email accounts",
		Usage: `iql account <add|list|discover|verify|sync|disconnect|remove> [flags]

  add          connect a mailbox
  list         show configured accounts
  discover     work out an address's mail servers
  verify       sign in to the incoming and outgoing servers
  sync         sync now: send changes made here, read every folder, and
               read all older mail (--no-history to leave that to the
               running server, which keeps every account live anyway)
  disconnect   forget the login and stop syncing; the mail stays
  remove       delete an account and its messages

add flags:
  --id <id>            identifier (defaults to a slug of --name)
  --name <name>        display name
  --email <address>    the account's own address, excluded from Top Senders
  --host <host>        IMAP host; leave it out to work it out from --email
  --port <n>           IMAP port (default 993)
  --user <user>        IMAP username (defaults to --email)
  --no-ssl             STARTTLS on a plain port (usually 143) instead of TLS
                       from the first byte; a password is never sent unencrypted
  --smtp-host <host>   SMTP host, required to send
  --smtp-port <n>      SMTP port (default 587; 465 for TLS from the first byte)
  --smtp-user <user>   the outgoing server's own login, when it differs
  --ispdb              when working out servers, also ask Mozilla's database
                       (which tells Mozilla the domain)

The password is never taken as a flag: it is read from INBOXQL_ACCOUNT_PASSWORD,
from stdin when piped, or prompted for without echo. A password in argv is
visible in shell history and to anyone running ps. An outgoing password that
differs is read from INBOXQL_SMTP_PASSWORD.

discover:  iql account discover <address> [--ispdb]`,
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
	case "discover":
		return accountDiscover(ctx, rest)
	case "disconnect":
		return accountDisconnect(ctx, rest)
	default:
		return Fail(ExitUsage, "unknown subcommand %q (want add, list, discover, verify, sync, disconnect or remove)", sub)
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
	smtpUser := fs.String("smtp-user", "", "SMTP username, when it differs")
	ispdb := fs.Bool("ispdb", false, "also ask Mozilla's ISP database")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if *host == "" {
		if *email == "" {
			return Fail(ExitUsage, "--host is required, or --email to work the servers out from")
		}
		found, _, err := autoconfig.Discover(context.Background(), *email, autoconfig.Options{ISPDB: *ispdb})
		if err != nil {
			return Fail(ExitUsage, "could not work out the servers for %s (%v); pass --host and --smtp-host", *email, err)
		}
		if found.Unsupported != "" {
			return Fail(ExitNotConfigured, "%s", found.Unsupported)
		}
		*host = found.IMAP.Host
		if !set["port"] {
			*port = found.IMAP.Port
		}
		if !set["no-ssl"] {
			*noSSL = !found.IMAP.TLS
		}
		if !set["user"] {
			*user = found.Username
		}
		if !set["smtp-host"] {
			*smtpHost = found.SMTP.Host
		}
		if !set["smtp-port"] && found.SMTP.Port != 0 {
			*smtpPort = found.SMTP.Port
		}
		if !ctx.JSON {
			ctx.Printf("Servers for %s, from %s: %s:%d and %s:%d\n", *email, found.Source,
				*host, *port, *smtpHost, *smtpPort)
			if found.Note != "" {
				ctx.Printf("%s\n", found.Note)
			}
		}
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

	smtpPassword := ""
	if *smtpUser != "" {
		if smtpPassword, err = ctx.ReadSecret("INBOXQL_SMTP_PASSWORD", "SMTP password"); err != nil {
			return err
		}
	}

	acc := &account.Account{
		ID: *id, Name: *name, Email: *email,
		Host: *host, Port: *port, User: *user,
		Password: password, SSL: !*noSSL,
		SMTPHost: *smtpHost, SMTPPort: *smtpPort,
		SMTPUser: *smtpUser, SMTPPassword: smtpPassword,
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

	p := ctx.Printer()
	t := p.NewTable("ID", "EMAIL", "IMAP", "LAST SYNC")
	for _, a := range accounts {
		imap := a.Host
		if a.Port != 0 {
			imap = a.Host + ":" + itoa(a.Port)
		}

		// A failed sync is the one thing in this listing that wants the eye.
		status, state := a.LastSyncStatus, ui.Note
		if status == "" {
			status = "never"
		}
		if a.LastSyncError != "" {
			status += " (" + ui.Truncate(a.LastSyncError, 30) + ")"
			state = ui.Bad
		}
		t.Row(a.ID, a.Email, imap, t.Cell(state, status))
	}
	if err := t.Flush(); err != nil {
		return Fail(ExitError, "writing accounts: %v", err)
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
		stored := 0
		if stats != nil {
			stored = stats.TotalMessages
		}
		if !ctx.Confirm(sprintf("Remove account %s and delete its %s?", id, count(stored, "stored message", "stored messages"))) {
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

	type check struct {
		OK      bool   `json:"ok"`
		Skipped bool   `json:"skipped,omitempty"`
		Kind    string `json:"kind,omitempty"`
		Advice  string `json:"advice,omitempty"`
		Detail  string `json:"detail,omitempty"`
	}
	type verdict struct {
		ID string `json:"id"`
		// OK, Kind and Detail are the incoming server's, where they always were.
		OK     bool   `json:"ok"`
		Kind   string `json:"kind,omitempty"`
		Detail string `json:"detail,omitempty"`
		SMTP   check  `json:"smtp"`
	}
	var results []verdict
	failed := false

	p := ctx.Printer()
	for _, acc := range accounts {
		v := verdict{ID: acc.ID}
		c, err := sync.ConnectIMAP(acc)
		if err == nil {
			c.Logout()
			v.OK = true
		} else {
			failed = true
			kind, _ := account.Classify(err, acc.Host)
			v.Kind, v.Detail = kind, err.Error()
		}
		if acc.SMTPHost == "" {
			v.SMTP = check{Skipped: true, Advice: "no outgoing server; this account cannot send"}
		} else if err := mailer.Verify(acc); err != nil {
			failed = true
			kind, advice := account.Classify(err, acc.SMTPHost)
			v.SMTP = check{Kind: kind, Advice: advice, Detail: err.Error()}
		} else {
			v.SMTP = check{OK: true}
		}
		results = append(results, v)

		if !ctx.JSON {
			report := func(label, host string, port int, ok bool, detail string) {
				if ok {
					p.Status(ui.OK, acc.ID, sprintf("%s %s:%d", label, host, port))
					return
				}
				// The advice is the headline; the server's own words are the
				// detail you need when the advice is not enough.
				_, advice := account.Classify(errString(detail), host)
				p.Status(ui.Bad, acc.ID, sprintf("%s %s:%d — %s", label, host, port, advice))
				p.Printf("        %s\n", p.Dim(detail))
			}
			report("IMAP", acc.Host, acc.Port, v.OK, v.Detail)
			if !v.SMTP.Skipped {
				report("SMTP", acc.SMTPHost, acc.SMTPPort, v.SMTP.OK, v.SMTP.Detail)
			}
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

func accountDiscover(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("account discover", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	ispdb := fs.Bool("ispdb", false, "also ask Mozilla's ISP database")
	if err := parseArgs(fs, args); err != nil || fs.NArg() != 1 {
		return Fail(ExitUsage, "usage: iql account discover <address> [--ispdb]")
	}
	found, tried, err := autoconfig.Discover(context.Background(), fs.Arg(0), autoconfig.Options{ISPDB: *ispdb})
	if err != nil && !errors.Is(err, autoconfig.ErrNotFound) {
		return Fail(ExitUsage, "%v", err)
	}
	if ctx.JSON {
		if err := ctx.EmitJSON(map[string]any{"found": found != nil, "settings": found, "tried": tried}); err != nil {
			return err
		}
		if found == nil {
			return &Error{Code: ExitNotFound, Err: err}
		}
		return nil
	}
	p := ctx.Printer()
	for _, a := range tried {
		p.Printf("  %-10s %s — %s\n", a.Source, a.Asked, a.Result)
	}
	if found == nil {
		hint := ""
		if !*ispdb {
			hint = " Try --ispdb to also ask Mozilla's database, which tells Mozilla the domain."
		}
		return Fail(ExitNotFound, "nothing found; enter the servers with iql account add --host.%s", hint)
	}
	p.Printf("\n%s (from %s)\n", orDash(found.Provider), found.Source)
	if found.Unsupported != "" {
		p.Printf("%s\n", found.Unsupported)
		return nil
	}
	p.Printf("  IMAP  %s:%d %s\n  SMTP  %s:%d %s\n  user  %s\n", found.IMAP.Host, found.IMAP.Port, tlsWord(found.IMAP.TLS),
		found.SMTP.Host, found.SMTP.Port, tlsWord(found.SMTP.TLS), found.Username)
	if found.Note != "" {
		p.Printf("\n%s\n", found.Note)
	}
	return nil
}

func tlsWord(implicit bool) string {
	if implicit {
		return "TLS"
	}
	return "STARTTLS"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func accountDisconnect(ctx *Context, args []string) error {
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql account disconnect <id>")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()
	if err := store.DisconnectAccount(args[0]); errors.Is(err, sql.ErrNoRows) {
		return Fail(ExitNotFound, "no account with id %q", args[0])
	} else if err != nil {
		return Fail(ExitError, "failed to disconnect: %v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]string{"disconnected": args[0]})
	}
	ctx.Printf("Disconnected %s: its login is forgotten and it will not sync. Its mail stays.\n", args[0])
	return nil
}

func accountSync(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("account sync", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	noHistory := fs.Bool("no-history", false, "new mail and changes only; leave older mail for the running server")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	args = fs.Args()
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql account sync <id> [--no-history]")
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
	if acc.Disconnected {
		return Fail(ExitError, "%s is disconnected: its mail is kept, but it has no login to sync with", acc.ID)
	}

	// Run inline rather than in a goroutine: a CLI invocation that returned
	// before the work finished would be useless in a cron job. It pushes
	// changes queued here, reads every folder, and reads all of the older
	// mail unless told not to — which on a large first sync is hours, so the
	// progress goes to stderr.
	manager := sync.NewSyncManager(1)
	manager.Blobs = blobstore.New(ctx.DataDir)
	store.UpdateAccountStatus(acc.ID, "syncing", "")
	last := time.Time{}
	report, err := manager.RunToCompletion(acc, !*noHistory, func(p store.MirrorProgress) {
		if time.Since(last) > 2*time.Second {
			fmt.Fprintf(ctx.Stderr, "  history: %d of %d\n", p.Stored, p.OnServer)
			last = time.Now()
		}
	})
	if err != nil {
		store.UpdateAccountStatus(acc.ID, "error", err.Error())
	} else {
		store.UpdateAccountStatus(acc.ID, "success", "")
		store.MarkAccountSynced(acc.ID)
	}

	updated, err := store.GetAccount(acc.ID)
	if err != nil {
		return Fail(ExitError, "sync finished but the account could not be re-read: %v", err)
	}
	stats, _ := store.GetAccountStats(acc.ID)

	progress, _ := store.MirrorProgressFor(acc.ID)
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{
			"id":       updated.ID,
			"status":   updated.LastSyncStatus,
			"error":    updated.LastSyncError,
			"stats":    stats,
			"report":   report,
			"progress": progress,
		})
	}

	ctx.Printf("Sync %s: %s\n", updated.ID, updated.LastSyncStatus)
	ctx.Printf("  %d new, %d matched to mail already here, %d changed on the server, %d changes sent to it\n",
		report.New, report.Linked, report.Changed, report.Pushed)
	if !progress.HistoryDone {
		ctx.Printf("  older mail: %d of %d here so far\n", progress.Stored, progress.OnServer)
	}
	reportWaitingAnnotators(ctx)
	if updated.LastSyncError != "" {
		ctx.Printf("  %s\n", updated.LastSyncError)
	}
	if stats != nil {
		ctx.Printf("  %s stored\n", count(stats.TotalMessages, "message", "messages"))
	}
	if updated.LastSyncStatus == "error" {
		return &Error{Code: ExitError, Err: errString("sync failed")}
	}
	return nil
}
