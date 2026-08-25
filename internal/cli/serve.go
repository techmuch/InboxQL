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
var Version = "0.0.17"

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
  --addr <host:port>   listen address (default ":8080", or $INBOXQL_ADDR)
  --open               automatically open the dashboard in your default browser
  --trust-local        skip the password for connections from this machine
                       (or set INBOXQL_TRUST_LOCAL=1)

Binding to :8080 exposes InboxQL on every interface. It has no TLS of its own, so
put it behind a reverse proxy before exposing it beyond localhost.

--trust-local is for a single-user desktop install and must not be combined with
a reverse proxy. A proxy relays every request over loopback, so with this on,
everyone reaching the proxy is signed in as the administrator. InboxQL cannot
tell the two deployments apart — it is bound to loopback in both — which is why
this is a flag you set rather than something detected.`,
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

// trustFlagSet records whether --trust-local was passed explicitly, so the
// startup banner can name the environment variable when that is what enabled
// it. A person who cannot see why they are not being asked for a password
// needs to be told where the setting came from.
var trustFlagSet *bool

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
	addr := fs.String("addr", envOr("INBOXQL_ADDR", ":8080"), "listen address")
	openFlag := fs.Bool("open", false, "open browser on start")
	// Settable from the environment like --addr and --data, so a single-user
	// desktop install can opt in once in a shell profile instead of on every
	// invocation. The default stays off either way: a deployment that never
	// mentions it never gets it.
	trust := fs.Bool("trust-local", envBool("INBOXQL_TRUST_LOCAL"),
		"skip the password for connections from this machine")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "trust-local" {
			explicit = true
		}
	})
	trustFlagSet = &explicit

	auth.SetTrustLocal(*trust)

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

	var displayURL string
	if strings.HasPrefix(*addr, ":") {
		displayURL = "http://localhost" + *addr
	} else {
		displayURL = "http://" + *addr
	}

	p := ctx.Printer()
	p.Printf("\n%s\n\n", p.Cyan(banner))
	p.Printf("  %s %s — %s\n", p.Bold("InboxQL"), p.Dim("v"+Version), p.Dim("Email for Engineers"))
	p.Printf("  %s\n", p.Dim("──────────────────────────────────────────────────"))
	p.Printf("  %-12s %s\n", p.Dim("Web UI:"), p.Bold(displayURL))
	p.Printf("  %-12s %s\n", p.Dim("Data dir:"), ctx.DataDir)
	p.Printf("  %-12s %s\n", p.Dim("Status:"), p.Green("Ready & listening"))

	// State the auth posture on every start. It is the one setting whose
	// wrong value is invisible until someone else is reading the mail.
	if *trust {
		source := "--trust-local"
		if !*trustFlagSet && envBool("INBOXQL_TRUST_LOCAL") {
			source = "INBOXQL_TRUST_LOCAL"
		}
		p.Printf("  %-12s %s\n", p.Dim("Auth:"),
			p.Yellow("passwordless for this machine ("+source+")"))
		if !boundToLoopback(*addr) {
			p.Printf("\n  %s %s\n", p.Yellow("warning:"),
				"--trust-local with a non-loopback listen address.")
			p.Printf("  %s\n", p.Dim("Anyone who can reach "+displayURL+" over loopback — a reverse"))
			p.Printf("  %s\n", p.Dim("proxy on this host, or an SSH tunnel — is signed in as the"))
			p.Printf("  %s\n", p.Dim("administrator. Drop --trust-local, or bind to 127.0.0.1."))
		}
	} else {
		p.Printf("  %-12s %s\n", p.Dim("Auth:"), "password required")
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
