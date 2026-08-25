package cli

import (
	"flag"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/user/inboxql/internal/api"
	"github.com/user/inboxql/internal/store"
)

// Version is the release version, overridable at build time with
//
//	go build -ldflags "-X github.com/user/inboxql/internal/cli.Version=1.2.3"
var Version = "0.0.14"

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
		Usage: `iql start [--addr <host:port>] [--data <dir>]

Serves the dashboard and API. The data directory must already exist; run
` + "`iql init`" + ` first.

Flags:
  --addr <host:port>   listen address (default ":8080", or $INBOXQL_ADDR)

Binding to :8080 exposes InboxQL on every interface. It has no TLS of its own, so
put it behind a reverse proxy before exposing it beyond localhost.`,
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

func runStart(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	addr := fs.String("addr", envOr("INBOXQL_ADDR", ":8080"), "listen address")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

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
	p.Printf("  %-12s %s\n\n", p.Dim("Status:"), p.Green("Ready & listening"))

	if err := http.ListenAndServe(*addr, handler); err != nil {
		return Fail(ExitError, "server stopped: %v", err)
	}
	return nil
}
