package cli

import (
	"bufio"
	"context"
	"flag"
	"os"
	"strings"

	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/ews"
	"github.com/user/inboxql/internal/store"
)

// connectExchange connects an Exchange Server mailbox. The password comes
// from INBOXQL_EXCHANGE_PASSWORD, stdin, or a prompt — never a flag.
func connectExchange(ctx *Context, e *calsync.Engine, args []string) error {
	fs := flag.NewFlagSet("connect exchange", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	url := fs.String("url", "", "https://mail.example.com/EWS/Exchange.asmx")
	user := fs.String("username", "", `DOMAIN\user or user@example.com`)
	email := fs.String("email", "", "your address")
	auth := fs.String("auth", "ntlm", "ntlm or basic")
	if err := parseArgs(fs, args); err != nil || *url == "" || *user == "" {
		return Fail(ExitUsage, "usage: iql calendar connect exchange --url https://…/EWS/Exchange.asmx --username DOMAIN\\\\user --email you@example.com [--auth basic]")
	}
	password := os.Getenv("INBOXQL_EXCHANGE_PASSWORD")
	if password == "" {
		if IsTerminal() {
			ctx.Printf("Password for %s: ", *user)
		}
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		password = strings.TrimRight(line, "\r\n")
	}
	if password == "" {
		return Fail(ExitUsage, "no password: set INBOXQL_EXCHANGE_PASSWORD or pipe it in")
	}
	acct, err := ews.Connect(context.Background(), *url, *user, password, *email, *auth)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if err := e.SyncAccount(context.Background(), acct.ID); err != nil {
		return Fail(ExitError, "connected, but the first sync failed: %v", err)
	}
	cals, _ := store.ListCalendars()
	n := 0
	for _, c := range cals {
		if c.AccountID == acct.ID {
			n++
		}
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"account": acct, "calendars": n})
	}
	ctx.Printf("Connected %s and synced %d calendars.\n", acct.Email, n)
	return nil
}
