package cli

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/user/inboxql/internal/api"
	"github.com/user/inboxql/internal/auth"
	"github.com/user/inboxql/internal/store"
)

// Version is the release version, overridable at build time with
//
//	go build -ldflags "-X github.com/user/inboxql/internal/cli.Version=1.2.3"
var Version = "0.0.19"

func init() {
	register(&Command{
		Name:    "version",
		Summary: "print the InboxQL version",
		Usage: `iql version [--json]

Prints the version, the revision it was built from when available, and the Go
toolchain used. Include this in bug reports.`,
		Run: runVersion,
	})

	register(&Command{
		Name:    "start",
		Aliases: []string{"serve"},
		Summary: "run the web server",
		Usage: `iql start [--addr <host:port>] [--open] [--data <dir>]

Serves the dashboard and API. The data directory must already exist; run
` + "`iql init`" + ` first.

Flags:
  --addr <host:port>   listen address (default "127.0.0.1:8080", or $INBOXQL_ADDR)
  --open               automatically open the dashboard in your default browser
  --require-password   always ask for a password, even on this machine
  --trust-local        keep passwordless access when serving beyond localhost

By default InboxQL listens on localhost only and does not ask for a password
there: it is your machine, and you are already the only one who can reach it.

Serving beyond localhost changes that. Give --addr a public address and the
password is required, because the audience is no longer just you. InboxQL has
no TLS of its own, so put it behind a reverse proxy first.

A password is also required for any request that arrived through a proxy,
whatever the listen address, since a proxy on this host relays every request
over loopback — its peer address says nothing about who sent it.

--trust-local forces passwordless access on anyway. Do not use it with a
reverse proxy: everyone who can reach the proxy would be signed in as the
administrator.`,
		Run: runStart,
	})
}

func runVersion(ctx *Context, args []string) error {
	revision := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				revision = s.Value
			}
		}
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]string{
			"version":  Version,
			"revision": revision,
			"go":       runtime.Version(),
			"platform": runtime.GOOS + "/" + runtime.GOARCH,
		})
	}

	ctx.Printf("iql %s\n", Version)
	if revision != "" {
		ctx.Printf("revision %s\n", revision)
	}
	ctx.Printf("%s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}

const banner = `    ____       __               ____    __ 
   /  _/____  / /_  ____  _  __/ __ \  / / 
   / / / __ \/ __ \/ __ \| |/_/ / / / / /  
 _/ / / / / / /_/ / /_/ />  </ /_/ / / /___
/___//_/ /_/_.___/\____/_/|_|\___\_\/_____/`

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// trustDecision works out whether loopback clients may skip the password, and
// returns a phrase explaining why for the startup banner.
//
// Passwordless local access is the default because InboxQL is a desktop
// application: it listens on localhost, so reaching it means being on the
// machine already, and prompting the owner of the machine for a password
// protects nothing.
//
// Two situations withdraw it, and both are about the audience widening beyond
// the person at the keyboard:
//
//   - The listen address is not loopback, so the port is reachable from the
//     network and "whoever can connect" is no longer "whoever is here".
//   - The request arrived through a proxy, checked per-request in the auth
//     middleware. A proxy on this host relays everything over loopback, so its
//     peer address describes the proxy, not the client.
//
// --trust-local overrides the first; nothing overrides the second.
func trustDecision(addr string, requirePassword, forceTrust bool) (bool, string) {
	switch {
	case requirePassword:
		return false, "password required (--require-password)"
	case boundToLoopback(addr):
		return true, "passwordless on this machine"
	case forceTrust:
		return true, "passwordless, forced on a public address (--trust-local)"
	default:
		return false, "password required (listening beyond localhost)"
	}
}

// boundToLoopback reports whether a listen address accepts only local
// connections. An address with no host — ":8080" — listens on every
// interface, which is the case worth warning about.
func boundToLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	return auth.IsLoopback(host)
}

func runStart(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	// Loopback by default. A desktop application has no business listening on
	// every interface unasked, and binding locally is also what makes
	// passwordless access defensible: reaching the port at all means being on
	// this machine.
	addr := fs.String("addr", envOr("INBOXQL_ADDR", "127.0.0.1:8080"), "listen address")
	openFlag := fs.Bool("open", false, "open browser on start")
	requirePassword := fs.Bool("require-password", envBool("INBOXQL_REQUIRE_PASSWORD"),
		"always ask for a password, even on this machine")
	// Only needed to force passwordless access on when the listen address
	// would otherwise switch it off.
	forceTrust := fs.Bool("trust-local", envBool("INBOXQL_TRUST_LOCAL"),
		"keep passwordless access when serving beyond localhost")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	local, reason := trustDecision(*addr, *requirePassword, *forceTrust)
	auth.SetTrustLocal(local)

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	// The API needs the data directory for the attachment blob store.
	api.SetDataDir(ctx.DataDir)

	handler, err := api.Router()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	// Show the name people type. A loopback bind is reached as localhost, and
	// printing 127.0.0.1 just invites someone to wonder whether it differs.
	displayURL := "http://" + *addr
	if host, port, err := net.SplitHostPort(*addr); err == nil {
		if host == "" || auth.IsLoopback(host) {
			displayURL = "http://localhost:" + port
		}
	}

	p := ctx.Printer()
	p.Printf("\n%s\n\n", p.Cyan(banner))
	p.Printf("  %s %s — %s\n", p.Bold("InboxQL"), p.Dim("v"+Version), p.Dim("Email for Engineers"))
	p.Printf("  %s\n", p.Dim("──────────────────────────────────────────────────"))
	p.Printf("  %-12s %s\n", p.Dim("Web UI:"), p.Bold(displayURL))
	p.Printf("  %-12s %s\n", p.Dim("Data dir:"), ctx.DataDir)
	p.Printf("  %-12s %s\n", p.Dim("Status:"), p.Green("Ready & listening"))

	// State the auth posture on every start. It is the one setting whose wrong
	// value is invisible until someone else is reading the mail.
	if local {
		p.Printf("  %-12s %s\n", p.Dim("Auth:"), p.Green(reason))
	} else {
		p.Printf("  %-12s %s\n", p.Dim("Auth:"), reason)
	}

	if local && !boundToLoopback(*addr) {
		p.Printf("\n  %s %s\n", p.Yellow("warning:"),
			"passwordless access is forced on a public address.")
		p.Printf("  %s\n", p.Dim("Anyone who can reach this port is signed in as the administrator."))
		p.Printf("  %s\n", p.Dim("Drop --trust-local unless you are certain."))
	}
	p.Printf("\n")

	if *openFlag {
		go func() {
			time.Sleep(100 * time.Millisecond)
			if err := openBrowser(displayURL); err != nil {
				log.Printf("Failed to open browser: %v", err)
			}
		}()
	}

	if err := http.ListenAndServe(*addr, handler); err != nil {
		return Fail(ExitError, "server stopped: %v", err)
	}
	return nil
}
