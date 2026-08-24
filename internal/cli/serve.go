package cli

import (
	"flag"
	"log"
	"net/http"
	"runtime"
	"runtime/debug"

	"github.com/user/uea/internal/api"
	"github.com/user/uea/internal/store"
)

// Version is the release version, overridable at build time with
//
//	go build -ldflags "-X github.com/user/uea/internal/cli.Version=1.2.3"
var Version = "0.0.1"

func init() {
	register(&Command{
		Name:    "version",
		Summary: "print the UEA version",
		Usage: `uea version [--json]

Prints the version, the revision it was built from when available, and the Go
toolchain used. Include this in bug reports.`,
		Run: runVersion,
	})

	register(&Command{
		Name:    "serve",
		Summary: "run the web server",
		Usage: `uea serve [--addr <host:port>] [--data <dir>]

Serves the dashboard and API. The data directory must already exist; run
` + "`uea init`" + ` first.

Flags:
  --addr <host:port>   listen address (default ":8080", or $UEA_ADDR)

Binding to :8080 exposes UEA on every interface. It has no TLS of its own, so
put it behind a reverse proxy before exposing it beyond localhost.`,
		Run: runServe,
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

	ctx.Printf("uea %s\n", Version)
	if revision != "" {
		ctx.Printf("revision %s\n", revision)
	}
	ctx.Printf("%s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}

func runServe(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	addr := fs.String("addr", envOr("UEA_ADDR", ":8080"), "listen address")
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

	log.Printf("Data directory: %s", ctx.DataDir)
	ctx.Printf("UEA %s listening on http://localhost%s\n", Version, *addr)

	if err := http.ListenAndServe(*addr, handler); err != nil {
		return Fail(ExitError, "server stopped: %v", err)
	}
	return nil
}
