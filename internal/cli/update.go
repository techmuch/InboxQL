package cli

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/user/inboxql/internal/service"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/update"
)

func init() {
	register(&Command{
		Name:    "update",
		Summary: "install the newest release, safely",
		Usage: `iql update [--check] [--yes] [--json]

Finds the newest published release and, when it is newer, installs it:

  1. downloads the archive for this platform and checks it against the
     release's SHA256SUMS — a mismatch, or no checksum at all, stops here
  2. backs up the mailbox into its backups folder
  3. stops the service, if it is running
  4. replaces this binary
  5. starts the service again

The mailbox is upgraded when the new version first opens it, after the backup.

  --check   only say whether there is a newer release
  --yes     do not ask before installing

Installed with Homebrew? Use ` + "`brew upgrade inboxql`" + ` instead; this refuses, so the two
never disagree about which binary is installed.`,
		Run: runUpdate,
	})
}

func runUpdate(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	check := fs.Bool("check", false, "only report")
	yes := fs.Bool("yes", false, "do not ask")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	exe, err := os.Executable()
	if err != nil {
		return Fail(ExitError, "cannot find this binary: %v", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	// Two updaters for one file disagree about what is installed. Homebrew
	// keeps its binaries under a Cellar; leave those to it.
	if strings.Contains(exe, "/Cellar/") {
		return Fail(ExitUsage, "this copy was installed by Homebrew (%s). Run `brew upgrade inboxql`.", exe)
	}

	rel, err := update.Latest()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	newer := update.Newer(rel.Version(), Version)
	if *check || !newer {
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"current": Version, "latest": rel.Version(),
				"newer": newer, "url": rel.URL})
		}
		if newer {
			ctx.Printf("%s is available (this is %s). `iql update` installs it.\n%s\n",
				rel.Version(), Version, rel.URL)
		} else {
			ctx.Printf("Up to date: %s is the newest release.\n", Version)
		}
		return nil
	}

	assetName, err := update.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Fail(ExitNotConfigured, "%v", err)
	}
	asset, err := rel.Find(assetName)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	sumsAsset, err := rel.Find("SHA256SUMS")
	if err != nil {
		return Fail(ExitError, "%v — not installing an unverified release", err)
	}

	if !*yes {
		if !isTerminal(ctx.Stdin) {
			return Fail(ExitUsage, "updating replaces this binary and upgrades your mailbox; pass --yes to do it without asking")
		}
		ctx.Printf("Update InboxQL from %s to %s? Your mailbox is backed up first. [y/N] ", Version, rel.Version())
		line, _ := bufio.NewReader(ctx.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			ctx.Printf("Nothing changed.\n")
			return nil
		}
	}

	p := ctx.Printer()
	step := func(s string) { ctx.Printf("%s %s\n", p.Dim("·"), s) }

	// 1. Download and verify before anything is touched.
	step(fmt.Sprintf("downloading %s", assetName))
	archive, err := update.Download(asset)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	sums, err := update.Download(sumsAsset)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if err := update.Verify(sums, assetName, archive); err != nil {
		return Fail(ExitError, "%v", err)
	}
	binName := "iql"
	if runtime.GOOS == "windows" {
		binName = "iql.exe"
	}
	binary, err := update.Extract(archive, assetName, binName)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	step("checksum verified")

	// 2. Back up the mailbox, if there is one, while the old server still
	// runs: the online backup API copies a live database consistently.
	backup := ""
	if _, err := os.Stat(ctx.dbPath()); err == nil {
		if err := ctx.OpenStore(); err != nil {
			return err
		}
		backup = filepath.Join(ctx.DataDir, "backups",
			fmt.Sprintf("inboxql-pre-%s-%s.db", rel.Version(), time.Now().Format("20060102-150405")))
		err := store.BackupTo(backup)
		store.CloseDB()
		if err != nil {
			return Fail(ExitError, "backing up before the update: %v — nothing was changed", err)
		}
		step("mailbox backed up to " + backup)
	}

	// 3. Stop the service, so nothing runs the old binary against a database
	// the new one is about to upgrade.
	mgr, _ := service.ForThisMachine()
	restart := false
	if mgr != nil {
		// Only the service that runs this binary. A development build being
		// updated has no business stopping the installed service.
		if st, err := mgr.Status(); err == nil && st.Installed && (st.Running || st.Starting) && serviceRuns(mgr, exe) {
			if err := mgr.Stop(); err != nil {
				return Fail(ExitError, "stopping the service: %v — nothing was changed", err)
			}
			restart = true
			step("service stopped")
		}
	}

	// 4. Swap the binary.
	if err := update.Replace(exe, binary); err != nil {
		if restart {
			_ = mgr.Start()
		}
		return Fail(ExitError, "%v — the old version is still installed", err)
	}
	step("installed " + exe)

	// 5. Start it again.
	if restart {
		if err := mgr.Start(); err != nil {
			return Fail(ExitError, "updated, but the service did not start: %v\nRun `iql service start`.", err)
		}
		step("service started")
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"from": Version, "to": rel.Version(), "binary": exe,
			"backup": backup, "serviceRestarted": restart})
	}
	ctx.Printf("\nUpdated to %s.\n", p.Bold(rel.Version()))
	if runtime.GOOS == "darwin" {
		ctx.Printf("%s\n", p.Dim("If you import from Apple Mail, re-grant Full Disk Access to "+exe+
			": macOS ties the grant to the exact binary, and this is a new one."))
	}
	return nil
}

// isTerminal reports whether r is an interactive terminal.
func isTerminal(r any) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// serviceRuns reports whether the installed service definition launches exe.
// When the definition cannot be read, it is assumed to: stopping the service
// needlessly is cheaper than upgrading a database under a running server.
func serviceRuns(mgr service.Manager, exe string) bool {
	b, err := os.ReadFile(mgr.File())
	if err != nil {
		return true
	}
	return strings.Contains(string(b), exe)
}
