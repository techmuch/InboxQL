package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/ews"
	"github.com/user/inboxql/internal/gcal"
	"github.com/user/inboxql/internal/store"
)

// newCalendarEngine is the calendar engine with every provider this build
// knows how to reach.
func newCalendarEngine() *calsync.Engine {
	e := calsync.New()
	registerCalendarProviders(e)
	return e
}

// registerCalendarProviders tells the engine how to reach each provider.
func registerCalendarProviders(e *calsync.Engine) {
	e.Register(store.ProviderGoogle, gcal.New)
	e.Register(store.ProviderEWS, ews.New)
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// calendarAccounts lists, connects, syncs and disconnects calendar accounts.
func calendarAccounts(ctx *Context, e *calsync.Engine, sub string, args []string) error {
	switch sub {
	case "connect":
		if len(args) == 0 {
			return Fail(ExitUsage, "usage: iql calendar connect google|exchange [flags]")
		}
		switch args[0] {
		case "google":
			return connectGoogle(ctx, e, args[1:])
		case "exchange", "ews":
			return connectExchange(ctx, e, args[1:])
		}
		return Fail(ExitUsage, "connect google or exchange, not %q", args[0])
	case "sync":
		if len(args) != 1 {
			return Fail(ExitUsage, "usage: iql calendar sync <account-id>")
		}
		if err := e.SyncAccount(context.Background(), args[0]); err != nil {
			return Fail(ExitError, "%v", err)
		}
		a, _ := store.GetCalendarAccount(args[0])
		if ctx.JSON {
			return ctx.EmitJSON(a)
		}
		ctx.Printf("Synced %s.\n", a.Name)
		return nil
	case "disconnect":
		if len(args) != 1 {
			return Fail(ExitUsage, "usage: iql calendar disconnect <account-id>")
		}
		a, err := store.GetCalendarAccount(args[0])
		if err != nil {
			return Fail(ExitNotFound, "no calendar account %q", args[0])
		}
		if err := store.DeleteCalendarAccount(a.ID); err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"disconnected": a.ID})
		}
		ctx.Printf("Disconnected %s. Its calendars are gone from here; nothing at %s was touched.\n", a.Name, a.Provider)
		return nil
	}
	accts, err := store.ListCalendarAccounts()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		if accts == nil {
			accts = []store.CalendarAccount{}
		}
		return ctx.EmitJSON(map[string]any{"accounts": accts})
	}
	if len(accts) == 0 {
		ctx.Printf("No calendar accounts. `iql calendar connect google` or `iql calendar connect exchange`.\n")
		return nil
	}
	p := ctx.Printer()
	for _, a := range accts {
		status := a.Status
		if a.LastError != "" {
			status += ": " + a.LastError
		}
		ctx.Printf("%-34s %-7s %s  %s\n", a.ID, a.Provider, p.Bold(a.Name), p.Dim(status))
	}
	return nil
}

// connectGoogle signs in with a loopback listener of its own, on a random
// port, so it works without a running server. The client secret is read
// from the environment or a prompt, never from a flag.
func connectGoogle(ctx *Context, e *calsync.Engine, args []string) error {
	fs := flag.NewFlagSet("connect google", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	clientID := fs.String("client-id", os.Getenv("INBOXQL_GOOGLE_CLIENT_ID"), "OAuth client ID (Desktop app)")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if !strings.HasSuffix(*clientID, ".apps.googleusercontent.com") {
		return Fail(ExitUsage, "--client-id is a Google OAuth client ID for a Desktop app; see `iql help calendar` and the Calendars guide")
	}
	secret := os.Getenv("INBOXQL_GOOGLE_CLIENT_SECRET")
	if secret == "" {
		if !IsTerminal() {
			return Fail(ExitUsage, "set INBOXQL_GOOGLE_CLIENT_SECRET")
		}
		ctx.Printf("Client secret: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		secret = strings.TrimSpace(line)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	defer ln.Close()
	flow := gcal.NewFlow(*clientID, secret, fmt.Sprintf("http://%s/oauth/google/callback", ln.Addr().String()))
	type outcome struct {
		acct *store.CalendarAccount
		err  error
	}
	done := make(chan outcome, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/google/callback" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("state") != flow.State {
			http.Error(w, "this sign-in is not the one in progress", http.StatusBadRequest)
			return
		}
		if msg := r.URL.Query().Get("error"); msg != "" {
			fmt.Fprintf(w, "Not connected: %s. You can close this tab.", msg)
			done <- outcome{err: fmt.Errorf("Google said: %s", msg)}
			return
		}
		acct, err := flow.Finish(r.Context(), r.URL.Query().Get("code"))
		if err != nil {
			fmt.Fprintf(w, "Not connected: %v", err)
		} else {
			fmt.Fprintf(w, "Connected %s. You can close this tab.", acct.Email)
		}
		done <- outcome{acct, err}
	})}
	go srv.Serve(ln)
	defer srv.Close()
	ctx.Printf("Sign in to Google in your browser. If it does not open, visit:\n\n  %s\n\n", flow.URL())
	_ = browse(flow.URL())
	select {
	case o := <-done:
		if o.err != nil {
			return Fail(ExitError, "%v", o.err)
		}
		ctx.Printf("Connected %s. Reading its calendars…\n", o.acct.Email)
		if err := e.SyncAccount(context.Background(), o.acct.ID); err != nil {
			return Fail(ExitError, "connected, but the first sync failed: %v", err)
		}
		cals, _ := store.ListCalendars()
		n := 0
		for _, c := range cals {
			if c.AccountID == o.acct.ID {
				n++
			}
		}
		ctx.Printf("Synced %d calendars.\n", n)
		return nil
	case <-time.After(10 * time.Minute):
		return Fail(ExitError, "no answer from Google in ten minutes; nothing was connected")
	}
}
