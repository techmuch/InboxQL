package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/user/inboxql/internal/api"
	"github.com/user/inboxql/internal/auth"
	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/machine"
	"github.com/user/inboxql/internal/serverlock"
	"github.com/user/inboxql/internal/store"
)

// Version is the release version, overridable at build time with
//
//	go build -ldflags "-X github.com/user/inboxql/internal/cli.Version=1.2.3"
var Version = "0.0.83"

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
		Usage: `iql start [--addr <host:port>] [--open] [--dev] [--data <dir>]

Serves the dashboard and API. The data directory must already exist; run
` + "`iql init`" + ` first.

Flags:
  --addr <host:port>   listen address (default: $INBOXQL_ADDR, the machine settings, then 127.0.0.1:8420)
  --open               automatically open the dashboard in your default browser
  --dev                monitor the executable for changes and restart automatically
  --require-password   always ask for a password, even on this machine
  --trust-local        keep passwordless access when serving beyond localhost
  --service            how the login service runs it; starts local models on
                       first use rather than at launch

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

func runDevSupervisor(ctx *Context) error {
	exe, err := os.Executable()
	if err != nil {
		return Fail(ExitError, "could not determine executable path: %v", err)
	}

	initialStat, err := os.Stat(exe)
	if err != nil {
		return Fail(ExitError, "could not stat executable: %v", err)
	}

	p := ctx.Printer()
	p.Printf("\n%s\n", p.Yellow("Started in --dev mode. Watching binary for changes..."))

	for {
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Env = append(os.Environ(), "INBOXQL_DEV_CHILD=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Start(); err != nil {
			return Fail(ExitError, "failed to start child: %v", err)
		}

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		restart := false
	WatchLoop:
		for {
			select {
			case <-done:
				break WatchLoop
			case <-time.After(500 * time.Millisecond):
				stat, err := os.Stat(exe)
				if err != nil {
					continue
				}
				if stat.Size() != initialStat.Size() || stat.ModTime() != initialStat.ModTime() {
					p.Printf("\n%s\n", p.Yellow("Binary changed, restarting server..."))
					initialStat = stat
					restart = true
					cmd.Process.Kill()
					<-done
					break WatchLoop
				}
			}
		}

		// A child that exits asking for a restart — Settings → Restart —
		// is started again like one whose binary changed.
		if !restart && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == exitRestart {
			p.Printf("\n%s\n", p.Yellow("Restart requested, restarting server..."))
			restart = true
		}
		if !restart {
			return nil
		}
		time.Sleep(250 * time.Millisecond) // short pause before restarting
	}
}

func runStart(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	// Loopback by default. A desktop application has no business listening on
	// every interface unasked, and binding locally is also what makes
	// passwordless access defensible: reaching the port at all means being on
	// this machine.
	addr := fs.String("addr", ctx.defaultAddr(), "listen address")
	openFlag := fs.Bool("open", false, "open browser on start")
	devFlag := fs.Bool("dev", false, "monitor the binary for changes and restart")
	requirePassword := fs.Bool("require-password", envBool("INBOXQL_REQUIRE_PASSWORD"),
		"always ask for a password, even on this machine")
	// Only needed to force passwordless access on when the listen address
	// would otherwise switch it off.
	forceTrust := fs.Bool("trust-local", envBool("INBOXQL_TRUST_LOCAL"),
		"keep passwordless access when serving beyond localhost")
	// Set by the service definitions, never typed: it changes what a server
	// does at login rather than how it serves.
	serviceMode := fs.Bool("service", false, "run as the login service (starts local models on first use)")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if *devFlag && os.Getenv("INBOXQL_DEV_CHILD") == "" {
		return runDevSupervisor(ctx)
	}

	local, reason := trustDecision(*addr, *requirePassword, *forceTrust)
	auth.SetTrustLocal(local)

	// Show the name people type. A loopback bind is reached as localhost, and
	// printing 127.0.0.1 just invites someone to wonder whether it differs.
	// A hostname from the machine settings wins, because it is the address
	// somebody chose to type.
	displayURL := "http://" + *addr
	if host, port, err := net.SplitHostPort(*addr); err == nil {
		if host == "" || auth.IsLoopback(host) {
			displayURL = "http://localhost:" + port
			if ctx.Machine != nil && ctx.Machine.Hostname != "" {
				displayURL = "http://" + ctx.Machine.Hostname + ":" + port
			}
		}
	}

	// One server per mailbox, taken before the database is opened, so a
	// second server never gets as far as migrating it. When one is already
	// running, this is not an error: point at it. That is what somebody
	// typing `iql start` while the service runs actually wants.
	lock, other, err := acquireLock(ctx.DataDir, serverlock.Info{
		PID: os.Getpid(), URL: displayURL, Addr: *addr, Version: Version,
		Schema: store.SchemaVersion, Started: time.Now(),
	})
	if errors.Is(err, serverlock.ErrHeld) {
		url := ""
		if other != nil {
			url = other.URL
		}
		p := ctx.Printer()
		if url != "" {
			p.Printf("InboxQL is already running for %s at %s\n", ctx.DataDir, p.Bold(url))
			if *openFlag {
				_ = openBrowser(url)
			}
		} else {
			p.Printf("InboxQL is already running for %s.\n", ctx.DataDir)
		}
		p.Printf("%s\n", p.Dim("Stop it with `iql service stop` if it is the service, or use --data for another mailbox."))
		return nil
	}
	if err != nil {
		return Fail(ExitError, "locking %s: %v", ctx.DataDir, err)
	}
	defer lock.Release()

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	// The API needs the data directory for the attachment blob store and LLM logs.
	api.SetDataDir(ctx.DataDir)

	if *serviceMode {
		// As a login service, a local model server started here would hold
		// its memory from login to logout whether or not anything uses it.
		// Started on first use instead.
		llm.StartOnFirstUse(ctx.DataDir)
	} else {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("LLM auto-start recovered from panic: %v", r)
				}
			}()
			_ = llm.AutoStartIfConfigured(context.Background(), ctx.DataDir)
		}()
	}

	revision := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				revision = s.Value
			}
		}
	}
	api.SetVersionInfo(api.VersionInfo{
		Version:    Version,
		Revision:   revision,
		Dev:        *devFlag || os.Getenv("INBOXQL_DEV_CHILD") == "1",
		InstanceID: fmt.Sprintf("%d", time.Now().UnixNano()),
	})

	handler, err := api.Router()
	if err != nil {
		return Fail(ExitError, "%v", err)
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

	// A zero-value http.Server has no timeouts at all, so a client that opens
	// a connection and sends a header byte a minute holds a goroutine
	// indefinitely. Harmless on loopback with one user; --addr is a documented,
	// supported mode where it is not.
	//
	// WriteTimeout is generous because a large query or an export legitimately
	// takes a while, and cutting one off mid-response is worse than the
	// slow-client risk it defends against.
	srv := &http.Server{
		Addr: *addr,
		// Outermost, so a request is timed including whatever the auth
		// middleware does to it, and so a request refused before reaching a
		// handler is still recorded.
		Handler:           api.LogRequests(handler),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	// Restart is asked for by the settings page and carried out here, where
	// the listener, the lock and the database can be let go in order.
	restartReq := make(chan struct{}, 1)
	mode := "foreground"
	switch {
	case *serviceMode:
		mode = "service"
	case os.Getenv("INBOXQL_DEV_CHILD") == "1":
		mode = "dev"
	}
	addrSource := "default"
	switch {
	case flagWasSet(fs, "addr"):
		addrSource = "flag"
	case strings.TrimSpace(os.Getenv("INBOXQL_ADDR")) != "":
		addrSource = "env"
	case ctx.Machine != nil && ctx.Machine.Addr != "":
		addrSource = "settings"
	}
	exe := selfBinary()
	var started *machine.Settings
	if ctx.Machine != nil {
		c := *ctx.Machine
		started = &c
	}
	api.SetRuntime(api.Runtime{
		Mode: mode, Binary: exe, PID: os.Getpid(), Started: time.Now(),
		DataDir: ctx.DataDir, DataSource: ctx.DataSource, Addr: *addr, AddrSource: addrSource,
		URL: displayURL, AuthLocal: local, AuthReason: reason, Settings: started,
		Restart: func() error {
			if mode == "service" {
				return restartViaService(exe)
			}
			select {
			case restartReq <- struct{}{}:
			default:
			}
			return nil
		},
		Update: func(logPath string, done func(error)) error {
			return startUpdate(exe, mode, ctx.DataDir, logPath, func(err error) {
				done(err)
				// Under the service the updater restarts it; under --dev the
				// supervisor sees the binary change. In a terminal nobody
				// else will, so this server becomes the new binary itself.
				if err == nil && mode == "foreground" {
					select {
					case restartReq <- struct{}{}:
					default:
					}
				}
			})
		},
	})

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	select {
	case err := <-serveErr:
		return Fail(ExitError, "server stopped: %v", err)
	case <-restartReq:
	}

	p.Printf("\n%s\n", p.Yellow("Restarting..."))
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shutdown)
	cancel()
	store.CloseDB()
	lock.Release()
	if mode == "dev" {
		return Fail(exitRestart, "")
	}
	if err := replaceSelf(exe); err != nil {
		return Fail(ExitError, "restarting: %v — start it again with `iql start`", err)
	}
	return nil
}
