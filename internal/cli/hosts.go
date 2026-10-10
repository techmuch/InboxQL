package cli

import (
	"errors"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/user/inboxql/internal/hostsfile"
	"github.com/user/inboxql/internal/machine"
)

func init() {
	register(&Command{
		Name:    "hosts",
		Summary: "give the local site a name, such as http://inboxql.localhost:8420",
		Usage: `iql hosts <set <name>|remove|show> [--json]

Points a name at this machine by adding two lines to the hosts file, between
marker comments so nothing else in the file is touched:

  set <name>   e.g. inboxql.localhost; remembered in ~/.iql/settings.json
  remove       take the lines out again
  show         what is set, and whether the name resolves

Changing the hosts file needs administrator rights. Run the command as
yourself first — it records the name in your settings — and it prints the
exact command to finish with when it cannot write the file itself.

.local is refused: macOS resolves it over multicast DNS before reading the
hosts file, so every page would wait. inboxql.localhost is reserved for this.`,
		Run: runHosts,
	})
}

func runHosts(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	name, _ := subcommand(rest)
	if sub == "" {
		sub = "show"
	}
	// Under sudo, HOME is root's (or the user's, depending on the platform's
	// sudoers), so the settings this would update may not be the person's.
	// The hosts file is all an elevated run is for; the name was recorded by
	// the unprivileged run that printed this command.
	elevated := os.Getenv("SUDO_USER") != ""

	switch sub {
	case "set":
		warning, err := hostsfile.Check(name)
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if !elevated {
			if err := rememberHostname(ctx, name); err != nil {
				return err
			}
		}
		if warning != "" {
			ctx.Printf("%s %s\n", ctx.Printer().Yellow("note:"), warning)
		}
		if err := hostsfile.Write(name); err != nil {
			return hostsWriteFailed(ctx, err, "set "+name)
		}
		return hostsReport(ctx, "Set.")

	case "remove":
		if !elevated {
			if err := rememberHostname(ctx, ""); err != nil {
				return err
			}
		}
		if err := hostsfile.Write(""); err != nil {
			return hostsWriteFailed(ctx, err, "remove")
		}
		return hostsReport(ctx, "Removed.")

	case "show":
		return hostsReport(ctx, "")

	default:
		return Fail(ExitUsage, "unknown subcommand %q (want set, remove or show)", sub)
	}
}

// rememberHostname records the name in the machine settings, so the server,
// `iql where` and the service all print the address somebody chose.
func rememberHostname(ctx *Context, name string) error {
	if ctx.Machine == nil {
		if name == "" {
			return nil
		}
		return Fail(ExitNotConfigured, "no machine settings at %s — run `iql setup` first, so the name has somewhere to be remembered",
			machine.Path())
	}
	ctx.Machine.Hostname = name
	if err := machine.Save(ctx.Machine); err != nil {
		return Fail(ExitError, "saving %s: %v", machine.Path(), err)
	}
	return nil
}

// hostsWriteFailed explains how to finish when the hosts file is not writable.
func hostsWriteFailed(ctx *Context, err error, args string) error {
	if !errors.Is(err, hostsfile.ErrPermission) {
		return Fail(ExitError, "changing %s: %v", hostsfile.Path(), err)
	}
	how := "sudo iql hosts " + args
	if runtime.GOOS == "windows" {
		how = "run `iql hosts " + args + "` from a PowerShell opened with \"Run as administrator\""
	} else {
		how = "run `" + how + "`"
	}
	return Fail(ExitError, "%s needs administrator rights to change. The name is saved in your settings; to finish, %s.",
		hostsfile.Path(), how)
}

func hostsReport(ctx *Context, message string) error {
	content, _ := hostsfile.Read()
	inFile := hostsfile.Current(content)
	setting := ""
	if ctx.Machine != nil {
		setting = ctx.Machine.Hostname
	}
	name := inFile
	if name == "" {
		name = setting
	}
	resolves := false
	if name != "" {
		if addrs, err := net.LookupHost(name); err == nil {
			for _, a := range addrs {
				if ip := net.ParseIP(a); ip != nil && ip.IsLoopback() {
					resolves = true
				}
			}
		}
	}
	url := ""
	if name != "" {
		port := machine.DefaultPort
		if a := ctx.defaultAddr(); strings.Contains(a, ":") {
			port = a[strings.LastIndex(a, ":")+1:]
		}
		url = "http://" + name + ":" + port
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"hostsFile": hostsfile.Path(), "inHostsFile": inFile,
			"inSettings": setting, "resolves": resolves, "url": url, "message": message})
	}
	p := ctx.Printer()
	if message != "" {
		ctx.Printf("%s\n", message)
	}
	if name == "" {
		ctx.Printf("  No name set. `iql hosts set inboxql.localhost` gives the site one.\n")
		return nil
	}
	ctx.Printf("  %-10s %s\n", p.Dim("name"), name)
	ctx.Printf("  %-10s %s\n", p.Dim("hosts"), map[bool]string{true: "set in " + hostsfile.Path(), false: p.Yellow("not in " + hostsfile.Path())}[inFile != ""])
	if resolves {
		ctx.Printf("  %-10s %s\n", p.Dim("open"), p.Bold(url))
	} else {
		ctx.Printf("  %-10s %s\n", p.Dim("resolves"), p.Yellow("not yet — on macOS, `sudo dscacheutil -flushcache` if the file is already set"))
	}
	return nil
}
