package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/user/inboxql/internal/machine"
	"github.com/user/inboxql/internal/service"
)

func init() {
	register(&Command{
		Name:    "service",
		Summary: "run InboxQL in the background, from login to logout",
		Usage: `iql service <install|uninstall|start|stop|restart|status> [--json]

Installs InboxQL as your own background service: a LaunchAgent on macOS, a
systemd user unit on Linux, a logon task on Windows. It starts when you log in,
stops when you log out, and never keeps a laptop awake.

  install     write the service definition and start it
  uninstall   stop it and remove the definition; your mail is not touched
  start       start it now
  stop        stop it until the next login
  restart     stop and start, e.g. after changing ~/.iql/settings.json
  status      whether it is installed, whether it is running, and where

The service runs this binary and reads ~/.iql/settings.json for the mailbox, so
run ` + "`iql setup`" + ` first. If you move or rebuild the binary, install again.`,
		Run: runService,
	})
}

func runService(ctx *Context, args []string) error {
	sub, _ := subcommand(args)
	if sub == "" {
		sub = "status"
	}

	mgr, err := service.ForThisMachine()
	if err != nil {
		return Fail(ExitNotConfigured, "%v", err)
	}
	// A logon task on Windows has exited by the time the server runs, so the
	// server is found — and stopped — through the lock it holds.
	if w, ok := mgr.(interface{ SetServerPID(func() int) }); ok {
		w.SetServerPID(func() int {
			if info, held := readServerLock(ctx.DataDir); held && info != nil {
				return info.PID
			}
			return 0
		})
	}

	switch sub {
	case "install":
		return serviceInstall(ctx, mgr)
	case "uninstall":
		if err := mgr.Uninstall(); err != nil {
			return Fail(ExitError, "%v", err)
		}
		return serviceReport(ctx, mgr, "Removed the service. Your mailbox and settings are untouched.")
	case "start":
		if err := mgr.Start(); err != nil {
			return Fail(ExitError, "%v", err)
		}
		return serviceReport(ctx, mgr, "Started.")
	case "stop":
		if err := mgr.Stop(); err != nil {
			return Fail(ExitError, "%v", err)
		}
		return serviceReport(ctx, mgr, "Stopped until the next login.")
	case "restart":
		if err := mgr.Restart(); err != nil {
			return Fail(ExitError, "%v", err)
		}
		return serviceReport(ctx, mgr, "Restarted.")
	case "status":
		return serviceReport(ctx, mgr, "")
	default:
		return Fail(ExitUsage, "unknown subcommand %q (want install, uninstall, start, stop, restart or status)", sub)
	}
}

func serviceInstall(ctx *Context, mgr service.Manager) error {
	// The service reads the machine settings and nothing else, so without them
	// it would start in whatever folder launchd chose and find no mailbox.
	if ctx.Machine == nil {
		return Fail(ExitNotConfigured,
			"no machine settings at %s.\n\nRun `iql setup` first: the service reads them to find your mailbox.",
			machine.Path())
	}
	bin, err := os.Executable()
	if err != nil {
		return Fail(ExitError, "cannot find this binary: %v", err)
	}
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}
	spec := service.Spec{Binary: bin, Home: machine.Home()}
	if h := os.Getenv("INBOXQL_HOME"); h != "" {
		spec.HomeOverride = machine.Home()
	}
	if err := mgr.Install(spec); err != nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		st, _ := mgr.Status()
		return ctx.EmitJSON(map[string]any{"installed": true, "binary": bin, "status": st,
			"url": serviceURL(ctx), "notes": mgr.Notes()})
	}
	p := ctx.Printer()
	ctx.Printf("Installed %s (%s).\n", p.Bold("InboxQL"), mgr.Platform())
	ctx.Printf("  %-8s %s\n", p.Dim("runs"), bin)
	ctx.Printf("  %-8s %s\n", p.Dim("mailbox"), ctx.Machine.DataDir)
	ctx.Printf("  %-8s %s\n", p.Dim("open"), p.Bold(serviceURL(ctx)))
	ctx.Printf("  %-8s %s\n", p.Dim("log"), service.LogFile(machine.Home()))
	for _, n := range mgr.Notes() {
		ctx.Printf("\n%s", p.Dim(n))
	}
	ctx.Printf("\n")
	// A build in a temporary folder or a checkout is the usual way to install
	// a service that silently breaks the next time somebody runs `go build`.
	if strings.Contains(bin, "go-build") || strings.Contains(bin, string(filepath.Separator)+"tmp"+string(filepath.Separator)) {
		ctx.Printf("\n%s The service runs %s; if that file is rebuilt or deleted, run `iql service install` again.\n",
			p.Yellow("note:"), bin)
	}
	return nil
}

func serviceReport(ctx *Context, mgr service.Manager, message string) error {
	st, err := mgr.Status()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"status": st, "url": serviceURL(ctx), "message": message})
	}
	p := ctx.Printer()
	if message != "" {
		ctx.Printf("%s\n", message)
	}
	state := p.Dim("not installed")
	switch {
	case st.Running:
		state = p.Green("running")
	case st.Starting:
		state = p.Green("starting")
	case st.Installed:
		state = p.Yellow("installed, not running")
	}
	ctx.Printf("  %-8s %s %s\n", p.Dim("service"), state, p.Dim("("+st.Platform+", "+st.Detail+")"))
	if st.Installed {
		ctx.Printf("  %-8s %s\n", p.Dim("file"), st.File)
	}
	if st.Running {
		ctx.Printf("  %-8s %s\n", p.Dim("open"), p.Bold(serviceURL(ctx)))
	}
	return nil
}

// serviceURL is the address to open: the running server's own, or what the
// settings say it will be.
func serviceURL(ctx *Context) string {
	if info, held := readServerLock(ctx.DataDir); held && info != nil && info.URL != "" {
		return info.URL
	}
	addr := ctx.defaultAddr()
	host, port := "localhost", machine.DefaultPort
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		port = addr[i+1:]
	}
	if ctx.Machine != nil && ctx.Machine.Hostname != "" {
		host = ctx.Machine.Hostname
	}
	return "http://" + host + ":" + port
}
